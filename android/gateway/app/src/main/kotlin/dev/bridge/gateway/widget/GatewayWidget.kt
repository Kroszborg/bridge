package dev.bridge.gateway.widget

import android.content.Context
import android.text.format.DateUtils
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.DpSize
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.glance.ColorFilter
import androidx.glance.GlanceId
import androidx.glance.GlanceModifier
import androidx.glance.Image
import androidx.glance.ImageProvider
import androidx.glance.LocalContext
import androidx.glance.LocalSize
import androidx.glance.action.actionStartActivity
import androidx.glance.action.clickable
import androidx.glance.appwidget.GlanceAppWidget
import androidx.glance.appwidget.GlanceAppWidgetReceiver
import androidx.glance.appwidget.SizeMode
import androidx.glance.appwidget.appWidgetBackground
import androidx.glance.appwidget.provideContent
import androidx.glance.background
import androidx.glance.layout.Alignment
import androidx.glance.layout.Box
import androidx.glance.layout.Column
import androidx.glance.layout.Row
import androidx.glance.layout.Spacer
import androidx.glance.layout.fillMaxSize
import androidx.glance.layout.fillMaxWidth
import androidx.glance.layout.height
import androidx.glance.layout.padding
import androidx.glance.layout.size
import androidx.glance.layout.width
import androidx.glance.text.FontWeight
import androidx.glance.text.Text
import androidx.glance.text.TextStyle
import androidx.glance.color.ColorProvider
import androidx.glance.unit.ColorProvider
import dev.bridge.gateway.R
import dev.bridge.gateway.container
import dev.bridge.gateway.sms.LastMessage
import dev.bridge.gateway.sms.Outbox
import dev.bridge.gateway.ui.MainActivity
import java.text.DateFormat

/**
 * Home-screen status: whether this phone is online for its project, what it sent
 * today and how the last message went. Tapping it opens the app. The same
 * content serves both picker entries and adapts to the size the user picks.
 */
class GatewayWidget : GlanceAppWidget() {
    override val sizeMode = SizeMode.Responsive(setOf(SMALL, SMALL_TALL, WIDE, WIDE_TALL))

    override suspend fun provideGlance(context: Context, id: GlanceId) {
        val updater = context.container.widgets
        val initial = updater.current()
        provideContent {
            val model by updater.model.collectAsState()
            WidgetContent(model ?: initial)
        }
    }

    private companion object {
        val SMALL = DpSize(110.dp, 40.dp)
        val SMALL_TALL = DpSize(110.dp, 110.dp)
        val WIDE = DpSize(250.dp, 40.dp)
        val WIDE_TALL = DpSize(250.dp, 110.dp)
    }
}

/** "Gateway status" in the widget picker: 2×1 by default. */
class GatewayWidgetReceiver : GlanceAppWidgetReceiver() {
    override val glanceAppWidget: GlanceAppWidget = GatewayWidget()
}

/** "Gateway status, wide" in the widget picker: 4×1 by default. */
class GatewayWideWidgetReceiver : GlanceAppWidgetReceiver() {
    override val glanceAppWidget: GlanceAppWidget = GatewayWidget()
}

@Composable
private fun WidgetContent(m: WidgetModel) {
    val size = LocalSize.current
    Box(
        GlanceModifier
            .fillMaxSize()
            .appWidgetBackground()
            .background(ImageProvider(R.drawable.widget_background))
            .clickable(actionStartActivity<MainActivity>())
            .padding(horizontal = 14.dp, vertical = 10.dp),
        contentAlignment = Alignment.CenterStart,
    ) {
        val tall = size.height >= 90.dp
        if (size.width >= 220.dp) Wide(m, tall) else Small(m, tall)
    }
}

@Composable
private fun Small(m: WidgetModel, tall: Boolean) {
    Column(GlanceModifier.fillMaxWidth()) {
        StatusLine(m, withSince = false)
        Text(m.project ?: "Tap to pair this phone", style = text(WidgetColors.muted, 12), maxLines = 1)
        if (tall && m.project != null) {
            Spacer(GlanceModifier.height(8.dp))
            Text("${m.sentToday} sent today", style = text(WidgetColors.text, 13, bold = true), maxLines = 1)
            lastLine(m.last)?.let { Text(it, style = text(WidgetColors.muted, 12), maxLines = 1) }
        }
    }
}

