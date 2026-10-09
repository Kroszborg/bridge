package dev.bridge.gateway.gateway

import android.content.Context
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update

/**
 * Tracks whether the default network can reach the internet. The connection
 * manager waits on this instead of polling, so an offline phone stays idle.
 *
 * Only the current default network counts: during a Wi-Fi/mobile handover some
 * phones report the old network lost after the new one is already up, and
 * treating that late loss as "offline" used to strand the connection.
 */
class NetworkMonitor(context: Context, private val log: (String) -> Unit = {}) {
    private val connectivity = context.getSystemService(ConnectivityManager::class.java)
    private val _available = MutableStateFlow(currentlyAvailable())
    val available: StateFlow<Boolean> = _available.asStateFlow()

    private val _changes = MutableStateFlow(0L)

    /** Bumps when the default network switches or Android stops blocking it; a live socket may be on the old one. */
    val changes: StateFlow<Long> = _changes.asStateFlow()

    // Callbacks arrive one at a time on ConnectivityManager's thread.
    @Volatile private var current: Network? = null
    @Volatile private var transport: String = "none"

    private val callback = object : ConnectivityManager.NetworkCallback() {
        override fun onAvailable(network: Network) = switchTo(network)

        override fun onCapabilitiesChanged(network: Network, caps: NetworkCapabilities) {
            switchTo(network)
            transport = transportOf(caps)
            set(caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET))
        }

        override fun onLost(network: Network) {
            if (current != null && network != current) {
                log("Ignored the loss of a previous network ($network); still on $current")
                return
            }
            current = null
            transport = "none"
            set(false)
        }

        override fun onBlockedStatusChanged(network: Network, blocked: Boolean) {
            if (blocked) {
                log("Android is blocking Bridge's network access (battery saver, data saver or a background restriction)")
            } else {
                log("Network access is no longer blocked")
                _changes.update { it + 1 }
            }
        }
    }

    init {
        runCatching { connectivity?.registerDefaultNetworkCallback(callback) }
            .onFailure { log("Could not watch the network: ${it.message}") }
    }

    private fun switchTo(network: Network) {
        if (network == current) return
        val previous = current
        current = network
        if (previous != null) log("Default network changed from $previous to $network")
        _changes.update { it + 1 }
    }

    private fun set(value: Boolean) {
        if (_available.value != value) log(if (value) "Network available ($transport)" else "Network lost")
        _available.value = value
    }

    private fun currentlyAvailable(): Boolean {
        val cm = connectivity ?: return true
        val caps = cm.getNetworkCapabilities(cm.activeNetwork) ?: return false
        return caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
    }

    private fun transportOf(caps: NetworkCapabilities): String = when {
        caps.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) -> "Wi-Fi"
        caps.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR) -> "mobile data"
        caps.hasTransport(NetworkCapabilities.TRANSPORT_ETHERNET) -> "ethernet"
        caps.hasTransport(NetworkCapabilities.TRANSPORT_VPN) -> "VPN"
        else -> "other"
    }
}
