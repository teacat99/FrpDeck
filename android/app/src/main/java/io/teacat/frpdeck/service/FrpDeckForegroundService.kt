package io.teacat.frpdeck.service

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.net.VpnService
import android.os.Build
import android.os.IBinder
import android.util.Log
import androidx.core.app.NotificationCompat
import frpdeckmobile.Frpdeckmobile
import frpdeckmobile.VpnRequestHandler
import io.teacat.frpdeck.FrpDeckApp
import io.teacat.frpdeck.MainActivity
import io.teacat.frpdeck.R
import io.teacat.frpdeck.vpn.FrpDeckVpnService

/**
 * Foreground Service that owns the lifecycle of the gomobile-backed
 * FrpDeck server.
 *
 * Lifecycle contract:
 *   - ACTION_START → Engine.start() + register VpnRequestHandler +
 *     post the persistent notification + startForeground(...)
 *   - ACTION_STOP  → ClearVpnRequestHandler + Engine.stop() +
 *     stopForeground + stopSelf
 *
 * VPN integration (P6′/P7′):
 *   - The foreground Service is the natural owner of the
 *     [VpnRequestHandler] because it shares the gomobile process with
 *     the engine and outlives any Activity. When the engine reports
 *     that a tunnel matching `frpcd.TunnelRequiresSystemRoute` just
 *     went active, this Service:
 *       1. checks `VpnService.prepare(this) == null` (already granted),
 *       2. if granted → startService(FrpDeckVpnService) with the
 *          dispatched socks5 URL,
 *       3. if not granted → posts a high-priority notification asking
 *          the user to tap to authorise (deep-links into the WebView's
 *          AndroidSettingsView so the user lands on the bridge button
 *          they need).
 *   - We never auto-launch PrepareActivity from a background service —
 *     that requires an Activity context the user is currently
 *     interacting with, otherwise the system VPN consent dialog gets
 *     buried behind the launcher.
 *
 * START_STICKY ensures Android restarts the Service when memory is
 * reclaimed; combined with REQUEST_IGNORE_BATTERY_OPTIMIZATIONS it gives
 * the embedded frpc the best chance to stay up across Doze + per-OEM
 * tightening.
 */
class FrpDeckForegroundService : Service() {

    private val tag = "FrpDeck/FgSvc"

    private var vpnHandlerRegistered = false

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onCreate() {
        super.onCreate()
        ensureChannel(this)
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        when (intent?.action) {
            ACTION_STOP -> {
                stopFrpDeck()
                return START_NOT_STICKY
            }
            else -> startFrpDeck()
        }
        return START_STICKY
    }

