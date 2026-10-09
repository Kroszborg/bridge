package dev.bridge.gateway

import dev.bridge.gateway.account.SessionCookieJar
import dev.bridge.gateway.net.AccountApi
import dev.bridge.gateway.net.BridgeApiException
import dev.bridge.gateway.net.BridgeJson
import dev.bridge.gateway.net.SendMessageBody
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.jsonObject
import mockwebserver3.MockResponse
import mockwebserver3.MockWebServer
import okhttp3.OkHttpClient
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Before
import org.junit.Test

class AccountApiTest {
    private lateinit var server: MockWebServer
    private var persisted: String? = null
    private val jar = SessionCookieJar({ null }, { persisted = it })
    private val api = AccountApi(OkHttpClient.Builder().cookieJar(jar).build())
    private val base get() = server.url("/").toString().trimEnd('/')

    @Before fun setUp() { server = MockWebServer().apply { start() } }

    @After fun tearDown() { server.close() }

    private fun json(code: Int, body: String, vararg headers: Pair<String, String>) =
        MockResponse.Builder().code(code).body(body).apply { headers.forEach { addHeader(it.first, it.second) } }.build()

    @Test
    fun `login keeps the session cookie and sends it without an Origin header`() = runTest {
        server.enqueue(
            json(
                200,
                """{"user":{"id":"usr_1","email":"ada@example.com","name":"Ada","operator":false,"email_verified":true,"created_at":"2026-01-01T00:00:00Z"},
                "organizations":[{"id":"org_1","name":"Acme","slug":"acme","role":"admin","created_at":"2026-01-01T00:00:00Z"}]}""",
                "Set-Cookie" to "bridge_session=s3cret; Path=/; Max-Age=2592000; HttpOnly; SameSite=Lax",
            ),
        )
        server.enqueue(json(200, """{"data":[{"id":"prj_1","organization_id":"org_1","name":"Default","slug":"default"}]}"""))

        val me = api.login(base, "ada@example.com", "correct horse")
        assertEquals("ada@example.com", me.user.email)
        assertTrue(me.organizations.single().isAdmin)
        assertNotNull("the cookie is persisted for the next start", persisted)

        val projects = api.projects(base, "org_1")
        assertEquals("Default", projects.single().name)

        val login = server.takeRequest()
        assertEquals("/v1/auth/login", login.url.encodedPath)
        val body = BridgeJson.parseToJsonElement(login.body!!.utf8()).jsonObject
        assertEquals(setOf("email", "password"), body.keys)

        val list = server.takeRequest()
        assertEquals("/v1/organizations/org_1/projects", list.url.encodedPath)
        assertEquals("bridge_session=s3cret", list.headers["Cookie"])
        assertNull("CSRF check allows cookie requests without Origin", list.headers["Origin"])
    }

    @Test
    fun `a restored jar signs requests after a restart`() = runTest {
        val me = """{"user":{"id":"usr_1","email":"a@b.co"},"organizations":[]}"""
        server.enqueue(json(200, me, "Set-Cookie" to "bridge_session=abc; Path=/; Max-Age=3600; HttpOnly"))
        server.enqueue(json(200, me))
        api.login(base, "a@b.co", "pw")

        val restored = SessionCookieJar({ persisted }, { })
        AccountApi(OkHttpClient.Builder().cookieJar(restored).build()).me(base)
        server.takeRequest()
        assertEquals("bridge_session=abc", server.takeRequest().headers["Cookie"])
    }

    @Test
    fun `wrong password and rate limits carry their status and retry time`() = runTest {
        server.enqueue(json(401, """{"error":{"code":"unauthenticated","message":"Email or password is incorrect."}}"""))
        server.enqueue(json(429, """{"error":{"code":"rate_limited","message":"Too many requests. Retry after 42 seconds."}}""", "Retry-After" to "42"))
        try {
            api.login(base, "a@b.co", "nope")
            fail("expected an exception")
        } catch (e: BridgeApiException) {
            assertEquals(401, e.status)
            assertFalse(e.isCredentialRejected)
        }
        try {
            api.login(base, "a@b.co", "nope")
            fail("expected an exception")
        } catch (e: BridgeApiException) {
            assertEquals("rate_limited", e.code)
            assertEquals(42, e.retryAfterSeconds)
        }
    }

    @Test
    fun `pairing token, send and plan limit use the dashboard endpoints`() = runTest {
        server.enqueue(
            json(
                201,
                """{"id":"pt_1","token":"bp_${"a".repeat(40)}","expires_at":"2026-10-08T10:10:00Z","api_url":"https://api.example.com",
                "pairing_uri":"bridge://pair?api=https%3A%2F%2Fapi.example.com&token=bp_${"a".repeat(40)}"}""",
            ),
        )
        server.enqueue(
            json(
                202,
                """{"id":"msg_1","status":"queued","direction":"outbound","purpose":"message","environment":"live","to":"+919876543210",
                "from":null,"body":"Hi","body_redacted":false,"provider":"android","attempts":0,"metadata":{},"created_at":"2026-10-08T10:00:00.123456Z"}""",
            ),
        )
        server.enqueue(
            json(402, """{"error":{"code":"plan_limit_reached","message":"Your Free plan includes 100 live messages a month."}}"""),
        )

        val token = api.createPairingToken(base, "prj_1")
        assertTrue(token.pairingUri.startsWith("bridge://pair?"))

        val sent = api.send(base, "prj_1", "live", SendMessageBody("+919876543210", "Hi", simSlot = 2), "key-1")
        assertEquals("queued", sent.status)
        assertFalse(sent.isFinal)

        try {
            api.send(base, "prj_1", "live", SendMessageBody("+919876543210", "Hi"), "key-2")
            fail("expected an exception")
        } catch (e: BridgeApiException) {
            assertTrue(e.isPlanLimit)
        }

        assertEquals("/v1/projects/prj_1/pairing-tokens", server.takeRequest().url.encodedPath)
        val send = server.takeRequest()
        assertEquals("/v1/projects/prj_1/messages", send.url.encodedPath)
        assertEquals("live", send.url.queryParameter("environment"))
        assertEquals("key-1", send.headers["Idempotency-Key"])
        val body = BridgeJson.parseToJsonElement(send.body!!.utf8()).jsonObject
        assertEquals(setOf("to", "message", "sim_slot"), body.keys)
    }

    @Test
    fun `billing is optional`() = runTest {
        server.enqueue(json(404, """{"error":{"code":"not_found","message":"Not found."}}"""))
        assertNull(api.billing(base, "org_1"))
        assertEquals("/v1/organizations/org_1/billing", server.takeRequest().url.encodedPath)
    }

    @Test
    fun `messages list asks for the environment and page`() = runTest {
        server.enqueue(
            json(
                200,
                """{"data":[{"id":"msg_2","status":"received","direction":"inbound","to":"","from":"+15550001111","body":"STOP",
                "created_at":"2026-10-08T09:00:00Z"}],"has_more":true}""",
            ),
        )
        val page = api.messages(base, "prj_1", "test", 50, startingAfter = "msg_9")
        assertTrue(page.hasMore)
        assertEquals("+15550001111", page.data.single().counterpart)
        assertTrue(page.data.single().isFinal)
        val req = server.takeRequest()
        assertEquals("test", req.url.queryParameter("environment"))
        assertEquals("50", req.url.queryParameter("limit"))
        assertEquals("msg_9", req.url.queryParameter("starting_after"))
    }
}
