package dev.bridge.gateway.diagnostics

/**
 * What diagnostics may show of sensitive values. Classes that hold a credential, a pairing
 * code, a password, a phone number or a message body override `toString` with these, so
 * printing one by accident (a log line, an exception message, a crash report) leaks nothing.
 */
object Redact {
    const val HIDDEN = "<redacted>"

    /** A phone number with all but its last two digits hidden: `***42`. Short values are hidden entirely. */
    fun number(value: String?): String = when {
        value == null -> "null"
        value.length <= 4 -> "***"
        else -> "***" + value.takeLast(2)
    }

    /** A message body as its length only. */
    fun body(value: String?): String = if (value == null) "null" else "<${value.length} chars>"
}
