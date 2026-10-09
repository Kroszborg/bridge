package dev.bridge.gateway.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.bridge.gateway.account.Account
import dev.bridge.gateway.data.Pairing
import dev.bridge.gateway.net.DeviceDto
import dev.bridge.gateway.ui.theme.Bridge

/** The project's phones, with this one highlighted. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun PhonesScreen(vm: PhonesViewModel, account: Account, pairing: Pairing?, onDisconnectThisPhone: () -> Unit) {
    val state by vm.state.collectAsStateWithLifecycle()
    val project = account.project ?: return
    val now = rememberNow(30_000)
    var renaming by remember { mutableStateOf<DeviceDto?>(null) }
    var removing by remember { mutableStateOf<DeviceDto?>(null) }
    LaunchedEffect(project.id) { vm.refresh() }

    // This phone first, then online phones, then the rest by name.
    val devices = state.devices.sortedWith(
        compareByDescending<DeviceDto> { it.id == pairing?.deviceId }.thenByDescending { it.status == "online" }.thenBy { it.name.lowercase() },
    )

    PullToRefreshBox(isRefreshing = state.refreshing, onRefresh = vm::refresh, modifier = Modifier.fillMaxSize()) {
        LazyColumn(
            Modifier.fillMaxSize(),
            contentPadding = PaddingValues(horizontal = 20.dp, vertical = 16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            item { ScreenHeader("Phones", project.name) }
            state.error?.let { item { Notice(it, action = "Retry" to vm::refresh) } }
            if (!state.refreshing && state.error == null && devices.isEmpty() && state.projectId == project.id) {
                item {
                    Text(
                        "No phones in this project yet. Pair this one from the Gateway tab.",
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.padding(vertical = 24.dp),
                    )
                }
            }
            items(devices, key = { it.id }) { device ->
                PhoneCard(
                    device = device,
                    isThisPhone = device.id == pairing?.deviceId,
                    canManage = account.isAdmin,
                    now = now,
                    onRename = { renaming = device },
                    onRemove = { if (device.id == pairing?.deviceId) onDisconnectThisPhone() else removing = device },
                )
            }
        }
    }

    renaming?.let { device ->
        RenameDialog(device.name, onDismiss = { renaming = null }) { name ->
            renaming = null
            vm.rename(device.id, name)
        }
    }
    removing?.let { device ->
        AlertDialog(
            onDismissRequest = { removing = null },
            title = { Text("Remove ${device.name}?") },
            text = { Text("Its credential is revoked at once and it stops sending for ${project.name}. It can be paired again later.") },
            confirmButton = { Button(onClick = { removing = null; vm.remove(device.id) }) { Text("Remove") } },
            dismissButton = { TextButton(onClick = { removing = null }) { Text("Cancel") } },
        )
    }
}

@Composable
private fun PhoneCard(device: DeviceDto, isThisPhone: Boolean, canManage: Boolean, now: Long, onRename: () -> Unit, onRemove: () -> Unit) {
    val online = device.status == "online"
    var menu by remember { mutableStateOf(false) }
    SectionCard {
        Row(verticalAlignment = Alignment.CenterVertically) {
            StatusDot(if (online) Bridge.colors.success else Bridge.colors.faint, live = online, size = 10.dp)
            Spacer(Modifier.width(12.dp))
            Column(Modifier.weight(1f)) {
                Text(device.name, style = MaterialTheme.typography.titleSmall, maxLines = 1, overflow = TextOverflow.Ellipsis)
                Text(
                    listOfNotNull(device.deviceModel, device.carrierName).joinToString(" · ").ifBlank { "Android phone" },
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
            if (isThisPhone) {
                Text("This phone", style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.primary)
            }
            if (canManage) {
                Column {
                    TextButton(onClick = { menu = true }) { Text("More") }
                    DropdownMenu(expanded = menu, onDismissRequest = { menu = false }) {
                        DropdownMenuItem(text = { Text("Rename") }, onClick = { menu = false; onRename() })
                        DropdownMenuItem(
                            text = { Text(if (isThisPhone) "Disconnect this phone" else "Remove", color = MaterialTheme.colorScheme.error) },
                            onClick = { menu = false; onRemove() },
                        )
                    }
                }
            }
        }
        Spacer(Modifier.height(10.dp))
        val detail = listOfNotNull(
            if (online) "Online" else "Offline · seen ${shortTime(device.lastSeenAt, now).ifBlank { "never" }}",
            device.batteryLevel?.let { "Battery $it%" + if (device.isCharging == true) " charging" else "" },
            device.networkType,
            "${device.totalSent} sent",
            device.totalFailed.takeIf { it > 0 }?.let { "$it failed" },
        ).joinToString(" · ")
        Text(detail, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

@Composable
private fun RenameDialog(current: String, onDismiss: () -> Unit, onSave: (String) -> Unit) {
    var name by rememberSaveable { mutableStateOf(current) }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Rename phone") },
        text = {
            OutlinedTextField(value = name, onValueChange = { if (it.length <= 80) name = it }, singleLine = true, label = { Text("Name") })
        },
        confirmButton = { Button(onClick = { onSave(name) }, enabled = name.isNotBlank() && name.trim() != current) { Text("Save") } },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Cancel") } },
    )
}