@Composable
private fun Wide(m: WidgetModel, tall: Boolean) {
    Column(GlanceModifier.fillMaxWidth()) {
        Row(GlanceModifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Box(
                GlanceModifier.size(36.dp).background(ImageProvider(R.drawable.widget_tile)),
                contentAlignment = Alignment.Center,
            ) {
                Image(
                    ImageProvider(R.drawable.ic_bridge_mark),
                    contentDescription = null,
                    colorFilter = ColorFilter.tint(WidgetColors.onPrimary),
                    modifier = GlanceModifier.size(22.dp),
                )
            }
            Spacer(GlanceModifier.width(12.dp))
            Column(GlanceModifier.defaultWeight()) {
                StatusLine(m, withSince = true)
                Text(m.project ?: "Tap to pair this phone", style = text(WidgetColors.muted, 12), maxLines = 1)
            }
            if (m.project != null) {
                Spacer(GlanceModifier.width(8.dp))
                Column(horizontalAlignment = Alignment.End) {
                    Text("${m.sentToday}", style = text(WidgetColors.text, 18, bold = true), maxLines = 1)
                    Text("sent today", style = text(WidgetColors.muted, 11), maxLines = 1)
                }
            }
        }
        if (tall && m.project != null) {
            Spacer(GlanceModifier.height(10.dp))
            val failed = if (m.failedToday > 0) "${m.failedToday} failed today" else "No failures today"
            Text(failed, style = text(if (m.failedToday > 0) WidgetColors.danger else WidgetColors.muted, 12), maxLines = 1)
            lastLine(m.last)?.let { Text(it, style = text(WidgetColors.muted, 12), maxLines = 1) }
        }
    }
}

@Composable
private fun StatusLine(m: WidgetModel, withSince: Boolean) {
    val context = LocalContext.current
    val (label, color) = when (m.status) {
        WidgetStatus.Online -> "Online" to WidgetColors.success
        WidgetStatus.Connecting -> "Connecting" to WidgetColors.warning
        WidgetStatus.Offline -> "Offline" to WidgetColors.danger
        WidgetStatus.NotPaired -> "Not paired" to WidgetColors.faint
    }
    val since = m.since?.takeIf { withSince && m.status != WidgetStatus.NotPaired }?.let { " · since ${time(context, it)}" } ?: ""
    Row(verticalAlignment = Alignment.CenterVertically) {
        Image(
            ImageProvider(R.drawable.widget_dot),
            contentDescription = null,
            colorFilter = ColorFilter.tint(color),
            modifier = GlanceModifier.size(8.dp),
        )
        Spacer(GlanceModifier.width(6.dp))
        Text("$label$since", style = text(WidgetColors.text, 15, bold = true), maxLines = 1)
    }
}

private fun lastLine(last: LastMessage?): String? {
    last ?: return null
    val what = when (last.state) {
        Outbox.STATE_DELIVERED -> "Delivered"
        Outbox.STATE_SENT -> "Sent"
        Outbox.STATE_FAILED -> "Failed"
        Outbox.STATE_UNDELIVERED -> "Not delivered"
        else -> "Sending"
    }
    return "Last message: $what · ${DateUtils.formatSameDayTime(last.updatedAt, System.currentTimeMillis(), DateFormat.SHORT, DateFormat.SHORT)}"
}

private fun time(context: Context, at: Long): String = android.text.format.DateFormat.getTimeFormat(context).format(java.util.Date(at))

private fun text(color: ColorProvider, size: Int, bold: Boolean = false) =
    TextStyle(color = color, fontSize = size.sp, fontWeight = if (bold) FontWeight.Bold else FontWeight.Normal)

/** The app's light and dark palette; keep in step with the widget_* colours used by the picker previews. */
private object WidgetColors {
    val text = ColorProvider(day = Color(0xFF1A1714), night = Color(0xFFFAFAFA))
    val muted = ColorProvider(day = Color(0xFF5E564C), night = Color(0xFFA1A1AA))
    val faint = ColorProvider(day = Color(0xFFA39B90), night = Color(0xFF71717A))
    val success = ColorProvider(day = Color(0xFF15803D), night = Color(0xFF4ADE80))
    val warning = ColorProvider(day = Color(0xFFB45309), night = Color(0xFFFBBF24))
    val danger = ColorProvider(day = Color(0xFFDC2626), night = Color(0xFFF87171))
    val onPrimary = ColorProvider(day = Color(0xFFFFFFFF), night = Color(0xFF0A0A0B))
}
