package dev.bridge.gateway.account

import dev.bridge.gateway.net.BridgeJson
import kotlinx.serialization.Serializable
import kotlinx.serialization.builtins.ListSerializer
import okhttp3.Cookie
import okhttp3.CookieJar
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull

/**
 * Keeps the API's session cookie across app restarts. [restore] is read on first
 * use, on an OkHttp thread; [persist] receives the serialized cookies (or null
 * when none are left) after every change. The caller stores them encrypted.
 * Only cookies that outlive the process are kept.
 */
class SessionCookieJar(
    restore: () -> String?,
    private val persist: (String?) -> Unit,
    private val now: () -> Long = System::currentTimeMillis,
) : CookieJar {
    private val cookies by lazy { decode(restore()).toMutableList() }

    @Synchronized
    override fun saveFromResponse(url: HttpUrl, cookies: List<Cookie>) {
        if (cookies.isEmpty()) return
        for (cookie in cookies) {
            // A Set-Cookie for the same name, domain and path replaces the old one; an expired one clears it.
            this.cookies.removeAll { it.name == cookie.name && it.domain == cookie.domain && it.path == cookie.path }
            if (cookie.persistent && cookie.expiresAt > now()) this.cookies += cookie
        }
        persist(encode())
    }

    @Synchronized
    override fun loadForRequest(url: HttpUrl): List<Cookie> {
        val t = now()
        if (cookies.removeAll { it.expiresAt <= t }) persist(encode())
        return cookies.filter { it.matches(url) }
    }

    @Synchronized
    fun clear() {
        if (cookies.isEmpty()) return
        cookies.clear()
        persist(null)
    }

    private fun encode(): String? = cookies.takeIf { it.isNotEmpty() }?.let { list ->
        BridgeJson.encodeToString(ListSerializer(StoredCookie.serializer()), list.map { StoredCookie(originOf(it), it.toString()) })
    }

    private fun decode(raw: String?): List<Cookie> {
        if (raw.isNullOrBlank()) return emptyList()
        val stored = runCatching { BridgeJson.decodeFromString(ListSerializer(StoredCookie.serializer()), raw) }.getOrNull() ?: return emptyList()
        val t = now()
        return stored.mapNotNull { s ->
            val url = s.url.toHttpUrlOrNull() ?: return@mapNotNull null
            Cookie.parse(url, s.setCookie)?.takeIf { it.expiresAt > t }
        }
    }

    // Re-parsing a cookie against its own host restores host-only cookies exactly.
    private fun originOf(c: Cookie) = "${if (c.secure) "https" else "http"}://${c.domain}${c.path}"

    @Serializable
    private data class StoredCookie(val url: String, val setCookie: String)
}
