package dev.bridge.gateway.sms

import android.app.Activity
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.telephony.SmsMessage
import dev.bridge.gateway.container
import dev.bridge.gateway.gateway.Frames
import dev.bridge.gateway.gateway.ReportFrame

/** Receives Android's per-segment send results and carrier delivery reports. */
class SmsResultReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        val messageId = intent.getStringExtra(EXTRA_MESSAGE_ID) ?: return
        val attempt = intent.getIntExtra(EXTRA_ATTEMPT, 0)
        val c = context.container
        when (intent.action) {
            ACTION_SENT -> {
                val ok = resultCode == Activity.RESULT_OK
                when (val outcome = c.outbox.recordSendResult(messageId, attempt, ok, resultCode)) {
                    is SendOutcome.Sent -> c.reports.enqueue(
                        ReportFrame(Frames.SMS_SENT, messageId, attempt, segments = outcome.segments),
                    )
                    is SendOutcome.Failed -> {
                        // The radio's own cause code, when the phone reports one.
                        val f = sendFailure(outcome.code, intent.getIntExtra("errorCode", -1))
                        c.reports.enqueue(
                            ReportFrame(Frames.SMS_FAILED, messageId, attempt, errorCode = f.code, errorMessage = f.message, retryable = f.retryable),
                        )
                    }
                    null -> Unit // more segments to come
                }
            }
            ACTION_DELIVERED -> {
                val pdu = intent.getByteArrayExtra("pdu") ?: return
                val format = intent.getStringExtra("format")
                val status = runCatching { SmsMessage.createFromPdu(pdu, format).status }.getOrDefault(-1)
                when (c.outbox.recordDelivery(messageId, attempt, classifyDelivery(status))) {
                    DeliveryOutcome.Delivered -> c.reports.enqueue(ReportFrame(Frames.SMS_DELIVERY, messageId, attempt, delivered = true))
                    DeliveryOutcome.Failed -> c.reports.enqueue(
                        ReportFrame(
                            Frames.SMS_DELIVERY, messageId, attempt, delivered = false, errorCode = "delivery_failed",
                            errorMessage = "The carrier reported the message undelivered (status 0x${status.toString(16)}).",
                        ),
                    )
                    null -> Unit
                }
            }
        }
    }

    companion object {
        const val ACTION_SENT = "dev.bridge.gateway.SMS_SENT"
        const val ACTION_DELIVERED = "dev.bridge.gateway.SMS_DELIVERED"
        const val EXTRA_MESSAGE_ID = "message_id"
        const val EXTRA_ATTEMPT = "attempt"
    }
}
