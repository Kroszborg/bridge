package dev.bridge.gateway.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import dev.bridge.gateway.BuildConfig
import dev.bridge.gateway.data.Pairing
import dev.bridge.gateway.gateway.ConnectionState
import dev.bridge.gateway.pairing.PairingRequest
import dev.bridge.gateway.push.PushState
import dev.bridge.gateway.push.PushStatus
import dev.bridge.gateway.ui.theme.Bridge
import dev.bridge.gateway.ui.theme.RedHatMono

/** What keeps the gateway reliable, checked each time the app resumes. */
data class ReliabilityChecks(
    val smsAllowed: Boolean = true,
    val phoneStateAllowed: Boolean = true,
    val forwardInbound: Boolean = false,
    val receiveSmsAllowed: Boolean = true,
    val notificationsAllowed: Boolean = true,
    val batteryUnrestricted: Boolean = true,
    val push: PushStatus? = null,
    val sentToday: Int = 0,
    val failedToday: Int = 0,
)

data class StatusActions(
    val allowSms: () -> Unit,
    val allowPhoneState: () -> Unit,
    val allowReceiveSms: () -> Unit,
    val allowNotifications: () -> Unit,
    val allowBackground: () -> Unit,
    val setupPush: () -> Unit,
    val reconnect: () -> Unit,
    val unpair: () -> Unit,
)

