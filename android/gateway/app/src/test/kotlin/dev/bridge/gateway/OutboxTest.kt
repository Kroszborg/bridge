package dev.bridge.gateway

import androidx.test.core.app.ApplicationProvider
import dev.bridge.gateway.gateway.Frames
import dev.bridge.gateway.gateway.ReportFrame
import dev.bridge.gateway.sms.DeliveryClass
import dev.bridge.gateway.sms.DeliveryOutcome
import dev.bridge.gateway.sms.Outbox
import dev.bridge.gateway.sms.PendingReport
import dev.bridge.gateway.sms.ReportPump
import dev.bridge.gateway.sms.SendJob
import dev.bridge.gateway.sms.SendOutcome
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [36], application = android.app.Application::class)
class OutboxTest {
    private lateinit var outbox: Outbox
    private val job = SendJob("msg_1", attempt = 1, to = "+919876543210", body = "hello", simSlot = null)

    @Before fun setUp() {
        outbox = Outbox(ApplicationProvider.getApplicationContext(), name = null) // in-memory
    }

    @After fun tearDown() = outbox.close()

    @Test
    fun `a redelivered job is recognised but a new attempt is accepted`() {
        assertTrue(outbox.insertJob(job))
        assertFalse("same message and attempt must not be sent twice", outbox.insertJob(job))
        assertTrue("a retry is a new attempt", outbox.insertJob(job.copy(attempt = 2)))
    }

    @Test
    fun `send outcome waits for every segment`() {
        outbox.insertJob(job)
        outbox.markDispatched("msg_1", 1, parts = 3)
        assertNull(outbox.recordSendResult("msg_1", 1, ok = true, resultCode = -1))
        assertNull(outbox.recordSendResult("msg_1", 1, ok = true, resultCode = -1))
        assertEquals(SendOutcome.Sent(3), outbox.recordSendResult("msg_1", 1, ok = true, resultCode = -1))
        assertEquals(Outbox.STATE_SENT, outbox.state("msg_1", 1))
        // A stray duplicate result after the outcome changes nothing.
        assertNull(outbox.recordSendResult("msg_1", 1, ok = false, resultCode = 4))
    }

    @Test
    fun `one failed segment fails the message with the first error`() {
        outbox.insertJob(job)
        outbox.markDispatched("msg_1", 1, parts = 2)
        assertNull(outbox.recordSendResult("msg_1", 1, ok = false, resultCode = 4))
        assertEquals(SendOutcome.Failed(4), outbox.recordSendResult("msg_1", 1, ok = true, resultCode = -1))
        assertEquals(Outbox.STATE_FAILED, outbox.state("msg_1", 1))
    }

    @Test
    fun `delivery needs a final report for every segment`() {
        outbox.insertJob(job)
        outbox.markDispatched("msg_1", 1, parts = 2)
        outbox.recordSendResult("msg_1", 1, true, -1)
        outbox.recordSendResult("msg_1", 1, true, -1)
        assertNull(outbox.recordDelivery("msg_1", 1, DeliveryClass.Delivered))
        assertNull("temporary errors are not final", outbox.recordDelivery("msg_1", 1, DeliveryClass.Pending))
        assertEquals(DeliveryOutcome.Delivered, outbox.recordDelivery("msg_1", 1, DeliveryClass.Delivered))
        assertEquals(Outbox.STATE_DELIVERED, outbox.state("msg_1", 1))
    }

    @Test
    fun `undelivered segment marks the message undelivered`() {
        outbox.insertJob(job)
        outbox.markDispatched("msg_1", 1, parts = 1)
        outbox.recordSendResult("msg_1", 1, true, -1)
        assertEquals(DeliveryOutcome.Failed, outbox.recordDelivery("msg_1", 1, DeliveryClass.Failed))
    }

    @Test
    fun `reports are kept until acknowledged and pump stops when offline`() {
        val sent = mutableListOf<String>()
        var online = false
        val pump = ReportPump(outbox) { text -> if (online) sent.add(text) else false }

        pump.enqueue(ReportFrame(Frames.SMS_ACCEPTED, "msg_1", 1))
        pump.enqueue(ReportFrame(Frames.SMS_SENT, "msg_1", 1, segments = 1))
        assertTrue(sent.isEmpty())
        assertEquals(2, outbox.pendingReports().size)

        online = true
        pump.flush()
        assertEquals(2, sent.size)
        assertTrue(sent[0].contains("\"sms_accepted\"") && sent[0].contains("\"attempt\":1"))

        pump.acknowledged("msg_1", Frames.SMS_ACCEPTED)
        assertEquals(listOf(Frames.SMS_SENT), outbox.pendingReports().map(PendingReport::type))
    }

    @Test
    fun `counts and pruning`() {
        outbox.insertJob(job)
        outbox.markDispatched("msg_1", 1, 1)
        outbox.recordSendResult("msg_1", 1, true, -1)
        outbox.insertJob(job.copy(messageId = "msg_2"))
        outbox.markFailed("msg_2", 1)
        assertEquals(1 to 1, outbox.countsSince(0))
        outbox.prune(System.currentTimeMillis() + 1_000)
        assertEquals(0 to 0, outbox.countsSince(0))
    }
}
