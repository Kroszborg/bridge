package dev.bridge.gateway.net

import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.serialization.KSerializer
import kotlinx.serialization.serializer
import okhttp3.Call
import okhttp3.Callback
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Response
import java.io.IOException
import kotlin.coroutines.resume
import kotlin.coroutines.resumeWithException

/** An error response from the Bridge API, with its stable code and request ID. */
class BridgeApiException(
    val status: Int,
    val code: String,
    override val message: String,
    val requestId: String?,
) : IOException(message) {
    /** The server no longer accepts this device's credential; the app must forget it. */
    val isCredentialRejected: Boolean
        get() = status == 401 && code in REJECTED_CODES

    companion object {
        val REJECTED_CODES = setOf("device_revoked", "invalid_device_credential")
    }
}

/** REST client for the device-facing Bridge endpoints. */
class BridgeApi(private val http: OkHttpClient) {
    private val jsonType = "application/json".toMediaType()

    suspend fun pair(apiUrl: String, request: PairRequest): PairResponse =
        call(post("$apiUrl/v1/device/pair", request, serializer()), null, serializer())

    suspend fun heartbeat(apiUrl: String, credential: String, request: HeartbeatRequest): HeartbeatResponse =
        call(post("$apiUrl/v1/device/heartbeat", request, serializer()), credential, serializer())

    suspend fun registerPush(apiUrl: String, credential: String, registration: PushRegistration) {
        val body = BridgeJson.encodeToString(serializer<PushRegistration>(), registration).toRequestBody(jsonType)
        callUnit(Request.Builder().url("$apiUrl/v1/device/push").put(body), credential)
    }

    suspend fun self(apiUrl: String, credential: String): SelfDevice =
        call(Request.Builder().url("$apiUrl/v1/device").get(), credential, serializer())

    suspend fun unpair(apiUrl: String, credential: String) {
        callUnit(Request.Builder().url("$apiUrl/v1/device").delete(), credential)
    }

    private fun <T> post(url: String, body: T, ser: KSerializer<T>): Request.Builder =
        Request.Builder().url(url).post(json(body, ser))

    private fun <T> json(body: T, ser: KSerializer<T>): RequestBody =
        BridgeJson.encodeToString(ser, body).toRequestBody(jsonType)

    private suspend fun <R> call(builder: Request.Builder, credential: String?, ser: KSerializer<R>): R {
        execute(builder, credential).use { response ->
            val text = response.body.string()
            return BridgeJson.decodeFromString(ser, text)
        }
    }

    private suspend fun callUnit(builder: Request.Builder, credential: String?) {
        execute(builder, credential).close()
    }

    private suspend fun execute(builder: Request.Builder, credential: String?): Response {
        builder.header("Accept", "application/json")
        if (credential != null) builder.header("Authorization", "Bearer $credential")
        val response = http.newCall(builder.build()).await()
        if (!response.isSuccessful) {
            response.use { throw parseError(it.code, it.body.string(), it.header("X-Request-Id")) }
        }
        return response
    }

    companion object {
        fun parseError(status: Int, body: String, requestId: String?): BridgeApiException {
            val parsed = runCatching { BridgeJson.decodeFromString(ErrorEnvelope.serializer(), body).error }.getOrNull()
            return BridgeApiException(
                status = status,
                code = parsed?.code ?: "http_$status",
                message = parsed?.message ?: "The server returned HTTP $status.",
                requestId = parsed?.requestId ?: requestId,
            )
        }
    }
}

/** Suspends until the call completes; cancelling the coroutine cancels the request. */
suspend fun Call.await(): Response = suspendCancellableCoroutine { cont ->
    cont.invokeOnCancellation { cancel() }
    enqueue(object : Callback {
        override fun onResponse(call: Call, response: Response) = cont.resume(response) { _, _, _ -> response.close() }
        override fun onFailure(call: Call, e: IOException) = cont.resumeWithException(e)
    })
}
