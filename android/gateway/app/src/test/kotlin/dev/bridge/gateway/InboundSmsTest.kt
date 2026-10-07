package dev.bridge.gateway

import android.content.Intent
import androidx.test.core.app.ApplicationProvider
import dev.bridge.gateway.gateway.Frames
import dev.bridge.gateway.gateway.InboundSmsFrame
import dev.bridge.gateway.gateway.ServerFrame
import dev.bridge.gateway.net.BridgeJson
import dev.bridge.gateway.sms.InboundSms
import dev.bridge.gateway.sms.Outbox
import dev.bridge.gateway.sms.ReportPump
import dev.bridge.gateway.sms.SmsPart
import kotlinx.serialization.json.jsonObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [36], application = android.app.Application::class)
class InboundSmsTest {
    @Test
    fun `parts of a concatenated SMS become one message per sender`() {
        var n = 0
        val frames = InboundSms.group(
            listOf(
                SmsPart("AX-HDFCBK", "Your OTP is 482913. ", 2_000),
                SmsPart("+919800000001", "hi", 1_500),
                SmsPart("AX-HDFCBK", "Do not share it.", 2_100),
            ),
            simSlot = 2,
            newId = { "in-${++n}" },
        )
        assertEquals(2, frames.size)
        val bank = frames.first { it.from == "AX-HDFCBK" }
        assertEquals("Your OTP is 482913. Do not share it.", bank.body)
        assertEquals(2_000, bank.receivedAt)
        assertEquals(2, bank.simSlot)
        assertEquals(2, frames.map { it.inboundId }.toSet().size)
    }

    @Test
    fun `the frame matches the server protocol`() {
        val json = BridgeJson.parseToJsonElement(
            InboundSmsFrame("in-1", "AX-HDFCBK", "code 1234", 1_730_000_000_000, simSlot = null).encode(),
        ).jsonObject
        assertEquals(setOf("type", "inbound_id", "from", "body", "received_at"), json.keys)
        assertEquals("\"${Frames.SMS_RECEIVED}\"", json["type"].toString())
    }

    @Test
    fun `sim slot comes from the broadcast when Android names it`() {
        assertEquals(2, InboundSms.simSlot(Intent().putExtra("android.telephony.extra.SLOT_INDEX", 1)))
        assertEquals(1, InboundSms.simSlot(Intent().putExtra("slot", 0)))
        assertNull(InboundSms.simSlot(Intent()))
    }

    @Test
    fun `forwarding setting arrives in welcome and config frames`() {
        val welcome = BridgeJson.decodeFromString(ServerFrame.serializer(), """{"type":"welcome","forward_inbound":true}""")
        assertEquals(true, welcome.forwardInbound)
        val config = BridgeJson.decodeFromString(ServerFrame.serializer(), """{"type":"config","forward_inbound":false}""")
        assertEquals(false, config.forwardInbound)
        assertNull(BridgeJson.decodeFromString(ServerFrame.serializer(), """{"type":"welcome"}""").forwardInbound)
    }

    @Test
    fun `an incoming SMS is kept until the server acknowledges it`() {
        val outbox = Outbox(ApplicationProvider.getApplicationContext(), name = null)
        val sent = mutableListOf<String>()
        var connected = false
        val pump = ReportPump(outbox) { if (connected) sent.add(it) else false }
        val frame = InboundSmsFrame("in-9", "+919800000001", "hello", 1_000)

        pump.enqueueRaw(frame.inboundId, Frames.SMS_RECEIVED, frame.encode())
        assertTrue("offline: nothing sent yet", sent.isEmpty())
        connected = true
        pump.flush()
        pump.flush()
        assertEquals("resent until acknowledged", 2, sent.size)
        pump.acknowledged("in-9", Frames.SMS_RECEIVED)
        pump.flush()
        assertEquals(2, sent.size)
        outbox.close()
    }
}
