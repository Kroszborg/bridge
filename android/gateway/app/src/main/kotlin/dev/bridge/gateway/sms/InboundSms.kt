package dev.bridge.gateway.sms

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.provider.Telephony
import android.util.Log
import dev.bridge.gateway.AppContainer
import dev.bridge.gateway.container
import dev.bridge.gateway.diagnostics.Redact
import dev.bridge.gateway.gateway.ConnectionState
import dev.bridge.gateway.gateway.Frames
import dev.bridge.gateway.gateway.GatewayService
import dev.bridge.gateway.gateway.InboundSmsFrame
import dev.bridge.gateway.gateway.SyncWorker
import java.util.UUID

/** One PDU of a received SMS. */
data class SmsPart(val from: String, val body: String, val timestampMs: Long) {
    override fun toString() = "SmsPart(from=${Redact.number(from)}, body=${Redact.body(body)}, timestampMs=$timestampMs)"
}

object InboundSms {
    /**
     * Joins the parts of one broadcast into messages. Android delivers every
     * part of a concatenated SMS in a single broadcast, in order, so parts from
     * the same sender belong to the same message.
     */
    fun group(parts: List<SmsPart>, simSlot: Int?, newId: () -> String = { UUID.randomUUID().toString() }): List<InboundSmsFrame> =
        parts.groupBy { it.from }.map { (from, ps) ->
            InboundSmsFrame(
                inboundId = newId(),
                from = from,
                body = ps.joinToString("") { it.body },
                receivedAt = ps.minOf { it.timestampMs },
                simSlot = simSlot,
            )
        }

    /** The SIM slot (1-based) the broadcast came from, when Android says. */
    fun simSlot(intent: Intent): Int? {
        // SubscriptionManager.EXTRA_SLOT_INDEX (API 30); older builds use vendor extras.
        val index = intent.getIntExtra("android.telephony.extra.SLOT_INDEX", intent.getIntExtra("slot", -1))
        return if (index in 0..1) index + 1 else null
    }
}

/**
 * Receives every SMS the phone gets. Messages are forwarded only while the
 * server has forwarding turned on for this device; otherwise they are ignored
 * and never leave the phone.
 */
class SmsReceivedReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action != Telephony.Sms.Intents.SMS_RECEIVED_ACTION) return
        val parts = runCatching {
            Telephony.Sms.Intents.getMessagesFromIntent(intent).orEmpty().mapNotNull { m ->
                val from = m?.originatingAddress ?: return@mapNotNull null
                SmsPart(from, m.messageBody.orEmpty(), m.timestampMillis)
            }
        }.getOrElse {
            Log.w(AppContainer.TAG, "Could not read an incoming SMS", it)
            return
        }
        if (parts.isEmpty()) return
        val slot = InboundSms.simSlot(intent)
        val app = context.applicationContext
        val pending = goAsync()
        app.container.launch {
            try {
                val c = app.container
                if (c.store.pairing() == null || !c.store.forwardInbound()) return@launch
                for (frame in InboundSms.group(parts, slot)) {
                    c.reports.enqueueRaw(frame.inboundId, Frames.SMS_RECEIVED, frame.encode())
                }
                // Stored until the server acknowledges; make sure a connection is coming.
                if (c.connection.state.value !is ConnectionState.Connected && !GatewayService.start(app, nudge = true)) {
                    SyncWorker.runNow(app)
                }
            } finally {
                pending.finish()
            }
        }
    }
}
