package dev.bridge.gateway

import android.app.Application
import android.app.NotificationChannel
import android.app.NotificationManager
import dev.bridge.gateway.push.flavorPush

class BridgeApplication : Application() {
    lateinit var container: AppContainer
        private set

    override fun onCreate() {
        super.onCreate()
        container = AppContainer(this)
        createChannels()
        container.launch {
            val pairing = container.store.pairing() ?: return@launch
            runCatching { flavorPush.onAppStart(this@BridgeApplication, pairing) }
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
