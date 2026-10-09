package dev.bridge.gateway.diagnostics

import android.app.ActivityManager
import android.app.ApplicationExitInfo
import android.content.ContentValues
import android.content.Context
import android.database.sqlite.SQLiteDatabase
import android.database.sqlite.SQLiteOpenHelper
import android.os.Build
import android.util.Log
import androidx.annotation.RequiresApi
import androidx.core.content.edit
import dev.bridge.gateway.BuildConfig
import dev.bridge.gateway.gateway.DeviceStatusReader
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.concurrent.Executor
import java.util.concurrent.Executors

/** One line of the connection log. */
data class GatewayEvent(val at: Long, val kind: String, val message: String, val warn: Boolean = false)

/**
 * A small persisted log of what the gateway did and why: process starts and how the
 * previous process ended, service starts and stops, connects, closes, retries,
 * network changes, revokes and pairing attempts. It keeps the newest [capacity]
 * events, survives process death and unpairing, and mirrors lines to Logcat under
 * [TAG] (all of them in debug builds; release builds strip Log.i, so only warnings),
 * so a disconnect on a user's phone can be explained afterwards. Messages never
 * contain credentials, pairing codes, phone numbers or message bodies.
 *
 * Writes go to one background thread; [record] never blocks the caller.
 */
class EventLog(
    context: Context,
    name: String? = "events.db",
    private val capacity: Int = 200,
    private val writer: Executor = Executors.newSingleThreadExecutor { r -> Thread(r, "bridge-event-log").apply { isDaemon = true } },
    private val clock: () -> Long = System::currentTimeMillis,
) : SQLiteOpenHelper(context, name, null, VERSION) {
    private val _events = MutableStateFlow<List<GatewayEvent>>(emptyList())

    /** The stored events, oldest first. */
    val events: StateFlow<List<GatewayEvent>> = _events.asStateFlow()

    init {
        writer.execute { _events.value = runCatching { load() }.getOrDefault(emptyList()) }
    }

    override fun onCreate(db: SQLiteDatabase) {
        db.execSQL(
            """
            CREATE TABLE events (
                id      INTEGER PRIMARY KEY AUTOINCREMENT,
                at      INTEGER NOT NULL,
                kind    TEXT NOT NULL,
                message TEXT NOT NULL,
                warn    INTEGER NOT NULL DEFAULT 0
            )
            """.trimIndent(),
        )
    }

    override fun onUpgrade(db: SQLiteDatabase, oldVersion: Int, newVersion: Int) = Unit

    /** Records an event in the background. Safe to call from any thread. */
    fun record(kind: String, message: String, warn: Boolean = false, at: Long = clock()) {
        val event = GatewayEvent(at, kind, message.take(MAX_MESSAGE), warn)
        logcat(event)
        writer.execute { store(event) }
    }

    /** Records an event on the calling thread, for a crash that is about to end the process. */
    fun recordNow(kind: String, message: String) {
        val event = GatewayEvent(clock(), kind, message.take(MAX_MESSAGE), warn = true)
        logcat(event)
        store(event)
    }

    fun clear() {
        writer.execute {
            runCatching { writableDatabase.delete("events", null, null) }
            _events.value = emptyList()
        }
    }

    /** The whole log as plain text, with the facts needed to read it, for pasting into a bug report. */
    fun export(events: List<GatewayEvent> = this.events.value): String = buildString {
        appendLine("Bridge gateway ${BuildConfig.VERSION_NAME} (${BuildConfig.FLAVOR}) · ${DeviceStatusReader.deviceModel()} · Android ${Build.VERSION.RELEASE}")
        appendLine("Exported ${format(clock(), withZone = true)}")
        events.forEach { e -> appendLine("${format(e.at)}  ${e.kind.padEnd(7)} ${if (e.warn) "! " else ""}${e.message}") }
    }

    /**
     * Logs how earlier processes ended (crash, low memory, killed by the system or the
     * phone maker's battery manager), once each. Android keeps these from version 11.
     */
    fun recordPreviousExits(context: Context) {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) writer.execute { runCatching { readExits(context) } }
    }

    @RequiresApi(Build.VERSION_CODES.R)
    private fun readExits(context: Context) {
        val prefs = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
        val seen = prefs.getLong(KEY_LAST_EXIT, 0)
        val exits = context.getSystemService(ActivityManager::class.java)
            .getHistoricalProcessExitReasons(context.packageName, 0, 5)
            .filter { it.timestamp > seen }
            .sortedBy { it.timestamp }
        exits.forEach { exit ->
            val detail = exit.description?.takeIf { it.isNotBlank() }?.let { " · $it" } ?: ""
            val event = GatewayEvent(
                exit.timestamp, APP,
                "Previous process ended: ${exitReason(exit.reason)} while ${importance(exit.importance)}$detail".take(MAX_MESSAGE),
                warn = exit.reason != ApplicationExitInfo.REASON_EXIT_SELF && exit.reason != ApplicationExitInfo.REASON_USER_REQUESTED,
            )
            logcat(event)
            store(event)
        }
        exits.maxOfOrNull { it.timestamp }?.let { latest -> prefs.edit { putLong(KEY_LAST_EXIT, latest) } }
    }

    /** Records uncaught exceptions before Android ends the process. */
    fun installCrashRecorder() {
        val previous = Thread.getDefaultUncaughtExceptionHandler()
        Thread.setDefaultUncaughtExceptionHandler { thread, error ->
            runCatching { recordNow(APP, "Crash on ${thread.name}: ${describe(error)}") }
            previous?.uncaughtException(thread, error)
        }
    }

    private fun store(event: GatewayEvent) {
        runCatching {
            val db = writableDatabase
            db.insert(
                "events", null,
                ContentValues().apply {
                    put("at", event.at)
                    put("kind", event.kind)
                    put("message", event.message)
                    put("warn", if (event.warn) 1 else 0)
                },
            )
            db.execSQL("DELETE FROM events WHERE id <= (SELECT MAX(id) FROM events) - ?", arrayOf<Any>(capacity))
        }
        _events.update { list -> (list + event).sortedBy { it.at }.takeLast(capacity) }
    }

    private fun load(): List<GatewayEvent> =
        readableDatabase.rawQuery("SELECT at, kind, message, warn FROM events ORDER BY at, id", null).use { c ->
            buildList { while (c.moveToNext()) add(GatewayEvent(c.getLong(0), c.getString(1), c.getString(2), c.getInt(3) != 0)) }
        }

    private fun logcat(event: GatewayEvent) {
        val line = "[${event.kind}] ${event.message}"
        if (event.warn) Log.w(TAG, line) else Log.i(TAG, line)
    }

    companion object {
        /** Logcat tag for everything the gateway logs: `adb logcat -s BridgeGateway`. */
        const val TAG = "BridgeGateway"

        const val APP = "app"
        const val SERVICE = "service"
        const val CONNECTION = "conn"
        const val NETWORK = "net"
        const val WORKER = "worker"
        const val PAIRING = "pair"
        const val PUSH = "push"

        private const val VERSION = 1
        private const val MAX_MESSAGE = 600
        private const val PREFS = "diagnostics"
        private const val KEY_LAST_EXIT = "last_exit_logged"

        private val timeFormat = DateTimeFormatter.ofPattern("yyyy-MM-dd HH:mm:ss.SSS")
        private val zonedFormat = DateTimeFormatter.ofPattern("yyyy-MM-dd HH:mm:ss xxx")

        fun format(at: Long, withZone: Boolean = false): String =
            Instant.ofEpochMilli(at).atZone(ZoneId.systemDefault()).format(if (withZone) zonedFormat else timeFormat)

        /** An exception's type, message and the first lines of Bridge's own code it went through. */
        fun describe(error: Throwable): String {
            val root = generateSequence(error) { it.cause }.last()
            val frames = (error.stackTrace.filter { it.className.startsWith("dev.bridge") }.take(3).ifEmpty { error.stackTrace.take(2) })
                .joinToString(" < ") { "${it.className.substringAfterLast('.')}.${it.methodName}:${it.lineNumber}" }
            val cause = if (root !== error) " (cause ${root.javaClass.simpleName}: ${root.message})" else ""
            return "${error.javaClass.name}: ${error.message}$cause at $frames"
        }

        // ApplicationExitInfo.REASON_* values; literals because several are newer than minSdk.
        private fun exitReason(reason: Int): String = when (reason) {
            1 -> "exited itself"
            2 -> "killed by a signal"
            3 -> "killed for low memory"
            4 -> "crashed"
            5 -> "crashed in native code"
            6 -> "not responding (ANR)"
            7 -> "failed to start"
            8 -> "a permission changed"
            9 -> "used too many resources"
            10 -> "stopped by the user"
            11 -> "stopped by the user (force stop)"
            12 -> "a dependency died"
            13 -> "killed by the system or the phone maker's battery manager"
            14 -> "frozen by the system"
            15 -> "the app's state changed (disabled or updated)"
            16 -> "the app was updated"
            else -> "unknown reason ($reason)"
        }

        private fun importance(importance: Int): String = when {
            importance <= ActivityManager.RunningAppProcessInfo.IMPORTANCE_FOREGROUND -> "in the foreground"
            importance <= ActivityManager.RunningAppProcessInfo.IMPORTANCE_FOREGROUND_SERVICE -> "running the gateway service"
            importance <= ActivityManager.RunningAppProcessInfo.IMPORTANCE_VISIBLE -> "visible"
            importance <= ActivityManager.RunningAppProcessInfo.IMPORTANCE_SERVICE -> "running a service"
            importance <= ActivityManager.RunningAppProcessInfo.IMPORTANCE_CACHED -> "in the background"
            else -> "not running ($importance)"
        }
    }
}
