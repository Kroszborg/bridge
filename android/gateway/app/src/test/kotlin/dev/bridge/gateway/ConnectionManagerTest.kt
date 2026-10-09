package dev.bridge.gateway

import dev.bridge.gateway.gateway.Backoff
import dev.bridge.gateway.gateway.CloseCodes
import dev.bridge.gateway.gateway.ConnectionManager
import dev.bridge.gateway.gateway.ConnectionState
import dev.bridge.gateway.gateway.GatewaySession
import dev.bridge.gateway.gateway.GatewaySocket
import dev.bridge.gateway.gateway.HeartbeatSchedule
import dev.bridge.gateway.gateway.SocketEvents
import dev.bridge.gateway.gateway.SocketOpener
import dev.bridge.gateway.gateway.StatusSnapshot
import dev.bridge.gateway.net.DeviceStatus
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.int
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.long
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.IOException
import kotlin.random.Random

@OptIn(ExperimentalCoroutinesApi::class)
class ConnectionManagerTest {
    private class FakeSocket : GatewaySocket {
        val sent = mutableListOf<String>()
        var closedWith: Int? = null
        override fun send(text: String): Boolean = sent.add(text)
        override fun close(code: Int, reason: String) {
            if (closedWith == null) closedWith = code
        }
    }

    private class FakeOpener : SocketOpener {
        val sockets = mutableListOf<FakeSocket>()
        val events = mutableListOf<SocketEvents>()
        var lastCredential: String? = null
        var failNext = false
        override fun open(url: String, credential: String, events: SocketEvents): GatewaySocket {
            if (failNext) {
                failNext = false
                throw IllegalStateException("socket factory broke")
            }
            lastCredential = credential
            this.events += events
            return FakeSocket().also { sockets += it }
        }
    }

    private class Harness(val scope: TestScope, charging: Boolean = true) {
        val opener = FakeOpener()
        val network = MutableStateFlow(true)
        val networkChanges = MutableStateFlow(0L)
        var revokedWith: String? = null
        var revokeHook: suspend () -> Unit = {}
        var snapshotFails = false
        val log = mutableListOf<String>()
        var syncs = 0
        val frames = mutableListOf<dev.bridge.gateway.gateway.ServerFrame>()
        var connects = 0
        val manager = ConnectionManager(
            scope = scope.backgroundScope,
            opener = opener,
            networkAvailable = network,
            session = { GatewaySession("wss://bridge.test/v1/device/connect", "bd_secret", HeartbeatSchedule(15, 600, 60, 300)) },
            snapshot = {
                check(!snapshotFails) { "status unavailable" }
                StatusSnapshot(DeviceStatus(batteryLevel = 50), charging = charging, powerSave = false)
            },
            onRevoked = {
                revokedWith = it
                revokeHook()
            },
            onSync = { syncs++ },
            onFrame = {
                check(it.type != "explode") { "bad frame handler" }
                frames += it
            },
            onConnected = { connects++ },
            networkChanges = networkChanges,
            log = { log += it },
            backoff = Backoff(random = Random(42)),
            clock = { scope.testScheduler.currentTime },
        )
        val socket get() = opener.sockets.last()
        val events get() = opener.events.last()

        fun heartbeats() = socket.sent.map { Json.parseToJsonElement(it).jsonObject }.filter { it["type"]?.jsonPrimitive?.content == "heartbeat" }

        fun ack(seq: Long) = events.onMessage("""{"type":"heartbeat_ack","seq":$seq}""")
    }

    @Test
    fun `sends a heartbeat on open and then at the charging interval`() = runTest {
        val h = Harness(this)
        h.manager.start()
        runCurrent()
        assertEquals(1, h.opener.sockets.size)
        assertEquals("bd_secret", h.opener.lastCredential)

        h.events.onOpen()
        h.events.onMessage("""{"type":"welcome","device_id":"dev_1","protocol_version":1,"future_field":true}""")
        runCurrent()
        val first = h.heartbeats().single()
        assertEquals(1L, first["seq"]!!.jsonPrimitive.long)
        assertEquals(60, first["next_in"]!!.jsonPrimitive.int)
        assertEquals(50, first["status"]!!.jsonObject["battery_level"]!!.jsonPrimitive.int)

        h.ack(1)
        runCurrent()
        assertTrue(h.manager.state.value is ConnectionState.Connected)

        advanceTimeBy(59_000)
        runCurrent()
        assertEquals(1, h.heartbeats().size)
        advanceTimeBy(1_500)
        runCurrent()
        assertEquals(2, h.heartbeats().size)
    }

    @Test
    fun `uses the longer interval on battery`() = runTest {
        val h = Harness(this, charging = false)
        h.manager.start()
        runCurrent()
        h.events.onOpen()
        runCurrent()
        assertEquals(300, h.heartbeats().single()["next_in"]!!.jsonPrimitive.int)
    }

