package dev.bridge.gateway

import dev.bridge.gateway.pairing.NormalizedRect
import dev.bridge.gateway.pairing.PixelRect
import dev.bridge.gateway.pairing.ScanGeometry
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class ScanGeometryTest {
    private val delta = 0.01f

    @Test
    fun `viewfinder is 70 percent of the smaller side of a phone held upright`() {
        // 360 × 760 dp inside the bars; 184 dp reserved for the title and hint.
        assertEquals(252f, ScanGeometry.finderSide(360f, 760f, 184f), delta)
    }

    @Test
    fun `viewfinder fits beside the side columns in landscape`() {
        // 760 × 340 dp: 70 % of the height, with the 300 dp of side columns still free.
        assertEquals(238f, ScanGeometry.finderSide(760f, 340f, 300f), delta)
    }

    @Test
    fun `viewfinder shrinks to leave room for the controls in a short window`() {
        // Split screen, 400 × 420 dp: 70 % of 400 would be 280, but only 420 − 184 − 16 = 220 is free.
        assertEquals(220f, ScanGeometry.finderSide(400f, 420f, 184f), delta)
    }

    @Test
    fun `viewfinder is capped on tablets`() {
        assertEquals(ScanGeometry.FINDER_MAX_DP, ScanGeometry.finderSide(1280f, 800f, 300f), delta)
    }

    @Test
    fun `a tiny window keeps a usable viewfinder and it is never negative`() {
        // 200 × 150 dp leaves no room beside the controls: 70 % of 150 regardless.
        assertEquals(105f, ScanGeometry.finderSide(200f, 150f, 300f), delta)
        assertEquals(0f, ScanGeometry.finderSide(10f, 10f, 300f), delta)
        assertEquals(0f, ScanGeometry.finderSide(0f, 0f, 0f), delta)
    }

    @Test
    fun `normalizes view pixels and rejects empty areas`() {
        assertEquals(NormalizedRect(0.25f, 0.5f, 0.75f, 0.75f), NormalizedRect.of(100f, 400f, 300f, 600f, 400f, 800f))
        assertNull(NormalizedRect.of(0f, 0f, 0f, 0f, 400f, 800f))
        assertNull(NormalizedRect.of(10f, 10f, 20f, 20f, 0f, 0f))
    }

    @Test
    fun `an upright frame maps directly`() {
        val r = ScanGeometry.toBuffer(NormalizedRect(0.25f, 0.25f, 0.75f, 0.75f), PixelRect(0, 0, 1000, 800), rotationDegrees = 0, margin = 0f)
        assertEquals(PixelRect(250, 200, 750, 600), r)
    }

    @Test
    fun `a sensor turned 90 degrees swaps the axes`() {
        // The left strip of the upright preview is the top strip of a buffer turned 90° clockwise to display.
        val r = ScanGeometry.toBuffer(NormalizedRect(0f, 0f, 0.5f, 1f), PixelRect(0, 0, 1280, 720), rotationDegrees = 90, margin = 0f)
        assertEquals(PixelRect(0, 360, 1280, 720), r)
        // The top strip of the upright preview is the left strip of the buffer.
        val top = ScanGeometry.toBuffer(NormalizedRect(0f, 0f, 1f, 0.25f), PixelRect(0, 0, 1280, 720), rotationDegrees = 90, margin = 0f)
        assertEquals(PixelRect(0, 0, 320, 720), top)
    }

    @Test
    fun `180 and 270 degrees`() {
        val frame = PixelRect(0, 0, 1000, 1000)
        val corner = NormalizedRect(0f, 0f, 0.2f, 0.1f) // top left of the upright preview
        assertEquals(PixelRect(800, 900, 1000, 1000), ScanGeometry.toBuffer(corner, frame, 180, margin = 0f))
        assertEquals(PixelRect(900, 0, 1000, 200), ScanGeometry.toBuffer(corner, frame, 270, margin = 0f))
        assertEquals(ScanGeometry.toBuffer(corner, frame, 90, margin = 0f), ScanGeometry.toBuffer(corner, frame, -270, margin = 0f))
    }

    @Test
    fun `stays inside the visible crop of the frame`() {
        // A portrait preview shows only the middle of a 16:9 landscape buffer.
        val visible = PixelRect(0, 100, 1280, 620)
        val centre = ScanGeometry.toBuffer(NormalizedRect(0.15f, 0.3f, 0.85f, 0.7f), visible, rotationDegrees = 90, margin = 0f)
        assertEquals(PixelRect(384, 178, 896, 542), centre)
        // The margin never reaches outside what is visible.
        val grown = ScanGeometry.toBuffer(NormalizedRect(0f, 0f, 1f, 1f), visible, rotationDegrees = 90)
        assertEquals(visible, grown)
    }

    @Test
    fun `adds the decode margin around the viewfinder`() {
        val r = ScanGeometry.toBuffer(NormalizedRect(0.4f, 0.4f, 0.6f, 0.6f), PixelRect(0, 0, 1000, 1000), rotationDegrees = 0, margin = 0.5f)
        assertEquals(PixelRect(300, 300, 700, 700), r)
        assertTrue(r.width > 0 && r.height > 0)
    }
}
