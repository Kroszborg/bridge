package dev.bridge.gateway.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.Checkbox
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.bridge.gateway.account.Account
import dev.bridge.gateway.data.Pairing
import dev.bridge.gateway.ui.theme.Bridge
import dev.bridge.gateway.ui.theme.RedHatMono

/** Sends one message, like the dashboard's Playground, and follows its status. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SendScreen(vm: SendViewModel, account: Account, pairing: Pairing?, onUpgrade: () -> Unit) {
    val state by vm.state.collectAsStateWithLifecycle()
    val project = account.project ?: return
    val now = rememberNow(5_000)
    var to by rememberSaveable { mutableStateOf("") }
    var body by rememberSaveable { mutableStateOf("") }
    var environment by rememberSaveable { mutableStateOf("test") }
    var sim by rememberSaveable { mutableIntStateOf(0) }
    var viaThisPhone by rememberSaveable { mutableStateOf(false) }
    val thisPhoneInProject = pairing?.projectId == project.id
    val liveBlocked = environment == "live" && !account.isAdmin

    Column(
        Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(horizontal = 20.dp, vertical = 16.dp),
        verticalArrangement = Arrangement.spacedBy(14.dp),
    ) {
        ScreenHeader("Send", project.name) { EnvironmentToggle(environment, { environment = it }) }

        Text(
            if (environment == "test") {
                "Test messages never reach a phone. Bridge simulates every status so you can see the flow."
            } else {
                "Live messages are sent through one of the project's phones, using its SIM and plan."
            },
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )

        OutlinedTextField(
            value = to, onValueChange = { to = it.trim() }, singleLine = true, modifier = Modifier.fillMaxWidth(),
            label = { Text("To") }, placeholder = { Text("+919876543210") },
            supportingText = { Text("International format, with the country code.") },
            textStyle = MaterialTheme.typography.bodyLarge.copy(fontFamily = RedHatMono),
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Phone),
        )
        OutlinedTextField(
            value = body, onValueChange = { if (it.length <= MAX_BODY) body = it }, modifier = Modifier.fillMaxWidth(),
            label = { Text("Message") }, minLines = 3,
            supportingText = { Text("${body.length} / $MAX_BODY") },
        )

        if (environment == "live") {
            Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                Eyebrow("SIM")
                SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth()) {
                    listOf("Phone's choice", "SIM 1", "SIM 2").forEachIndexed { i, label ->
                        SegmentedButton(selected = sim == i, onClick = { sim = i }, shape = SegmentedButtonDefaults.itemShape(i, 3)) { Text(label) }
                    }
                }
            }
            if (thisPhoneInProject) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Checkbox(checked = viaThisPhone, onCheckedChange = { viaThisPhone = it })
                    Text("Send through this phone", style = MaterialTheme.typography.bodyMedium)
                }
            }
            if (liveBlocked) {
                Notice("Only organization admins and owners can send live messages. Use Test, or ask an admin.", color = Bridge.colors.warning)
            }
        }

        state.error?.let { Notice(it, action = if (state.planLimit) "Upgrade" to onUpgrade else null) }

        if (state.sending) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp)
                Spacer(Modifier.width(12.dp))
                Text("Sending…", style = MaterialTheme.typography.bodyMedium)
            }
        } else {
            Button(
                onClick = {
                    val live = environment == "live"
                    vm.send(
                        to = to,
                        body = body,
                        environment = environment,
                        simSlot = sim.takeIf { live && it > 0 },
                        deviceId = pairing?.deviceId?.takeIf { live && viaThisPhone && thisPhoneInProject },
                    )
                },
                enabled = to.length >= 3 && body.isNotBlank() && !liveBlocked,
                modifier = Modifier.fillMaxWidth().height(52.dp),
            ) { Text(if (environment == "live") "Send live message" else "Send test message") }
        }

        state.sent?.let { sent ->
            Spacer(Modifier.height(4.dp))
            Eyebrow("Last message")
            MessageRow(sent, now)
            if (!sent.isFinal) {
                Text("Following its status…", style = MaterialTheme.typography.bodySmall, color = Bridge.colors.faint)
            }
            OutlinedButton(onClick = { vm.dismiss(); body = "" }) { Text("Write another") }
        }
    }
}

private const val MAX_BODY = 1600
