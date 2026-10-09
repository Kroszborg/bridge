package dev.bridge.gateway.ui

import android.app.Application
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import dev.bridge.gateway.account.AccountAction
import dev.bridge.gateway.account.AccountErrors
import dev.bridge.gateway.account.OrganizationProjects
import dev.bridge.gateway.account.PlanSummary
import dev.bridge.gateway.account.ServerUrls
import dev.bridge.gateway.container
import dev.bridge.gateway.net.ProjectDto
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlin.coroutines.cancellation.CancellationException

data class SignInState(val busy: Boolean = false, val error: String? = null)

data class PickerState(
    val loading: Boolean = false,
    val error: String? = null,
    val organizations: List<OrganizationProjects> = emptyList(),
)

/** The plan card. [hidden] when the server has no billing (404). */
data class PlanState(
    val loading: Boolean = false,
    val summary: PlanSummary? = null,
    val hidden: Boolean = false,
    val error: String? = null,
)

/** Sign-in, project choice, plan and sign-out. */
class AccountViewModel(app: Application) : AndroidViewModel(app) {
    private val c = app.container
    private val repo = c.account

    val account = repo.account
    val signedOutReason = repo.signedOutReason
    val lastServer: String get() = repo.lastServer
    val lastEmail: String get() = repo.lastEmail

    private val _signIn = MutableStateFlow(SignInState())
    val signIn: StateFlow<SignInState> = _signIn.asStateFlow()

    private val _picker = MutableStateFlow(PickerState())
    val picker: StateFlow<PickerState> = _picker.asStateFlow()

    private val _plan = MutableStateFlow(PlanState())
    val plan: StateFlow<PlanState> = _plan.asStateFlow()

    fun signIn(server: String, email: String, password: String, dashboard: String?) {
        if (_signIn.value.busy) return
        _signIn.value = SignInState(busy = true)
        repo.dismissSignedOutReason()
        viewModelScope.launch {
            val error = try {
                repo.signIn(server, email, password, dashboard)
                chooseProjectAutomatically()
                null
            } catch (e: CancellationException) {
                throw e
            } catch (e: IllegalArgumentException) {
                e.message
            } catch (e: Exception) {
                AccountErrors.describe(e, hostOf(server), AccountAction.SignIn)
            }
            _signIn.value = SignInState(error = error)
        }
    }

    /** Opens the project this phone is paired with, or the only project there is. */
    private suspend fun chooseProjectAutomatically() {
        val paired = c.store.pairing()?.let { runCatching { repo.findProject(it.projectId) }.getOrNull() }
        if (paired != null) {
            repo.selectProject(paired)
            return
        }
        val all = runCatching { repo.projects() }.getOrNull()?.flatMap { it.projects }.orEmpty()
        all.singleOrNull()?.let(repo::selectProject)
    }

    fun loadProjects() {
        _picker.update { it.copy(loading = true, error = null) }
        viewModelScope.launch {
            _picker.value = try {
                runCatching { repo.refreshProfile() }
                PickerState(organizations = repo.projects())
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                PickerState(error = describe(e), organizations = _picker.value.organizations)
            }
        }
    }

    fun selectProject(project: ProjectDto) {
        repo.selectProject(project)
        _plan.value = PlanState()
    }

    fun loadPlan() {
        _plan.update { it.copy(loading = true, error = null) }
        viewModelScope.launch {
            _plan.value = try {
                val billing = repo.billing()
                if (billing == null) PlanState(hidden = true) else PlanState(summary = PlanSummary.from(billing))
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                PlanState(error = describe(e), summary = _plan.value.summary)
            }
        }
    }

    fun setDashboardUrl(url: String): Boolean = repo.setDashboardUrl(url)

    /** Signs out; the gateway keeps running unless [alsoDisconnect]. */
    fun signOut(alsoDisconnect: Boolean) {
        viewModelScope.launch {
            repo.signOut()
            _plan.value = PlanState()
            _picker.value = PickerState()
            if (alsoDisconnect) c.unpair()
        }
    }

    fun dismissSignedOutReason() = repo.dismissSignedOutReason()

    private fun describe(e: Throwable) = AccountErrors.describe(e, hostOf(account.value?.apiUrl.orEmpty()), AccountAction.Load)

    private fun hostOf(url: String) = ServerUrls.host(url)
}