    @Test
    fun `reconnects when the server stops acknowledging`() = runTest {
        val h = Harness(this)
        h.manager.start()
        runCurrent()
        h.events.onOpen()
        runCurrent()
        advanceTimeBy(20_500) // ack timeout
        runCurrent()
        assertEquals(CloseCodes.ACK_TIMEOUT, h.opener.sockets[0].closedWith)
        val retrying = h.manager.state.value
        assertTrue("expected Retrying, got $retrying", retrying is ConnectionState.Retrying)

        advanceTimeBy(10_000)
        runCurrent()
        assertEquals(2, h.opener.sockets.size)
    }

    @Test
    fun `waits for the network instead of retrying`() = runTest {
        val h = Harness(this)
        h.network.value = false
        h.manager.start()
        runCurrent()
        assertEquals(ConnectionState.WaitingForNetwork, h.manager.state.value)
        advanceTimeBy(60_000)
        runCurrent()
        assertEquals(0, h.opener.sockets.size)

        h.network.value = true
        runCurrent()
        assertEquals(1, h.opener.sockets.size)
    }

    @Test
    fun `losing the network closes the socket and waits`() = runTest {
        val h = Harness(this)
        h.manager.start()
        runCurrent()
        h.events.onOpen()
        runCurrent()
        h.network.value = false
        runCurrent()
        assertTrue(h.opener.sockets[0].closedWith != null)
        assertEquals(ConnectionState.WaitingForNetwork, h.manager.state.value)
        advanceTimeBy(60_000)
        runCurrent()
        assertEquals(1, h.opener.sockets.size)

        h.network.value = true
        runCurrent()
        assertEquals(2, h.opener.sockets.size)
    }

    @Test
    fun `unpaired frame revokes and stops`() = runTest {
        val h = Harness(this)
        h.manager.start()
        runCurrent()
        h.events.onOpen()
        h.events.onMessage("""{"type":"unpaired","reason":"removed"}""")
        runCurrent()
        assertTrue(h.manager.state.value is ConnectionState.Revoked)
        assertTrue(h.revokedWith!!.contains("removed"))
        advanceTimeBy(600_000)
        runCurrent()
        assertEquals(1, h.opener.sockets.size)
    }

    @Test
    fun `a rejected credential at upgrade revokes`() = runTest {
        val h = Harness(this)
        h.manager.start()
        runCurrent()
        h.events.onFailure(IOException("401"), 401, "device_revoked")
        runCurrent()
        assertTrue(h.manager.state.value is ConnectionState.Revoked)
    }

    @Test
    fun `other failures retry`() = runTest {
        val h = Harness(this)
        h.manager.start()
        runCurrent()
        h.events.onFailure(IOException("boom"), 503, "service_unavailable")
        runCurrent()
        assertTrue(h.manager.state.value is ConnectionState.Retrying)
        assertEquals(null, h.revokedWith)
    }

    @Test
    fun `being replaced backs off harder`() = runTest {
        val h = Harness(this)
        h.manager.start()
        runCurrent()
        h.events.onOpen()
        runCurrent()
        h.events.onClosed(CloseCodes.REPLACED, "replaced")
        runCurrent()
        val state = h.manager.state.value as ConnectionState.Retrying
        assertTrue("attempt ${state.attempt}", state.attempt >= 4)
    }

    @Test
    fun `sync frame sends a heartbeat immediately`() = runTest {
        val h = Harness(this)
        h.manager.start()
        runCurrent()
        h.events.onOpen()
        runCurrent()
        h.ack(1)
        h.events.onMessage("""{"type":"sync","reason":"manual"}""")
        runCurrent()
        assertEquals(2, h.heartbeats().size)
        assertEquals(1, h.syncs)
    }

    @Test
    fun `nudge skips the backoff wait`() = runTest {
        val h = Harness(this)
        h.manager.start()
        runCurrent()
        h.events.onFailure(IOException("down"), null, null)
        runCurrent()
        assertTrue(h.manager.state.value is ConnectionState.Retrying)
        h.manager.nudge()
        runCurrent()
        assertEquals(2, h.opener.sockets.size)
    }

    @Test
    fun `stop closes the socket`() = runTest {
        val h = Harness(this)
        h.manager.start()
        runCurrent()
        h.events.onOpen()
        runCurrent()
        h.manager.stop()
        runCurrent()
        assertEquals(CloseCodes.NORMAL, h.socket.closedWith)
        assertEquals(ConnectionState.Stopped, h.manager.state.value)
    }

