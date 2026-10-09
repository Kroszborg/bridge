package dev.bridge.gateway.account

import android.content.Context
import androidx.core.content.edit
import dev.bridge.gateway.data.CredentialCipher
import dev.bridge.gateway.net.BridgeJson
import dev.bridge.gateway.net.OrganizationDto
import dev.bridge.gateway.net.UserDto
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.serialization.Serializable

/** The project the signed-in screens work with. */
@Serializable
data class SelectedProject(val id: String, val name: String, val organizationId: String)

/** Who is signed in, on which server. Contains no secrets; the session cookie is stored separately, encrypted. */
@Serializable
data class Account(
    val apiUrl: String,
    val dashboardUrl: String,
    val user: UserDto,
    val organizations: List<OrganizationDto>,
    val project: SelectedProject? = null,
) {
    val organization: OrganizationDto? get() = project?.let { p -> organizations.firstOrNull { it.id == p.organizationId } }

    /** Admins and owners manage phones, pair devices and send live messages. */
    val isAdmin: Boolean get() = organization?.isAdmin == true
}

/**
 * Account state in SharedPreferences rather than the gateway's DataStore: OkHttp's
 * cookie jar is synchronous, so the session must be readable without suspending.
 * The cookie is encrypted with its own Keystore key, separate from the device
 * credential's, so unpairing the phone does not sign the user out and vice versa.
 */
class AccountStore(context: Context, private val cipher: CredentialCipher) {
    private val prefs = context.getSharedPreferences("account", Context.MODE_PRIVATE)
    private val _account = MutableStateFlow(load())
    private val _signedOutReason = MutableStateFlow<String?>(null)

    val account: StateFlow<Account?> = _account.asStateFlow()

    /** Why the user was signed out without asking, shown once on the sign-in screen. */
    val signedOutReason: StateFlow<String?> = _signedOutReason.asStateFlow()

    /** What the sign-in form is prefilled with. */
    val lastServer: String? get() = prefs.getString(KEY_LAST_SERVER, null)
    val lastEmail: String? get() = prefs.getString(KEY_LAST_EMAIL, null)

    fun save(account: Account) {
        prefs.edit {
            putString(KEY_PROFILE, BridgeJson.encodeToString(Account.serializer(), account))
            putString(KEY_LAST_SERVER, account.apiUrl)
            putString(KEY_LAST_EMAIL, account.user.email)
        }
        _account.value = account
        _signedOutReason.value = null
    }

    fun clear(reason: String?) {
        prefs.edit {
            remove(KEY_PROFILE)
            remove(KEY_COOKIES)
        }
        cipher.deleteKey()
        _account.value = null
        _signedOutReason.value = reason
    }

    fun dismissSignedOutReason() {
        _signedOutReason.value = null
    }

    fun cookies(): String? = prefs.getString(KEY_COOKIES, null)?.let(cipher::decrypt)

    fun saveCookies(serialized: String?) {
        val sealed = serialized?.let(cipher::encrypt)
        prefs.edit { if (sealed == null) remove(KEY_COOKIES) else putString(KEY_COOKIES, sealed) }
    }

    private fun load(): Account? = prefs.getString(KEY_PROFILE, null)?.let {
        runCatching { BridgeJson.decodeFromString(Account.serializer(), it) }.getOrNull()
    }

    private companion object {
        const val KEY_PROFILE = "profile"
        const val KEY_COOKIES = "cookies_encrypted"
        const val KEY_LAST_SERVER = "last_server"
        const val KEY_LAST_EMAIL = "last_email"
    }
}
