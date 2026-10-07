package dev.bridge.gateway.net

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json

/** JSON settings shared by the REST client and the WebSocket protocol. */
val BridgeJson = Json {
    ignoreUnknownKeys = true // newer servers may add fields
    explicitNulls = false // omit unknown status values instead of sending null
    encodeDefaults = true
}

@Serializable
data class PairRequest(
    val token: String,
    @SerialName("installation_id") val installationId: String,
    val name: String? = null,
    @SerialName("device_model") val deviceModel: String,
    @SerialName("android_version") val androidVersion: String,
    @SerialName("app_version") val appVersion: String,
    @SerialName("app_flavor") val appFlavor: String,
)

@Serializable
data class HeartbeatPolicyDto(
    @SerialName("min_seconds") val minSeconds: Int = 15,
    @SerialName("max_seconds") val maxSeconds: Int = 600,
    @SerialName("charging_seconds") val chargingSeconds: Int = 60,
    @SerialName("on_battery_seconds") val onBatterySeconds: Int = 300,
)

@Serializable
data class UnifiedPushConfigDto(@SerialName("vapid_public_key") val vapidPublicKey: String)

@Serializable
data class FcmConfigDto(
    @SerialName("project_id") val projectId: String,
    @SerialName("app_id") val appId: String,
    @SerialName("api_key") val apiKey: String,
    @SerialName("sender_id") val senderId: String,
)

@Serializable
data class PushConfigDto(
    val unifiedpush: UnifiedPushConfigDto,
    val fcm: FcmConfigDto? = null,
)

@Serializable
data class PairResponse(
    @SerialName("device_id") val deviceId: String,
    @SerialName("project_id") val projectId: String,
    @SerialName("project_name") val projectName: String,
    val credential: String,
    @SerialName("api_url") val apiUrl: String,
    @SerialName("websocket_url") val websocketUrl: String,
    val heartbeat: HeartbeatPolicyDto = HeartbeatPolicyDto(),
    val push: PushConfigDto,
)

/** Device health reported with every heartbeat. Null fields are omitted. */
@Serializable
data class DeviceStatus(
    @SerialName("battery_level") val batteryLevel: Int? = null,
    @SerialName("is_charging") val isCharging: Boolean? = null,
    @SerialName("network_type") val networkType: String? = null,
    @SerialName("carrier_name") val carrierName: String? = null,
    @SerialName("sim_count") val simCount: Int? = null,
    @SerialName("device_model") val deviceModel: String? = null,
    @SerialName("android_version") val androidVersion: String? = null,
    @SerialName("app_version") val appVersion: String? = null,
    val sims: List<SimInfo>? = null,
)

/** A SIM slot. Phone numbers are never read or sent. */
@Serializable
data class SimInfo(
    val slot: Int,
    val carrier: String? = null,
    @SerialName("display_name") val displayName: String? = null,
)

@Serializable
data class HeartbeatRequest(
    @SerialName("next_in") val nextIn: Int,
    val status: DeviceStatus,
)

@Serializable
data class HeartbeatResponse(
    @SerialName("server_time") val serverTime: String,
    @SerialName("pending_jobs") val pendingJobs: Int = 0,
    @SerialName("forward_inbound") val forwardInbound: Boolean? = null,
)

@Serializable
data class PushRegistration(
    val provider: String,
    val endpoint: String,
    val p256dh: String? = null,
    val auth: String? = null,
)

@Serializable
data class SelfDevice(
    val id: String,
    val name: String,
    val status: String,
    @SerialName("project_name") val projectName: String,
)

@Serializable
data class ErrorEnvelope(val error: ErrorBody)

@Serializable
data class ErrorBody(
    val code: String,
    val message: String,
    @SerialName("request_id") val requestId: String? = null,
)
