package dev.bridge.gateway.gateway

import dev.bridge.gateway.net.BridgeApiException
import dev.bridge.gateway.diagnostics.Redact
import dev.bridge.gateway.net.BridgeJson
import dev.bridge.gateway.net.DeviceStatus
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.drop
import kotlinx.coroutines.flow.filter
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.merge
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.withTimeoutOrNull
import kotlin.time.Duration
import kotlin.time.Duration.Companion.minutes
import kotlin.time.Duration.Companion.seconds

/** What the app needs to open a gateway connection. */
data class GatewaySession(
    val websocketUrl: String,
    val credential: String,
    val schedule: HeartbeatSchedule,
) {
    override fun toString() = "GatewaySession(websocketUrl=$websocketUrl, credential=${Redact.HIDDEN}, schedule=$schedule)"
}

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
    data object NetworkChanged : Event
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
 * callback instead of retrying on a timer, but still tries every [offlineProbe]
 * in case Android's report is wrong.
 *
 * Only a real revoke (the server's `unpaired` frame or close code, or a rejected
 * credential) ends the loop. Every other close, failure or internal error
 * schedules a retry.
 */
class ConnectionManager(
    private val scope: CoroutineScope,
    private val opener: SocketOpener,
    private val networkAvailable: StateFlow<Boolean>,
    private val session: suspend () -> GatewaySession?,
    private val snapshot: suspend () -> StatusSnapshot,
    /** Runs in its own coroutine, so it may stop this manager. */
    private val onRevoked: suspend (String) -> Unit,
    private val onSync: suspend () -> Unit = {},
    /** Frames the connection itself does not handle (send_sms, report_ack). */
    private val onFrame: suspend (ServerFrame) -> Unit = {},
    /** Runs when a connection opens, e.g. to flush stored reports. */
    private val onConnected: suspend () -> Unit = {},
    /** Bumps when the default network changes; an open socket is then checked with a heartbeat. */
    private val networkChanges: StateFlow<Long> = MutableStateFlow(0L),
    /** Connection events, for the diagnostics log. */
    private val log: (String) -> Unit = {},
    private val backoff: Backoff = Backoff(),
    private val ackTimeout: Duration = 20.seconds,
    private val connectTimeout: Duration = 30.seconds,
    private val offlineProbe: Duration = 5.minutes,
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

    /** Whether the connection loop is running: connected, connecting or waiting to retry. */
    val isRunning: Boolean
        @Synchronized get() = job?.isActive == true

    /** Starts the connection loop if it is not running. */
    @Synchronized
    fun start(reason: String = "") {
        if (job?.isActive == true) return
        log("Connection loop started${if (reason.isNotEmpty()) " ($reason)" else ""}")
        job = scope.launch { runLoop() }
    }

    @Synchronized
    fun stop(reason: String = "") {
        if (job?.isActive == true) log("Connection stopped${if (reason.isNotEmpty()) " ($reason)" else ""}")
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
        while (currentCoroutineContext().isActive) {
            val s = session() ?: run {
                log("Not paired; nothing to connect to")
                _state.value = ConnectionState.Stopped
                return
            }
            if (!networkAvailable.value) {
                _state.value = ConnectionState.WaitingForNetwork
                log("Waiting for a network")
                // Android's report can be wrong (handovers, VPNs, vendor builds), so try now and then anyway.
                val woke = pause(offlineProbe)
                log(woke?.let { "Trying now: $it" } ?: "Still no network reported; trying anyway")
            }
            _state.value = ConnectionState.Connecting(attempt)
            val startedAt = clock()
            val outcome = try {
                connectOnce(s, attempt)
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                log("Connection error: ${e.javaClass.simpleName}: ${e.message}")
                Outcome.Retry("Unexpected error: ${e.message ?: e.javaClass.simpleName}")
            }
            if (outcome is Outcome.Revoked) {
                log("Revoked: ${outcome.message}")
                _state.value = ConnectionState.Revoked(outcome.message)
                // Forgetting the pairing stops this loop. Run here, that would cancel the cleanup halfway
                // and leave a dead pairing behind that blocks pairing again.
                scope.launch { onRevoked(outcome.message) }
                return
            }
            outcome as Outcome.Retry
            // A connection that lived a while resets the backoff.
            attempt = if (clock() - startedAt > 60_000 && !outcome.penalize) 0 else attempt + 1
            if (outcome.penalize) attempt = maxOf(attempt, 4)
            if (!networkAvailable.value) {
                log("Disconnected: ${outcome.reason}")
                continue // wait for the network instead of a timer
            }
            val wait = backoff.delayFor(attempt)
            _state.value = ConnectionState.Retrying(attempt, clock() + wait.inWholeMilliseconds, outcome.reason)
            log("Disconnected: ${outcome.reason}. Retrying in ${wait.inWholeSeconds} s (attempt $attempt)")
            pause(wait)?.let { log("Retrying now: $it") }
        }
    }

    /**
     * Waits up to [timeout]. Returns early, with the reason, on a nudge, when the
     * network comes back or when the default network changes; null on timeout.
     */
    private suspend fun pause(timeout: Duration): String? {
        @Suppress("ControlFlowWithEmptyBody")
        while (retryNow.tryReceive().isSuccess) { } // drop nudges from before the wait
        val online = if (networkAvailable.value) networkAvailable.drop(1) else networkAvailable
        return withTimeoutOrNull(timeout) {
            merge(
                retryNow.receiveAsFlow().map { "asked to reconnect" },
                online.filter { it }.map { "network available" },
                networkChanges.drop(1).map { "network changed" },
            ).first()
        }
    }

    private suspend fun connectOnce(s: GatewaySession, attempt: Int): Outcome {
        val channel = Channel<Event>(Channel.UNLIMITED)
        events = channel
        // Retries are logged with their reason and delay; one line per attempt keeps the log readable.
        if (attempt == 0) log("Connecting to ${hostOf(s.websocketUrl)}")
        val socket = opener.open(s.websocketUrl, s.credential, object : SocketEvents {
            override fun onOpen() { channel.trySend(Event.Open) }
            override fun onMessage(text: String) { channel.trySend(Event.Message(text)) }
            override fun onClosed(code: Int, reason: String) { channel.trySend(Event.Closed(code, reason)) }
            override fun onFailure(error: Throwable, httpStatus: Int?, errorCode: String?) {
                channel.trySend(Event.Failure(error, httpStatus, errorCode))
            }
        })
        // Transitions only: when probing while Android reports no network, the attempt must still get a chance.
        val watcher = scope.launch {
            launch { networkAvailable.drop(1).collect { if (!it) channel.trySend(Event.NetworkLost) } }
            launch { networkChanges.drop(1).collect { channel.trySend(Event.NetworkChanged) } }
        }

        var opened = false
        var connectedSince = 0L
        var seq = 0L
        var awaitingAck: Long? = null
        var ackDeadline = Long.MAX_VALUE
        var nextBeatAt = Long.MAX_VALUE
        var interval = s.schedule.chargingSeconds
        var lastAckAt: Long? = null

        suspend fun beat() {
            val snap = guarded("reading the phone's status") { snapshot() }
                ?: StatusSnapshot(DeviceStatus(), charging = false, powerSave = false)
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
                        log("Connected")
                        beat()
                        guarded("sending stored reports") { onConnected() }
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
                                guarded("handling sync") { onSync() }
                            }
                            Frames.UNPAIRED -> return Outcome.Revoked("This phone was removed from the project in the dashboard.")
                            else -> guarded("handling ${frame.type}") { onFrame(frame) } // welcome, config and message frames
                        }
                    }
                    is Event.Closed -> {
                        log("Server closed the connection (code ${event.code}${if (event.reason.isNotEmpty()) ", ${event.reason}" else ""})")
                        return when (event.code) {
                            CloseCodes.UNPAIRED -> Outcome.Revoked("This phone was removed from the project in the dashboard.")
                            CloseCodes.REPLACED -> Outcome.Retry("Another connection for this phone took over", penalize = true)
                            else -> Outcome.Retry("Connection closed (${event.code})")
                        }
                    }
                    is Event.Failure -> {
                        val http = event.httpStatus?.let { "HTTP $it${event.errorCode?.let { c -> " $c" } ?: ""} · " } ?: ""
                        log("Connection failed: $http${event.error.javaClass.simpleName}: ${event.error.message}")
                        if (event.httpStatus == 401 && event.errorCode in BridgeApiException.REJECTED_CODES) {
                            return Outcome.Revoked("The server no longer accepts this phone. Pair it again.")
                        }
                        return Outcome.Retry(event.httpStatus?.let { "Server returned HTTP $it" } ?: describe(event.error))
                    }
                    Event.Nudge -> if (opened) beat()
                    Event.NetworkLost -> return Outcome.Retry("Network lost")
                    Event.NetworkChanged -> if (opened) {
                        // The socket may still be bound to the old network; an unanswered heartbeat reconnects.
                        log("Network changed; checking the connection")
                        beat()
                    }
                }
            }
        } finally {
            watcher.cancel()
            events = null
            liveSocket = null
            socket.close(CloseCodes.NORMAL, "")
        }
    }

    /** Runs a callback; a failure is logged instead of taking the connection down. */
    private suspend fun <T> guarded(what: String, block: suspend () -> T): T? = try {
        block()
    } catch (e: CancellationException) {
        throw e
    } catch (e: Exception) {
        log("Error while $what: ${e.javaClass.simpleName}: ${e.message}")
        null
    }

    private fun hostOf(url: String): String = runCatching { java.net.URI(url).authority }.getOrNull() ?: url

    private fun describe(t: Throwable): String = when (t) {
        is java.net.UnknownHostException -> "Cannot resolve the server address"
        is java.net.ConnectException -> "Cannot reach the server"
        is javax.net.ssl.SSLException -> "Secure connection failed"
        is java.net.SocketTimeoutException -> "The connection timed out"
        else -> t.message ?: t.javaClass.simpleName
    }
}