    @Test
    fun `app frames are handed over and sendRaw only works while connected`() = runTest {
        val h = Harness(this)
        h.manager.start()
        runCurrent()
        assertEquals(false, h.manager.sendRaw("x"))
        h.events.onOpen()
        runCurrent()
        assertEquals(1, h.connects)
        assertEquals(true, h.manager.sendRaw("{\"type\":\"sms_accepted\"}"))
        assertTrue(h.socket.sent.any { it.contains("sms_accepted") })

        h.events.onMessage("""{"type":"send_sms","message_id":"msg_1","to":"+91","body":"hi","attempt":1}""")
        h.events.onMessage("""{"type":"report_ack","message_id":"msg_1","report":"sms_accepted"}""")
        runCurrent()
        assertEquals(listOf("send_sms", "report_ack"), h.frames.map { it.type })

        h.events.onClosed(1000, "")
        runCurrent()
        assertEquals(false, h.manager.sendRaw("x"))
    }

    // The incident behind the tests below: the app closed its socket a few seconds after pairing and
    // never tried again, because Android's network report said "offline" and nothing else woke it.

    @Test
    fun `a stuck offline report still tries now and then`() = runTest {
        val h = Harness(this)
        h.network.value = false
        h.manager.start()
        runCurrent()
        assertEquals(0, h.opener.sockets.size)
        advanceTimeBy(5 * 60_000 + 1_000)
        runCurrent()
        assertEquals(1, h.opener.sockets.size)
        // The probe is not cut short by the (wrong) offline report.
        h.events.onOpen()
        runCurrent()
        assertNull(h.socket.closedWith)
        assertTrue(h.manager.state.value is ConnectionState.Connected)
    }

    @Test
    fun `reconnect now works while waiting for the network`() = runTest {
        val h = Harness(this)
        h.network.value = false
        h.manager.start()
        runCurrent()
        assertEquals(ConnectionState.WaitingForNetwork, h.manager.state.value)
        h.manager.nudge()
        runCurrent()
        assertEquals(1, h.opener.sockets.size)
    }

    @Test
    fun `a network change checks an open connection and reconnects if it is dead`() = runTest {
        val h = Harness(this)
        h.manager.start()
        runCurrent()
        h.events.onOpen()
        runCurrent()
        h.ack(1)
        runCurrent()
        h.networkChanges.value = 1
        runCurrent()
        assertEquals(2, h.heartbeats().size)
        advanceTimeBy(20_500) // no acknowledgement on the old network
        runCurrent()
        assertEquals(CloseCodes.ACK_TIMEOUT, h.opener.sockets[0].closedWith)
        assertTrue(h.manager.state.value is ConnectionState.Retrying)
    }

    @Test
    fun `a network change during the backoff retries at once`() = runTest {
        val h = Harness(this)
        h.manager.start()
        runCurrent()
        h.events.onFailure(IOException("down"), null, null)
        runCurrent()
        assertTrue(h.manager.state.value is ConnectionState.Retrying)
        h.networkChanges.value = 1
        runCurrent()
        assertEquals(2, h.opener.sockets.size)
    }

    @Test
    fun `a failing frame handler does not drop the connection`() = runTest {
        val h = Harness(this)
        h.manager.start()
        runCurrent()
        h.events.onOpen()
        h.events.onMessage("""{"type":"explode"}""")
        h.events.onMessage("""{"type":"send_sms","message_id":"msg_1","to":"+91","body":"hi","attempt":1}""")
        runCurrent()
        assertNull(h.socket.closedWith)
        assertEquals(listOf("send_sms"), h.frames.map { it.type })
        assertTrue(h.log.any { it.contains("bad frame handler") })
    }

    @Test
    fun `heartbeats continue when the status cannot be read`() = runTest {
        val h = Harness(this)
        h.snapshotFails = true
        h.manager.start()
        runCurrent()
        h.events.onOpen()
        runCurrent()
        assertEquals(1, h.heartbeats().size)
        assertTrue(h.manager.state.value is ConnectionState.Connected)
    }

    @Test
    fun `an internal error retries instead of ending the loop`() = runTest {
        val h = Harness(this)
        h.opener.failNext = true
        h.manager.start()
        runCurrent()
        assertTrue(h.manager.state.value is ConnectionState.Retrying)
        advanceTimeBy(10_000)
        runCurrent()
        assertEquals(1, h.opener.sockets.size)
    }

    @Test
    fun `revoke cleanup that stops the manager still runs to the end`() = runTest {
        val h = Harness(this)
        var cleanedUp = false
        // Like AppContainer.forget: stop the manager first, then clear the pairing (a suspending write).
        h.revokeHook = {
            h.manager.stop()
            delay(10)
            cleanedUp = true
        }
        h.manager.start()
        runCurrent()
        h.events.onOpen()
        h.events.onClosed(CloseCodes.UNPAIRED, "device unpaired")
        runCurrent()
        advanceTimeBy(100)
        runCurrent()
        assertTrue("the pairing must be cleared after a revoke", cleanedUp)
        assertEquals(1, h.opener.sockets.size)
    }
}
