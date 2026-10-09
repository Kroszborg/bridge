package dev.bridge.gateway.gateway

import android.app.Notification
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Build
import androidx.core.app.NotificationCompat
import androidx.core.content.ContextCompat
import androidx.lifecycle.LifecycleService
import androidx.lifecycle.lifecycleScope
import dev.bridge.gateway.BridgeApplication
import dev.bridge.gateway.R
import dev.bridge.gateway.container
import dev.bridge.gateway.diagnostics.EventLog
import dev.bridge.gateway.ui.MainActivity
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.launch

/**
 * Keeps the gateway connection alive while the phone is idle. Android requires
 * a visible notification for this; it doubles as an at-a-glance status.
 *
 * The connection does not belong to the service: if Android (or a vendor
 * battery manager) stops the service, the connection keeps going while the
 * process lives and the watchdog brings the service back.
 */
class GatewayService : LifecycleService() {
    override fun onCreate() {
        super.onCreate()
        promote(buildNotification(ConnectionState.Connecting(0), null))
        isRunning = true
        stopRequested = false
        val c = container
        c.events.record(EventLog.SERVICE, "Gateway service started")
        lifecycleScope.launch {
            combine(c.connection.state, c.store.pairing) { state, pairing -> state to pairing?.projectName }
                .distinctUntilChanged()
                .collect { (state, project) ->
                    getSystemService(NotificationManager::class.java).notify(NOTIFICATION_ID, buildNotification(state, project))
                }
        }
        c.connection.start("service started")
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        super.onStartCommand(intent, flags, startId)
        // A null intent means Android restarted this sticky service after stopping the process.
        if (intent == null) container.events.record(EventLog.SERVICE, "Android restarted the gateway service", warn = true)
        if (intent?.action == ACTION_NUDGE) container.connection.nudge()
        container.connection.start("service start")
        return START_STICKY
    }

    override fun onTaskRemoved(rootIntent: Intent?) {
        // Several vendors stop an app's services when it is swiped away from recents.
        container.events.record(EventLog.SERVICE, "App removed from recent apps; scheduling a restart check")
        SyncWorker.restartSoon(this)
        super.onTaskRemoved(rootIntent)
    }

    override fun onDestroy() {
        isRunning = false
        if (stopRequested) {
            container.events.record(EventLog.SERVICE, "Gateway service stopped")
        } else {
            container.events.record(EventLog.SERVICE, "Android stopped the gateway service; restarting shortly", warn = true)
            SyncWorker.restartSoon(this)
        }
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

        /** Whether the service is running in this process. */
        @Volatile var isRunning = false
            private set

        @Volatile private var stopRequested = false

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
                context.container.events.record(EventLog.SERVICE, "Foreground service start refused: ${e.message}", warn = true)
                false
            } catch (e: SecurityException) {
                context.container.events.record(EventLog.SERVICE, "Foreground service start refused: ${e.message}", warn = true)
                false
            }
        }

        fun stop(context: Context) {
            stopRequested = true
            context.stopService(Intent(context, GatewayService::class.java))
        }
    }
}
