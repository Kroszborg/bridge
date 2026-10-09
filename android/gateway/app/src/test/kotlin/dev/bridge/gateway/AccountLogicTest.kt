package dev.bridge.gateway

import dev.bridge.gateway.account.AccountAction
import dev.bridge.gateway.account.AccountErrors
import dev.bridge.gateway.account.PlanSummary
import dev.bridge.gateway.account.ServerUrls
import dev.bridge.gateway.account.SessionCookieJar
import dev.bridge.gateway.net.BillingDto
import dev.bridge.gateway.net.BridgeApi
import dev.bridge.gateway.net.BridgeJson
import kotlinx.serialization.SerializationException
import okhttp3.Cookie
import okhttp3.HttpUrl.Companion.toHttpUrl
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.IOException

class AccountLogicTest {
    private fun api(status: Int, code: String, message: String = "Server says no.", retryAfter: String? = null) =
        BridgeApi.parseError(status, """{"error":{"code":"$code","message":"$message","request_id":"req_1"}}""", null, retryAfter)

    @Test
    fun `sign-in errors read like advice`() {
        val host = "api.example.com"
        assertEquals("Email or password is incorrect.", AccountErrors.describe(api(401, "unauthenticated"), host, AccountAction.SignIn))
        assertEquals(
            "Too many sign-in attempts. Try again in 42 seconds.",
            AccountErrors.describe(api(429, "rate_limited", retryAfter = "42"), host, AccountAction.SignIn),
        )
        assertEquals(
            "Too many sign-in attempts. Try again in 2 minutes.",
            AccountErrors.describe(api(429, "rate_limited", retryAfter = "61"), host, AccountAction.SignIn),
        )
        assertEquals(
            "Too many sign-in attempts. Try again in a few minutes.",
            AccountErrors.describe(api(429, "rate_limited"), host, AccountAction.SignIn),
        )
        assertTrue(AccountErrors.describe(api(404, "not_found"), host, AccountAction.SignIn).startsWith("$host is not a Bridge server"))
        assertTrue(AccountErrors.describe(IOException("timeout"), host, AccountAction.SignIn).startsWith("Could not reach $host"))
        assertTrue(AccountErrors.describe(SerializationException("x"), host, AccountAction.SignIn).contains("did not answer like a Bridge server"))
    }

    @Test
    fun `role, plan and server errors`() {
        val host = "api.example.com"
        assertTrue(AccountErrors.describe(api(403, "forbidden"), host, AccountAction.PairPhone).startsWith("Only organization admins and owners can pair phones"))
        assertTrue(AccountErrors.describe(api(403, "forbidden"), host, AccountAction.Send).contains("live messages"))
        assertEquals(
            "Your plan includes 3 phones.",
            AccountErrors.describe(api(402, "plan_limit_reached", "Your plan includes 3 phones."), host, AccountAction.PairPhone),
        )
        assertEquals("Your session has expired. Sign in again.", AccountErrors.describe(api(401, "unauthenticated"), host, AccountAction.Load))
        assertTrue(AccountErrors.describe(api(500, "internal_error"), host, AccountAction.Load).endsWith("(Request req_1)"))
        assertEquals("Server says no.", AccountErrors.describe(api(409, "opted_out"), host, AccountAction.Send))
    }

    @Test
    fun `dashboard address is derived from the API address`() {
        assertEquals(ServerUrls.HOSTED_DASHBOARD, ServerUrls.dashboardFor("https://api.bridge.kroszborg.co/"))
        assertEquals(ServerUrls.HOSTED_DASHBOARD, ServerUrls.dashboardFor("api.bridge.kroszborg.co"))
        assertEquals("https://app.sms.example.com", ServerUrls.dashboardFor("https://api.sms.example.com"))
        assertEquals("http://192.168.1.20:3000", ServerUrls.dashboardFor("http://192.168.1.20:8080"))
        assertEquals("https://bridge.example.com", ServerUrls.dashboardFor("https://bridge.example.com/api"))
        assertEquals(
            "https://app.example.com/organizations/org_1/billing",
            ServerUrls.billing("https://app.example.com/", "org_1"),
        )
        assertEquals("api.example.com:8443", ServerUrls.host("api.example.com:8443/"))
    }

