package dev.bridge.gateway.account

import dev.bridge.gateway.pairing.PairingUri
import java.net.URI

/** Addresses of the hosted service and how the dashboard is found from an API address. */
object ServerUrls {
    const val HOSTED_API = "https://api.bridge.kroszborg.co"
    const val HOSTED_DASHBOARD = "https://dashboard.bridge.kroszborg.co"
    const val PRIVACY = "https://bridge.kroszborg.co/privacy"
    const val SOURCE = "https://github.com/kroszborg/bridge"

    /**
     * A best guess at the dashboard for an API address; the user can correct it.
     * Hosted Bridge has a fixed pair. Self-hosted installs usually put the API on
     * `api.<domain>` and the dashboard on `app.<domain>` (see docs/self-hosting),
     * and local development uses ports 8080 and 3000. Anything else is assumed to
     * serve both from the same origin.
     */
    fun dashboardFor(apiUrl: String): String {
        val api = PairingUri.normalizeServerUrl(apiUrl) ?: return HOSTED_DASHBOARD
        if (api == HOSTED_API) return HOSTED_DASHBOARD
        val uri = runCatching { URI(api) }.getOrNull() ?: return api
        val host = uri.host.orEmpty()
        val port = if (uri.port == -1) "" else ":${uri.port}"
        return when {
            host.startsWith("api.") -> "${uri.scheme}://app.${host.removePrefix("api.")}$port"
            uri.port == 8080 -> "${uri.scheme}://$host:3000"
            else -> "${uri.scheme}://$host$port"
        }
    }

    /** The host:port shown in messages, for a URL or whatever the user typed. */
    fun host(url: String): String {
        val normalized = PairingUri.normalizeServerUrl(url) ?: return url.ifBlank { "the server" }
        return runCatching { URI(normalized).authority }.getOrNull() ?: normalized
    }

    /** Where the organization's plan is managed. */
    fun billing(dashboardUrl: String, organizationId: String) = "${dashboardUrl.trimEnd('/')}/organizations/$organizationId/billing"
}
