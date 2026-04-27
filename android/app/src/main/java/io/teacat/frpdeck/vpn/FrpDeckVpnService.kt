package io.teacat.frpdeck.vpn

import android.app.PendingIntent
import android.content.Intent
import android.net.VpnService
import android.os.Build
import android.os.ParcelFileDescriptor
import android.util.Log
import frpdeckmobile.Frpdeckmobile
import io.teacat.frpdeck.MainActivity
import io.teacat.frpdeck.R
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/**
 * VpnService backing the P6′/P7′ "VPN by configuration" model.
 *
 * Lifecycle is now driven by frpc rather than by the user:
 *
 *   - frpc transitions a `role=visitor + plugin=socks5` tunnel to active
 *   - the gomobile bridge fires VpnRequestHandler.OnVpnRequest with the
 *     loopback `socks5://host:port` URL the visitor is exposing
 *   - FrpDeckForegroundService receives the callback, calls
 *     `VpnService.prepare()` to confirm the user has granted the
 *     consent (it should — the user presses "Request VPN permission"
 *     once via the Vue WebView's AndroidSettingsView, or the engine
 *     bounces them through PrepareActivity if they have not), then
 *     starts THIS service with EXTRA_SOCKS5_URL set
 *
 * Routing strategy is fixed catch-all: 0.0.0.0/0 + ::/0. The original
 * P7 v1 had per-CIDR / per-app rules; those were removed in P6′/P7′
 * because:
 *   1. allowedPackages cannot be edited remotely (the desktop browser
 *      cannot enumerate the device's PackageManager).
 *   2. cidr/domain rules add no practical value when the only output
 *      is a single SOCKS5 visitor — multiple visitors cannot be
 *      per-route split anyway because the VpnService Builder owns one
 *      tun fd and tun2socks one outbound URL.
 *   3. Catch-all gives the most predictable user experience and
 *      removes a whole class of "I configured it wrong and traffic
 *      silently goes around the tunnel" support questions.
 *
 * Users who want partial routing should configure their device-level
 * "Always-on VPN" exclusion list at the system level instead.
 */
class FrpDeckVpnService : VpnService() {

    private val tag = "FrpDeck/VPN"
    private var tun: ParcelFileDescriptor? = null

    override fun onCreate() {
        super.onCreate()
        Log.i(tag, "vpn service created")
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        when (intent?.action) {
            ACTION_STOP -> {
                stopVpn()
                return START_NOT_STICKY
            }
            else -> {
                val socks = intent?.getStringExtra(EXTRA_SOCKS5_URL).orEmpty()
                startVpn(socks)
            }
        }
        return START_STICKY
    }

    override fun onRevoke() {
        Log.w(tag, "vpn revoked by user/system")
        stopVpn()
        super.onRevoke()
    }

    private fun startVpn(socks5URL: String) {
        if (tun != null) {
            Log.w(tag, "startVpn called but tun already up — ignoring")
            return
        }
        if (socks5URL.isBlank()) {
            Log.w(tag, "startVpn called without socks5 url — refusing to attach")
            stopSelf()
            return
        }

        val builder = Builder()
            .setSession(getString(R.string.vpn_session_name))
            .setMtu(1500)
            .addAddress("10.111.222.1", 32)
            .addAddress("fd00:ffff::1", 128)
            .addDnsServer("1.1.1.1")
            .addDnsServer("9.9.9.9")
            .addRoute("0.0.0.0", 0)
            .addRoute("::", 0)

        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            builder.setMetered(false)
        }

        // Always exclude FrpDeck itself so the embedded server traffic
        // (loopback + the upstream frps connection) doesn't loop back
        // into the tun. Without this every packet the SOCKS5 visitor
        // emits would re-enter the tun and recurse.
        try {
            builder.addDisallowedApplication(packageName)
        } catch (e: Throwable) {
            Log.w(tag, "addDisallowedApplication: ${e.message}")
        }

        val configIntent = PendingIntent.getActivity(
            this,
            0,
            Intent(this, MainActivity::class.java),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )
        builder.setConfigureIntent(configIntent)

        tun = try {
            builder.establish()
        } catch (e: Throwable) {
            Log.e(tag, "establish failed: ${e.message}")
            null
        }

        if (tun == null) {
            Log.w(tag, "tun fd was not established; service idle")
            stopSelf()
            return
        }

        val fdInt = tun?.fd ?: -1
        try {
            Frpdeckmobile.vpnAttach(fdInt.toLong(), 1500L, socks5URL)
            _running.value = true
            _activeProxy.value = socks5URL
            Log.i(tag, "tun fd $fdInt up; proxy=$socks5URL")
        } catch (e: Throwable) {
            Log.e(tag, "tun2socks attach failed: ${e.message}")
            try { tun?.close() } catch (_: Throwable) {}
            tun = null
            _running.value = false
            stopSelf()
        }
    }

    private fun stopVpn() {
        try { Frpdeckmobile.vpnDetach() } catch (e: Throwable) {
            Log.w(tag, "tun2socks detach: ${e.message}")
        }
        try {
            tun?.close()
        } catch (e: Throwable) {
            Log.w(tag, "close tun: ${e.message}")
        }
        tun = null
        _running.value = false
        _activeProxy.value = ""
        stopSelf()
    }

    override fun onDestroy() {
        stopVpn()
        super.onDestroy()
    }

    companion object {
        const val ACTION_START = "io.teacat.frpdeck.vpn.START"
        const val ACTION_STOP  = "io.teacat.frpdeck.vpn.STOP"
        const val EXTRA_SOCKS5_URL = "io.teacat.frpdeck.vpn.SOCKS5_URL"

        private val _running = MutableStateFlow(false)
        val running: StateFlow<Boolean> = _running.asStateFlow()

        private val _activeProxy = MutableStateFlow("")
        val activeProxy: StateFlow<String> = _activeProxy.asStateFlow()
    }
}
