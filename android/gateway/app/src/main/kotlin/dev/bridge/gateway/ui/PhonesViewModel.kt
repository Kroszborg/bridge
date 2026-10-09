package dev.bridge.gateway.ui

import android.app.Application
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import dev.bridge.gateway.account.AccountAction
import dev.bridge.gateway.account.AccountErrors
import dev.bridge.gateway.account.ServerUrls
import dev.bridge.gateway.container
import dev.bridge.gateway.net.DeviceDto
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlin.coroutines.cancellation.CancellationException

data class PhonesState(
    val devices: List<DeviceDto> = emptyList(),
    val projectId: String? = null,
    val refreshing: Boolean = false,
    val error: String? = null,
)

/** The project's phones, with rename and remove for admins. */
class PhonesViewModel(app: Application) : AndroidViewModel(app) {
    private val repo = app.container.account
    private val _state = MutableStateFlow(PhonesState())
    val state: StateFlow<PhonesState> = _state.asStateFlow()

    fun refresh() {
        val projectId = repo.account.value?.project?.id ?: return
        _state.update { it.copy(refreshing = true, error = null) }
        viewModelScope.launch {
            try {
                val devices = repo.devices().filterNot { it.isRemoved }
                _state.update { it.copy(devices = devices, projectId = projectId, refreshing = false) }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                _state.update { it.copy(refreshing = false, error = describe(e, AccountAction.Load)) }
            }
        }
    }

    fun rename(deviceId: String, name: String) = change(AccountAction.ManagePhone) {
        val updated = repo.renameDevice(deviceId, name.trim())
        _state.update { s -> s.copy(devices = s.devices.map { if (it.id == deviceId) updated else it }) }
    }

    /** Removes another phone from the project. This phone is disconnected through the gateway instead. */
    fun remove(deviceId: String) = change(AccountAction.ManagePhone) {
        repo.removeDevice(deviceId)
        _state.update { s -> s.copy(devices = s.devices.filterNot { it.id == deviceId }) }
    }

    private fun change(action: AccountAction, block: suspend () -> Unit) {
        _state.update { it.copy(error = null) }
        viewModelScope.launch {
            try {
                block()
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                _state.update { it.copy(error = describe(e, action)) }
            }
        }
    }

    private fun describe(e: Throwable, action: AccountAction) =
        AccountErrors.describe(e, ServerUrls.host(repo.account.value?.apiUrl.orEmpty()), action)
}
