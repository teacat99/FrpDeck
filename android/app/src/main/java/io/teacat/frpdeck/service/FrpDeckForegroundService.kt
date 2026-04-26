package io.teacat.frpdeck.service

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.IBinder
import androidx.core.app.NotificationCompat
import io.teacat.frpdeck.FrpDeckApp
import io.teacat.frpdeck.MainActivity
import io.teacat.frpdeck.R

/**
 * Foreground Service that owns the lifecycle of the gomobile-backed
 * FrpDeck server.
 *
 * Lifecycle contract:
 *   - ACTION_START → Engine.start() + post the persistent notification
 *     + startForeground(...)
 *   - ACTION_STOP  → Engine.stop()  + stopForeground + stopSelf
 *
 * START_STICKY ensures Android restarts the Service when memory is
 * reclaimed; combined with REQUEST_IGNORE_BATTERY_OPTIMIZATIONS it gives
 * the embedded frpc the best chance to stay up across Doze + per-OEM
 * tightening.
 */
class FrpDeckForegroundService : Service() {

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
            stopSelf()
        }
    }

    private fun stopFrpDeck() {
        val app = applicationContext as FrpDeckApp
        app.engine.stop()
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.N) {
            stopForeground(STOP_FOREGROUND_REMOVE)
        } else {
            @Suppress("DEPRECATION")
            stopForeground(true)
        }
        stopSelf()
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

    companion object {
        const val ACTION_START = "io.teacat.frpdeck.ACTION_START"
        const val ACTION_STOP  = "io.teacat.frpdeck.ACTION_STOP"

        const val CHANNEL_ID = "frpdeck_running"
        const val NOTIFICATION_ID = 1001

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
