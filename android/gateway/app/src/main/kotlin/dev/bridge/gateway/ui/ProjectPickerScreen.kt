package dev.bridge.gateway.ui

import androidx.compose.foundation.clickable
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
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.bridge.gateway.net.ProjectDto
import dev.bridge.gateway.ui.theme.Bridge

/** Lists the user's organizations and projects; picking one opens it. */
@Composable
fun ProjectPickerScreen(
    vm: AccountViewModel,
    currentProjectId: String?,
    onPicked: (ProjectDto) -> Unit,
    onBack: (() -> Unit)?,
    onSignOut: () -> Unit,
) {
    val state by vm.picker.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { vm.loadProjects() }

    LazyColumn(
        Modifier.fillMaxSize().safeDrawingPadding(),
        contentPadding = PaddingValues(horizontal = 20.dp, vertical = 16.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        item {
            Row(verticalAlignment = Alignment.CenterVertically) {
                if (onBack != null) TextButton(onClick = onBack, contentPadding = PaddingValues(0.dp)) { Text("Back") }
                Spacer(Modifier.weight(1f))
                TextButton(onClick = onSignOut) { Text("Sign out") }
            }
            Spacer(Modifier.height(8.dp))
            ScreenHeader("Choose a project", "Messages, sending and phones work within one project.")
        }
        state.error?.let { item { Notice(it, action = "Retry" to vm::loadProjects) } }
        if (state.loading && state.organizations.isEmpty()) {
            item {
                Column(Modifier.fillMaxWidth().padding(32.dp), horizontalAlignment = Alignment.CenterHorizontally) {
                    CircularProgressIndicator()
                }
            }
        }
        items(state.organizations, key = { it.organization.id }) { group ->
            SectionCard {
                Eyebrow("${group.organization.name} · ${group.organization.role}")
                Spacer(Modifier.height(8.dp))
                if (group.projects.isEmpty()) {
                    Text(
                        "No projects yet. Create one in the dashboard.",
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                group.projects.forEachIndexed { i, project ->
                    if (i > 0) HorizontalDivider(color = Bridge.colors.border)
                    Row(
                        Modifier
                            .fillMaxWidth()
                            .clickable { onPicked(project) }
                            .padding(vertical = 14.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Text(project.name, style = MaterialTheme.typography.titleSmall, modifier = Modifier.weight(1f))
                        if (project.id == currentProjectId) {
                            Text("Current", style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.primary)
                        }
                    }
                }
            }
        }
        if (!state.loading && state.error == null && state.organizations.isEmpty()) {
            item {
                Text(
                    "This account is not a member of any organization.",
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                Spacer(Modifier.height(12.dp))
                OutlinedButton(onClick = vm::loadProjects) { Text("Refresh") }
            }
        }
    }
}
