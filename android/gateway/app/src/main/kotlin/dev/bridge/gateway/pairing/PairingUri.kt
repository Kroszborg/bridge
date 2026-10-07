package dev.bridge.gateway.pairing

import java.net.URI
import java.net.URLDecoder

/** A request to pair with a Bridge server, from a QR code, deep link or manual entry. */
data class PairingRequest(val apiUrl: String, val token: String) {
    /** The server is reached over plain HTTP; credentials travel unencrypted. */
    val isCleartext: Boolean get() = apiUrl.startsWith("http://")

    /** Host shown to the user when confirming which server to trust. */
    val host: String get() = runCatching { URI(apiUrl).authority }.getOrNull() ?: apiUrl
}

object PairingUri {
    private const val TOKEN_PREFIX = "bp_"

    /** Parses `bridge://pair?api=<url>&token=<bp_…>`. Returns null for anything else. */
    fun parse(raw: String): PairingRequest? {
        val uri = runCatching { URI(raw.trim()) }.getOrNull() ?: return null
        if (!uri.scheme.equals("bridge", ignoreCase = true) || uri.host != "pair") return null
        val params = uri.rawQuery.orEmpty().split('&').mapNotNull { part ->
            val eq = part.indexOf('=')
            if (eq <= 0) null else decode(part.substring(0, eq)) to decode(part.substring(eq + 1))
        }.toMap()
        val api = normalizeServerUrl(params["api"] ?: return null) ?: return null
        val token = params["token"]?.trim() ?: return null
        if (!isValidToken(token)) return null
        return PairingRequest(api, token)
    }

    /** Builds a request from what the user typed; returns a message on invalid input. */
    fun fromManual(serverUrl: String, code: String): Result<PairingRequest> {
        val api = normalizeServerUrl(serverUrl)
            ?: return Result.failure(IllegalArgumentException("Enter the server address shown in the dashboard, for example https://bridge.example.com."))
        val token = code.trim()
        if (!isValidToken(token)) {
            return Result.failure(IllegalArgumentException("The pairing code starts with bp_. Copy it again from the dashboard."))
        }
        return Result.success(PairingRequest(api, token))
    }

    /** Adds https:// when no scheme is given and drops trailing slashes and fragments. */
    fun normalizeServerUrl(input: String): String? {
        var s = input.trim()
        if (s.isEmpty()) return null
        if (!s.contains("://")) s = "https://$s"
        val uri = runCatching { URI(s) }.getOrNull() ?: return null
        val scheme = uri.scheme?.lowercase()
        if (scheme != "https" && scheme != "http") return null
        if (uri.host.isNullOrBlank() || uri.rawQuery != null || uri.userInfo != null) return null
        val port = if (uri.port == -1) "" else ":${uri.port}"
        val path = uri.rawPath.orEmpty().trimEnd('/')
        return "$scheme://${uri.host}$port$path"
    }

    private fun isValidToken(token: String) =
        token.startsWith(TOKEN_PREFIX) && token.length in 20..128 && token.all { it.isLetterOrDigit() || it == '_' }

    // The Charset overload needs API 33; the String overload works on every supported version.
    private fun decode(s: String): String = URLDecoder.decode(s, "UTF-8")
}
