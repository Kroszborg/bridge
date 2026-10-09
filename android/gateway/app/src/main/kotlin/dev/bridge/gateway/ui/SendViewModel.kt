package dev.bridge.gateway.ui

import android.app.Application
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import dev.bridge.gateway.account.AccountAction
import dev.bridge.gateway.account.AccountErrors
import dev.bridge.gateway.account.ServerUrls
import dev.bridge.gateway.container
import dev.bridge.gateway.net.BridgeApiException
import dev.bridge.gateway.net.MessageDto
import dev.bridge.gateway.net.SendMessageBody
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import java.util.UUID
import kotlin.coroutines.cancellation.CancellationException

data class SendState(
    val sending: Boolean = false,
    val error: String? = null,
    /** The error was a plan limit: offer the dashboard's billing page. */
    val planLimit: Boolean = false,
    /** The message just sent, refreshed until its status is final. */
    val sent: MessageDto? = null,
)

/** Composes one message through the dashboard Playground's endpoint. */
class SendViewModel(app: Application) : AndroidViewModel(app) {
    private val repo = app.container.account
    private val _state = MutableStateFlow(SendState())
    val state: StateFlow<SendState> = _state.asStateFlow()
    private var watching: Job? = null

    // Retrying the same request after a network error reuses its key, so it cannot be sent twice.
    private var lastAttempt: Pair<String, SendMessageBody>? = null
    private var idempotencyKey = UUID.randomUUID().toString()

    fun send(to: String, body: String, environment: String, simSlot: Int?, deviceId: String?) {
        if (_state.value.sending) return
        watching?.cancel()
        val request = SendMessageBody(to.trim(), body, deviceId, simSlot)
        if (lastAttempt != environment to request) idempotencyKey = UUID.randomUUID().toString()
        lastAttempt = environment to request
        _state.value = SendState(sending = true)
        viewModelScope.launch {
            try {
                val message = repo.send(environment, request, idempotencyKey)
                lastAttempt = null
                _state.value = SendState(sent = message)
                watch(message)
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                val host = ServerUrls.host(repo.account.value?.apiUrl.orEmpty())
                _state.value = SendState(
                    error = AccountErrors.describe(e, host, AccountAction.Send),
                    planLimit = e is BridgeApiException && e.isPlanLimit,
                )
            }
        }
    }

    /** Polls the message until it reaches a final status, for up to two minutes. */
    private fun watch(message: MessageDto) {
        watching = viewModelScope.launch {
            var current = message
            repeat(POLLS) {
                if (current.isFinal) return@launch
                delay(POLL_MS)
                current = runCatching { repo.message(current.id) }.getOrNull() ?: current
                _state.update { if (it.sent?.id == current.id) it.copy(sent = current) else it }
            }
        }
    }

    fun dismiss() {
        watching?.cancel()
        _state.value = SendState()
    }

    private companion object {
        const val POLL_MS = 2_000L
        const val POLLS = 60
    }
}
