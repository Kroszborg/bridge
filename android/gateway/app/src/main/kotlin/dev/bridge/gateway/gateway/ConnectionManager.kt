package dev.bridge.gateway.gateway

import dev.bridge.gateway.net.BridgeApiException
import dev.bridge.gateway.net.BridgeJson
import dev.bridge.gateway.net.DeviceStatus
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.launchIn
import kotlinx.coroutines.flow.onEach
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.withTimeoutOrNull
import kotlin.coroutines.coroutineContext
import kotlin.time.Duration
import kotlin.time.Duration.Companion.seconds

/** What the app needs to open a gateway connection. */
data class GatewaySession(
    val websocketUrl: String,
    val credential: String,
    val schedule: HeartbeatSchedule,
)

/** A health snapshot plus the power state that decides the heartbeat interval. */
data class StatusSnapshot(val status: DeviceStatus, val charging: Boolean, val powerSave: Boolean)

/** Events a socket implementation reports. Implementations must be thread-safe. */
interface SocketEvents {
    fun onOpen()
    fun onMessage(text: String)
    fun onClosed(code: Int, reason: String)
    fun onFailure(error: Throwable, httpStatus: Int?, errorCode: String?)
}

interface GatewaySocket {
    fun send(text: String): Boolean
    fun close(code: Int, reason: String)
}

fun interface SocketOpener {
    fun open(url: String, credential: String, events: SocketEvents): GatewaySocket
}

sealed interface ConnectionState {
    data object Stopped : ConnectionState
    data object WaitingForNetwork : ConnectionState
    data class Connecting(val attempt: Int) : ConnectionState
    data class Connected(val since: Long, val lastAckAt: Long?, val intervalSeconds: Int) : ConnectionState
    data class Retrying(val attempt: Int, val retryAt: Long, val reason: String) : ConnectionState
    data class Revoked(val message: String) : ConnectionState
}

private sealed interface Event {
    data object Open : Event
    data class Message(val text: String) : Event
    data class Closed(val code: Int, val reason: String) : Event
    data class Failure(val error: Throwable, val httpStatus: Int?, val errorCode: String?) : Event
    data object Nudge : Event
    data object NetworkLost : Event
}

private sealed interface Outcome {
    val reason: String

    data class Revoked(val message: String) : Outcome {
        override val reason get() = message
    }
    data class Retry(override val reason: String, val penalize: Boolean = false) : Outcome
}

/**
 * Keeps one authenticated WebSocket to the Bridge server alive.
 *
 * Battery behaviour: the app sends one small heartbeat per interval (60 s
 * charging, 5 min on battery, 10 min in battery saver) and the server answers
 * it; there are no WebSocket pings. While offline it waits for the network
 * callback instead of retrying blindly.
 */
