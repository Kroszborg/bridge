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
import dev.bridge.gateway.pairing.PairingConfirm
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
    onAbout: () -> Unit,
    /** Signed out: offers sign-in. */
    onSignIn: (() -> Unit)? = null,
    /** Signed in with a project: pairs with it without a QR code. */
    onPairWithAccount: (() -> Unit)? = null,
    /** Opens the plan page when pairing hit a plan limit. */
    onUpgrade: (() -> Unit)? = null,
    onOpenLog: (() -> Unit)? = null,
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

        val project = state.account?.project
        if (onPairWithAccount != null && project != null) {
            SectionCard {
                Eyebrow("Signed in")
                Spacer(Modifier.height(10.dp))
                Text(
                    "Pair this phone with ${project.name} directly, without a QR code. Needs an admin or owner role.",
                    style = MaterialTheme.typography.bodyMedium,
                )
            }
        } else {
            SectionCard {
                Eyebrow("How to pair")
                Spacer(Modifier.height(10.dp))
                listOf(
                    "Open the Bridge dashboard and go to Phones.",
                    "Click Pair device to show a pairing code.",
                    "Scan it here. The code works once and expires after 10 minutes.",
                ).forEachIndexed { i, step ->
                    Text("${i + 1}.  $step", style = MaterialTheme.typography.bodyMedium, modifier = Modifier.padding(vertical = 3.dp))
                }
            }
        }

        state.error?.let {
            Notice(it, action = if (state.planLimit && onUpgrade != null) "Upgrade" to onUpgrade else null)
        }

        if (state.busy) {
            Column(horizontalAlignment = androidx.compose.ui.Alignment.CenterHorizontally, modifier = Modifier.fillMaxWidth()) {
                CircularProgressIndicator()
                Spacer(Modifier.height(8.dp))
                Text("Pairing…", style = MaterialTheme.typography.bodyMedium)
            }
        } else {
            if (onPairWithAccount != null && project != null) {
                Button(onClick = onPairWithAccount, modifier = Modifier.fillMaxWidth().height(52.dp)) { Text("Pair with ${project.name}") }
                OutlinedButton(onClick = onScan, modifier = Modifier.fillMaxWidth().height(52.dp)) { Text("Scan pairing code") }
            } else {
                Button(onClick = onScan, modifier = Modifier.fillMaxWidth().height(52.dp)) { Text("Scan pairing code") }
            }
            OutlinedButton(onClick = { manualOpen = true }, modifier = Modifier.fillMaxWidth().height(52.dp)) {
                Text("Enter code manually")
            }
            if (onSignIn != null) {
                OutlinedButton(onClick = onSignIn, modifier = Modifier.fillMaxWidth().height(52.dp)) { Text("Sign in with your account") }
            }
        }

        Row(horizontalArrangement = Arrangement.spacedBy(20.dp)) {
            TextButton(onClick = onAbout, contentPadding = androidx.compose.foundation.layout.PaddingValues(0.dp)) { Text("About & privacy") }
            if (onOpenLog != null) {
                TextButton(onClick = onOpenLog, contentPadding = androidx.compose.foundation.layout.PaddingValues(0.dp)) { Text("Connection log") }
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
fun ConfirmPairingDialog(confirm: PairingConfirm, onConfirm: () -> Unit, onCancel: () -> Unit) {
    val request = confirm.request
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
                confirm.replacing?.let { current ->
                    Text(
                        "This phone is still paired with $current. Connecting replaces that pairing once the server accepts the code.",
                        style = MaterialTheme.typography.bodyMedium,
                        color = Bridge.colors.warning,
                    )
                }
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

/** After picking a project on an unpaired phone: offer to make this phone one of its gateways. */
@Composable
fun OfferPairingDialog(projectName: String, onPair: () -> Unit, onLater: () -> Unit) {
    AlertDialog(
        onDismissRequest = onLater,
        title = { Text("Use this phone for $projectName?") },
        text = {
            Text(
                "It will send the SMS that $projectName asks it to, using this phone's SIM and mobile plan. " +
                    "You can disconnect it at any time.",
                style = MaterialTheme.typography.bodyMedium,
            )
        },
        confirmButton = { Button(onClick = onPair) { Text("Pair this phone") } },
        dismissButton = { TextButton(onClick = onLater) { Text("Not now") } },
    )
}

@Composable
fun DisconnectDialog(projectName: String, onConfirm: () -> Unit, onCancel: () -> Unit) {
    AlertDialog(
        onDismissRequest = onCancel,
        title = { Text("Disconnect this phone?") },
        text = { Text("It stops sending messages for $projectName and is removed from the dashboard. You can pair it again later.") },
        confirmButton = { Button(onClick = onConfirm) { Text("Disconnect") } },
        dismissButton = { TextButton(onClick = onCancel) { Text("Cancel") } },
    )
}
