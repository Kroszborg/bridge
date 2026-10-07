package dev.bridge.gateway.gateway

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import dev.bridge.gateway.container

/** Restarts the gateway after a reboot or an app update. */
class BootReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action != Intent.ACTION_BOOT_COMPLETED && intent.action != Intent.ACTION_MY_PACKAGE_REPLACED) return
        val pending = goAsync()
        context.container.launch {
            try {
                if (context.container.store.pairing() != null) context.container.startGateway()
            } finally {
                pending.finish()
            }
        }
    }
}

/** Handles a push wake-up from either flavor. */
object WakeHandler {
    fun onWake(context: Context) {
        val app = context.applicationContext
        app.container.launch {
            if (app.container.store.pairing() == null) return@launch
            // High-priority pushes allow a foreground service start; if Android
            // still refuses, fall back to an expedited background sync.
            if (!GatewayService.start(app, nudge = true)) SyncWorker.runNow(app)
        }
    }
}
