package dev.bridge.gateway.data

import android.content.Context
import androidx.datastore.preferences.core.booleanPreferencesKey
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.longPreferencesKey
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import dev.bridge.gateway.net.BridgeJson
import dev.bridge.gateway.net.HeartbeatPolicyDto
import dev.bridge.gateway.net.PushConfigDto
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.map
import kotlinx.serialization.Serializable
import java.util.UUID

/** What the phone remembers about the server it is paired with. Contains no secrets. */
@Serializable
data class Pairing(
    val deviceId: String,
    val projectId: String,
    val projectName: String,
    val apiUrl: String,
    val websocketUrl: String,
    val heartbeat: HeartbeatPolicyDto,
    val push: PushConfigDto,
    val pairedAt: Long,
)

private val Context.dataStore by preferencesDataStore(name = "gateway")

/** Pairing state, the encrypted credential and push registration status. */
class GatewayStore(private val context: Context, private val cipher: CredentialCipher) {
    private object Keys {
        val installationId = stringPreferencesKey("installation_id")
        val pairing = stringPreferencesKey("pairing")
        val credential = stringPreferencesKey("credential_encrypted")
        val pushProvider = stringPreferencesKey("push_registered_provider")
        val pushFingerprint = stringPreferencesKey("push_registered_fingerprint")
        val pushAt = longPreferencesKey("push_registered_at")
        val unpairedReason = stringPreferencesKey("unpaired_reason")
        val forwardInbound = booleanPreferencesKey("forward_inbound")
    }

    private val data get() = context.dataStore.data

    val pairing: Flow<Pairing?> = data.map { prefs ->
        prefs[Keys.pairing]?.let { runCatching { BridgeJson.decodeFromString(Pairing.serializer(), it) }.getOrNull() }
    }

    /** Why the phone was last unpaired, shown once on the welcome screen. */
    val unpairedReason: Flow<String?> = data.map { it[Keys.unpairedReason] }

    /** The push provider this phone successfully registered with the server, if any. */
    val pushRegistered: Flow<String?> = data.map { it[Keys.pushProvider] }

    /** Whether the server wants this phone to forward the SMS it receives. Off until it says so. */
    val forwardInbound: Flow<Boolean> = data.map { it[Keys.forwardInbound] ?: false }

    suspend fun pairing(): Pairing? = pairing.first()

    suspend fun forwardInbound(): Boolean = forwardInbound.first()

    suspend fun setForwardInbound(forward: Boolean) {
        context.dataStore.edit { it[Keys.forwardInbound] = forward }
    }

    suspend fun credential(): String? = data.first()[Keys.credential]?.let(cipher::decrypt)

    /** A random ID created once per install. Never derived from hardware identifiers. */
    suspend fun installationId(): String {
        data.first()[Keys.installationId]?.let { return it }
        val id = UUID.randomUUID().toString()
        context.dataStore.edit { if (it[Keys.installationId] == null) it[Keys.installationId] = id }
        return data.first()[Keys.installationId] ?: id
    }

    suspend fun savePairing(pairing: Pairing, credential: String) {
        val encrypted = cipher.encrypt(credential)
        context.dataStore.edit {
            it[Keys.pairing] = BridgeJson.encodeToString(Pairing.serializer(), pairing)
            it[Keys.credential] = encrypted
            it.remove(Keys.unpairedReason)
            it.remove(Keys.pushProvider)
            it.remove(Keys.pushFingerprint)
            it.remove(Keys.forwardInbound)
        }
    }

    suspend fun pushFingerprint(): String? = data.first()[Keys.pushFingerprint]

    suspend fun setPushRegistered(provider: String, fingerprint: String) {
        context.dataStore.edit {
            it[Keys.pushProvider] = provider
            it[Keys.pushFingerprint] = fingerprint
            it[Keys.pushAt] = System.currentTimeMillis()
        }
    }

    suspend fun clearPush() {
        context.dataStore.edit {
            it.remove(Keys.pushProvider)
            it.remove(Keys.pushFingerprint)
        }
    }

    /** Forgets the server. The installation ID is kept so re-pairing updates the same device. */
    suspend fun clearPairing(reason: String?) {
        context.dataStore.edit {
            it.remove(Keys.pairing)
            it.remove(Keys.credential)
            it.remove(Keys.pushProvider)
            it.remove(Keys.pushFingerprint)
            it.remove(Keys.forwardInbound)
            if (reason != null) it[Keys.unpairedReason] = reason else it.remove(Keys.unpairedReason)
        }
        cipher.deleteKey()
    }

    suspend fun dismissUnpairedReason() {
        context.dataStore.edit { it.remove(Keys.unpairedReason) }
    }
}
