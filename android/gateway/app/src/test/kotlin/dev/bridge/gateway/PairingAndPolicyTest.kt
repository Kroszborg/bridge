package dev.bridge.gateway

import dev.bridge.gateway.gateway.Backoff
import dev.bridge.gateway.gateway.HeartbeatSchedule
import dev.bridge.gateway.pairing.PairingUri
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import kotlin.random.Random
import kotlin.time.Duration.Companion.minutes
import kotlin.time.Duration.Companion.seconds

class PairingAndPolicyTest {
    private val token = "bp_" + "A".repeat(43)

    @Test
    fun `parses the dashboard pairing URI`() {
        val r = PairingUri.parse("bridge://pair?api=https%3A%2F%2Fapi.example.com%2F&token=$token")!!
        assertEquals("https://api.example.com", r.apiUrl)
        assertEquals(token, r.token)
        assertEquals("api.example.com", r.host)
        assertEquals(false, r.isCleartext)
    }

    @Test
    fun `keeps ports and paths and flags cleartext`() {
        val r = PairingUri.parse("bridge://pair?token=$token&api=http%3A%2F%2F192.168.1.20%3A8080%2Fbridge")!!
        assertEquals("http://192.168.1.20:8080/bridge", r.apiUrl)
        assertEquals("192.168.1.20:8080", r.host)
        assertTrue(r.isCleartext)
    }

    @Test
    fun `rejects anything that is not a pairing code`() {
        listOf(
            "https://example.com",
            "bridge://other?api=https%3A%2F%2Fa.com&token=$token",
            "bridge://pair?api=https%3A%2F%2Fa.com",
            "bridge://pair?api=https%3A%2F%2Fa.com&token=bk_live_xyz",
            "bridge://pair?api=ftp%3A%2F%2Fa.com&token=$token",
            "bridge://pair?api=javascript%3Aalert(1)&token=$token",
            "bridge://pair?api=https%3A%2F%2Fuser%3Apass%40a.com&token=$token",
            "not a uri at all",
        ).forEach { assertNull(it, PairingUri.parse(it)) }
    }

    @Test
    fun `manual entry adds https and validates the code`() {
        val ok = PairingUri.fromManual("bridge.example.com/", "  $token ").getOrThrow()
        assertEquals("https://bridge.example.com", ok.apiUrl)
        assertTrue(PairingUri.fromManual("bridge.example.com", "123456").isFailure)
        assertTrue(PairingUri.fromManual("", token).isFailure)
    }

    @Test
    fun `backoff grows, caps and jitters`() {
        val b = Backoff(base = 1.seconds, max = 5.minutes, random = Random(7))
        val first = b.delayFor(0)
        assertTrue(first in 800.0.let { it.toLong() }.let { java.time.Duration.ofMillis(it) }.let { 0.8.seconds..1.2.seconds })
        var previousCap = 0L
        for (attempt in 0..30) {
            val d = b.delayFor(attempt)
            assertTrue("attempt $attempt gave $d", d <= 6.minutes)
            previousCap = maxOf(previousCap, d.inWholeMilliseconds)
        }
        assertTrue(b.delayFor(20) >= 4.minutes)
    }

    @Test
    fun `heartbeat interval follows power state within bounds`() {
        val s = HeartbeatSchedule(minSeconds = 15, maxSeconds = 600, chargingSeconds = 60, onBatterySeconds = 300)
        assertEquals(60, s.intervalSeconds(charging = true, powerSave = false))
        assertEquals(300, s.intervalSeconds(charging = false, powerSave = false))
        assertEquals(600, s.intervalSeconds(charging = false, powerSave = true))
        val tight = HeartbeatSchedule(minSeconds = 30, maxSeconds = 120, chargingSeconds = 5, onBatterySeconds = 900)
        assertEquals(30, tight.intervalSeconds(true, false))
        assertEquals(120, tight.intervalSeconds(false, false))
    }
}
