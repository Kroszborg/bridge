package dev.bridge.gateway.net

import kotlinx.serialization.KSerializer
import kotlinx.serialization.serializer
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Response

/**
 * REST client for the signed-in user's endpoints, the same ones the dashboard uses.
 * The `bridge_session` cookie comes from the client's cookie jar. OkHttp sends no
 * Origin header, which the server's CSRF check accepts for non-browser clients.
 */
class AccountApi(private val http: OkHttpClient) {
    private val jsonType = "application/json".toMediaType()

    suspend fun login(apiUrl: String, email: String, password: String): MeResponse =
        call(post(url(apiUrl, "v1/auth/login"), LoginRequest(email, password), serializer()), serializer())

    suspend fun logout(apiUrl: String) {
        execute(Request.Builder().url(url(apiUrl, "v1/auth/logout")).post(EMPTY_BODY)).close()
    }

    suspend fun me(apiUrl: String): MeResponse = get(url(apiUrl, "v1/me"), serializer())

    suspend fun projects(apiUrl: String, organizationId: String): List<ProjectDto> =
        get(url(apiUrl, "v1/organizations", organizationId, "projects"), serializer<ListResponse<ProjectDto>>()).data

    suspend fun project(apiUrl: String, projectId: String): ProjectDto =
        get(url(apiUrl, "v1/projects", projectId), serializer())

    /** Admin-only: non-admins get 403 `forbidden`. */
    suspend fun createPairingToken(apiUrl: String, projectId: String): PairingTokenDto =
        call(Request.Builder().url(url(apiUrl, "v1/projects", projectId, "pairing-tokens")).post(EMPTY_BODY), serializer())

    suspend fun messages(apiUrl: String, projectId: String, environment: String, limit: Int, startingAfter: String? = null): MessageList {
        val u = url(apiUrl, "v1/projects", projectId, "messages").newBuilder()
            .addQueryParameter("environment", environment)
            .addQueryParameter("limit", limit.toString())
            .apply { if (startingAfter != null) addQueryParameter("starting_after", startingAfter) }
            .build()
        return get(u, serializer())
    }

    suspend fun message(apiUrl: String, projectId: String, messageId: String): MessageDto =
        get(url(apiUrl, "v1/projects", projectId, "messages", messageId), serializer())

    /** The dashboard Playground's send. Live sends are admin-only; a full plan answers 402 `plan_limit_reached`. */
    suspend fun send(apiUrl: String, projectId: String, environment: String, body: SendMessageBody, idempotencyKey: String): MessageDto {
        val u = url(apiUrl, "v1/projects", projectId, "messages").newBuilder().addQueryParameter("environment", environment).build()
        return call(post(u, body, serializer()).header("Idempotency-Key", idempotencyKey), serializer())
    }

    suspend fun devices(apiUrl: String, projectId: String): List<DeviceDto> =
        get(url(apiUrl, "v1/projects", projectId, "devices"), serializer<ListResponse<DeviceDto>>()).data

    suspend fun renameDevice(apiUrl: String, projectId: String, deviceId: String, name: String): DeviceDto {
        val body = BridgeJson.encodeToString(serializer<UpdateDeviceBody>(), UpdateDeviceBody(name)).toRequestBody(jsonType)
        return call(Request.Builder().url(url(apiUrl, "v1/projects", projectId, "devices", deviceId)).patch(body), serializer())
    }

    suspend fun removeDevice(apiUrl: String, projectId: String, deviceId: String) {
        execute(Request.Builder().url(url(apiUrl, "v1/projects", projectId, "devices", deviceId)).delete()).close()
    }

    /** Null when the server has no billing endpoint (self-hosted or older servers). */
    suspend fun billing(apiUrl: String, organizationId: String): BillingDto? = try {
        get(url(apiUrl, "v1/organizations", organizationId, "billing"), serializer())
    } catch (e: BridgeApiException) {
        if (e.status == 404) null else throw e
    }

    private fun url(apiUrl: String, path: String, vararg segments: String): HttpUrl =
        apiUrl.toHttpUrl().newBuilder()
            .addPathSegments(path)
            .apply { segments.forEach { addPathSegment(it) } }
            .build()

    private fun <T> post(url: HttpUrl, body: T, ser: KSerializer<T>): Request.Builder =
        Request.Builder().url(url).post(BridgeJson.encodeToString(ser, body).toRequestBody(jsonType))

    private suspend fun <R> get(url: HttpUrl, ser: KSerializer<R>): R = call(Request.Builder().url(url).get(), ser)

    private suspend fun <R> call(builder: Request.Builder, ser: KSerializer<R>): R =
        execute(builder).use { BridgeJson.decodeFromString(ser, it.body.string()) }

    private suspend fun execute(builder: Request.Builder): Response {
        builder.header("Accept", "application/json")
        val response = http.newCall(builder.build()).await()
        if (!response.isSuccessful) {
            response.use {
                throw BridgeApi.parseError(it.code, it.body.string(), it.header("X-Request-Id"), it.header("Retry-After"))
            }
        }
        return response
    }

    private companion object {
        // Endpoints without a request body, sent like the dashboard sends them.
        val EMPTY_BODY = ByteArray(0).toRequestBody(null)
    }
}
