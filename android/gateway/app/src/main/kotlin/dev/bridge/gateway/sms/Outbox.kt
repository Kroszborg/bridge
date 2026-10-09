package dev.bridge.gateway.sms

import android.content.ContentValues
import android.content.Context
import android.database.sqlite.SQLiteDatabase
import android.database.sqlite.SQLiteOpenHelper
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update

/** A send job as received from the server. */
data class SendJob(
    val messageId: String,
    val attempt: Int,
    val to: String,
    val body: String,
    val simSlot: Int?,
)

/** A report waiting for the server's acknowledgement. */
data class PendingReport(val messageId: String, val type: String, val payload: String)

/** The outcome once every segment of a message has a send result. */
sealed interface SendOutcome {
    data class Sent(val segments: Int) : SendOutcome
    data class Failed(val code: Int) : SendOutcome
}

/** The newest message this phone handled: its state (see [Outbox] STATE_*) and when that last changed. */
data class LastMessage(val state: String, val updatedAt: Long)

/** The outcome once every segment has a final delivery report. */
sealed interface DeliveryOutcome {
    data object Delivered : DeliveryOutcome
    data object Failed : DeliveryOutcome
}

/**
 * Durable record of jobs and unacknowledged reports, so neither a process
 * restart nor a dropped connection loses a status or sends a message twice.
 * Jobs are keyed by (message ID, attempt): a redelivered job is recognised,
 * while a deliberate retry (a new attempt) is sent again.
 */
