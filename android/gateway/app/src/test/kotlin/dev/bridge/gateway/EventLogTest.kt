package dev.bridge.gateway

import androidx.test.core.app.ApplicationProvider
import dev.bridge.gateway.diagnostics.EventLog
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import java.util.concurrent.Executor

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [36], application = android.app.Application::class)
class EventLogTest {
    private var now = 1_000L
    private lateinit var log: EventLog

    @Before fun setUp() {
        // In memory, writing on the calling thread so the test sees each event at once.
        log = EventLog(ApplicationProvider.getApplicationContext(), name = null, capacity = 5, writer = Executor { it.run() }, clock = { now++ })
    }

    @After fun tearDown() = log.close()

    @Test
    fun `keeps only the newest events`() {
        repeat(8) { log.record(EventLog.CONNECTION, "event $it") }
        assertEquals((3..7).map { "event $it" }, log.events.value.map { it.message })
    }

    @Test
    fun `export is plain text with the device facts and every event`() {
        log.record(EventLog.CONNECTION, "Connected")
        log.record(EventLog.CONNECTION, "Server closed the connection (code 1000)", warn = true)
        val text = log.export()
        assertTrue(text.lineSequence().first().startsWith("Bridge gateway "))
        assertTrue(text.contains("conn    Connected"))
        assertTrue(text.contains("! Server closed the connection (code 1000)"))
    }

    @Test
    fun `describe names the exception and where it came from`() {
        val error = RuntimeException("boom", IllegalStateException("root"))
        val text = EventLog.describe(error)
        assertTrue(text, text.startsWith("java.lang.RuntimeException: boom (cause IllegalStateException: root)"))
    }
}
