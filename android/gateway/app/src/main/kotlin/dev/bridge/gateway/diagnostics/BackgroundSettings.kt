package dev.bridge.gateway.diagnostics

import android.content.ActivityNotFoundException
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.os.Build
import android.provider.Settings
import androidx.core.content.edit
import androidx.core.net.toUri

/**
 * Phone makers whose own battery managers stop background apps beyond what
 * Android does (see dontkillmyapp.com). Each has a settings page, under a
 * different name, where the user allows an app to start and keep running.
 */
enum class VendorSkin(val brand: String, val setting: String) {
    ColorOS("", "auto launch and background activity"),
    Xiaomi("Xiaomi", "Autostart and no battery restrictions"),
    Vivo("Vivo", "background start-up and high background power use"),
    Samsung("Samsung", "unrestricted battery use, and keep Bridge out of sleeping apps"),
    Huawei("Huawei", "manual launch management with all three switches on"),
}

/** Opens the vendor's auto-start or battery page, with safe fallbacks. */
object BackgroundSettings {
    private const val PREFS = "diagnostics"
    private const val KEY_OPENED = "vendor_settings_opened_at"

    /** The vendor skin of this phone, or null for one that follows stock Android. */
    fun skin(manufacturer: String = Build.MANUFACTURER): VendorSkin? = when (manufacturer.lowercase()) {
        "oppo", "realme", "oneplus" -> VendorSkin.ColorOS
        "xiaomi", "redmi", "poco" -> VendorSkin.Xiaomi
        "vivo", "iqoo" -> VendorSkin.Vivo
        "samsung" -> VendorSkin.Samsung
        "huawei", "honor" -> VendorSkin.Huawei
        else -> null
    }

    /** The brand to name in the app: "Realme" rather than the skin. */
    fun brand(skin: VendorSkin): String = skin.brand.ifEmpty { Build.MANUFACTURER.replaceFirstChar { it.uppercase() } }

    /** Known pages, most specific first. Vendors move these between versions, so several are tried. */
    fun intents(skin: VendorSkin, packageName: String): List<Intent> {
        fun page(pkg: String, cls: String) = Intent().setComponent(ComponentName(pkg, cls))
        val pages = when (skin) {
            VendorSkin.ColorOS -> listOf(
                page("com.coloros.safecenter", "com.coloros.safecenter.permission.startup.StartupAppListActivity"),
                page("com.coloros.safecenter", "com.coloros.safecenter.startupapp.StartupAppListActivity"),
                page("com.oppo.safe", "com.oppo.safe.permission.startup.StartupAppListActivity"),
                page("com.oneplus.security", "com.oneplus.security.chainlaunch.view.ChainLaunchAppListActivity"),
            )
            VendorSkin.Xiaomi -> listOf(
                page("com.miui.securitycenter", "com.miui.permcenter.autostart.AutoStartManagementActivity"),
                page("com.miui.powerkeeper", "com.miui.powerkeeper.ui.HiddenAppsConfigActivity")
                    .putExtra("package_name", packageName).putExtra("package_label", "Bridge"),
            )
            VendorSkin.Vivo -> listOf(
                page("com.vivo.permissionmanager", "com.vivo.permissionmanager.activity.BgStartUpManagerActivity"),
                page("com.iqoo.secure", "com.iqoo.secure.ui.phoneoptimize.BgStartUpManager"),
                page("com.iqoo.secure", "com.iqoo.secure.ui.phoneoptimize.AddWhiteListActivity"),
            )
            VendorSkin.Samsung -> listOf(
                page("com.samsung.android.lool", "com.samsung.android.sm.battery.ui.BatteryActivity"),
                page("com.samsung.android.lool", "com.samsung.android.sm.ui.battery.BatteryActivity"),
            )
            VendorSkin.Huawei -> listOf(
                page("com.huawei.systemmanager", "com.huawei.systemmanager.startupmgr.ui.StartupNormalAppListActivity"),
                page("com.huawei.systemmanager", "com.huawei.systemmanager.optimize.process.ProtectActivity"),
                page("com.hihonor.systemmanager", "com.hihonor.systemmanager.startupmgr.ui.StartupNormalAppListActivity"),
            )
        }
        return pages + appDetails(packageName)
    }

    /** Bridge's own page in system settings; its battery section has the same switches on most phones. */
    fun appDetails(packageName: String): Intent =
        Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS, "package:$packageName".toUri())

    /**
     * Opens the first page that exists. Vendor pages are often missing or not
     * exported on newer versions, which throws; the next one is tried.
     */
    fun open(context: Context, skin: VendorSkin): Boolean {
        for (intent in intents(skin, context.packageName)) {
            try {
                context.startActivity(intent)
                context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit { putLong(KEY_OPENED, System.currentTimeMillis()) }
                return true
            } catch (_: ActivityNotFoundException) {
            } catch (_: SecurityException) {
            }
        }
        return false
    }

    /** Whether the user has opened the vendor settings from the app at least once. */
    fun opened(context: Context): Boolean =
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).getLong(KEY_OPENED, 0) > 0
}
