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
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.Checkbox
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.bridge.gateway.BuildConfig
import dev.bridge.gateway.account.Account
import dev.bridge.gateway.account.PlanSummary
import dev.bridge.gateway.account.ServerUrls
import dev.bridge.gateway.data.Pairing
import dev.bridge.gateway.ui.theme.Bridge
import dev.bridge.gateway.ui.theme.RedHatMono

/** Who is signed in, the plan, the server, about, and sign-out. */
@Composable
fun AccountScreen(vm: AccountViewModel, account: Account, pairing: Pairing?, onSwitchProject: () -> Unit) {
    val plan by vm.plan.collectAsStateWithLifecycle()
    val uri = LocalUriHandler.current
    var confirmSignOut by remember { mutableStateOf(false) }
    var editDashboard by remember { mutableStateOf(false) }
    val org = account.organization
    LaunchedEffect(account.project?.organizationId) { vm.loadPlan() }

    Column(
        Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(horizontal = 20.dp, vertical = 16.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        ScreenHeader("Account", account.user.email)

        SectionCard {
            Eyebrow("Project")
            Spacer(Modifier.height(8.dp))
            Text(account.project?.name ?: "None chosen", style = MaterialTheme.typography.titleMedium)
            org?.let {
                Text("${it.name} · ${it.role}", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            Spacer(Modifier.height(12.dp))
            FilledTonalButton(onClick = onSwitchProject) { Text("Switch project") }
        }

        if (!plan.hidden && org != null) {
            PlanCard(plan, onManage = { uri.openSafely(ServerUrls.billing(account.dashboardUrl, org.id)) }, onRetry = vm::loadPlan)
        }

        SectionCard {
            Eyebrow("Server")
            Spacer(Modifier.height(8.dp))
            LabeledValue("API", account.apiUrl)
            LabeledValue("Dashboard", account.dashboardUrl)
            Row {
                TextButton(onClick = { uri.openSafely(account.dashboardUrl) }, contentPadding = PaddingValues(0.dp)) { Text("Open dashboard") }
                Spacer(Modifier.width(16.dp))
                TextButton(onClick = { editDashboard = true }, contentPadding = PaddingValues(0.dp)) { Text("Change address") }
            }
        }

        AboutCard(onOpen = uri::openSafely)

        OutlinedButton(onClick = { confirmSignOut = true }, modifier = Modifier.fillMaxWidth()) { Text("Sign out") }
    }

    if (confirmSignOut) {
        SignOutDialog(
            paired = pairing != null,
            onDismiss = { confirmSignOut = false },
            onConfirm = { disconnect -> confirmSignOut = false; vm.signOut(disconnect) },
        )
    }
    if (editDashboard) {
        DashboardDialog(account.dashboardUrl, onDismiss = { editDashboard = false }) { vm.setDashboardUrl(it).also { ok -> if (ok) editDashboard = false } }
    }
}

@Composable
private fun PlanCard(plan: PlanState, onManage: () -> Unit, onRetry: () -> Unit) {
    SectionCard {
        Eyebrow("Plan & usage")
        Spacer(Modifier.height(8.dp))
        val summary = plan.summary
        when {
            summary != null -> PlanDetails(summary, onManage)
            plan.error != null -> Notice(plan.error, action = "Retry" to onRetry)
            else -> Text("Loading…", style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}

@Composable
private fun PlanDetails(summary: PlanSummary, onManage: () -> Unit) {
    Row(verticalAlignment = Alignment.CenterVertically) {
        Text(summary.title, style = MaterialTheme.typography.titleMedium, modifier = Modifier.weight(1f))
        summary.price?.let { Text(it, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant) }
    }
    summary.status?.let {
        Text("Status: ${it.replace('_', ' ')}", style = MaterialTheme.typography.bodySmall, color = Bridge.colors.warning)
    }
    summary.lines.forEach { line ->
        Spacer(Modifier.height(12.dp))
        Row {
            Text(line.label, style = MaterialTheme.typography.bodySmall, modifier = Modifier.weight(1f))
            Text(
                line.text,
                style = MaterialTheme.typography.bodySmall,
                color = if (line.atLimit) Bridge.colors.danger else MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        line.fraction?.let { f ->
            Spacer(Modifier.height(4.dp))
            LinearProgressIndicator(
                progress = { f },
                modifier = Modifier.fillMaxWidth(),
                color = if (line.atLimit) Bridge.colors.danger else MaterialTheme.colorScheme.primary,
                trackColor = Bridge.colors.raised,
            )
        }
    }
    if (!summary.selfHosted) {
        Spacer(Modifier.height(14.dp))
        FilledTonalButton(onClick = onManage) { Text(if (summary.anyAtLimit) "Upgrade" else "Manage plan") }
    }
}

@Composable
private fun LabeledValue(label: String, value: String) {
    Column(Modifier.padding(vertical = 4.dp)) {
        Text(label, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        Text(value, style = MaterialTheme.typography.bodySmall.copy(fontFamily = RedHatMono), maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}

@Composable
private fun SignOutDialog(paired: Boolean, onDismiss: () -> Unit, onConfirm: (Boolean) -> Unit) {
    var disconnect by rememberSaveable { mutableStateOf(false) }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Sign out?") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(
                    if (paired) "This phone keeps working as a gateway after you sign out." else "You can sign in again at any time.",
                    style = MaterialTheme.typography.bodyMedium,
                )
                if (paired) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Checkbox(checked = disconnect, onCheckedChange = { disconnect = it })
                        Text("Also disconnect this phone", style = MaterialTheme.typography.bodyMedium)
                    }
                }
            }
        },
        confirmButton = { Button(onClick = { onConfirm(disconnect) }) { Text("Sign out") } },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Cancel") } },
    )
}

@Composable
private fun DashboardDialog(current: String, onDismiss: () -> Unit, onSave: (String) -> Unit) {
    var value by rememberSaveable { mutableStateOf(current) }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Dashboard address") },
        text = {
            OutlinedTextField(
                value = value, onValueChange = { value = it.trim() }, singleLine = true,
                label = { Text("Address") }, keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri),
                supportingText = { Text("Used to open billing and the dashboard.") },
            )
        },
        confirmButton = { Button(onClick = { onSave(value) }, enabled = value.isNotBlank()) { Text("Save") } },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Cancel") } },
    )
}

/** Version, licence, privacy policy and source code. */
@Composable
fun AboutCard(onOpen: (String) -> Unit) {
    SectionCard {
        Eyebrow("About")
        Spacer(Modifier.height(8.dp))
        AboutBody(onOpen)
    }
}

@Composable
private fun AboutBody(onOpen: (String) -> Unit) {
    Text("Bridge ${BuildConfig.VERSION_NAME} · ${BuildConfig.FLAVOR}", style = MaterialTheme.typography.titleSmall)
    Text(
        "Open source under the AGPL-3.0. The app talks only to the server you pair it with or sign in to; it has no analytics or ads.",
        style = MaterialTheme.typography.bodySmall,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
    )
    Row {
        TextButton(onClick = { onOpen(ServerUrls.PRIVACY) }, contentPadding = PaddingValues(0.dp)) { Text("Privacy policy") }
        Spacer(Modifier.width(16.dp))
        TextButton(onClick = { onOpen(ServerUrls.SOURCE) }, contentPadding = PaddingValues(0.dp)) { Text("Source code") }
    }
}

/** About and privacy, reachable without signing in. */
@Composable
fun AboutDialog(onDismiss: () -> Unit) {
    val uri = LocalUriHandler.current
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("About Bridge") },
        text = { Column { AboutBody(uri::openSafely) } },
        confirmButton = { TextButton(onClick = onDismiss) { Text("Close") } },
    )
}
