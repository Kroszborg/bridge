package dev.bridge.gateway.gateway

import dev.bridge.gateway.net.BridgeJson
import dev.bridge.gateway.net.ErrorEnvelope
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener

/** WebSocket transport on OkHttp. Liveness is handled by app-level heartbeats, not pings. */
class OkHttpSocketOpener(private val client: OkHttpClient, private val userAgent: String) : SocketOpener {
    override fun open(url: String, credential: String, events: SocketEvents): GatewaySocket {
        val request = Request.Builder()
            .url(url)
            .header("Authorization", "Bearer $credential")
            .header("User-Agent", userAgent)
            .build()
        val ws = client.newWebSocket(request, object : WebSocketListener() {
            override fun onOpen(webSocket: WebSocket, response: Response) = events.onOpen()
            override fun onMessage(webSocket: WebSocket, text: String) = events.onMessage(text)
            override fun onClosing(webSocket: WebSocket, code: Int, reason: String) {
                webSocket.close(code, null)
                events.onClosed(code, reason)
            }
            override fun onClosed(webSocket: WebSocket, code: Int, reason: String) = events.onClosed(code, reason)
            override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
                val code = response?.let { r ->
                    runCatching { r.body.string() }.getOrNull()?.let { body ->
                        runCatching { BridgeJson.decodeFromString(ErrorEnvelope.serializer(), body).error.code }.getOrNull()
                    }
                }
                events.onFailure(t, response?.code, code)
            }
        })
        return object : GatewaySocket {
            override fun send(text: String) = ws.send(text)
            override fun close(code: Int, reason: String) {
                if (!ws.close(code, reason.ifEmpty { null })) ws.cancel()
            }
        }
    }
}