class Outbox(context: Context, name: String? = "outbox.db") :
    SQLiteOpenHelper(context, name, null, VERSION) {
    private val _changes = MutableStateFlow(0L)

    /** Bumps on every change to a job, so the widget can refresh without polling. */
    val changes: StateFlow<Long> = _changes.asStateFlow()

    private fun changed() = _changes.update { it + 1 }

    override fun onCreate(db: SQLiteDatabase) {
        db.execSQL(
            """
            CREATE TABLE jobs (
                message_id      TEXT NOT NULL,
                attempt         INTEGER NOT NULL,
                recipient       TEXT NOT NULL,
                body            TEXT,
                sim_slot        INTEGER,
                parts_total     INTEGER NOT NULL DEFAULT 0,
                parts_sent      INTEGER NOT NULL DEFAULT 0,
                parts_failed    INTEGER NOT NULL DEFAULT 0,
                first_error     INTEGER,
                parts_delivered INTEGER NOT NULL DEFAULT 0,
                parts_undelivered INTEGER NOT NULL DEFAULT 0,
                state           TEXT NOT NULL,
                created_at      INTEGER NOT NULL,
                updated_at      INTEGER NOT NULL,
                PRIMARY KEY (message_id, attempt)
            )
            """.trimIndent(),
        )
        db.execSQL(
            """
            CREATE TABLE reports (
                message_id TEXT NOT NULL,
                type       TEXT NOT NULL,
                payload    TEXT NOT NULL,
                created_at INTEGER NOT NULL,
                PRIMARY KEY (message_id, type)
            )
            """.trimIndent(),
        )
    }

    override fun onUpgrade(db: SQLiteDatabase, oldVersion: Int, newVersion: Int) = Unit

    /** Records a new job. Returns false if this (message, attempt) was already handled. */
    @Synchronized
    fun insertJob(job: SendJob, now: Long = System.currentTimeMillis()): Boolean {
        val values = ContentValues().apply {
            put("message_id", job.messageId)
            put("attempt", job.attempt)
            put("recipient", job.to)
            put("body", job.body)
            job.simSlot?.let { put("sim_slot", it) }
            put("state", STATE_ACCEPTED)
            put("created_at", now)
            put("updated_at", now)
        }
        return (writableDatabase.insertWithOnConflict("jobs", null, values, SQLiteDatabase.CONFLICT_IGNORE) != -1L).also { if (it) changed() }
    }

    /** Records how many segments Android will send, and drops the body: it is no longer needed. */
    @Synchronized
    fun markDispatched(messageId: String, attempt: Int, parts: Int) {
        writableDatabase.execSQL(
            "UPDATE jobs SET parts_total = ?, body = NULL, state = ?, updated_at = ? WHERE message_id = ? AND attempt = ?",
            arrayOf<Any>(parts, STATE_SENDING, System.currentTimeMillis(), messageId, attempt),
        )
        changed()
    }

    /** Applies one segment's send result. Returns the outcome when the last segment reports. */
    @Synchronized
    fun recordSendResult(messageId: String, attempt: Int, ok: Boolean, resultCode: Int): SendOutcome? {
        val db = writableDatabase
        val column = if (ok) "parts_sent" else "parts_failed"
        db.execSQL(
            "UPDATE jobs SET $column = $column + 1, first_error = COALESCE(first_error, ?), updated_at = ? " +
                "WHERE message_id = ? AND attempt = ? AND state = ?",
            arrayOf<Any?>(if (ok) null else resultCode, System.currentTimeMillis(), messageId, attempt, STATE_SENDING),
        )
        db.rawQuery(
            "SELECT parts_total, parts_sent, parts_failed, first_error FROM jobs WHERE message_id = ? AND attempt = ? AND state = ?",
            arrayOf(messageId, attempt.toString(), STATE_SENDING),
        ).use { c ->
            if (!c.moveToFirst()) return null
            val total = c.getInt(0)
            val sent = c.getInt(1)
            val failed = c.getInt(2)
            if (total == 0 || sent + failed < total) return null
            val outcome = if (failed > 0) SendOutcome.Failed(c.getInt(3)) else SendOutcome.Sent(total)
            setState(messageId, attempt, if (failed > 0) STATE_FAILED else STATE_SENT)
            return outcome
        }
    }

    /** Applies one segment's delivery report. Returns the outcome when the last one is final. */
    @Synchronized
    fun recordDelivery(messageId: String, attempt: Int, delivery: DeliveryClass): DeliveryOutcome? {
        if (delivery == DeliveryClass.Pending || delivery == DeliveryClass.Unknown) return null
        val db = writableDatabase
        val column = if (delivery == DeliveryClass.Delivered) "parts_delivered" else "parts_undelivered"
        db.execSQL(
            "UPDATE jobs SET $column = $column + 1, updated_at = ? WHERE message_id = ? AND attempt = ? AND state = ?",
            arrayOf<Any>(System.currentTimeMillis(), messageId, attempt, STATE_SENT),
        )
        db.rawQuery(
            "SELECT parts_total, parts_delivered, parts_undelivered FROM jobs WHERE message_id = ? AND attempt = ? AND state = ?",
            arrayOf(messageId, attempt.toString(), STATE_SENT),
        ).use { c ->
            if (!c.moveToFirst()) return null
            val total = c.getInt(0)
            val delivered = c.getInt(1)
            val undelivered = c.getInt(2)
            if (delivered + undelivered < total) return null
            val outcome = if (undelivered > 0) DeliveryOutcome.Failed else DeliveryOutcome.Delivered
            setState(messageId, attempt, if (undelivered > 0) STATE_UNDELIVERED else STATE_DELIVERED)
            return outcome
        }
    }

    /** Marks a job failed before Android ever took it (no permission, missing SIM, exception). */
    @Synchronized
    fun markFailed(messageId: String, attempt: Int) {
        writableDatabase.execSQL(
            "UPDATE jobs SET body = NULL WHERE message_id = ? AND attempt = ?",
            arrayOf<Any>(messageId, attempt),
        )
        setState(messageId, attempt, STATE_FAILED)
    }

    private fun setState(messageId: String, attempt: Int, state: String) {
        writableDatabase.execSQL(
            "UPDATE jobs SET state = ?, updated_at = ? WHERE message_id = ? AND attempt = ?",
            arrayOf<Any>(state, System.currentTimeMillis(), messageId, attempt),
        )
        changed()
    }

    @Synchronized
    fun state(messageId: String, attempt: Int): String? =
        readableDatabase.rawQuery(
            "SELECT state FROM jobs WHERE message_id = ? AND attempt = ?",
            arrayOf(messageId, attempt.toString()),
        ).use { if (it.moveToFirst()) it.getString(0) else null }

    /** Stores a report until the server acknowledges it; a newer report of the same type replaces it. */
    @Synchronized
    fun addReport(report: PendingReport) {
        val values = ContentValues().apply {
            put("message_id", report.messageId)
            put("type", report.type)
            put("payload", report.payload)
            put("created_at", System.currentTimeMillis())
        }
        writableDatabase.insertWithOnConflict("reports", null, values, SQLiteDatabase.CONFLICT_REPLACE)
    }

    @Synchronized
    fun pendingReports(): List<PendingReport> =
        readableDatabase.rawQuery("SELECT message_id, type, payload FROM reports ORDER BY created_at", null).use { c ->
            buildList { while (c.moveToNext()) add(PendingReport(c.getString(0), c.getString(1), c.getString(2))) }
        }

    @Synchronized
    fun ackReport(messageId: String, type: String) {
        writableDatabase.delete("reports", "message_id = ? AND type = ?", arrayOf(messageId, type))
    }

    /** Messages this phone finished since [sinceMs]: (sent or delivered, failed). */
    @Synchronized
    fun countsSince(sinceMs: Long): Pair<Int, Int> =
        readableDatabase.rawQuery(
            "SELECT " +
                "COALESCE(SUM(CASE WHEN state IN ('$STATE_SENT', '$STATE_DELIVERED', '$STATE_UNDELIVERED') THEN 1 ELSE 0 END), 0), " +
                "COALESCE(SUM(CASE WHEN state = '$STATE_FAILED' THEN 1 ELSE 0 END), 0) " +
                "FROM jobs WHERE updated_at >= ?",
            arrayOf(sinceMs.toString()),
        ).use { c -> if (c.moveToFirst()) c.getInt(0) to c.getInt(1) else 0 to 0 }

    /** The most recently updated job, or null when this phone has handled none. */
    @Synchronized
    fun lastMessage(): LastMessage? =
        readableDatabase.rawQuery("SELECT state, updated_at FROM jobs ORDER BY updated_at DESC LIMIT 1", null).use { c ->
            if (c.moveToFirst()) LastMessage(c.getString(0), c.getLong(1)) else null
        }

    /** Forgets finished jobs older than [olderThanMs]. Reports are kept until acknowledged. */
    @Synchronized
    fun prune(olderThanMs: Long) {
        writableDatabase.delete(
            "jobs",
            "updated_at < ? AND state IN ('$STATE_FAILED', '$STATE_DELIVERED', '$STATE_UNDELIVERED', '$STATE_SENT')",
            arrayOf(olderThanMs.toString()),
        )
    }

    @Synchronized
    fun clear() {
        writableDatabase.delete("jobs", null, null)
        writableDatabase.delete("reports", null, null)
        changed()
    }

    companion object {
        private const val VERSION = 1
        const val STATE_ACCEPTED = "accepted"
        const val STATE_SENDING = "sending"
        const val STATE_SENT = "sent"
        const val STATE_FAILED = "failed"
        const val STATE_DELIVERED = "delivered"
        const val STATE_UNDELIVERED = "undelivered"
    }
}
