package dev.bridge.gateway.ui

import android.app.Application
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import dev.bridge.gateway.account.Account
import dev.bridge.gateway.account.AccountAction
import dev.bridge.gateway.account.AccountErrors
import dev.bridge.gateway.account.ServerUrls
import dev.bridge.gateway.container
import dev.bridge.gateway.data.Pairing
import dev.bridge.gateway.diagnostics.EventLog
import dev.bridge.gateway.gateway.ConnectionState
import dev.bridge.gateway.net.BridgeApiException
import dev.bridge.gateway.pairing.PairingConfirm
import dev.bridge.gateway.pairing.PairingFlow
import dev.bridge.gateway.pairing.PairingRequest
import dev.bridge.gateway.pairing.PairingUri
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import kotlin.coroutines.cancellation.CancellationException

data class UiState(
    val loading: Boolean = true,
    val pairing: Pairing? = null,
    val connection: ConnectionState = ConnectionState.Stopped,
    val unpairedReason: String? = null,
    val pushRegistered: Boolean = false,
    val busy: Boolean = false,
    val error: String? = null,
    /** The last error was a plan limit, so the dashboard's billing page can help. */
    val planLimit: Boolean = false,
    /** A pairing request waiting for the user to confirm the server. */
    val confirm: PairingConfirm? = null,
    /** The signed-in account, if any. The gateway works without one. */
    val account: Account? = null,
)

class MainViewModel(app: Application) : AndroidViewModel(app) {
    private val c = app.container
    private val pairingFlow = PairingFlow(pair = { c.pair(it) }, log = { c.events.record(EventLog.PAIRING, it) })

    val state: StateFlow<UiState> = combine(
        c.store.pairing, c.connection.state, c.store.unpairedReason, c.store.pushRegistered,
        combine(pairingFlow.state, c.account.account, ::Pair),
    ) { pairing, connection, reason, push, (p, account) ->
        UiState(
            loading = false, pairing = pairing, connection = connection, unpairedReason = reason,
            pushRegistered = push != null, busy = p.busy, error = p.error, planLimit = p.planLimit, confirm = p.confirm,
            account = account,
        )
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), UiState())

    /**
     * Every pairing goes through an explicit confirmation naming the server. A
     * phone that is still paired gets the same dialog, saying what it replaces.
     */
    fun requestPairing(request: PairingRequest) {
        viewModelScope.launch {
            // Read the store, not the screen state: that can still be loading when a link opens the app.
            pairingFlow.request(request, c.store.pairing()?.projectName)
        }
    }

    fun cancelPairing() = pairingFlow.cancel()

    // In the app's scope: leaving the screen must not cancel a pairing halfway through.
    fun confirmPairing() {
        c.launch { pairingFlow.confirm() }
    }

    /**
     * Pairs with the signed-in account's project: creates a pairing code as the
     * dashboard would, then exchanges it exactly like a scanned one. The user chose
     * this server by signing in to it, so there is no separate confirmation.
     */
    fun pairWithAccount() {
        if (state.value.pairing != null || !pairingFlow.begin()) return
        c.launch {
            val host = ServerUrls.host(state.value.account?.apiUrl.orEmpty())
            val request = try {
                val token = c.account.createPairingToken()
                PairingUri.parse(token.pairingUri) ?: PairingUri.fromManual(token.apiUrl, token.token).getOrThrow()
            } catch (e: CancellationException) {
                pairingFlow.fail("Pairing was interrupted. Try again.")
                throw e
            } catch (e: Exception) {
                pairingFlow.fail(AccountErrors.describe(e, host, AccountAction.PairPhone), planLimit = e is BridgeApiException && e.isPlanLimit)
                return@launch
            }
            pairingFlow.exchange(request)
        }
    }

    fun showError(message: String) = pairingFlow.showError(message)

    fun dismissError() = pairingFlow.dismissError()

    fun dismissUnpairedReason() {
        viewModelScope.launch { c.store.dismissUnpairedReason() }
    }

    fun reconnectNow() {
        c.events.record(EventLog.CONNECTION, "Reconnect requested in the app")
        c.startGateway("reconnect requested")
        c.connection.nudge()
    }

    /** Opening the app always brings back a gateway that Android or a vendor battery manager stopped. */
    fun ensureRunning() {
        c.launch { c.ensureRunning("app opened") }
    }

    fun unpair() {
        if (!pairingFlow.begin()) return
        c.launch {
            try {
                c.unpair()
            } finally {
                pairingFlow.finish()
            }
        }
    }
}
