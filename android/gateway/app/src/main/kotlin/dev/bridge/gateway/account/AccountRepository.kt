package dev.bridge.gateway.account

import dev.bridge.gateway.net.AccountApi
import dev.bridge.gateway.net.BillingDto
import dev.bridge.gateway.net.BridgeApiException
import dev.bridge.gateway.net.DeviceDto
import dev.bridge.gateway.net.MessageDto
import dev.bridge.gateway.net.MessageList
import dev.bridge.gateway.net.OrganizationDto
import dev.bridge.gateway.net.PairingTokenDto
import dev.bridge.gateway.net.ProjectDto
import dev.bridge.gateway.net.SendMessageBody
import dev.bridge.gateway.pairing.PairingUri
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.withContext

/** An organization and the projects the user can open in it. */
data class OrganizationProjects(val organization: OrganizationDto, val projects: List<ProjectDto>)

/**
 * The signed-in side of the app. Every call uses the stored session; a 401 means
 * the session ended elsewhere (signed out, expired, password changed), so the
 * account is forgotten and the UI returns to sign-in. The gateway is unaffected.
 */
class AccountRepository(
    private val api: AccountApi,
    private val store: AccountStore,
    private val cookies: SessionCookieJar,
) {
    val account: StateFlow<Account?> = store.account
    val signedOutReason: StateFlow<String?> = store.signedOutReason
    val lastServer: String get() = store.lastServer ?: ServerUrls.HOSTED_API
    val lastEmail: String get() = store.lastEmail.orEmpty()

    /** Signs in and remembers the account. Throws [IllegalArgumentException] for an unusable server address. */
    suspend fun signIn(server: String, email: String, password: String, dashboard: String?): Account {
        val apiUrl = PairingUri.normalizeServerUrl(server)
            ?: throw IllegalArgumentException("Enter the server address, for example ${ServerUrls.HOSTED_API}.")
        val dashboardUrl = dashboard?.takeIf { it.isNotBlank() }?.let(PairingUri::normalizeServerUrl) ?: ServerUrls.dashboardFor(apiUrl)
        withContext(Dispatchers.IO) { cookies.clear() }
        val me = api.login(apiUrl, email.trim(), password)
        val account = Account(apiUrl, dashboardUrl, me.user, me.organizations)
        store.save(account)
        return account
    }

    /** Ends the session on the server when possible and forgets it locally either way. */
    suspend fun signOut() {
        val current = account.value ?: return
        runCatching { api.logout(current.apiUrl) }
        withContext(Dispatchers.IO) {
            cookies.clear()
            store.clear(null)
        }
    }

    fun dismissSignedOutReason() = store.dismissSignedOutReason()

    fun selectProject(project: ProjectDto) {
        val current = account.value ?: return
        store.save(current.copy(project = SelectedProject(project.id, project.name, project.organizationId)))
    }

    fun setDashboardUrl(url: String): Boolean {
        val current = account.value ?: return false
        val normalized = PairingUri.normalizeServerUrl(url) ?: return false
        store.save(current.copy(dashboardUrl = normalized))
        return true
    }

    /** Refreshes the user's organizations and roles, which can change in the dashboard. */
    suspend fun refreshProfile() {
        val me = authed { api.me(it) }
        val current = account.value ?: return
        store.save(current.copy(user = me.user, organizations = me.organizations))
    }

    suspend fun projects(): List<OrganizationProjects> {
        val current = account.value ?: return emptyList()
        return current.organizations.map { org -> OrganizationProjects(org, authed { api.projects(it, org.id) }) }
    }

    /** A project by ID, or null when the user cannot see it on this server. */
    suspend fun findProject(projectId: String): ProjectDto? = try {
        authed { api.project(it, projectId) }
    } catch (e: BridgeApiException) {
        if (e.status == 404 || e.status == 422) null else throw e
    }

    suspend fun createPairingToken(): PairingTokenDto = withProject { url, p -> api.createPairingToken(url, p.id) }

    suspend fun messages(environment: String, limit: Int, startingAfter: String? = null): MessageList =
        withProject { url, p -> api.messages(url, p.id, environment, limit, startingAfter) }

    suspend fun message(messageId: String): MessageDto = withProject { url, p -> api.message(url, p.id, messageId) }

    suspend fun send(environment: String, body: SendMessageBody, idempotencyKey: String): MessageDto =
        withProject { url, p -> api.send(url, p.id, environment, body, idempotencyKey) }

    suspend fun devices(): List<DeviceDto> = withProject { url, p -> api.devices(url, p.id) }

    suspend fun renameDevice(deviceId: String, name: String): DeviceDto =
        withProject { url, p -> api.renameDevice(url, p.id, deviceId, name) }

    suspend fun removeDevice(deviceId: String) = withProject { url, p -> api.removeDevice(url, p.id, deviceId) }

    /** Null when the server does not offer plans. */
    suspend fun billing(): BillingDto? = withProject { url, p -> api.billing(url, p.organizationId) }

    private suspend fun <T> withProject(block: suspend (String, SelectedProject) -> T): T {
        val project = account.value?.project ?: throw IllegalStateException("Choose a project first.")
        return authed { block(it, project) }
    }

    private suspend fun <T> authed(block: suspend (String) -> T): T {
        val current = account.value ?: throw IllegalStateException("Sign in first.")
        try {
            return block(current.apiUrl)
        } catch (e: BridgeApiException) {
            if (e.status == 401) {
                withContext(Dispatchers.IO) {
                    cookies.clear()
                    store.clear("Your session ended. Sign in again to see messages and phones. The gateway keeps running.")
                }
            }
            throw e
        }
    }
}
