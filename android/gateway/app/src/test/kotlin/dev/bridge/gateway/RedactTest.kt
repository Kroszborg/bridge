package dev.bridge.gateway

import dev.bridge.gateway.gateway.GatewaySession
import dev.bridge.gateway.gateway.HeartbeatSchedule
import dev.bridge.gateway.gateway.InboundSmsFrame
import dev.bridge.gateway.gateway.ServerFrame
import dev.bridge.gateway.net.LoginRequest
import dev.bridge.gateway.pairing.PairingRequest
import dev.bridge.gateway.sms.SendJob
import dev.bridge.gateway.sms.SmsPart
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/** Secrets, phone numbers and message bodies never appear when these objects are printed. */
class RedactTest {
    private val token = "bp_" + "S".repeat(43)
    private val number = "+15550123456"
    private val body = "Your code is 918273"

    private fun assertHidden(printed: String, vararg secrets: String) {
        for (secret in secrets) assertFalse("$printed shows $secret", printed.contains(secret))
    }

    @Test
    fun `credentials and pairing codes are hidden`() {
        assertHidden(PairingRequest("https://api.example.com", token).toString(), token)
        assertHidden(LoginRequest("me@example.com", "hunter2-password").toString(), "hunter2-password")
        val session = GatewaySession("wss://api.example.com/v1/device/ws", "bdc_secret_credential", HeartbeatSchedule(15, 600, 60, 300)).toString()
        assertHidden(session, "bdc_secret_credential")
        assertTrue(session.contains("api.example.com"))
    }

    @Test
    fun `phone numbers keep only their last two digits and bodies only their length`() {
        val job = SendJob("msg_1", 1, number, body, simSlot = null).toString()
        assertHidden(job, number, "0123456", body)
        assertTrue(job.contains("***56") && job.contains("<19 chars>") && job.contains("msg_1"))
        assertHidden(SmsPart(number, body, 0).toString(), number, body)
        assertHidden(InboundSmsFrame("in_1", number, body, 0).toString(), number, body)
        assertHidden(ServerFrame(type = "send_sms", messageId = "msg_1", to = number, body = body).toString(), number, body)
    }
}
