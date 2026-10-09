package dev.bridge.gateway.ui

import android.content.ActivityNotFoundException
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.UriHandler
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import dev.bridge.gateway.ui.theme.Bridge
import java.text.DateFormat
import java.time.OffsetDateTime
import java.util.Date

/** Title row at the top of each signed-in tab. */
@Composable
fun ScreenHeader(title: String, subtitle: String?, modifier: Modifier = Modifier, trailing: @Composable () -> Unit = {}) {
    Row(modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f)) {
            Text(title, style = MaterialTheme.typography.headlineSmall)
            if (subtitle != null) {
                Text(
                    subtitle,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
        trailing()
    }
}

/** A message status as a small coloured chip. */
@Composable
fun StatusChip(status: String) {
    val colors = Bridge.colors
    val color = when (status) {
        "delivered" -> colors.success
        "sent", "received" -> MaterialTheme.colorScheme.primary
        "sending" -> colors.warning
        "failed" -> colors.danger
        else -> colors.faint // created, queued
    }
    Text(
        status.replaceFirstChar { it.uppercase() },
        style = MaterialTheme.typography.labelMedium,
        color = color,
        modifier = Modifier
            .clip(RoundedCornerShape(50))
            .background(color.copy(alpha = 0.14f))
            .padding(horizontal = 10.dp, vertical = 3.dp),
    )
}

/** Live or test, as the dashboard's environment switch. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun EnvironmentToggle(environment: String, onChange: (String) -> Unit, modifier: Modifier = Modifier) {
    val options = listOf("live" to "Live", "test" to "Test")
    SingleChoiceSegmentedButtonRow(modifier) {
        options.forEachIndexed { i, (value, label) ->
            SegmentedButton(
                selected = environment == value,
                onClick = { onChange(value) },
                shape = SegmentedButtonDefaults.itemShape(i, options.size),
            ) { Text(label) }
        }
    }
}

/** A line of inline feedback, in the error colour unless [color] says otherwise. */
@Composable
fun Notice(text: String, color: Color = MaterialTheme.colorScheme.error, action: Pair<String, () -> Unit>? = null) {
    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        Text(text, style = MaterialTheme.typography.bodyMedium, color = color, modifier = Modifier.weight(1f))
        if (action != null) {
            Spacer(Modifier.width(4.dp))
            FilledTonalButton(onClick = action.second) { Text(action.first) }
        }
    }
}

/** Opens a web page in the browser; does nothing if the phone has none. */
fun UriHandler.openSafely(url: String) {
    try {
        openUri(url)
    } catch (_: ActivityNotFoundException) {
    } catch (_: IllegalArgumentException) {
    }
}

/** Milliseconds since the epoch for an API timestamp (RFC 3339), or null. */
fun parseTimestamp(value: String?): Long? =
    value?.let { runCatching { OffsetDateTime.parse(it).toInstant().toEpochMilli() }.getOrNull() }

/** "3 min ago" within a day, the date and time after that. */
fun shortTime(value: String?, nowMs: Long): String {
    val ms = parseTimestamp(value) ?: return ""
    return if (nowMs - ms < 24 * 3600_000L) relative(ms, nowMs) else DateFormat.getDateTimeInstance(DateFormat.MEDIUM, DateFormat.SHORT).format(Date(ms))
}
