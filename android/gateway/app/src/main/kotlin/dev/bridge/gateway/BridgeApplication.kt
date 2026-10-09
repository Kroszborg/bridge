package dev.bridge.gateway

import android.app.Application
import android.app.NotificationChannel
import android.app.NotificationManager
import dev.bridge.gateway.diagnostics.EventLog
import dev.bridge.gateway.push.flavorPush
import kotlinx.coroutines.delay

class BridgeApplication : Application() {
    lateinit var container: AppContainer
        private set

    override fun onCreate() {
        super.onCreate()
        container = AppContainer(this)
        container.events.installCrashRecorder()
        container.events.record(EventLog.APP, "App process started (${BuildConfig.VERSION_NAME}, ${BuildConfig.FLAVOR})")
        container.events.recordPreviousExits(this)
        createChannels()
        container.widgets.start()
        container.launch {
            val pairing = container.store.pairing() ?: return@launch
            runCatching { flavorPush.onAppStart(this@BridgeApplication, pairing) }
            // Whatever started the process (a push, an SMS report, the widget, Android restarting the
            // service), bring the gateway back. The pause lets a service that is being restarted go first.
            delay(2_000)
            container.ensureRunning("app process started")
        }
    }

    private fun createChannels() {
        val nm = getSystemService(NotificationManager::class.java)
        nm.createNotificationChannel(
            NotificationChannel(CHANNEL_GATEWAY, "Gateway connection", NotificationManager.IMPORTANCE_LOW).apply {
                description = "Shows whether this phone is connected to your Bridge server."
                setShowBadge(false)
            },
        )
    }

    companion object {
        const val CHANNEL_GATEWAY = "gateway"
    }
}

val android.content.Context.container: AppContainer
    get() = (applicationContext as BridgeApplication).container
