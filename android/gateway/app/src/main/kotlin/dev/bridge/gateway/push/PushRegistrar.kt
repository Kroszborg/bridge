package dev.bridge.gateway.push

import android.app.Activity
import android.content.Context
import dev.bridge.gateway.data.Pairing

enum class PushState { Ready, NeedsSetup, Unavailable }

data class PushStatus(val provider: String, val state: PushState, val detail: String)

/**
 * Wake-up registration. Each build flavor provides one implementation as
 * [flavorPush]: UnifiedPush in `foss`, Firebase Cloud Messaging in `gms`.
 * Registrations complete asynchronously and report back through
 * [dev.bridge.gateway.AppContainer.registerPush].
 */
interface PushRegistrar {
    /** Human-readable provider name. */
    val provider: String

    /** Called when the app process starts with an existing pairing. */
    fun onAppStart(context: Context, pairing: Pairing) {}

    fun status(context: Context, pairing: Pairing?, registered: Boolean): PushStatus

    /** Interactive setup; may show the distributor picker. */
    fun setup(activity: Activity, pairing: Pairing)

    /** Background re-registration, for example from the periodic sync. */
    fun refresh(context: Context, pairing: Pairing)

    /** Drops the registration when the phone is unpaired. */
    fun reset(context: Context)
}
