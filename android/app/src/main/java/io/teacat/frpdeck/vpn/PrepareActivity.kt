package io.teacat.frpdeck.vpn

import android.app.Activity
import android.content.Intent
import android.net.VpnService
import android.os.Bundle
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AppCompatActivity

/**
 * Transparent trampoline that drives `VpnService.prepare(...)` to
 * completion and reports the result back to MainActivity via a result
 * intent.
 *
 * Why a separate Activity instead of running prepare() inline in
 * MainActivity:
 *   - The system VPN consent dialog must be launched via
 *     `startActivityForResult` from an Activity context. MainActivity
 *     hosts the WebView and is heavy; using a dedicated Activity keeps
 *     the result plumbing isolated and avoids re-creating the WebView
 *     when the system dialog destroys/re-creates the host.
 *   - It is invoked both from the WebView's JS bridge (user-driven
 *     "Request VPN permission" button on AndroidSettingsView.vue) and
 *     from FrpDeckForegroundService when the engine notifies it that
 *     a tunnel just transitioned to a state that requires the system
 *     route.
 *
 * Result contract: caller starts this Activity with `startActivityForResult`
 * (or its modern `registerForActivityResult` equivalent). The result
 * Intent carries `RESULT_OK` when the user granted permission (or it
 * was already granted) and `RESULT_CANCELED` otherwise. The Intent
 * extras include `EXTRA_REQUEST_ID` so the caller can correlate
 * concurrent requests.
 */
class PrepareActivity : AppCompatActivity() {

    private var requestId: String = ""

    private val launcher = registerForActivityResult(
        ActivityResultContracts.StartActivityForResult()
    ) { res ->
        finishWithResult(res.resultCode == Activity.RESULT_OK)
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        requestId = intent?.getStringExtra(EXTRA_REQUEST_ID).orEmpty()

        val prepareIntent = VpnService.prepare(this)
        if (prepareIntent == null) {
            finishWithResult(true)
            return
        }
        try {
            launcher.launch(prepareIntent)
        } catch (t: Throwable) {
            finishWithResult(false)
        }
    }

    private fun finishWithResult(granted: Boolean) {
        val data = Intent().apply {
            putExtra(EXTRA_REQUEST_ID, requestId)
            putExtra(EXTRA_GRANTED, granted)
        }
        setResult(if (granted) Activity.RESULT_OK else Activity.RESULT_CANCELED, data)
        finish()
        // Skip the default activity-close animation so the launching
        // surface (the WebView) doesn't briefly slide. The post-Android
        // 14 replacement (overrideActivityTransition) is API 34 only —
        // we still target API 29 minSdk so the deprecated call is the
        // right one for our compatibility window.
        @Suppress("DEPRECATION")
        overridePendingTransition(0, 0)
    }

    companion object {
        const val EXTRA_REQUEST_ID = "io.teacat.frpdeck.vpn.REQUEST_ID"
        const val EXTRA_GRANTED = "io.teacat.frpdeck.vpn.GRANTED"
    }
}
