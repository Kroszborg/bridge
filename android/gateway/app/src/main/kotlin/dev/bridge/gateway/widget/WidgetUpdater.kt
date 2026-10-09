package dev.bridge.gateway.widget

import android.content.Context
import androidx.glance.appwidget.GlanceAppWidgetManager
import androidx.glance.appwidget.updateAll
import dev.bridge.gateway.AppContainer
import dev.bridge.gateway.gateway.ConnectionState
import dev.bridge.gateway.sms.LastMessage
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.FlowPreview
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.debounce
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.util.Calendar

/** The widget's four states. Heartbeats and retries do not change these, so they cause no updates. */
enum class WidgetStatus { Online, Connecting, Offline, NotPaired }

/** What the widget shows. */
data class WidgetModel(
    val status: WidgetStatus,
    val project: String?,
    /** When [status] began, while this process has been watching. */
    val since: Long?,
    val sentToday: Int,
    val failedToday: Int,
    val last: LastMessage?,
)

fun widgetStatus(connection: ConnectionState, paired: Boolean): WidgetStatus = when {
    !paired -> WidgetStatus.NotPaired
    connection is ConnectionState.Connected -> WidgetStatus.Online
    connection is ConnectionState.Connecting || connection is ConnectionState.Retrying -> WidgetStatus.Connecting
    else -> WidgetStatus.Offline
}

/**
 * Pushes the app's own state changes to the home-screen widgets: connection
 * state transitions and message reports, debounced, and only while a widget is
 * placed. There is no polling; Android's own 30-minute refresh only rolls the
 * daily count over.
 */
class WidgetUpdater(private val context: Context, private val c: AppContainer, private val clock: () -> Long = System::currentTimeMillis) {
    private val _model = MutableStateFlow<WidgetModel?>(null)
    val model: StateFlow<WidgetModel?> = _model.asStateFlow()

    @Volatile private var since: Pair<WidgetStatus, Long>? = null

    @OptIn(FlowPreview::class)
    fun start() {
        c.scope.launch {
            combine(
                c.connection.state,
                c.store.pairing.map { it?.projectName }.distinctUntilChanged(),
                c.outbox.changes,
            ) { state, project, outbox -> Triple(widgetStatus(state, project != null), project, outbox) }
                .distinctUntilChanged() // a heartbeat changes the connection state but not the widget
                .debounce(DEBOUNCE_MS)
                .collectLatest { (status, project, _) ->
                    try {
                        if (!hasWidgets()) return@collectLatest
                        _model.value = build(status, project)
                        GatewayWidget().updateAll(context)
                    } catch (e: CancellationException) {
                        throw e
                    } catch (_: Exception) {
                        // A widget host hiccup must not stop later updates.
                    }
                }
        }
    }

    /** A fresh model, read from the stores; used when a widget is drawn without the updater running. */
    suspend fun current(): WidgetModel {
        val project = c.store.pairing.first()?.projectName
        return build(widgetStatus(c.connection.state.value, project != null), project).also { _model.value = it }
    }

    private suspend fun build(status: WidgetStatus, project: String?): WidgetModel = withContext(Dispatchers.IO) {
        val started = since?.takeIf { it.first == status } ?: (status to clock()).also { since = it }
        val (sent, failed) = c.outbox.countsSince(startOfToday())
        WidgetModel(status, project, started.second, sent, failed, c.outbox.lastMessage())
    }

    private suspend fun hasWidgets(): Boolean =
        GlanceAppWidgetManager(context).getGlanceIds(GatewayWidget::class.java).isNotEmpty()

    private fun startOfToday(): Long = Calendar.getInstance().apply {
        set(Calendar.HOUR_OF_DAY, 0)
        set(Calendar.MINUTE, 0)
        set(Calendar.SECOND, 0)
        set(Calendar.MILLISECOND, 0)
    }.timeInMillis

    private companion object {
        const val DEBOUNCE_MS = 1_000L
    }
}