    private fun startFrpDeck() {
        val app = applicationContext as FrpDeckApp
        val result = app.engine.start()
        result.onSuccess {
            registerVpnHandler()
            val notification = buildNotification()
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
                startForeground(
                    NOTIFICATION_ID,
                    notification,
                    ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC,
                )
            } else {
                startForeground(NOTIFICATION_ID, notification)
            }
        }
        result.onFailure {
            Log.e(tag, "engine start failed", it)
            stopSelf()
        }
    }

    private fun stopFrpDeck() {
        unregisterVpnHandler()
        val app = applicationContext as FrpDeckApp
        app.engine.stop()
        // Tear down the VpnService too — it has no point without the
        // engine. Best-effort: if the user already pressed stop on
        // VpnService directly the second stop is a no-op.
        try {
            stopService(Intent(this, FrpDeckVpnService::class.java))
        } catch (t: Throwable) {
            Log.w(tag, "stop vpn service: ${t.message}")
        }
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.N) {
            stopForeground(STOP_FOREGROUND_REMOVE)
        } else {
            @Suppress("DEPRECATION")
            stopForeground(true)
        }
        stopSelf()
    }

    private fun registerVpnHandler() {
        if (vpnHandlerRegistered) return
        Frpdeckmobile.setVpnRequestHandler(object : VpnRequestHandler {
            override fun onVpnRequest(tunnelID: Long, tunnelName: String?, socks5URL: String?) {
                handleVpnRequest(socks5URL.orEmpty(), tunnelName.orEmpty())
            }
        })
        vpnHandlerRegistered = true
    }

    private fun unregisterVpnHandler() {
        if (!vpnHandlerRegistered) return
        try {
            Frpdeckmobile.clearVpnRequestHandler()
        } catch (t: Throwable) {
            Log.w(tag, "clear vpn handler: ${t.message}")
        }
        vpnHandlerRegistered = false
    }

    private fun handleVpnRequest(socks5URL: String, tunnelName: String) {
        if (socks5URL.isBlank()) return
        if (FrpDeckVpnService.running.value && FrpDeckVpnService.activeProxy.value == socks5URL) {
            return
        }
        val granted = VpnService.prepare(this) == null
        if (granted) {
            val start = Intent(this, FrpDeckVpnService::class.java)
                .setAction(FrpDeckVpnService.ACTION_START)
                .putExtra(FrpDeckVpnService.EXTRA_SOCKS5_URL, socks5URL)
            try {
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                    startForegroundService(start)
                } else {
                    startService(start)
                }
            } catch (t: Throwable) {
                Log.e(tag, "start VpnService failed: ${t.message}")
            }
        } else {
            postVpnAuthorisationNotification(tunnelName)
        }
    }

    private fun postVpnAuthorisationNotification(tunnelName: String) {
        val nm = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        val openIntent = Intent(this, MainActivity::class.java)
            .addFlags(Intent.FLAG_ACTIVITY_CLEAR_TOP or Intent.FLAG_ACTIVITY_SINGLE_TOP)
        val pi = PendingIntent.getActivity(
            this,
            10,
            openIntent,
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )
        val text = if (tunnelName.isNotBlank())
            getString(R.string.vpn_request_text) + " ($tunnelName)"
        else
            getString(R.string.vpn_request_text)
        val notif = NotificationCompat.Builder(this, CHANNEL_ID)
            .setSmallIcon(R.drawable.ic_notification)
            .setContentTitle(getString(R.string.vpn_request_title))
            .setContentText(text)
            .setContentIntent(pi)
            .setAutoCancel(true)
            .setPriority(NotificationCompat.PRIORITY_HIGH)
            .build()
        nm.notify(VPN_REQUEST_NOTIFICATION_ID, notif)
    }

    private fun buildNotification(): Notification {
        val openIntent = Intent(this, MainActivity::class.java)
        val openPi = PendingIntent.getActivity(
            this,
            0,
            openIntent,
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )
        val stopIntent = Intent(this, FrpDeckForegroundService::class.java)
            .setAction(ACTION_STOP)
        val stopPi = PendingIntent.getService(
            this,
            1,
            stopIntent,
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )
        return NotificationCompat.Builder(this, CHANNEL_ID)
            .setSmallIcon(R.drawable.ic_notification)
            .setContentTitle(getString(R.string.notification_running_title))
            .setContentText(getString(R.string.notification_running_text))
            .setOngoing(true)
            .setContentIntent(openPi)
            .addAction(
                NotificationCompat.Action.Builder(
                    R.drawable.ic_notification,
                    getString(R.string.notification_action_stop),
                    stopPi,
                ).build()
            )
            .build()
    }

    override fun onDestroy() {
        unregisterVpnHandler()
        super.onDestroy()
    }

    companion object {
        const val ACTION_START = "io.teacat.frpdeck.ACTION_START"
        const val ACTION_STOP  = "io.teacat.frpdeck.ACTION_STOP"

        const val CHANNEL_ID = "frpdeck_running"
        const val NOTIFICATION_ID = 1001
        private const val VPN_REQUEST_NOTIFICATION_ID = 1002

        fun ensureChannel(ctx: Context) {
            if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return
            val nm = ctx.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
            if (nm.getNotificationChannel(CHANNEL_ID) != null) return
            val channel = NotificationChannel(
                CHANNEL_ID,
                ctx.getString(R.string.notification_channel_running),
                NotificationManager.IMPORTANCE_LOW,
            ).apply {
                description = ctx.getString(R.string.notification_channel_running_desc)
                setShowBadge(false)
            }
            nm.createNotificationChannel(channel)
        }
    }
}