@Composable
fun StatusScreen(state: UiState, pairing: Pairing, checks: ReliabilityChecks, actions: StatusActions) {
    val now = rememberNow(1_000)
    var menu by remember { mutableStateOf(false) }
    var confirmUnpair by remember { mutableStateOf(false) }
    val host = PairingRequest(pairing.apiUrl, "").host

    Column(
        Modifier
            .fillMaxSize()
            .safeDrawingPadding()
            .verticalScroll(rememberScrollState())
            .padding(horizontal = 20.dp, vertical = 16.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            BridgeTile(36.dp)
            Spacer(Modifier.width(12.dp))
            Column(Modifier.weight(1f)) {
                Text("Bridge", style = MaterialTheme.typography.titleMedium)
                Text("SMS gateway", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            Column {
                TextButton(onClick = { menu = true }) { Text("More") }
                DropdownMenu(expanded = menu, onDismissRequest = { menu = false }) {
                    DropdownMenuItem(text = { Text("Reconnect now") }, onClick = { menu = false; actions.reconnect() })
                    DropdownMenuItem(
                        text = { Text("Disconnect this phone", color = MaterialTheme.colorScheme.error) },
                        onClick = { menu = false; confirmUnpair = true },
                    )
                }
            }
        }

        ConnectionCard(state.connection, pairing, host, now, actions.reconnect)
        ChecklistCard(checks, pairing, actions)

        SectionCard {
            Eyebrow("Today")
            Spacer(Modifier.height(8.dp))
            Row {
                Column(Modifier.weight(1f)) {
                    Text("${checks.sentToday}", style = MaterialTheme.typography.headlineSmall)
                    Text("sent", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
                Column(Modifier.weight(1f)) {
                    Text(
                        "${checks.failedToday}",
                        style = MaterialTheme.typography.headlineSmall,
                        color = if (checks.failedToday > 0) Bridge.colors.danger else Color.Unspecified,
                    )
                    Text("failed", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
            }
        }

        SectionCard {
            Eyebrow("This phone")
            Spacer(Modifier.height(12.dp))
            InfoRow("Device ID", pairing.deviceId, mono = true)
            InfoRow("Project", pairing.projectName)
            InfoRow("Server", pairing.apiUrl, mono = true)
            InfoRow("App", "${BuildConfig.VERSION_NAME} · ${BuildConfig.FLAVOR}")
            InfoRow("Incoming", if (checks.forwardInbound) "Forwarded to the server" else "Not forwarded")
        }

        Text(
            "Bridge only sends what your server asks for. Carrier limits on this SIM still apply.",
            style = MaterialTheme.typography.bodySmall,
            color = Bridge.colors.faint,
            modifier = Modifier.padding(horizontal = 4.dp),
        )
    }

    if (confirmUnpair) {
        AlertDialog(
            onDismissRequest = { confirmUnpair = false },
            title = { Text("Disconnect this phone?") },
            text = { Text("It stops sending messages for ${pairing.projectName} and is removed from the dashboard. You can pair it again later.") },
            confirmButton = {
                Button(onClick = { confirmUnpair = false; actions.unpair() }) { Text("Disconnect") }
            },
            dismissButton = { TextButton(onClick = { confirmUnpair = false }) { Text("Cancel") } },
        )
    }
}

@Composable
private fun ConnectionCard(state: ConnectionState, pairing: Pairing, host: String, now: Long, onReconnect: () -> Unit) {
    val colors = Bridge.colors
    val (title, color, live) = when (state) {
        is ConnectionState.Connected -> Triple("Connected", colors.success, true)
        is ConnectionState.Connecting -> Triple("Connecting…", MaterialTheme.colorScheme.primary, false)
        is ConnectionState.Retrying -> Triple("Reconnecting", colors.warning, false)
        ConnectionState.WaitingForNetwork -> Triple("Waiting for network", colors.warning, false)
        is ConnectionState.Revoked -> Triple("Removed", colors.danger, false)
        ConnectionState.Stopped -> Triple("Stopped", colors.faint, false)
    }
    SectionCard {
        Row(verticalAlignment = Alignment.CenterVertically) {
            StatusDot(color, live, 12.dp)
            Spacer(Modifier.width(12.dp))
            Text(title, style = MaterialTheme.typography.headlineSmall)
        }
        Spacer(Modifier.height(6.dp))
        Text(
            "${pairing.projectName} · $host",
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
        Spacer(Modifier.height(14.dp))
        HorizontalDivider(color = colors.border)
        Spacer(Modifier.height(14.dp))
        val detail = when (state) {
            is ConnectionState.Connected ->
                "Last check-in ${relative(state.lastAckAt ?: state.since, now)}. Checks in every ${formatInterval(state.intervalSeconds)}."
            is ConnectionState.Retrying -> "${state.reason}. Trying again ${inFuture(state.retryAt, now)}."
            ConnectionState.WaitingForNetwork -> "The phone has no internet connection. Bridge reconnects as soon as it does."
            is ConnectionState.Connecting -> "Opening a secure connection to the server."
            is ConnectionState.Revoked -> state.message
            ConnectionState.Stopped -> "The gateway service is not running."
        }
        Text(detail, style = MaterialTheme.typography.bodyMedium)
        if (state is ConnectionState.Retrying || state == ConnectionState.Stopped) {
            Spacer(Modifier.height(12.dp))
            FilledTonalButton(onClick = onReconnect) { Text("Reconnect now") }
        }
    }
}

@Composable
private fun ChecklistCard(checks: ReliabilityChecks, pairing: Pairing, actions: StatusActions) {
    SectionCard {
        Eyebrow("Reliability")
        Spacer(Modifier.height(4.dp))
        CheckRow(
            ok = checks.smsAllowed,
            title = "Send SMS",
            detail = if (checks.smsAllowed) "Bridge can send messages through this phone's SIM." else "Required. Without it every message to this phone fails.",
            action = "Allow" to actions.allowSms,
        )
        CheckRow(
            ok = checks.phoneStateAllowed,
            warnOnly = true,
            title = "Choose a SIM",
            detail = if (checks.phoneStateAllowed) "Bridge can list SIMs so the dashboard can pick one." else "Optional, for dual-SIM phones. Lets Bridge list SIM slots. Your phone number is never read.",
            action = "Allow" to actions.allowPhoneState,
        )
        if (checks.forwardInbound) {
            CheckRow(
                ok = checks.receiveSmsAllowed,
                title = "Forward incoming SMS",
                detail = if (checks.receiveSmsAllowed) {
                    "SMS this phone receives are sent to your server, as the dashboard asks."
                } else {
                    "Your server asks this phone to forward the SMS it receives. Allow Bridge to receive SMS."
                },
                action = "Allow" to actions.allowReceiveSms,
            )
        }
        CheckRow(
            ok = checks.notificationsAllowed,
            title = "Status notification",
            detail = if (checks.notificationsAllowed) "Android shows the gateway's status while it runs." else "Allow notifications so Android keeps the gateway running visibly.",
            action = "Allow" to actions.allowNotifications,
        )
        CheckRow(
            ok = checks.batteryUnrestricted,
            title = "Background use",
            detail = if (checks.batteryUnrestricted) "Battery optimisation will not stop the gateway." else "Android may pause the gateway to save battery. Allow unrestricted background use.",
            action = "Allow" to actions.allowBackground,
        )
        checks.push?.let { push ->
            CheckRow(
                ok = push.state == PushState.Ready,
                warnOnly = push.state == PushState.Unavailable,
                title = "Wake-ups · ${push.provider}",
                detail = push.detail,
                action = if (push.state == PushState.NeedsSetup) "Turn on" to actions.setupPush else null,
            )
        }
        if (pairing.apiUrl.startsWith("http://")) {
            CheckRow(
                ok = false,
                warnOnly = true,
                title = "Unencrypted server",
                detail = "This server uses plain HTTP. Use HTTPS unless the server is on a network you trust.",
                action = null,
            )
        }
    }
}

@Composable
private fun CheckRow(ok: Boolean, title: String, detail: String, action: Pair<String, () -> Unit>?, warnOnly: Boolean = false) {
    val colors = Bridge.colors
    Row(Modifier.fillMaxWidth().padding(vertical = 10.dp), verticalAlignment = Alignment.Top) {
        StatusDot(if (ok) colors.success else if (warnOnly) colors.warning else colors.danger, live = false, size = 8.dp)
        Spacer(Modifier.width(12.dp))
        Column(Modifier.weight(1f)) {
            Text(title, style = MaterialTheme.typography.titleSmall)
            Text(detail, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        if (!ok && action != null) {
            Spacer(Modifier.width(8.dp))
            FilledTonalButton(onClick = action.second) { Text(action.first) }
        }
    }
}

@Composable
private fun InfoRow(label: String, value: String, mono: Boolean = false) {
    Row(Modifier.fillMaxWidth().padding(vertical = 6.dp), verticalAlignment = Alignment.CenterVertically) {
        Text(label, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.width(88.dp))
        Text(
            value,
            style = if (mono) MaterialTheme.typography.bodySmall.copy(fontFamily = RedHatMono) else MaterialTheme.typography.bodyMedium,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
            color = if (mono) MaterialTheme.colorScheme.onSurface else Color.Unspecified,
        )
    }
}

private fun formatInterval(seconds: Int) = if (seconds < 60) "$seconds s" else "${seconds / 60} min"
