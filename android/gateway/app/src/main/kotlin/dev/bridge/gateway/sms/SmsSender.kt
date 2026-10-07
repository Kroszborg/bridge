package dev.bridge.gateway.sms

import android.Manifest
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import android.telephony.SmsManager
import android.telephony.SubscriptionManager
import android.util.Log
import androidx.core.content.ContextCompat
import androidx.core.net.toUri
import dev.bridge.gateway.AppContainer
import dev.bridge.gateway.gateway.Frames
import dev.bridge.gateway.gateway.ReportFrame
import dev.bridge.gateway.net.BridgeJson

/**
 * Sends SMS through Android and turns every step into a report for the server.
 * Reports go through [ReportPump], which stores them until the server acknowledges.
 */
class SmsSender(
    private val context: Context,
    private val outbox: Outbox,
    private val reports: ReportPump,
) {
    /** Handles a send_sms job. Redelivered jobs are recognised and never sent twice. */
    fun handle(job: SendJob) {
        if (!outbox.insertJob(job)) {
            Log.i(AppContainer.TAG, "Job ${job.messageId} attempt ${job.attempt} already handled; resending its reports")
            reports.flush()
            return
        }
        reports.enqueue(ReportFrame(Frames.SMS_ACCEPTED, job.messageId, job.attempt))
        send(job)
    }

    private fun send(job: SendJob) {
        if (ContextCompat.checkSelfPermission(context, Manifest.permission.SEND_SMS) != PackageManager.PERMISSION_GRANTED) {
            fail(job, "permission_denied", "The Bridge app does not have permission to send SMS on this phone.", retryable = true)
            return
        }
        val manager = smsManager(job.simSlot) ?: return fail(
            job, "sim_unavailable",
            "SIM ${job.simSlot} is not available on this phone, or Bridge lacks Phone access to find it.", retryable = true,
        )
        try {
            val parts = manager.divideMessage(job.body)
            val sent = ArrayList<PendingIntent>(parts.size)
            val delivered = ArrayList<PendingIntent>(parts.size)
            for (i in parts.indices) {
                sent += resultIntent(SmsResultReceiver.ACTION_SENT, job, i, mutable = false)
                // Delivery reports carry the status PDU as an extra, which needs a mutable intent.
                delivered += resultIntent(SmsResultReceiver.ACTION_DELIVERED, job, i, mutable = true)
            }
            outbox.markDispatched(job.messageId, job.attempt, parts.size)
            if (parts.size == 1) {
                manager.sendTextMessage(job.to, null, parts[0], sent[0], delivered[0])
            } else {
                manager.sendMultipartTextMessage(job.to, null, parts, sent, delivered)
            }
        } catch (e: Exception) {
            Log.w(AppContainer.TAG, "Android refused to send ${job.messageId}", e)
            fail(job, "send_error", "Android refused the message: ${e.message ?: e.javaClass.simpleName}", retryable = true)
        }
    }

    private fun fail(job: SendJob, code: String, message: String, retryable: Boolean) {
        outbox.markFailed(job.messageId, job.attempt)
        reports.enqueue(ReportFrame(Frames.SMS_FAILED, job.messageId, job.attempt, errorCode = code, errorMessage = message, retryable = retryable))
    }

    private fun resultIntent(action: String, job: SendJob, part: Int, mutable: Boolean): PendingIntent {
        val intent = Intent(action)
            .setClass(context, SmsResultReceiver::class.java)
            // A unique data URI keeps each segment's PendingIntent distinct.
            .setData("bridge-sms://${job.messageId}/${job.attempt}/$part".toUri())
            .putExtra(SmsResultReceiver.EXTRA_MESSAGE_ID, job.messageId)
            .putExtra(SmsResultReceiver.EXTRA_ATTEMPT, job.attempt)
        val flags = PendingIntent.FLAG_UPDATE_CURRENT or
            if (mutable && Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) PendingIntent.FLAG_MUTABLE
            else if (mutable) 0 else PendingIntent.FLAG_IMMUTABLE
        return PendingIntent.getBroadcast(context, 0, intent, flags)
    }

    /** The SmsManager for a SIM slot (1-based), or the default SMS SIM when [slot] is null. */
    private fun smsManager(slot: Int?): SmsManager? {
        val base: SmsManager = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            context.getSystemService(SmsManager::class.java)
        } else {
            @Suppress("DEPRECATION")
            SmsManager.getDefault()
        }
        if (slot == null) return base
        if (ContextCompat.checkSelfPermission(context, Manifest.permission.READ_PHONE_STATE) != PackageManager.PERMISSION_GRANTED) {
            return null
        }
        val subscriptions = context.getSystemService(SubscriptionManager::class.java) ?: return null
        val info = subscriptions.getActiveSubscriptionInfoForSimSlotIndex(slot - 1) ?: return null
        return if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            base.createForSubscriptionId(info.subscriptionId)
        } else {
            @Suppress("DEPRECATION")
            SmsManager.getSmsManagerForSubscriptionId(info.subscriptionId)
        }
    }
}

/** Stores reports until the server acknowledges them, and sends them whenever connected. */
class ReportPump(private val outbox: Outbox, private val send: (String) -> Boolean) {
    fun enqueue(report: ReportFrame) =
        enqueueRaw(report.messageId, report.type, BridgeJson.encodeToString(ReportFrame.serializer(), report))

    /** Queues any frame the server acknowledges with report_ack(message_id = [id], report = [type]). */
    fun enqueueRaw(id: String, type: String, payload: String) {
        outbox.addReport(PendingReport(id, type, payload))
        flush()
    }

    @Synchronized
    fun flush() {
        for (r in outbox.pendingReports()) {
            if (!send(r.payload)) return // not connected; retried on the next connection
        }
    }

    fun acknowledged(messageId: String, type: String) = outbox.ackReport(messageId, type)
}
