package dev.bridge.gateway.ui.theme

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Typography
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.Font
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontVariation
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.sp
import dev.bridge.gateway.R

/** Bridge colours that Material 3 has no slot for. */
@Immutable
data class BridgeColors(
    val success: Color,
    val warning: Color,
    val danger: Color,
    val faint: Color,
    val card: Color,
    val raised: Color,
    val border: Color,
)

private val DarkBridge = BridgeColors(
    success = Color(0xFF4ADE80), warning = Color(0xFFFBBF24), danger = Color(0xFFF87171),
    faint = Color(0xFF71717A), card = Color(0xFF111113), raised = Color(0xFF18181B), border = Color(0xFF27272A),
)

private val LightBridge = BridgeColors(
    success = Color(0xFF15803D), warning = Color(0xFFB45309), danger = Color(0xFFDC2626),
    faint = Color(0xFFA39B90), card = Color(0xFFFFFFFF), raised = Color(0xFFF2EFE9), border = Color(0xFFE5E0D8),
)

val LocalBridgeColors = staticCompositionLocalOf { DarkBridge }

private val DarkScheme = darkColorScheme(
    primary = Color(0xFF3EEBC0),
    onPrimary = Color(0xFF0A0A0B),
    background = Color(0xFF09090B),
    onBackground = Color(0xFFFAFAFA),
    surface = Color(0xFF09090B),
    onSurface = Color(0xFFFAFAFA),
    surfaceVariant = Color(0xFF18181B),
    onSurfaceVariant = Color(0xFFA1A1AA),
    surfaceContainer = Color(0xFF111113),
    surfaceContainerHigh = Color(0xFF18181B),
    surfaceContainerHighest = Color(0xFF1F1F23),
    outline = Color(0xFF27272A),
    outlineVariant = Color(0xFF1F1F23),
    error = Color(0xFFF87171),
    onError = Color(0xFF0A0A0B),
)

private val LightScheme = lightColorScheme(
    primary = Color(0xFF0F7A62),
    onPrimary = Color(0xFFFFFFFF),
    background = Color(0xFFFAF8F4),
    onBackground = Color(0xFF1A1714),
    surface = Color(0xFFFAF8F4),
    onSurface = Color(0xFF1A1714),
    surfaceVariant = Color(0xFFF2EFE9),
    onSurfaceVariant = Color(0xFF5E564C),
    surfaceContainer = Color(0xFFFFFFFF),
    surfaceContainerHigh = Color(0xFFF2EFE9),
    surfaceContainerHighest = Color(0xFFEFEBE4),
    outline = Color(0xFFE5E0D8),
    outlineVariant = Color(0xFFEFEBE4),
    error = Color(0xFFDC2626),
    onError = Color(0xFFFFFFFF),
)

private fun variable(res: Int, weight: Int) =
    Font(res, FontWeight(weight), variationSettings = FontVariation.Settings(FontVariation.weight(weight)))

val RedHatText = FontFamily(
    variable(R.font.red_hat_text, 400),
    variable(R.font.red_hat_text, 500),
    variable(R.font.red_hat_text, 600),
)
val RedHatDisplay = FontFamily(
    variable(R.font.red_hat_display, 600),
    variable(R.font.red_hat_display, 700),
)
val RedHatMono = FontFamily(variable(R.font.red_hat_mono, 400), variable(R.font.red_hat_mono, 500))

private val BridgeTypography = Typography().run {
    copy(
        headlineMedium = TextStyle(fontFamily = RedHatDisplay, fontWeight = FontWeight.Bold, fontSize = 28.sp, lineHeight = 34.sp, letterSpacing = (-0.4).sp),
        headlineSmall = TextStyle(fontFamily = RedHatDisplay, fontWeight = FontWeight.Bold, fontSize = 22.sp, lineHeight = 28.sp, letterSpacing = (-0.3).sp),
        titleLarge = TextStyle(fontFamily = RedHatDisplay, fontWeight = FontWeight.SemiBold, fontSize = 20.sp, lineHeight = 26.sp),
        titleMedium = TextStyle(fontFamily = RedHatDisplay, fontWeight = FontWeight.SemiBold, fontSize = 16.sp, lineHeight = 22.sp),
        titleSmall = TextStyle(fontFamily = RedHatText, fontWeight = FontWeight.SemiBold, fontSize = 14.sp, lineHeight = 20.sp),
        bodyLarge = TextStyle(fontFamily = RedHatText, fontSize = 16.sp, lineHeight = 24.sp),
        bodyMedium = TextStyle(fontFamily = RedHatText, fontSize = 14.sp, lineHeight = 21.sp),
        bodySmall = TextStyle(fontFamily = RedHatText, fontSize = 12.sp, lineHeight = 18.sp),
        labelLarge = TextStyle(fontFamily = RedHatText, fontWeight = FontWeight.SemiBold, fontSize = 14.sp, lineHeight = 20.sp),
        labelMedium = TextStyle(fontFamily = RedHatText, fontWeight = FontWeight.Medium, fontSize = 12.sp, lineHeight = 16.sp),
        labelSmall = TextStyle(fontFamily = RedHatText, fontWeight = FontWeight.SemiBold, fontSize = 11.sp, lineHeight = 16.sp, letterSpacing = 1.2.sp),
    )
}

@Composable
fun BridgeTheme(dark: Boolean = isSystemInDarkTheme(), content: @Composable () -> Unit) {
    CompositionLocalProvider(LocalBridgeColors provides if (dark) DarkBridge else LightBridge) {
        MaterialTheme(colorScheme = if (dark) DarkScheme else LightScheme, typography = BridgeTypography, content = content)
    }
}

object Bridge {
    val colors: BridgeColors
        @Composable get() = LocalBridgeColors.current
}
