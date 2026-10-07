package dev.bridge.gateway.ui

import android.app.Application
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import dev.bridge.gateway.container
import dev.bridge.gateway.data.Pairing
import dev.bridge.gateway.gateway.ConnectionState
import dev.bridge.gateway.net.BridgeApiException
import dev.bridge.gateway.pairing.PairingRequest
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import java.io.IOException

data class UiState(
    val loading: Boolean = true,
    val pairing: Pairing? = null,
    val connection: ConnectionState = ConnectionState.Stopped,
    val unpairedReason: String? = null,
    val pushRegistered: Boolean = false,
    val busy: Boolean = false,
    val error: String? = null,
    /** A pairing request waiting for the user to confirm the server. */
    val confirm: PairingRequest? = null,
)

private data class Local(val busy: Boolean = false, val error: String? = null, val confirm: PairingRequest? = null)

class MainViewModel(app: Application) : AndroidViewModel(app) {
    private val c = app.container
    private val local = MutableStateFlow(Local())

    val state: StateFlow<UiState> = combine(
        c.store.pairing, c.connection.state, c.store.unpairedReason, c.store.pushRegistered, local,
    ) { pairing, connection, reason, push, l ->
        UiState(
            loading = false, pairing = pairing, connection = connection, unpairedReason = reason,
            pushRegistered = push != null, busy = l.busy, error = l.error, confirm = l.confirm,
        )
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), UiState())

    /** Every pairing goes through an explicit confirmation naming the server. */
    fun requestPairing(request: PairingRequest) {
        if (state.value.pairing != null) {
            local.update { it.copy(error = "This phone is already paired. Disconnect it first to pair with another server.") }
            return
        }
        local.update { it.copy(confirm = request, error = null) }
    }

    fun cancelPairing() = local.update { it.copy(confirm = null) }

    fun confirmPairing() {
        val request = local.value.confirm ?: return
        local.update { it.copy(confirm = null, busy = true, error = null) }
        viewModelScope.launch {
            val error = try {
                c.pair(request)
                null
            } catch (e: BridgeApiException) {
                e.message
            } catch (e: IOException) {
                "Could not reach ${request.host}. Check the address and that this phone can reach the server. (${e.message})"
            } catch (e: kotlinx.serialization.SerializationException) {
                "${request.host} did not answer like a Bridge server. Check the address."
            }
            local.update { it.copy(busy = false, error = error) }
        }
    }

    fun showError(message: String) = local.update { it.copy(error = message) }

    fun dismissError() = local.update { it.copy(error = null) }

    fun dismissUnpairedReason() {
        viewModelScope.launch { c.store.dismissUnpairedReason() }
    }

    fun reconnectNow() {
        c.startGateway()
        c.connection.nudge()
    }

    fun unpair() {
        local.update { it.copy(busy = true) }
        viewModelScope.launch {
            c.unpair()
            local.update { it.copy(busy = false) }
        }
    }
}
