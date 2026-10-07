package dev.bridge.gateway

import dev.bridge.gateway.gateway.HeartbeatFrame
import dev.bridge.gateway.net.BridgeApi
import dev.bridge.gateway.net.BridgeApiException
import dev.bridge.gateway.net.BridgeJson
import dev.bridge.gateway.net.DeviceStatus
import dev.bridge.gateway.net.HeartbeatRequest
import dev.bridge.gateway.net.PairRequest
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.jsonObject
import mockwebserver3.MockResponse
import mockwebserver3.MockWebServer
import okhttp3.OkHttpClient
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Before
import org.junit.Test

class BridgeApiTest {
    private lateinit var server: MockWebServer
    private val api = BridgeApi(OkHttpClient())
    private val base get() = server.url("/").toString().trimEnd('/')

    @Before fun setUp() { server = MockWebServer().apply { start() } }

    @After fun tearDown() { server.close() }

    @Test
    fun `pair sends snake_case fields and parses the response`() = runTest {
        server.enqueue(
            MockResponse.Builder().code(200).body(
                """{"device_id":"dev_1","project_id":"prj_1","project_name":"Default","credential":"bd_x",
                "api_url":"https://a.test","websocket_url":"wss://a.test/v1/device/connect",
                "heartbeat":{"min_seconds":15,"max_seconds":600,"charging_seconds":60,"on_battery_seconds":300},
                "push":{"unifiedpush":{"vapid_public_key":"BKey"}},"unexpected":"ignored"}""",
            ).build(),
        )
        val res = api.pair(base, PairRequest("bp_tok", "install-1", null, "Pixel 7", "15", "0.1.0", "foss"))
        assertEquals("dev_1", res.deviceId)
        assertEquals("BKey", res.push.unifiedpush.vapidPublicKey)
        assertNull(res.push.fcm)

        val req = server.takeRequest()
        assertEquals("/v1/device/pair", req.url.encodedPath)
        val body = BridgeJson.parseToJsonElement(req.body!!.utf8()).jsonObject
        assertTrue(body.containsKey("installation_id"))
        assertTrue(body.containsKey("app_flavor"))
        assertFalse("null name must be omitted", body.containsKey("name"))
        assertNull(req.headers["Authorization"])
    }

    @Test
    fun `errors carry the code, message and request id`() = runTest {
        server.enqueue(
            MockResponse.Builder().code(401).addHeader("X-Request-Id", "req_hdr").body(
                """{"error":{"code":"device_revoked","message":"This device was removed.","request_id":"req_1"}}""",
            ).build(),
        )
        try {
            api.heartbeat(base, "bd_cred", HeartbeatRequest(60, DeviceStatus()))
            fail("expected an exception")
        } catch (e: BridgeApiException) {
            assertEquals(401, e.status)
            assertEquals("device_revoked", e.code)
            assertEquals("req_1", e.requestId)
            assertTrue(e.isCredentialRejected)
        }
        assertEquals("Bearer bd_cred", server.takeRequest().headers["Authorization"])
    }

    @Test
    fun `non-JSON errors still produce a useful exception`() = runTest {
        server.enqueue(MockResponse.Builder().code(502).body("<html>Bad gateway</html>").build())
        try {
            api.self(base, "bd_cred")
            fail("expected an exception")
        } catch (e: BridgeApiException) {
            assertEquals("http_502", e.code)
            assertFalse(e.isCredentialRejected)
        }
    }

    @Test
    fun `heartbeat frame matches the server protocol`() {
        val json = BridgeJson.encodeToString(
            HeartbeatFrame.serializer(),
            HeartbeatFrame(seq = 3, nextIn = 300, status = DeviceStatus(batteryLevel = 80, isCharging = false, networkType = "wifi")),
        )
        val obj = BridgeJson.parseToJsonElement(json).jsonObject
        assertEquals("\"heartbeat\"", obj["type"].toString())
        assertEquals("300", obj["next_in"].toString())
        val status = obj["status"]!!.jsonObject
        assertEquals(setOf("battery_level", "is_charging", "network_type"), status.keys)
    }
}
