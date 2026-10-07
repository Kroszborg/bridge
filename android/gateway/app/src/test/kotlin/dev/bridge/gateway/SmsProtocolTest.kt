package dev.bridge.gateway

import dev.bridge.gateway.gateway.Frames
import dev.bridge.gateway.gateway.ReportFrame
import dev.bridge.gateway.gateway.ServerFrame
import dev.bridge.gateway.net.BridgeJson
import dev.bridge.gateway.sms.DeliveryClass
import dev.bridge.gateway.sms.classifyDelivery
import dev.bridge.gateway.sms.sendFailure
import kotlinx.serialization.json.jsonObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class SmsProtocolTest {
    @Test
    fun `send_sms frames parse`() {
        val f = BridgeJson.decodeFromString(
            ServerFrame.serializer(),
            """{"type":"send_sms","message_id":"msg_1","to":"+919876543210","body":"hi","sim_slot":2,"attempt":3}""",
        )
        assertEquals(Frames.SEND_SMS, f.type)
        assertEquals("msg_1", f.messageId)
        assertEquals(2, f.simSlot)
        assertEquals(3, f.attempt)
    }

    @Test
    fun `report frames omit empty fields and match the server`() {
        val sent = BridgeJson.parseToJsonElement(
            BridgeJson.encodeToString(ReportFrame.serializer(), ReportFrame(Frames.SMS_SENT, "msg_1", 2, segments = 3)),
        ).jsonObject
        assertEquals(setOf("type", "message_id", "attempt", "segments"), sent.keys)

        val failed = BridgeJson.parseToJsonElement(
            BridgeJson.encodeToString(
                ReportFrame.serializer(),
                ReportFrame(Frames.SMS_FAILED, "msg_1", 1, errorCode = "no_service", errorMessage = "x", retryable = true),
            ),
        ).jsonObject
        assertEquals("true", failed["retryable"].toString())
        assertEquals("\"no_service\"", failed["error_code"].toString())
    }

    @Test
    fun `android send errors map to stable codes`() {
        assertEquals("radio_off", sendFailure(2).code)
        assertTrue(sendFailure(4).retryable)
        assertEquals("limit_exceeded", sendFailure(5).code)
        assertFalse("permanent failures are not retried", sendFailure(8).retryable)
        assertEquals("android_error_999", sendFailure(999).code)
        // A generic failure may already have reached the network: never retried, to avoid duplicates.
        assertFalse(sendFailure(1).retryable)
        assertFalse(sendFailure(999).retryable)
        assertTrue("explicit retry from the network", sendFailure(101).retryable)
        assertEquals("network_reject", sendFailure(102).code)
        assertTrue(sendFailure(1, modemError = 38).message.contains("Modem cause 38."))
    }

    @Test
    fun `delivery status classes follow TP-Status`() {
        assertEquals(DeliveryClass.Delivered, classifyDelivery(0x00))
        assertEquals(DeliveryClass.Delivered, classifyDelivery(0x02))
        assertEquals(DeliveryClass.Pending, classifyDelivery(0x20))
        assertEquals(DeliveryClass.Failed, classifyDelivery(0x40))
        assertEquals(DeliveryClass.Failed, classifyDelivery(0x60))
        assertEquals(DeliveryClass.Unknown, classifyDelivery(-1))
    }
}
