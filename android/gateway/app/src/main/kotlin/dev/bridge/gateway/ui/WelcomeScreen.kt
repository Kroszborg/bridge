package dev.bridge.gateway.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import dev.bridge.gateway.pairing.PairingRequest
import dev.bridge.gateway.pairing.PairingUri
import dev.bridge.gateway.ui.theme.Bridge
import dev.bridge.gateway.ui.theme.RedHatMono

@Composable
fun WelcomeScreen(
    state: UiState,
    onScan: () -> Unit,
    onManual: (PairingRequest) -> Unit,
    onManualError: (String) -> Unit,
    onDismissReason: () -> Unit,
) {
    var manualOpen by rememberSaveable { mutableStateOf(false) }

    Column(
        Modifier
            .fillMaxSize()
            .safeDrawingPadding()
            .verticalScroll(rememberScrollState())
            .padding(horizontal = 24.dp, vertical = 32.dp),
        verticalArrangement = Arrangement.spacedBy(20.dp),
    ) {
        BridgeTile(56.dp)
        Spacer(Modifier.height(8.dp))
        Text("Turn this phone into an SMS gateway", style = MaterialTheme.typography.headlineMedium)
        Text(
            "Pair it with your Bridge server. Messages your applications send are delivered through this phone's SIM, " +
                "and every status change is reported back.",
            style = MaterialTheme.typography.bodyLarge,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )

        state.unpairedReason?.let { reason ->
            SectionCard {
                Text("Disconnected", style = MaterialTheme.typography.titleSmall, color = Bridge.colors.warning)
                Spacer(Modifier.height(4.dp))
                Text(reason, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                TextButton(onClick = onDismissReason, contentPadding = androidx.compose.foundation.layout.PaddingValues(0.dp)) {
                    Text("Dismiss")
                }
            }
        }

        SectionCard {
            Eyebrow("How to pair")
            Spacer(Modifier.height(10.dp))
            listOf(
                "Open the Bridge dashboard and go to Devices.",
                "Click Pair device to show a pairing code.",
                "Scan it here. The code works once and expires after 10 minutes.",
            ).forEachIndexed { i, step ->
                Text("${i + 1}.  $step", style = MaterialTheme.typography.bodyMedium, modifier = Modifier.padding(vertical = 3.dp))
            }
        }

        state.error?.let {
            Text(it, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.error)
        }

        if (state.busy) {
            Column(horizontalAlignment = androidx.compose.ui.Alignment.CenterHorizontally, modifier = Modifier.fillMaxWidth()) {
                CircularProgressIndicator()
                Spacer(Modifier.height(8.dp))
                Text("Pairing…", style = MaterialTheme.typography.bodyMedium)
            }
        } else {
            Button(onClick = onScan, modifier = Modifier.fillMaxWidth().height(52.dp)) { Text("Scan pairing code") }
            OutlinedButton(onClick = { manualOpen = true }, modifier = Modifier.fillMaxWidth().height(52.dp)) {
                Text("Enter code manually")
            }
        }
    }

    if (manualOpen) {
        ManualPairDialog(
            onDismiss = { manualOpen = false },
            onSubmit = { server, code ->
                PairingUri.fromManual(server, code)
                    .onSuccess { manualOpen = false; onManual(it) }
                    .onFailure { onManualError(it.message ?: "Check the server address and code.") }
            },
        )
    }
}

@Composable
private fun ManualPairDialog(onDismiss: () -> Unit, onSubmit: (String, String) -> Unit) {
    var server by rememberSaveable { mutableStateOf("") }
    var code by rememberSaveable { mutableStateOf("") }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Enter pairing details") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                Text(
                    "Both values are shown under \"Cannot scan?\" in the dashboard's pairing dialog.",
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                OutlinedTextField(
                    value = server, onValueChange = { server = it.trim() }, singleLine = true,
                    label = { Text("Server URL") }, placeholder = { Text("https://bridge.example.com") },
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri),
                )
                OutlinedTextField(
                    value = code, onValueChange = { code = it.trim() }, singleLine = true,
                    label = { Text("Pairing code") }, placeholder = { Text("bp_…") },
                    textStyle = MaterialTheme.typography.bodyMedium.copy(fontFamily = RedHatMono),
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Ascii),
                )
            }
        },
        confirmButton = { Button(onClick = { onSubmit(server, code) }, enabled = server.isNotBlank() && code.isNotBlank()) { Text("Continue") } },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Cancel") } },
    )
}

/** Confirms the server before trusting it; pairing links can come from anywhere. */
@Composable
fun ConfirmPairingDialog(request: PairingRequest, onConfirm: () -> Unit, onCancel: () -> Unit) {
    AlertDialog(
        onDismissRequest = onCancel,
        title = { Text("Connect to ${request.host}?") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Text(
                    "This phone will send the SMS that ${request.host} asks it to, using your SIM and your mobile plan. " +
                        "Only continue if you run this server or trust whoever does.",
                    style = MaterialTheme.typography.bodyMedium,
                )
                if (request.isCleartext) {
                    Text(
                        "This server uses plain HTTP. Anyone on the network path can read the phone's credential and messages. " +
                            "Use HTTPS outside a trusted local network.",
                        style = MaterialTheme.typography.bodyMedium,
                        color = Bridge.colors.warning,
                    )
                }
            }
        },
        confirmButton = { Button(onClick = onConfirm) { Text("Connect") } },
        dismissButton = { TextButton(onClick = onCancel) { Text("Cancel") } },
    )
}
