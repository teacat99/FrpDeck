package io.teacat.frpdeck

import android.Manifest
import android.app.Activity
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.util.Log
import android.view.View
import android.webkit.WebChromeClient
import android.webkit.WebResourceRequest
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.activity.OnBackPressedCallback
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AppCompatActivity
import androidx.core.content.ContextCompat
import io.teacat.frpdeck.backup.BackupBundle
import io.teacat.frpdeck.bridge.FrpDeckBridge
import io.teacat.frpdeck.service.FrpDeckForegroundService
import io.teacat.frpdeck.vpn.PrepareActivity
import org.json.JSONObject

/**
 * Single-activity WebView host (P6′/P7′ rewrite).
 *
 * The Activity does three things and three things only:
 *
 *   1. Starts the gomobile-backed FrpDeck engine (synchronously) and
 *      the foreground Service that owns its lifecycle when the user
 *      navigates away.
 *   2. Hosts a single full-screen WebView pointed at the engine's
 *      loopback origin, exposing `window.frpdeck` (see [FrpDeckBridge])
 *      so the Vue SPA can request native features that no browser-side
 *      API can satisfy (VPN consent, SAF I/O, external links).
 *   3. Owns the modern Activity-result launchers for those native
 *      features and feeds the outcomes back to the JS Promise the
 *      bridge handed out via `bridge.resolve(reqId, ...)`.
 *
 * Why no Compose, no Retrofit, no Tabs? See plan.md §15 "P6′/P7′
 * 重写". The decision is to reuse the desktop Vue SPA verbatim so the
 * Android shell stays tiny and never falls behind the desktop feature
 * set.
 */
class MainActivity : AppCompatActivity() {

    private val tag = "FrpDeck/Main"

    private lateinit var webView: WebView
    private lateinit var bridge: FrpDeckBridge

    /** Maps an outstanding `(reqId)` from the JS bridge to the kind of
     *  native operation we launched, so the result handler can format
     *  the resolved payload correctly. Pending entries are dropped on
     *  Activity finish — the JS Promise will hang, which is fine
     *  because the Activity is the only thing keeping the WebView
     *  alive. */
    private val pendingExportName = HashMap<String, String>()
    private val pendingVpnReq = HashSet<String>()
    private val pendingExportReq = HashSet<String>()
    private val pendingImportReq = HashSet<String>()

    private val notificationPermissionLauncher = registerForActivityResult(
        ActivityResultContracts.RequestPermission()
    ) { /* result ignored — the foreground notification is decorative. */ }

    private val vpnPrepareLauncher = registerForActivityResult(
        ActivityResultContracts.StartActivityForResult()
    ) { res ->
        val data = res.data
        val reqId = data?.getStringExtra(PrepareActivity.EXTRA_REQUEST_ID).orEmpty()
        val granted = res.resultCode == Activity.RESULT_OK
        if (reqId.isNotEmpty() && pendingVpnReq.remove(reqId)) {
            bridge.resolve(
                reqId,
                ok = granted,
                message = if (granted) "vpn permission granted" else "user denied vpn permission",
            )
        }
    }

    private val exportLauncher = registerForActivityResult(
        ActivityResultContracts.CreateDocument("application/zip")
    ) { uri: Uri? ->
        val reqId = pendingExportReq.firstOrNull().orEmpty()
        if (reqId.isNotEmpty()) pendingExportReq.remove(reqId)
        pendingExportName.remove(reqId)
        if (uri == null) {
            if (reqId.isNotEmpty()) {
                bridge.resolve(reqId, ok = false, message = "export cancelled")
            }
            return@registerForActivityResult
        }
        try {
            val bytes = BackupBundle.export(this, uri)
            bridge.resolve(
                reqId,
                ok = true,
                message = "exported $bytes bytes",
                extra = JSONObject().put("bytes", bytes).put("uri", uri.toString()),
            )
        } catch (t: Throwable) {
            Log.e(tag, "export failed", t)
            if (reqId.isNotEmpty()) {
                bridge.resolve(reqId, ok = false, message = t.message ?: "export failed")
            }
        }
    }

