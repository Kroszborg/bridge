package dev.bridge.gateway

import dev.bridge.gateway.net.BridgeApiException
import dev.bridge.gateway.pairing.PairingFlow
import dev.bridge.gateway.pairing.PairingRequest
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.IOException

@OptIn(ExperimentalCoroutinesApi::class)
class PairingFlowTest {
    private val first = PairingRequest("https://api.bridge.test", "bp_aaaaaaaaaaaaaaaaaaaaaaaa")
    private val second = PairingRequest("https://api.bridge.test", "bp_bbbbbbbbbbbbbbbbbbbbbbbb")

    @Test
    fun `pairing again after a revoke sends a new request`() = runTest {
        val sent = mutableListOf<PairingRequest>()
        val flow = PairingFlow(pair = { sent += it })

        flow.request(first, currentProject = null)
        flow.confirm()
        // The dashboard removed the phone; it was forgotten and scans a fresh code.
        flow.request(second, currentProject = null)
        flow.confirm()

        assertEquals(listOf(first, second), sent)
        assertFalse(flow.state.value.busy)
        assertNull(flow.state.value.error)
    }

    @Test
    fun `a code scanned while still paired is offered as a replacement`() = runTest {
        val sent = mutableListOf<PairingRequest>()
        val flow = PairingFlow(pair = { sent += it })

        flow.request(second, currentProject = "Acme")
        val confirm = flow.state.value.confirm
        assertNotNull("the request must not be refused", confirm)
        assertEquals("Acme", confirm!!.replacing)

        flow.confirm()
        assertEquals(listOf(second), sent)
    }

    @Test
    fun `a failed attempt shows an error and the next one still goes out`() = runTest {
        var calls = 0
        val flow = PairingFlow(pair = { if (++calls == 1) throw IOException("timeout") })

        flow.request(first, null)
        flow.confirm()
        assertTrue(flow.state.value.error!!.contains("Could not reach api.bridge.test"))
        assertFalse(flow.state.value.busy)

        flow.request(second, null)
        assertNull("a new code clears the old error", flow.state.value.error)
        flow.confirm()
        assertEquals(2, calls)
        assertNull(flow.state.value.error)
    }

    @Test
    fun `an unexpected exception still ends with an error and the controls enabled`() = runTest {
        val flow = PairingFlow(pair = { throw IllegalStateException("keystore unavailable") })
        flow.request(first, null)
        flow.confirm()
        assertFalse(flow.state.value.busy)
        assertTrue(flow.state.value.error!!.contains("keystore unavailable"))
    }

    @Test
    fun `server errors are shown as the server words them`() = runTest {
        val flow = PairingFlow(pair = {
            throw BridgeApiException(410, "pairing_token_used", "This pairing code was already used. Create a new one.", null)
        })
        flow.request(first, null)
        flow.confirm()
        assertEquals("This pairing code was already used. Create a new one.", flow.state.value.error)
    }

    @Test
    fun `a second confirmation while pairing is ignored`() = runTest {
        val gate = CompletableDeferred<Unit>()
        var calls = 0
        val flow = PairingFlow(pair = { calls++; gate.await() })

        flow.request(first, null)
        launch { flow.confirm() }
        runCurrent()
        assertTrue(flow.state.value.busy)

        flow.request(second, null)
        flow.confirm()
        assertEquals(1, calls)

        gate.complete(Unit)
        runCurrent()
        assertFalse(flow.state.value.busy)
    }

    @Test
    fun `cancelling clears the confirmation without sending`() = runTest {
        var calls = 0
        val flow = PairingFlow(pair = { calls++ })
        flow.request(first, null)
        flow.cancel()
        flow.confirm()
        assertNull(flow.state.value.confirm)
        assertEquals(0, calls)
    }
}
