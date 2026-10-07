package dev.bridge.gateway.ui

import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.scale
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.ColorFilter
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import dev.bridge.gateway.R
import dev.bridge.gateway.ui.theme.Bridge
import kotlinx.coroutines.delay

/** The Bridge mark on a phosphor tile. */
@Composable
fun BridgeTile(size: Dp = 48.dp) {
    Box(
        Modifier
            .size(size)
            .clip(RoundedCornerShape(size * 0.28f))
            .background(MaterialTheme.colorScheme.primary),
        contentAlignment = Alignment.Center,
    ) {
        Image(
            painter = painterResource(R.drawable.ic_bridge_mark),
            contentDescription = null,
            colorFilter = ColorFilter.tint(MaterialTheme.colorScheme.onPrimary),
            modifier = Modifier.size(size * 0.62f),
        )
    }
}

/** A status dot; [live] adds a slow pulse, like a signal. */
@Composable
fun StatusDot(color: Color, live: Boolean, size: Dp = 10.dp) {
    Box(contentAlignment = Alignment.Center) {
        if (live) {
            val pulse = rememberInfiniteTransition(label = "pulse")
            val s by pulse.animateFloat(1f, 2.6f, infiniteRepeatable(tween(1600), RepeatMode.Restart), label = "scale")
            val a by pulse.animateFloat(0.45f, 0f, infiniteRepeatable(tween(1600), RepeatMode.Restart), label = "alpha")
            Box(Modifier.size(size).scale(s).clip(CircleShape).background(color.copy(alpha = a)))
        }
        Box(Modifier.size(size).clip(CircleShape).background(color))
    }
}

@Composable
fun SectionCard(modifier: Modifier = Modifier, content: @Composable ColumnScope.() -> Unit) {
    Column(
        modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(16.dp))
            .background(Bridge.colors.card)
            .border(1.dp, Bridge.colors.border, RoundedCornerShape(16.dp))
            .padding(20.dp),
        content = content,
    )
}

@Composable
fun Eyebrow(text: String, modifier: Modifier = Modifier) {
    Text(text.uppercase(), style = MaterialTheme.typography.labelSmall, color = Bridge.colors.faint, modifier = modifier)
}

/** Re-composes every [periodMs] so relative times stay current. */
@Composable
fun rememberNow(periodMs: Long = 5_000): Long {
    var now by remember { mutableLongStateOf(System.currentTimeMillis()) }
    LaunchedEffect(periodMs) {
        while (true) {
            delay(periodMs)
            now = System.currentTimeMillis()
        }
    }
    return now
}

fun relative(thenMs: Long?, nowMs: Long): String {
    if (thenMs == null) return "never"
    val s = ((nowMs - thenMs) / 1000).coerceAtLeast(0)
    return when {
        s < 5 -> "just now"
        s < 60 -> "$s s ago"
        s < 3600 -> "${s / 60} min ago"
        else -> "${s / 3600} h ago"
    }
}

fun inFuture(atMs: Long, nowMs: Long): String {
    val s = ((atMs - nowMs) / 1000).coerceAtLeast(0)
    return if (s < 60) "in $s s" else "in ${s / 60} min"
}