    private val importLauncher = registerForActivityResult(
        ActivityResultContracts.OpenDocument()
    ) { uri: Uri? ->
        val reqId = pendingImportReq.firstOrNull().orEmpty()
        if (reqId.isNotEmpty()) pendingImportReq.remove(reqId)
        if (uri == null) {
            if (reqId.isNotEmpty()) {
                bridge.resolve(reqId, ok = false, message = "import cancelled")
            }
            return@registerForActivityResult
        }
        try {
            // Restore is destructive and the engine writes to frpdeck.db
            // while running; stop it before importing.
            val app = applicationContext as FrpDeckApp
            app.engine.stop()
            val entries = BackupBundle.import(this, uri)
            // Re-start the engine so the WebView page can reload onto
            // the restored state without the user having to relaunch.
            app.engine.start()
            bridge.resolve(
                reqId,
                ok = true,
                message = "imported $entries entries",
                extra = JSONObject().put("entries", entries).put("uri", uri.toString()),
            )
            // The cheapest way to pick up a fresh DB is a full reload.
            webView.post { webView.reload() }
        } catch (t: Throwable) {
            Log.e(tag, "import failed", t)
            if (reqId.isNotEmpty()) {
                bridge.resolve(reqId, ok = false, message = t.message ?: "import failed")
            }
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        requestNotificationPermissionIfNeeded()

        // Boot the engine before we ever loadUrl so the loopback origin
        // exists. start() is synchronous (FrpDeckEngine guarantees the
        // listener is bound by the time it returns), so by the time we
        // call webView.loadUrl below the gin server is already serving.
        val app = applicationContext as FrpDeckApp
        val startResult = app.engine.start()
        startResult.onFailure {
            Log.e(tag, "engine start failed", it)
        }
        // Foreground service supplies the persistent notification + the
        // FOREGROUND_SERVICE_TYPE_DATA_SYNC declaration the OS requires
        // for long-running network workloads. It re-uses the same
        // engine singleton via FrpDeckApp so calling Engine.start() is
        // idempotent.
        ContextCompat.startForegroundService(
            this,
            Intent(this, FrpDeckForegroundService::class.java)
                .setAction(FrpDeckForegroundService.ACTION_START),
        )

        webView = WebView(this).apply {
            visibility = View.VISIBLE
            settings.apply {
                javaScriptEnabled = true
                domStorageEnabled = true
                databaseEnabled = true
                useWideViewPort = true
                loadWithOverviewMode = true
                cacheMode = android.webkit.WebSettings.LOAD_DEFAULT
                allowFileAccess = false
                allowContentAccess = false
                mediaPlaybackRequiresUserGesture = false
            }
            // Enable WebView contents-debugging unconditionally. The
            // WebView only ever loads `http://127.0.0.1:18080/` and the
            // `webview_devtools_remote_<pid>` socket is a unix-domain
            // socket reachable only via `adb`, so opening this is no
            // worse than installing the APK in the first place. Real
            // users will appreciate being able to `chrome://inspect`
            // their device when something goes wrong.
            WebView.setWebContentsDebuggingEnabled(true)

            webChromeClient = object : WebChromeClient() {
                override fun onConsoleMessage(
                    consoleMessage: android.webkit.ConsoleMessage,
                ): Boolean {
                    Log.i(
                        "FrpDeck/Web",
                        "${consoleMessage.messageLevel()}: ${consoleMessage.message()} (${consoleMessage.sourceId()}:${consoleMessage.lineNumber()})",
                    )
                    return true
                }
            }
            webViewClient = LoopbackOnlyWebViewClient()
        }

        bridge = FrpDeckBridge(this, webView)
        webView.addJavascriptInterface(bridge, "frpdeck")

        setContentView(webView)

        // Custom back-press: if the WebView has history, navigate back
        // inside it; otherwise the platform default (finish) wins.
        onBackPressedDispatcher.addCallback(
            this,
            object : OnBackPressedCallback(true) {
                override fun handleOnBackPressed() {
                    if (webView.canGoBack()) {
                        webView.goBack()
                    } else {
                        isEnabled = false
                        onBackPressedDispatcher.onBackPressed()
                    }
                }
            },
        )

        val origin = startResult.fold(
            onSuccess = { "http://${app.engine.listenAddr.value}/" },
            onFailure = { "about:blank" },
        )
        webView.loadUrl(origin)
    }

    override fun onDestroy() {
        // Detach the JS bridge so a leaked WebView reference can't fire
        // native code after the Activity is gone.
        try {
            webView.removeJavascriptInterface("frpdeck")
        } catch (_: Throwable) {}
        super.onDestroy()
    }

    private fun requestNotificationPermissionIfNeeded() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            val granted = ContextCompat.checkSelfPermission(
                this,
                Manifest.permission.POST_NOTIFICATIONS,
            ) == PackageManager.PERMISSION_GRANTED
            if (!granted) {
                notificationPermissionLauncher.launch(Manifest.permission.POST_NOTIFICATIONS)
            }
        }
    }

    /** Called by [FrpDeckBridge] on the UI thread. */
    fun launchVpnPermissionRequest(reqId: String) {
        pendingVpnReq.add(reqId)
        val intent = Intent(this, PrepareActivity::class.java).apply {
            putExtra(PrepareActivity.EXTRA_REQUEST_ID, reqId)
        }
        vpnPrepareLauncher.launch(intent)
    }

    /** Called by [FrpDeckBridge] on the UI thread. */
    fun launchExportBackup(reqId: String, suggestedName: String) {
        // CreateDocument can only have one outstanding launch at a
        // time per Activity-result registration. We serialise by
        // dropping a previous request — the JS side gets a stuck
        // promise but the user is the bottleneck so this is OK.
        pendingExportReq.clear()
        pendingExportName.clear()
        pendingExportReq.add(reqId)
        pendingExportName[reqId] = suggestedName
        val name = if (suggestedName.isNotBlank()) suggestedName else "frpdeck-backup.zip"
        exportLauncher.launch(name)
    }

    /** Called by [FrpDeckBridge] on the UI thread. */
    fun launchImportBackup(reqId: String) {
        pendingImportReq.clear()
        pendingImportReq.add(reqId)
        importLauncher.launch(arrayOf("application/zip", "application/octet-stream"))
    }

    /**
     * Pin the WebView to the loopback origin. Anything else is shunted
     * into the system browser via Intent.ACTION_VIEW. This is the
     * counterpart to the [FrpDeckBridge] security note: as long as the
     * bridge is only exposed to the engine origin we control, it's safe.
     */
    private inner class LoopbackOnlyWebViewClient : WebViewClient() {
        override fun shouldOverrideUrlLoading(view: WebView?, request: WebResourceRequest?): Boolean {
            val url = request?.url ?: return false
            val host = url.host.orEmpty()
            if (host == "127.0.0.1" || host == "localhost") return false
            try {
                val intent = Intent(Intent.ACTION_VIEW, url)
                intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
                startActivity(intent)
            } catch (t: Throwable) {
                Log.w(tag, "external open failed: ${t.message}")
            }
            return true
        }
    }
}
