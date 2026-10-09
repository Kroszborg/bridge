package dev.bridge.gateway.net

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

// Session-authenticated (dashboard) endpoints. Field names follow packages/api-types/openapi.json.

@Serializable
data class LoginRequest(val email: String, val password: String)

@Serializable
data class UserDto(
    val id: String,
    val email: String,
    val name: String = "",
)

@Serializable
data class OrganizationDto(
    val id: String,
    val name: String,
    val slug: String = "",
    /** owner, admin or member. */
    val role: String = "member",
) {
    val isAdmin: Boolean get() = role == "owner" || role == "admin"
}

@Serializable
data class MeResponse(
    val user: UserDto,
    val organizations: List<OrganizationDto> = emptyList(),
)

@Serializable
data class ProjectDto(
    val id: String,
    @SerialName("organization_id") val organizationId: String,
    val name: String,
    val slug: String = "",
)

@Serializable
data class ListResponse<T>(val data: List<T> = emptyList())

@Serializable
data class PairingTokenDto(
    val id: String,
    val token: String,
    @SerialName("expires_at") val expiresAt: String,
    @SerialName("api_url") val apiUrl: String,
    @SerialName("pairing_uri") val pairingUri: String,
)

@Serializable
data class MessageDto(
    val id: String,
    /** created, queued, sending, sent, delivered, failed or received. */
    val status: String,
    val direction: String = "outbound",
    val purpose: String = "message",
    val environment: String = "live",
    val to: String = "",
    val from: String? = null,
    val body: String? = null,
    @SerialName("body_redacted") val bodyRedacted: Boolean = false,
    val segments: Int? = null,
    val provider: String = "android",
    @SerialName("device_id") val deviceId: String? = null,
    @SerialName("sim_slot") val simSlot: Int? = null,
    @SerialName("error_code") val errorCode: String? = null,
    @SerialName("error_message") val errorMessage: String? = null,
    @SerialName("created_at") val createdAt: String,
) {
    val isInbound: Boolean get() = direction == "inbound"

    /** No further status changes are expected. */
    val isFinal: Boolean get() = status in FINAL_STATUSES

    /** The other party: the recipient of an outbound message, the sender of an inbound one. */
    val counterpart: String get() = if (isInbound) from.orEmpty() else to

    companion object {
        val FINAL_STATUSES = setOf("delivered", "failed", "received")
    }
}

@Serializable
data class MessageList(
    val data: List<MessageDto> = emptyList(),
    @SerialName("has_more") val hasMore: Boolean = false,
)

@Serializable
data class SendMessageBody(
    val to: String,
    val message: String,
    @SerialName("device_id") val deviceId: String? = null,
    @SerialName("sim_slot") val simSlot: Int? = null,
)

@Serializable
data class DeviceSimDto(
    val slot: Int,
    val carrier: String? = null,
    @SerialName("display_name") val displayName: String? = null,
)

@Serializable
data class DeviceDto(
    val id: String,
    val name: String,
    /** online, offline or disabled (removed). */
    val status: String,
    @SerialName("last_seen_at") val lastSeenAt: String? = null,
    @SerialName("battery_level") val batteryLevel: Int? = null,
    @SerialName("is_charging") val isCharging: Boolean? = null,
    @SerialName("network_type") val networkType: String? = null,
    @SerialName("carrier_name") val carrierName: String? = null,
    @SerialName("device_model") val deviceModel: String? = null,
    @SerialName("app_version") val appVersion: String? = null,
    @SerialName("app_flavor") val appFlavor: String? = null,
    val sims: List<DeviceSimDto> = emptyList(),
    @SerialName("total_sent") val totalSent: Int = 0,
    @SerialName("total_failed") val totalFailed: Int = 0,
    @SerialName("revoked_at") val revokedAt: String? = null,
) {
    val isRemoved: Boolean get() = status == "disabled" || revokedAt != null
}

@Serializable
data class UpdateDeviceBody(val name: String? = null)

/** `GET /v1/organizations/{id}/billing`. Hosted Bridge only; self-hosted servers answer 404 or `enabled: false`. */
@Serializable
data class BillingDto(
    val enabled: Boolean = false,
    val plan: PlanDto? = null,
    val status: String = "",
    @SerialName("current_period_end") val currentPeriodEnd: String? = null,
    val usage: PlanUsageDto = PlanUsageDto(),
    @SerialName("period_start") val periodStart: String? = null,
    @SerialName("period_end") val periodEnd: String? = null,
)

@Serializable
data class PlanDto(
    /** free, pro, business or self_hosted. */
    val id: String,
    val name: String,
    @SerialName("price_cents") val priceCents: Int = 0,
    val currency: String = "USD",
    val limits: PlanLimitsDto = PlanLimitsDto(),
)

/** Null means unlimited. */
@Serializable
data class PlanLimitsDto(
    val phones: Int? = null,
    @SerialName("live_messages") val liveMessages: Int? = null,
    val projects: Int? = null,
    val members: Int? = null,
)

@Serializable
data class PlanUsageDto(
    val phones: Int = 0,
    @SerialName("live_messages") val liveMessages: Int = 0,
    val projects: Int = 0,
    val members: Int = 0,
)
