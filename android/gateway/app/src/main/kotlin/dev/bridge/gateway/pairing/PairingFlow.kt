package dev.bridge.gateway.pairing

import dev.bridge.gateway.net.BridgeApiException
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.serialization.SerializationException
import java.io.IOException

/** A pairing waiting for the user to confirm the server. */
data class PairingConfirm(
    val request: PairingRequest,
    /** The project this phone is paired with now; confirming replaces that pairing. */
    val replacing: String? = null,
)

data class PairingUi(
    val busy: Boolean = false,
    val error: String? = null,
    /** The last error was a plan limit, so the dashboard's billing page can help. */
    val planLimit: Boolean = false,
    val confirm: PairingConfirm? = null,
)

/**
 * The pairing steps the screens drive: confirm the server, exchange the code,
 * show the result. Every attempt ends with the controls enabled again and, on
 * failure, an error on screen, so the next code always sends a new request.
 * A code that arrives while the phone is still paired (for example after the
 * dashboard removed it while it was offline) is offered as a replacement
 * instead of being refused.
 */
class PairingFlow(
    private val pair: suspend (PairingRequest) -> Unit,
    private val log: (String) -> Unit = {},
) {
    private val _state = MutableStateFlow(PairingUi())
    val state: StateFlow<PairingUi> = _state.asStateFlow()

    /** Every pairing goes through an explicit confirmation naming the server. */
    fun request(request: PairingRequest, currentProject: String?) {
        log("Pairing code for ${request.host} received${currentProject?.let { "; it would replace the pairing with $it" } ?: ""}")
        _state.update { it.copy(confirm = PairingConfirm(request, currentProject), error = null, planLimit = false) }
    }

    fun cancel() {
        if (_state.value.confirm != null) log("Pairing cancelled")
        _state.update { it.copy(confirm = null) }
    }

    /** Exchanges the confirmed code. Does nothing while another pairing runs. */
    suspend fun confirm() {
        while (true) {
            val s = _state.value
            val confirm = s.confirm ?: return
            if (s.busy) return
            if (_state.compareAndSet(s, s.copy(confirm = null, busy = true, error = null, planLimit = false))) {
                exchange(confirm.request)
                return
            }
        }
    }

    /** Starts a pairing that needs no confirmation (the signed-in account's project). False while one runs. */
    fun begin(): Boolean {
        while (true) {
            val s = _state.value
            if (s.busy) return false
            if (_state.compareAndSet(s, s.copy(busy = true, error = null, planLimit = false))) return true
        }
    }

    /** Ends work started with [begin] that has nothing to report (disconnecting). */
    fun finish() = _state.update { it.copy(busy = false) }

    /** Ends a pairing started with [begin] that failed before a code existed. */
    fun fail(message: String, planLimit: Boolean = false) {
        log("Pairing failed: $message")
        _state.update { it.copy(busy = false, error = message, planLimit = planLimit) }
    }

    /** Exchanges a code for a credential and reports the result. */
    suspend fun exchange(request: PairingRequest) {
        _state.update { it.copy(busy = true, error = null, planLimit = false) }
        var planLimit = false
        val error = try {
            pair(request)
            null
        } catch (e: CancellationException) {
            _state.update { it.copy(busy = false) }
            throw e
        } catch (e: BridgeApiException) {
            planLimit = e.isPlanLimit
            e.message
        } catch (e: IOException) {
            "Could not reach ${request.host}. Check the address and that this phone can reach the server. (${e.message})"
        } catch (_: SerializationException) {
            "${request.host} did not answer like a Bridge server. Check the address."
        } catch (e: Exception) {
            "Pairing failed: ${e.message ?: e.javaClass.simpleName}. Try again; the connection log has the details."
        }
        _state.update { it.copy(busy = false, error = error, planLimit = planLimit) }
    }

    fun showError(message: String) = _state.update { it.copy(error = message, planLimit = false) }

    fun dismissError() = _state.update { it.copy(error = null, planLimit = false) }
}
