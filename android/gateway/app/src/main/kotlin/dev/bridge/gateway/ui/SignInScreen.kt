package dev.bridge.gateway.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.bridge.gateway.account.ServerUrls
import dev.bridge.gateway.pairing.PairingUri
import dev.bridge.gateway.ui.theme.Bridge
import dev.bridge.gateway.ui.theme.RedHatMono

/** Email and password sign-in, for the hosted service or a self-hosted server. */
@Composable
fun SignInScreen(vm: AccountViewModel, onBack: () -> Unit) {
    val state by vm.signIn.collectAsStateWithLifecycle()
    val reason by vm.signedOutReason.collectAsStateWithLifecycle()
    var server by rememberSaveable { mutableStateOf(vm.lastServer) }
    var email by rememberSaveable { mutableStateOf(vm.lastEmail) }
    var password by rememberSaveable { mutableStateOf("") }
    var advanced by rememberSaveable { mutableStateOf(false) }
    var dashboard by rememberSaveable { mutableStateOf("") }
    val submit = { if (!state.busy && email.isNotBlank() && password.isNotEmpty()) vm.signIn(server, email, password, dashboard) }

    Column(
        Modifier
            .fillMaxSize()
            .safeDrawingPadding()
            .verticalScroll(rememberScrollState())
            .padding(horizontal = 24.dp, vertical = 16.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        TextButton(onClick = onBack, contentPadding = PaddingValues(0.dp)) { Text("Back") }
        BridgeTile(48.dp)
        Text("Sign in to Bridge", style = MaterialTheme.typography.headlineMedium)
        Text(
            "See your messages, send one, and manage your phones. The gateway keeps working whether or not you are signed in.",
            style = MaterialTheme.typography.bodyLarge,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )

        reason?.let { Notice(it, color = Bridge.colors.warning) }

        OutlinedTextField(
            value = server, onValueChange = { server = it.trim() }, singleLine = true, modifier = Modifier.fillMaxWidth(),
            label = { Text("Server") },
            supportingText = { Text("Hosted Bridge, or your own server's API address.") },
            textStyle = MaterialTheme.typography.bodyMedium.copy(fontFamily = RedHatMono),
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri, imeAction = ImeAction.Next),
        )
        if (PairingUri.normalizeServerUrl(server)?.startsWith("http://") == true) {
            Notice(
                "This server uses plain HTTP: your password and session travel unencrypted. Use HTTPS unless the server is on a network you trust.",
                color = Bridge.colors.warning,
            )
        }
        OutlinedTextField(
            value = email, onValueChange = { email = it.trim() }, singleLine = true, modifier = Modifier.fillMaxWidth(),
            label = { Text("Email") },
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Email, imeAction = ImeAction.Next),
        )
        OutlinedTextField(
            value = password, onValueChange = { password = it }, singleLine = true, modifier = Modifier.fillMaxWidth(),
            label = { Text("Password") },
            visualTransformation = PasswordVisualTransformation(),
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password, imeAction = ImeAction.Done),
            keyboardActions = KeyboardActions(onDone = { submit() }),
        )

        if (advanced) {
            OutlinedTextField(
                value = dashboard, onValueChange = { dashboard = it.trim() }, singleLine = true, modifier = Modifier.fillMaxWidth(),
                label = { Text("Dashboard address") },
                placeholder = { Text(ServerUrls.dashboardFor(server)) },
                supportingText = { Text("Where plans are managed. Leave empty to use the address shown.") },
                textStyle = MaterialTheme.typography.bodyMedium.copy(fontFamily = RedHatMono),
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri),
            )
        } else {
            TextButton(onClick = { advanced = true }, contentPadding = PaddingValues(0.dp)) { Text("Self-hosting? Set the dashboard address") }
        }

        state.error?.let { Notice(it) }

        if (state.busy) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp)
                Spacer(Modifier.width(12.dp))
                Text("Signing in…", style = MaterialTheme.typography.bodyMedium)
            }
        } else {
            Button(
                onClick = submit,
                enabled = server.isNotBlank() && email.isNotBlank() && password.isNotEmpty(),
                modifier = Modifier.fillMaxWidth().height(52.dp),
            ) { Text("Sign in") }
        }

        Text(
            "Your password goes only to this server. The app keeps the session it returns, encrypted on this phone, never the password.",
            style = MaterialTheme.typography.bodySmall,
            color = Bridge.colors.faint,
        )
    }
}
