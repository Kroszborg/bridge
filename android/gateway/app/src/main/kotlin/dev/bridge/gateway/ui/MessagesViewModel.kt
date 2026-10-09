package dev.bridge.gateway.ui

import android.app.Application
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import dev.bridge.gateway.account.AccountAction
import dev.bridge.gateway.account.AccountErrors
import dev.bridge.gateway.account.ServerUrls
import dev.bridge.gateway.container
import dev.bridge.gateway.net.MessageDto
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlin.coroutines.cancellation.CancellationException

data class MessagesState(
    val environment: String = "live",
    val messages: List<MessageDto> = emptyList(),
    val hasMore: Boolean = false,
    /** Which project the list belongs to, so a project switch reloads it. */
    val projectId: String? = null,
    val refreshing: Boolean = false,
    val loadingMore: Boolean = false,
    val error: String? = null,
)

/** Recent sent and received messages for the selected project. */
class MessagesViewModel(app: Application) : AndroidViewModel(app) {
    private val repo = app.container.account
    private val _state = MutableStateFlow(MessagesState())
    val state: StateFlow<MessagesState> = _state.asStateFlow()
    private var loading: Job? = null

    fun setEnvironment(environment: String) {
        if (environment == _state.value.environment) return
        _state.update { it.copy(environment = environment, messages = emptyList(), hasMore = false) }
        refresh()
    }

    fun refresh() {
        val projectId = repo.account.value?.project?.id ?: return
        val environment = _state.value.environment
        loading?.cancel()
        _state.update { it.copy(refreshing = true, error = null) }
        loading = viewModelScope.launch {
            try {
                val page = repo.messages(environment, PAGE)
                _state.update { it.copy(messages = page.data, hasMore = page.hasMore, projectId = projectId, refreshing = false) }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                _state.update { it.copy(refreshing = false, error = describe(e)) }
            }
        }
    }

    fun loadMore() {
        val s = _state.value
        val last = s.messages.lastOrNull() ?: return
        if (!s.hasMore || s.loadingMore || s.refreshing) return
        _state.update { it.copy(loadingMore = true, error = null) }
        loading = viewModelScope.launch {
            try {
                val page = repo.messages(s.environment, PAGE, startingAfter = last.id)
                _state.update { it.copy(messages = it.messages + page.data, hasMore = page.hasMore, loadingMore = false) }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                _state.update { it.copy(loadingMore = false, error = describe(e)) }
            }
        }
    }

    private fun describe(e: Throwable) =
        AccountErrors.describe(e, ServerUrls.host(repo.account.value?.apiUrl.orEmpty()), AccountAction.Load)

    private companion object {
        const val PAGE = 50
    }
}
