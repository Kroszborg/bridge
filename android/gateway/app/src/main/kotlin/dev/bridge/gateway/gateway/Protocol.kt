package dev.bridge.gateway.gateway

import dev.bridge.gateway.net.BridgeJson
import dev.bridge.gateway.net.DeviceStatus
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/** Frames exchanged with the server over /v1/device/connect (protocol version 1). */
object Frames {
    const val HEARTBEAT = "heartbeat"
    const val WELCOME = "welcome"
    const val HEARTBEAT_ACK = "heartbeat_ack"
    const val SYNC = "sync"
    const val UNPAIRED = "unpaired"
    const val SEND_SMS = "send_sms"
    const val REPORT_ACK = "report_ack"
    const val CONFIG = "config"

    const val SMS_ACCEPTED = "sms_accepted"
    const val SMS_SENT = "sms_sent"
    const val SMS_FAILED = "sms_failed"
    const val SMS_DELIVERY = "sms_delivery"
    const val SMS_RECEIVED = "sms_received"
}

/** WebSocket close codes the server uses. */
object CloseCodes {
    const val NORMAL = 1000
    const val GOING_AWAY = 1001
    const val REPLACED = 4001
    const val UNPAIRED = 4003
    const val HEARTBEAT_TIMEOUT = 4008
    const val ACK_TIMEOUT = 4100 // sent by the app when the server stops answering
}

@Serializable
data class HeartbeatFrame(
    val type: String = Frames.HEARTBEAT,
    val seq: Long,
    @SerialName("next_in") val nextIn: Int,
    val status: DeviceStatus,
)

@Serializable
data class ServerFrame(
    val type: String,
    val seq: Long? = null,
    @SerialName("device_id") val deviceId: String? = null,
    @SerialName("protocol_version") val protocolVersion: Int? = null,
    val reason: String? = null,
    @SerialName("message_id") val messageId: String? = null,
    val to: String? = null,
    val body: String? = null,
    @SerialName("sim_slot") val simSlot: Int? = null,
    val attempt: Int? = null,
    val report: String? = null,
    /** welcome and config: whether to forward the SMS this phone receives. */
    @SerialName("forward_inbound") val forwardInbound: Boolean? = null,
)

/** A message status report. Stored until the server acknowledges it. */
@Serializable
data class ReportFrame(
    val type: String,
    @SerialName("message_id") val messageId: String,
    val attempt: Int,
    val segments: Int? = null,
    @SerialName("error_code") val errorCode: String? = null,
    @SerialName("error_message") val errorMessage: String? = null,
    val retryable: Boolean? = null,
    val delivered: Boolean? = null,
)

/** An SMS this phone received, forwarded while the server has forwarding on. Stored until acknowledged. */
@Serializable
data class InboundSmsFrame(
    @SerialName("inbound_id") val inboundId: String,
    val from: String,
    val body: String,
    @SerialName("received_at") val receivedAt: Long,
    @SerialName("sim_slot") val simSlot: Int? = null,
    val type: String = Frames.SMS_RECEIVED,
) {
    fun encode(): String = BridgeJson.encodeToString(serializer(), this)
}
