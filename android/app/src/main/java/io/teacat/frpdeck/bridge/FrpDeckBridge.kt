package io.teacat.frpdeck.bridge

import android.net.Uri
import android.util.Log
import android.webkit.JavascriptInterface
import android.webkit.WebView
import io.teacat.frpdeck.FrpDeckApp
import io.teacat.frpdeck.MainActivity
import org.json.JSONObject

/**
 * `window.frpdeck` — the JavaScript bridge the Vue SPA uses to call the
 * native shell.
 *
 * Surface (P6′/P7′):
 *
 *   getPlatform(): String                        → "android"
 *   getVersion(): String                         → "FrpDeck-Android <ver> · frp <ver>"
 *   getAdminToken(): String                      → 24h JWT for the seed admin (so the SPA skips /login)
 *   isVpnPermissionGranted(): Boolean            → true iff VpnService.prepare() == null
 *   requestVpnPermission(reqId: String)          → resolves with { ok, message }
 *   exportBackup(reqId, suggestedName: String)   → resolves with { ok, message, bytes }
 *   importBackup(reqId: String)                  → resolves with { ok, message, files }
 *   openExternal(url: String)                    → fire-and-forget Intent.ACTION_VIEW
 *
 * Result delivery: any method that ends in `(reqId)` is async. The
 * native side eventually calls `window.frpdeck.__resolve(reqId, payload)`
 * from the WebView's JS context. The Vue side wraps these into Promises
 * via `composables/useNativeBridge.ts`.
 *
 * Threading: every `@JavascriptInterface` method runs on a binder
 * thread, NOT the main thread. Methods delegate to MainActivity helpers
 * which post to the activity's coroutine scope or Activity launchers as
 * appropriate.
 *
 * Security: `addJavascriptInterface` is only safe because the WebView
 * loads `http://127.0.0.1:18080/` — the embedded gomobile gin server
 * pinned to loopback. The application MUST NOT navigate the WebView to
 * any external origin — doing so would expose this bridge to arbitrary
 * remote JavaScript. MainActivity enforces this with a
 * [WebViewClient.shouldOverrideUrlLoading] gate that punts external
 * URLs to the system browser via `openExternal` → `Intent.ACTION_VIEW`.
 */
class FrpDeckBridge(
    private val activity: MainActivity,
    private val webView: WebView,
) {

    private val tag = "FrpDeck/Bridge"

    @JavascriptInterface
    fun getPlatform(): String = "android"

    @JavascriptInterface
    fun getVersion(): String {
        val app = activity.applicationContext as FrpDeckApp
        return try {
            "FrpDeck-Android · ${app.engine.version()}"
        } catch (t: Throwable) {
            "FrpDeck-Android"
        }
    }

    @JavascriptInterface
    fun getAdminToken(): String {
        val app = activity.applicationContext as FrpDeckApp
        return try {
            app.engine.adminToken()
        } catch (t: Throwable) {
            Log.w(tag, "getAdminToken failed: ${t.message}")
            ""
        }
    }

    @JavascriptInterface
    fun getServerBase(): String {
        val app = activity.applicationContext as FrpDeckApp
        val addr = app.engine.listenAddr.value
        return if (addr.isNotEmpty()) "http://$addr" else ""
    }

    @JavascriptInterface
    fun isVpnPermissionGranted(): Boolean {
        return android.net.VpnService.prepare(activity) == null
    }

    @JavascriptInterface
    fun requestVpnPermission(reqId: String) {
        Log.i(tag, "requestVpnPermission $reqId")
        activity.runOnUiThread { activity.launchVpnPermissionRequest(reqId) }
    }

    @JavascriptInterface
    fun exportBackup(reqId: String, suggestedName: String) {
        Log.i(tag, "exportBackup $reqId name=$suggestedName")
        activity.runOnUiThread { activity.launchExportBackup(reqId, suggestedName) }
    }

    @JavascriptInterface
    fun importBackup(reqId: String) {
        Log.i(tag, "importBackup $reqId")
        activity.runOnUiThread { activity.launchImportBackup(reqId) }
    }

    @JavascriptInterface
    fun openExternal(url: String) {
        try {
            val intent = android.content.Intent(android.content.Intent.ACTION_VIEW, Uri.parse(url))
            intent.addFlags(android.content.Intent.FLAG_ACTIVITY_NEW_TASK)
            activity.startActivity(intent)
        } catch (t: Throwable) {
            Log.w(tag, "openExternal failed: ${t.message}")
        }
    }

    /**
     * Resolve an async call from the native side back to the JS Promise.
     * `payload` is JSON-encoded; the JS layer destructures.
     */
    fun resolve(reqId: String, ok: Boolean, message: String, extra: JSONObject? = null) {
        val payload = JSONObject().apply {
            put("ok", ok)
            put("message", message)
            if (extra != null) {
                val it = extra.keys()
                while (it.hasNext()) {
                    val key = it.next()
                    put(key, extra.get(key))
                }
            }
        }
        val js = "window.frpdeck && window.frpdeck.__resolve && window.frpdeck.__resolve(${quote(reqId)}, ${payload})"
        webView.post { webView.evaluateJavascript(js, null) }
    }

    private fun quote(s: String): String {
        val sb = StringBuilder("\"")
        s.forEach { c ->
            when (c) {
                '\\' -> sb.append("\\\\")
                '"' -> sb.append("\\\"")
                '\n' -> sb.append("\\n")
                '\r' -> sb.append("\\r")
                else -> sb.append(c)
            }
        }
        sb.append('"')
        return sb.toString()
    }
}
