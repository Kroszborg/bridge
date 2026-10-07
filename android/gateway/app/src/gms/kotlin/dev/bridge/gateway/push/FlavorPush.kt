package dev.bridge.gateway.push

import android.app.Activity
import android.content.Context
import android.util.Log
import com.google.firebase.FirebaseApp
import com.google.firebase.FirebaseOptions
import com.google.firebase.messaging.FirebaseMessaging
import com.google.firebase.messaging.FirebaseMessagingService
import com.google.firebase.messaging.RemoteMessage
import dev.bridge.gateway.AppContainer
import dev.bridge.gateway.container
import dev.bridge.gateway.data.Pairing
import dev.bridge.gateway.gateway.WakeHandler
import dev.bridge.gateway.net.FcmConfigDto
import dev.bridge.gateway.net.PushRegistration

/**
 * gms flavor: wake-ups through Firebase Cloud Messaging. Firebase is
 * initialised at runtime from the configuration the Bridge server hands out
 * during pairing, so the build carries no google-services.json.
 */
val flavorPush: PushRegistrar = FirebaseRegistrar

object FirebaseRegistrar : PushRegistrar {
    override val provider = "Firebase"

    override fun onAppStart(context: Context, pairing: Pairing) {
        pairing.push.fcm?.let { ensureApp(context, it) }
    }

    override fun status(context: Context, pairing: Pairing?, registered: Boolean): PushStatus = when {
        pairing?.push?.fcm == null -> PushStatus(
            provider, PushState.Unavailable,
            "This Bridge server has no Firebase configuration. Ask its operator to add it, or install the foss build and use UnifiedPush.",
        )
        registered -> PushStatus(provider, PushState.Ready, "Bridge can wake this phone through Firebase.")
        else -> PushStatus(provider, PushState.NeedsSetup, "Turn on wake-ups to register this phone with Firebase.")
    }

    override fun setup(activity: Activity, pairing: Pairing) = refresh(activity, pairing)

    override fun refresh(context: Context, pairing: Pairing) {
        val config = pairing.push.fcm ?: return
        ensureApp(context, config)
        val app = context.applicationContext
        FirebaseMessaging.getInstance().token
            .addOnSuccessListener { token -> app.container.launch { app.container.registerPush(PushRegistration("fcm", token)) } }
            .addOnFailureListener { Log.w(AppContainer.TAG, "Firebase token request failed: ${it.message}") }
    }

    override fun reset(context: Context) {
        if (FirebaseApp.getApps(context).isNotEmpty()) FirebaseMessaging.getInstance().deleteToken()
    }

    private fun ensureApp(context: Context, config: FcmConfigDto) {
        if (FirebaseApp.getApps(context).isNotEmpty()) return
        FirebaseApp.initializeApp(
            context.applicationContext,
            FirebaseOptions.Builder()
                .setApplicationId(config.appId)
                .setApiKey(config.apiKey)
                .setProjectId(config.projectId)
                .setGcmSenderId(config.senderId)
                .build(),
        )
    }
}

class BridgeMessagingService : FirebaseMessagingService() {
    override fun onNewToken(token: String) {
        container.launch { container.registerPush(PushRegistration("fcm", token)) }
    }

    override fun onMessageReceived(message: RemoteMessage) {
        if (message.data["t"] == "wake") WakeHandler.onWake(this)
    }
}
