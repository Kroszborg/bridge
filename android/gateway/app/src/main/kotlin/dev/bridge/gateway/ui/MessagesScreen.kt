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
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontStyle
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.bridge.gateway.account.Account
import dev.bridge.gateway.net.MessageDto
import dev.bridge.gateway.ui.theme.Bridge
import dev.bridge.gateway.ui.theme.RedHatMono

/** Recent messages of the selected project, newest first. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun MessagesScreen(vm: MessagesViewModel, account: Account) {
    val state by vm.state.collectAsStateWithLifecycle()
    val project = account.project ?: return
    val now = rememberNow(30_000)
    LaunchedEffect(project.id) { vm.refresh() }

    PullToRefreshBox(isRefreshing = state.refreshing, onRefresh = vm::refresh, modifier = Modifier.fillMaxSize()) {
        LazyColumn(
            Modifier.fillMaxSize(),
            contentPadding = PaddingValues(horizontal = 20.dp, vertical = 16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            item {
                ScreenHeader("Messages", project.name) {
                    EnvironmentToggle(state.environment, vm::setEnvironment)
                }
            }
            state.error?.let { item { Notice(it, action = "Retry" to vm::refresh) } }
            if (!state.refreshing && state.error == null && state.messages.isEmpty() && state.projectId == project.id) {
                item {
                    Text(
                        if (state.environment == "test") "No test messages yet. Send one from the Send tab." else "No messages yet.",
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.padding(vertical = 24.dp),
                    )
                }
            }
            items(state.messages, key = { it.id }) { MessageRow(it, now) }
            if (state.hasMore) {
                item {
                    Column(Modifier.fillMaxWidth(), horizontalAlignment = Alignment.CenterHorizontally) {
                        OutlinedButton(onClick = vm::loadMore, enabled = !state.loadingMore) {
                            Text(if (state.loadingMore) "Loading…" else "Load older messages")
                        }
                    }
                }
            }
        }
    }
}

@Composable
fun MessageRow(m: MessageDto, now: Long) {
    SectionCard {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                (if (m.isInbound) "From " else "To ") + m.counterpart.ifBlank { "unknown" },
                style = MaterialTheme.typography.titleSmall.copy(fontFamily = RedHatMono),
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            Spacer(Modifier.width(8.dp))
            StatusChip(m.status)
        }
        Spacer(Modifier.height(6.dp))
        Text(
            m.body ?: "Message text removed after the retention period.",
            style = if (m.body == null) MaterialTheme.typography.bodyMedium.copy(fontStyle = FontStyle.Italic) else MaterialTheme.typography.bodyMedium,
            color = if (m.body == null) Bridge.colors.faint else MaterialTheme.colorScheme.onSurface,
            maxLines = 3,
            overflow = TextOverflow.Ellipsis,
        )
        if (m.status == "failed") {
            Spacer(Modifier.height(6.dp))
            Text(
                m.errorMessage ?: m.errorCode ?: "The message could not be sent.",
                style = MaterialTheme.typography.bodySmall,
                color = Bridge.colors.danger,
            )
        }
        Spacer(Modifier.height(6.dp))
        val detail = listOfNotNull(
            shortTime(m.createdAt, now),
            m.environment.takeIf { it == "test" }?.let { "test" },
            m.purpose.takeIf { it == "otp" }?.let { "verification code" },
            m.segments?.takeIf { it > 1 }?.let { "$it parts" },
        ).filter { it.isNotBlank() }.joinToString(" · ")
        Text(detail, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}
