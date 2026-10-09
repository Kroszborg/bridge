package dev.bridge.gateway.gateway

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import dev.bridge.gateway.container
import dev.bridge.gateway.diagnostics.EventLog

/** Restarts the gateway after a reboot or an app update. */
class BootReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        val what = when (intent.action) {
            Intent.ACTION_BOOT_COMPLETED, ACTION_QUICKBOOT -> "Phone started"
            Intent.ACTION_MY_PACKAGE_REPLACED -> "App updated"
            else -> return
        }
        val pending = goAsync()
        val c = context.container
        c.launch {
            try {
                c.events.record(EventLog.APP, what)
                c.ensureRunning(what.lowercase())
            } finally {
                pending.finish()
            }
        }
    }

    private companion object {
        // Sent instead of BOOT_COMPLETED by the fast-boot mode of some vendors' phones.
        const val ACTION_QUICKBOOT = "android.intent.action.QUICKBOOT_POWERON"
    }
}

/** Handles a push wake-up from either flavor. */
object WakeHandler {
    fun onWake(context: Context) {
        val app = context.applicationContext
        app.container.launch {
            if (app.container.store.pairing() == null) return@launch
            app.container.events.record(EventLog.PUSH, "Wake-up push received")
            app.container.connection.start("push wake-up")
            // High-priority pushes allow a foreground service start; if Android
            // still refuses, fall back to an expedited background sync.
            if (!GatewayService.start(app, nudge = true)) SyncWorker.runNow(app)
        }
    }
}
