package dev.bridge.gateway.gateway

import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import android.os.BatteryManager
import android.os.Build
import android.os.PowerManager
import android.telephony.TelephonyManager
import android.Manifest
import android.content.pm.PackageManager
import android.telephony.SubscriptionManager
import androidx.core.content.ContextCompat
import dev.bridge.gateway.BuildConfig
import dev.bridge.gateway.net.DeviceStatus
import dev.bridge.gateway.net.SimInfo

/**
 * Reads the health facts Bridge shows on the dashboard. Uses only APIs that
 * need no runtime permission; nothing that identifies the subscriber is read.
 */
class DeviceStatusReader(private val context: Context) {
    private val connectivity = context.getSystemService(ConnectivityManager::class.java)
    private val telephony = context.getSystemService(TelephonyManager::class.java)
    private val power = context.getSystemService(PowerManager::class.java)

    fun snapshot(): StatusSnapshot {
        val battery = context.registerReceiver(null, IntentFilter(Intent.ACTION_BATTERY_CHANGED))
        val level = battery?.let {
            val l = it.getIntExtra(BatteryManager.EXTRA_LEVEL, -1)
            val s = it.getIntExtra(BatteryManager.EXTRA_SCALE, -1)
            if (l >= 0 && s > 0) (l * 100 / s) else null
        }
        val plugged = (battery?.getIntExtra(BatteryManager.EXTRA_PLUGGED, 0) ?: 0) != 0
        val status = DeviceStatus(
            batteryLevel = level,
            isCharging = plugged,
            networkType = networkType(),
            carrierName = telephony?.networkOperatorName?.takeIf { it.isNotBlank() },
            simCount = simCount(),
            deviceModel = deviceModel(),
            androidVersion = Build.VERSION.RELEASE,
            appVersion = BuildConfig.VERSION_NAME,
            sims = sims(),
        )
        return StatusSnapshot(status, charging = plugged, powerSave = power?.isPowerSaveMode == true)
    }

    private fun networkType(): String {
        val caps = connectivity?.getNetworkCapabilities(connectivity.activeNetwork) ?: return "none"
        return when {
            caps.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) -> "wifi"
            caps.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR) -> "cellular"
            caps.hasTransport(NetworkCapabilities.TRANSPORT_ETHERNET) -> "ethernet"
            else -> "other"
        }
    }

    /** Active SIMs, when the user granted Phone access. Never includes phone numbers. */
    private fun sims(): List<SimInfo>? {
        if (ContextCompat.checkSelfPermission(context, Manifest.permission.READ_PHONE_STATE) != PackageManager.PERMISSION_GRANTED) {
            return null
        }
        val subs = context.getSystemService(SubscriptionManager::class.java) ?: return null
        return runCatching {
            subs.activeSubscriptionInfoList.orEmpty().map {
                SimInfo(slot = it.simSlotIndex + 1, carrier = it.carrierName?.toString(), displayName = it.displayName?.toString())
            }.sortedBy { it.slot }
        }.getOrNull()
    }

    private fun simCount(): Int? {
        val tm = telephony ?: return null
        return if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) tm.activeModemCount else @Suppress("DEPRECATION") tm.phoneCount
    }

    companion object {
        fun deviceModel(): String {
            val maker = Build.MANUFACTURER.replaceFirstChar { it.uppercase() }
            return if (Build.MODEL.startsWith(Build.MANUFACTURER, ignoreCase = true)) Build.MODEL else "$maker ${Build.MODEL}"
        }
    }
}
