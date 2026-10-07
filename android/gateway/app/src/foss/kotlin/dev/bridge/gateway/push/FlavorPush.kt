package dev.bridge.gateway.push

import android.app.Activity
import android.content.Context
import android.util.Log
import dev.bridge.gateway.AppContainer
import dev.bridge.gateway.container
import dev.bridge.gateway.data.Pairing
import dev.bridge.gateway.gateway.WakeHandler
import dev.bridge.gateway.net.PushRegistration
import org.unifiedpush.android.connector.FailedReason
import org.unifiedpush.android.connector.PushService
import org.unifiedpush.android.connector.UnifiedPush
import org.unifiedpush.android.connector.data.PushEndpoint
import org.unifiedpush.android.connector.data.PushMessage

/** foss flavor: wake-ups through UnifiedPush (ntfy or any other distributor). */
val flavorPush: PushRegistrar = UnifiedPushRegistrar

object UnifiedPushRegistrar : PushRegistrar {
    override val provider = "UnifiedPush"
    private const val MESSAGE_FOR_DISTRIBUTOR = "Bridge SMS gateway"

    override fun status(context: Context, pairing: Pairing?, registered: Boolean): PushStatus = when {
        registered -> PushStatus(provider, PushState.Ready, "Bridge can wake this phone through ${distributorName(context)}.")
        UnifiedPush.getDistributors(context).isEmpty() -> PushStatus(
            provider, PushState.Unavailable,
            "Install a UnifiedPush distributor such as ntfy so Bridge can wake this phone when its connection drops.",
        )
        else -> PushStatus(provider, PushState.NeedsSetup, "A distributor is installed. Turn on wake-ups to use it.")
    }

    override fun setup(activity: Activity, pairing: Pairing) {
        UnifiedPush.tryUseCurrentOrDefaultDistributor(activity) { ok ->
            if (ok) register(activity, pairing) else Log.w(AppContainer.TAG, "No UnifiedPush distributor selected")
        }
    }

    override fun refresh(context: Context, pairing: Pairing) {
        if (UnifiedPush.getAckDistributor(context) != null) register(context, pairing)
    }

    override fun reset(context: Context) {
        UnifiedPush.unregister(context)
    }

    private fun register(context: Context, pairing: Pairing) {
        UnifiedPush.register(context, messageForDistributor = MESSAGE_FOR_DISTRIBUTOR, vapid = pairing.push.unifiedpush.vapidPublicKey)
    }

    private fun distributorName(context: Context): String {
        val pkg = UnifiedPush.getAckDistributor(context) ?: return "UnifiedPush"
        return runCatching {
            val pm = context.packageManager
            pm.getApplicationLabel(pm.getApplicationInfo(pkg, 0)).toString()
        }.getOrDefault(pkg)
    }
}

/** Receives endpoints and wake-ups from the UnifiedPush distributor. */
class BridgePushService : PushService() {
    override fun onNewEndpoint(endpoint: PushEndpoint, instance: String) {
        val keys = endpoint.pubKeySet
        if (keys == null) {
            Log.w(AppContainer.TAG, "Distributor returned an endpoint without encryption keys; ignoring it")
            return
        }
        container.launch {
            container.registerPush(PushRegistration("unifiedpush", endpoint.url, keys.pubKey, keys.auth))
        }
    }

    override fun onMessage(message: PushMessage, instance: String) {
        WakeHandler.onWake(this)
    }

    override fun onRegistrationFailed(reason: FailedReason, instance: String) {
        Log.w(AppContainer.TAG, "UnifiedPush registration failed: $reason")
    }

    override fun onUnregistered(instance: String) {
        container.launch { container.store.clearPush() }
    }
}
