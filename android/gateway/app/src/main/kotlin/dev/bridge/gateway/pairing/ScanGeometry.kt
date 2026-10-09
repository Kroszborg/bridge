package dev.bridge.gateway.pairing

import kotlin.math.max
import kotlin.math.min
import kotlin.math.roundToInt

/** A rectangle in normalized coordinates (0..1) of the visible camera preview, upright as the user sees it. */
data class NormalizedRect(val left: Float, val top: Float, val right: Float, val bottom: Float) {
    init {
        require(left < right && top < bottom) { "empty rectangle" }
    }

    companion object {
        /** [left], [top], [right], [bottom] are in the same units as [width] and [height] (pixels of the preview view). */
        fun of(left: Float, top: Float, right: Float, bottom: Float, width: Float, height: Float): NormalizedRect? {
            if (width <= 0f || height <= 0f) return null
            val l = (left / width).coerceIn(0f, 1f)
            val t = (top / height).coerceIn(0f, 1f)
            val r = (right / width).coerceIn(0f, 1f)
            val b = (bottom / height).coerceIn(0f, 1f)
            return if (l < r && t < b) NormalizedRect(l, t, r, b) else null
        }
    }
}

/** A rectangle in pixels of a camera frame's buffer; [right] and [bottom] are exclusive. */
data class PixelRect(val left: Int, val top: Int, val right: Int, val bottom: Int) {
    val width: Int get() = right - left
    val height: Int get() = bottom - top
}

/** Sizing of the scanner's viewfinder and mapping it onto camera frames. */
object ScanGeometry {
    /** The viewfinder takes this share of the smaller side of the visible area. */
    const val FINDER_SHARE = 0.7f

    /** Never larger than this, in dp, so it stays a viewfinder on tablets and foldables. */
    const val FINDER_MAX_DP = 420f

    /** Below this, in dp, the viewfinder ignores the room reserved for the controls. */
    const val FINDER_MIN_DP = 120f

    /** Decoding also looks this far (a share of the viewfinder's side) outside it, for codes held slightly off-centre. */
    const val DECODE_MARGIN = 0.15f

    /**
     * The viewfinder's side in dp for a visible area of [widthDp] × [heightDp] (inside the
     * system bars and display cutouts), after [reservedDp] along the long axis for the
     * title, hint and buttons. Square, [FINDER_SHARE] of the smaller side, and never
     * larger than what is left once the controls have their room (unless that would make
     * it smaller than [FINDER_MIN_DP]).
     */
    fun finderSide(widthDp: Float, heightDp: Float, reservedDp: Float): Float {
        if (widthDp <= 0f || heightDp <= 0f) return 0f
        val portrait = heightDp >= widthDp
        val freeWidth = if (portrait) widthDp else widthDp - reservedDp
        val freeHeight = if (portrait) heightDp - reservedDp else heightDp
        val smaller = min(widthDp, heightDp)
        val fitted = min(FINDER_SHARE * smaller, min(freeWidth, freeHeight) - 16f)
        // In a very small window (split screen) the scanner matters more than the controls' room.
        val side = if (fitted >= FINDER_MIN_DP) fitted else min(FINDER_SHARE * smaller, smaller - 16f)
        return side.coerceIn(0f, FINDER_MAX_DP)
    }

    /**
     * Maps [region] (normalized, upright, over the visible preview) onto a camera frame.
     *
     * [visible] is the part of the frame the preview shows (the image's crop rect, which
     * CameraX sets from the preview's [androidx.camera.core.ViewPort] so preview and
     * analysis share one field of view). [rotationDegrees] is how far the buffer must be
     * turned clockwise to appear upright. The result grows by [margin] of the region's
     * size on each side and is clamped to [visible].
     */
    fun toBuffer(region: NormalizedRect, visible: PixelRect, rotationDegrees: Int, margin: Float = DECODE_MARGIN): PixelRect {
        val growX = (region.right - region.left) * margin
        val growY = (region.bottom - region.top) * margin
        val l = (region.left - growX).coerceAtLeast(0f)
        val t = (region.top - growY).coerceAtLeast(0f)
        val r = (region.right + growX).coerceAtMost(1f)
        val b = (region.bottom + growY).coerceAtMost(1f)

        val (x0, y0) = uprightToBuffer(l, t, rotationDegrees)
        val (x1, y1) = uprightToBuffer(r, b, rotationDegrees)
        val left = visible.left + (min(x0, x1) * visible.width).roundToInt()
        val top = visible.top + (min(y0, y1) * visible.height).roundToInt()
        val right = visible.left + (max(x0, x1) * visible.width).roundToInt()
        val bottom = visible.top + (max(y0, y1) * visible.height).roundToInt()
        return PixelRect(
            left.coerceIn(visible.left, visible.right - 1),
            top.coerceIn(visible.top, visible.bottom - 1),
            right.coerceIn(visible.left + 1, visible.right),
            bottom.coerceIn(visible.top + 1, visible.bottom),
        )
    }

    /** A normalized upright point to the buffer's normalized coordinates, undoing a clockwise [rotationDegrees] turn. */
    private fun uprightToBuffer(u: Float, v: Float, rotationDegrees: Int): Pair<Float, Float> =
        when (((rotationDegrees % 360) + 360) % 360) {
            90 -> v to 1f - u
            180 -> 1f - u to 1f - v
            270 -> 1f - v to u
            else -> u to v
        }
}