class ConnectionManager(
    private val scope: CoroutineScope,
    private val opener: SocketOpener,
    private val networkAvailable: StateFlow<Boolean>,
    private val session: suspend () -> GatewaySession?,
    private val snapshot: suspend () -> StatusSnapshot,
    private val onRevoked: suspend (String) -> Unit,
    private val onSync: suspend () -> Unit = {},
    /** Frames the connection itself does not handle (send_sms, report_ack). */
    private val onFrame: suspend (ServerFrame) -> Unit = {},
    /** Runs when a connection opens, e.g. to flush stored reports. */
    private val onConnected: suspend () -> Unit = {},
    private val backoff: Backoff = Backoff(),
    private val ackTimeout: Duration = 20.seconds,
    private val connectTimeout: Duration = 30.seconds,
    private val clock: () -> Long = System::currentTimeMillis,
) {
    private val _state = MutableStateFlow<ConnectionState>(ConnectionState.Stopped)
    val state: StateFlow<ConnectionState> = _state.asStateFlow()

    private var job: Job? = null
    @Volatile private var events: Channel<Event>? = null
    @Volatile private var liveSocket: GatewaySocket? = null

    /** Sends a frame on the open connection; false when not connected. */
    fun sendRaw(text: String): Boolean = liveSocket?.send(text) ?: false
    private val retryNow = Channel<Unit>(Channel.CONFLATED)

    /** Starts the connection loop if it is not running. */
    @Synchronized
    fun start() {
        if (job?.isActive == true) return
        job = scope.launch { runLoop() }
    }

    @Synchronized
    fun stop() {
        job?.cancel()
        job = null
        _state.value = ConnectionState.Stopped
    }

    /** Check in now: send a heartbeat if connected, or skip the backoff wait. */
    fun nudge() {
        events?.trySend(Event.Nudge)
        retryNow.trySend(Unit)
    }

    private suspend fun runLoop() {
        var attempt = 0
        while (coroutineContext.isActive) {
            val s = session() ?: run {
                _state.value = ConnectionState.Stopped
                return
            }
            if (!networkAvailable.value) {
                _state.value = ConnectionState.WaitingForNetwork
                networkAvailable.first { it }
            }
            _state.value = ConnectionState.Connecting(attempt)
            val startedAt = clock()
            val outcome = connectOnce(s)
            if (outcome is Outcome.Revoked) {
                _state.value = ConnectionState.Revoked(outcome.message)
                onRevoked(outcome.message)
                return
            }
            outcome as Outcome.Retry
            // A connection that lived a while resets the backoff.
            attempt = if (clock() - startedAt > 60_000 && !outcome.penalize) 0 else attempt + 1
            if (outcome.penalize) attempt = maxOf(attempt, 4)
            if (!networkAvailable.value) continue // wait for the network instead of a timer
            val wait = backoff.delayFor(attempt)
            _state.value = ConnectionState.Retrying(attempt, clock() + wait.inWholeMilliseconds, outcome.reason)
            while (retryNow.tryReceive().isSuccess) Unit // drop stale nudges
            withTimeoutOrNull(wait) { retryNow.receive() }
        }
    }

    private suspend fun connectOnce(s: GatewaySession): Outcome {
        val channel = Channel<Event>(Channel.UNLIMITED)
        events = channel
        val socket = opener.open(s.websocketUrl, s.credential, object : SocketEvents {
            override fun onOpen() { channel.trySend(Event.Open) }
            override fun onMessage(text: String) { channel.trySend(Event.Message(text)) }
            override fun onClosed(code: Int, reason: String) { channel.trySend(Event.Closed(code, reason)) }
            override fun onFailure(error: Throwable, httpStatus: Int?, errorCode: String?) {
                channel.trySend(Event.Failure(error, httpStatus, errorCode))
            }
        })
        val watcher = networkAvailable.onEach { if (!it) channel.trySend(Event.NetworkLost) }.launchIn(scope)

        var opened = false
        var connectedSince = 0L
        var seq = 0L
        var awaitingAck: Long? = null
        var ackDeadline = Long.MAX_VALUE
        var nextBeatAt = Long.MAX_VALUE
        var interval = s.schedule.chargingSeconds
        var lastAckAt: Long? = null

        suspend fun beat() {
            val snap = snapshot()
            interval = s.schedule.intervalSeconds(snap.charging, snap.powerSave)
            seq++
            val frame = HeartbeatFrame(seq = seq, nextIn = interval, status = snap.status)
            socket.send(BridgeJson.encodeToString(HeartbeatFrame.serializer(), frame))
            val now = clock()
            awaitingAck = seq
            ackDeadline = now + ackTimeout.inWholeMilliseconds
            nextBeatAt = now + interval * 1000L
            _state.value = ConnectionState.Connected(connectedSince, lastAckAt, interval)
        }

        try {
            val openDeadline = clock() + connectTimeout.inWholeMilliseconds
            while (true) {
                val now = clock()
                val wakeAt = if (opened) minOf(nextBeatAt, if (awaitingAck != null) ackDeadline else Long.MAX_VALUE) else openDeadline
                val waitMs = (wakeAt - now).coerceAtLeast(0)
                val event = withTimeoutOrNull(waitMs) { channel.receive() }

                if (event == null) {
                    val t = clock()
                    when {
                        !opened -> return Outcome.Retry("Timed out connecting to the server")
                        awaitingAck != null && t >= ackDeadline -> {
                            socket.close(CloseCodes.ACK_TIMEOUT, "no heartbeat acknowledgement")
                            return Outcome.Retry("The server stopped responding")
                        }
                        t >= nextBeatAt -> beat()
                    }
                    continue
                }

                when (event) {
                    Event.Open -> {
                        opened = true
                        connectedSince = clock()
                        liveSocket = socket
                        beat()
                        onConnected()
                    }
                    is Event.Message -> {
                        val frame = runCatching { BridgeJson.decodeFromString(ServerFrame.serializer(), event.text) }.getOrNull()
                            ?: continue
                        when (frame.type) {
                            Frames.HEARTBEAT_ACK -> if (frame.seq == awaitingAck) {
                                awaitingAck = null
                                ackDeadline = Long.MAX_VALUE
                                lastAckAt = clock()
                                _state.value = ConnectionState.Connected(connectedSince, lastAckAt, interval)
                            }
                            Frames.SYNC -> {
                                beat()
                                onSync()
                            }
                            Frames.UNPAIRED -> return Outcome.Revoked("This phone was removed from the project in the dashboard.")
                            else -> onFrame(frame) // welcome, config and message frames
                        }
                    }
                    is Event.Closed -> return when (event.code) {
                        CloseCodes.UNPAIRED -> Outcome.Revoked("This phone was removed from the project in the dashboard.")
                        CloseCodes.REPLACED -> Outcome.Retry("Another connection for this phone took over", penalize = true)
                        else -> Outcome.Retry("Connection closed (${event.code})")
                    }
                    is Event.Failure -> {
                        if (event.httpStatus == 401 && event.errorCode in BridgeApiException.REJECTED_CODES) {
                            return Outcome.Revoked("The server no longer accepts this phone. Pair it again.")
                        }
                        return Outcome.Retry(event.httpStatus?.let { "Server returned HTTP $it" } ?: describe(event.error))
                    }
                    Event.Nudge -> if (opened) beat()
                    Event.NetworkLost -> return Outcome.Retry("Network lost")
                }
            }
        } finally {
            watcher.cancel()
            events = null
            liveSocket = null
            socket.close(CloseCodes.NORMAL, "")
        }
    }

    private fun describe(t: Throwable): String = when (t) {
        is java.net.UnknownHostException -> "Cannot resolve the server address"
        is java.net.ConnectException -> "Cannot reach the server"
        is javax.net.ssl.SSLException -> "Secure connection failed"
        is java.net.SocketTimeoutException -> "The connection timed out"
        else -> t.message ?: t.javaClass.simpleName
    }
}