    @Test
    fun `hosted billing becomes usage lines`() {
        val billing = BridgeJson.decodeFromString(
            BillingDto.serializer(),
            """{"enabled":true,"plan":{"id":"free","name":"Free","price_cents":0,"currency":"USD",
            "limits":{"phones":1,"live_messages":100,"projects":1,"members":null}},"status":"active","current_period_end":null,
            "usage":{"phones":1,"live_messages":42,"projects":1,"members":3},
            "period_start":"2026-10-01T00:00:00Z","period_end":"2026-11-01T00:00:00Z"}""",
        )
        val s = PlanSummary.from(billing)
        assertEquals("Free", s.title)
        assertEquals("Free", s.price)
        assertNull("active is not worth showing", s.status)
        val messages = s.lines.first { it.label.startsWith("Live messages") }
        assertEquals("42 of 100", messages.text)
        assertEquals(0.42f, messages.fraction!!, 0.001f)
        val members = s.lines.first { it.label == "Members" }
        assertTrue(members.unlimited)
        assertNull(members.fraction)
        assertTrue("1 of 1 phones is at the limit", s.anyAtLimit)
    }

    @Test
    fun `self-hosted billing has no limits`() {
        val s = PlanSummary.from(BridgeJson.decodeFromString(BillingDto.serializer(), """{"enabled":false,"usage":{"phones":4}}"""))
        assertTrue(s.selfHosted)
        assertEquals("Self-hosted · no limits", s.title)
        assertTrue(s.lines.isEmpty())
        assertFalse(s.anyAtLimit)
    }

    @Test
    fun `prices are formatted per month`() {
        assertEquals("$19 / month", PlanSummary.price(1900, "USD"))
        assertEquals("$9.50 / month", PlanSummary.price(950, "usd"))
        assertEquals("EUR 12 / month", PlanSummary.price(1200, "eur"))
    }

    @Test
    fun `cookie jar persists, replaces and expires the session`() {
        var now = System.currentTimeMillis() // Cookie.parse stamps Max-Age against the real clock
        var stored: String? = null
        val url = "https://api.example.com/v1/me".toHttpUrl()
        val jar = SessionCookieJar({ null }, { stored = it }, now = { now })

        jar.saveFromResponse(url, listOfNotNull(Cookie.parse(url, "bridge_session=one; Path=/; Max-Age=60; Secure; HttpOnly")))
        assertEquals("one", jar.loadForRequest(url).single().value)
        assertTrue(jar.loadForRequest("https://other.example.com/".toHttpUrl()).isEmpty())

        // Restored from what was persisted, still scoped to the API host only.
        val restored = SessionCookieJar({ stored }, { }, now = { now })
        assertEquals("one", restored.loadForRequest(url).single().value)
        assertTrue(restored.loadForRequest("https://sub.api.example.com/".toHttpUrl()).isEmpty())

        // A refreshed session replaces the old cookie; the logout response clears it.
        jar.saveFromResponse(url, listOfNotNull(Cookie.parse(url, "bridge_session=two; Path=/; Max-Age=60")))
        assertEquals(listOf("two"), jar.loadForRequest(url).map { it.value })
        jar.saveFromResponse(url, listOfNotNull(Cookie.parse(url, "bridge_session=; Path=/; Max-Age=0")))
        assertTrue(jar.loadForRequest(url).isEmpty())
        assertNull(stored)

        // Expiry is enforced on load.
        jar.saveFromResponse(url, listOfNotNull(Cookie.parse(url, "bridge_session=three; Path=/; Max-Age=60")))
        now += 61_000
        assertTrue(jar.loadForRequest(url).isEmpty())
    }
}
