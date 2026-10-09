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
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Button
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import dev.bridge.gateway.diagnostics.GatewayEvent
import dev.bridge.gateway.ui.theme.Bridge
import dev.bridge.gateway.ui.theme.RedHatMono
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter

private val rowTime = DateTimeFormatter.ofPattern("d MMM HH:mm:ss")

/** The latest few events, on the status screen. */
@Composable
fun ConnectionLogCard(events: List<GatewayEvent>, onOpen: () -> Unit) {
    SectionCard {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Eyebrow("Connection log", Modifier.weight(1f))
            TextButton(onClick = onOpen) { Text("Show all") }
        }
        val recent = remember(events) { events.takeLast(4).asReversed() }
        if (recent.isEmpty()) {
            Text("Nothing logged yet.", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        recent.forEach { EventRow(it) }
    }
}

/** Every stored event, newest first, with a button that copies them for a bug report. */
@Composable
fun ConnectionLogScreen(events: List<GatewayEvent>, onCopy: () -> Unit, onBack: () -> Unit) {
    val newestFirst = remember(events) { events.asReversed() }
    Column(
        Modifier.fillMaxSize().safeDrawingPadding().padding(horizontal = 20.dp, vertical = 16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f)) {
                Text("Connection log", style = MaterialTheme.typography.titleLarge)
                Text(
                    "What the gateway did and why, kept on this phone only. Copy it into a bug report if Bridge disconnects.",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Button(onClick = onCopy, enabled = events.isNotEmpty()) { Text("Copy") }
            TextButton(onClick = onBack) { Text("Close") }
        }
        LazyColumn(Modifier.fillMaxWidth().weight(1f)) {
            items(newestFirst) { EventRow(it) }
        }
    }
}

@Composable
private fun EventRow(e: GatewayEvent) {
    Column(Modifier.fillMaxWidth().padding(vertical = 5.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                Instant.ofEpochMilli(e.at).atZone(ZoneId.systemDefault()).format(rowTime),
                style = MaterialTheme.typography.bodySmall.copy(fontFamily = RedHatMono),
                color = Bridge.colors.faint,
            )
            Spacer(Modifier.width(8.dp))
            Text(e.kind, style = MaterialTheme.typography.labelSmall, color = Bridge.colors.faint)
        }
        Spacer(Modifier.height(1.dp))
        Text(
            e.message,
            style = MaterialTheme.typography.bodySmall,
            color = if (e.warn) Bridge.colors.warning else MaterialTheme.colorScheme.onSurface,
        )
    }
}
