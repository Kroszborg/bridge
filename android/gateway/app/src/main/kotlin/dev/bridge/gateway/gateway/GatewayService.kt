package dev.bridge.gateway.gateway

import android.app.Notification
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Build
import android.util.Log
import androidx.core.app.NotificationCompat
import androidx.core.content.ContextCompat
import androidx.lifecycle.LifecycleService
import androidx.lifecycle.lifecycleScope
import dev.bridge.gateway.AppContainer
import dev.bridge.gateway.BridgeApplication
import dev.bridge.gateway.R
import dev.bridge.gateway.container
import dev.bridge.gateway.ui.MainActivity
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.launch

/**
 * Keeps the gateway connection alive while the phone is idle. Android requires
 * a visible notification for this; it doubles as an at-a-glance status.
 */
class GatewayService : LifecycleService() {
    override fun onCreate() {
        super.onCreate()
        promote(buildNotification(ConnectionState.Connecting(0), null))
        val c = container
        lifecycleScope.launch {
            combine(c.connection.state, c.store.pairing) { state, pairing -> state to pairing?.projectName }
                .distinctUntilChanged()
                .collect { (state, project) ->
                    getSystemService(NotificationManager::class.java).notify(NOTIFICATION_ID, buildNotification(state, project))
                }
        }
        c.connection.start()
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        super.onStartCommand(intent, flags, startId)
        if (intent?.action == ACTION_NUDGE) container.connection.nudge()
        container.connection.start()
        return START_STICKY
    }

    override fun onDestroy() {
        container.connection.stop()
        super.onDestroy()
    }

    private fun promote(notification: Notification) {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
            startForeground(NOTIFICATION_ID, notification, ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE)
        } else {
            startForeground(NOTIFICATION_ID, notification)
        }
    }

    private fun buildNotification(state: ConnectionState, project: String?): Notification {
        val text = when (state) {
            is ConnectionState.Connected -> "Connected${project?.let { " · $it" } ?: ""}"
            is ConnectionState.Connecting -> "Connecting…"
            is ConnectionState.Retrying -> "Reconnecting · ${state.reason}"
            ConnectionState.WaitingForNetwork -> "Waiting for a network connection"
            is ConnectionState.Revoked -> "Removed from the project"
            ConnectionState.Stopped -> "Stopped"
        }
        val open = PendingIntent.getActivity(
            this, 0, Intent(this, MainActivity::class.java), PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )
        return NotificationCompat.Builder(this, BridgeApplication.CHANNEL_GATEWAY)
            .setSmallIcon(R.drawable.ic_notification)
            .setContentTitle("Bridge gateway")
            .setContentText(text)
            .setContentIntent(open)
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .setSilent(true)
            .setCategory(NotificationCompat.CATEGORY_SERVICE)
            .setForegroundServiceBehavior(NotificationCompat.FOREGROUND_SERVICE_IMMEDIATE)
            .build()
    }

    companion object {
        private const val NOTIFICATION_ID = 1
        private const val ACTION_NUDGE = "dev.bridge.gateway.action.NUDGE"

        /**
         * Starts the service. Returns false when Android refuses a background
         * start (the app is not exempt from battery optimisation); the caller
         * then relies on WorkManager.
         */
        fun start(context: Context, nudge: Boolean = false): Boolean {
            val intent = Intent(context, GatewayService::class.java)
            if (nudge) intent.action = ACTION_NUDGE
            return try {
                ContextCompat.startForegroundService(context, intent)
                true
            } catch (e: IllegalStateException) {
                // ForegroundServiceStartNotAllowedException on Android 12+.
                Log.w(AppContainer.TAG, "Foreground service start refused: ${e.message}")
                false
            }
        }

        fun stop(context: Context) {
            context.stopService(Intent(context, GatewayService::class.java))
        }
    }
}
