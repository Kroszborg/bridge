package dev.bridge.gateway.pairing

import androidx.camera.core.ImageAnalysis
import androidx.camera.core.ImageProxy
import com.google.zxing.BarcodeFormat
import com.google.zxing.BinaryBitmap
import com.google.zxing.DecodeHintType
import com.google.zxing.PlanarYUVLuminanceSource
import com.google.zxing.ReaderException
import com.google.zxing.common.HybridBinarizer
import com.google.zxing.qrcode.QRCodeReader

/**
 * Decodes QR codes from camera frames with ZXing (Apache-2.0, no Google Play services).
 *
 * Only the part of each frame under the viewfinder (plus a small margin) is decoded:
 * [region] returns it in normalized, upright coordinates of the visible preview, or
 * null to decode everything the preview shows.
 */
class QrAnalyzer(
    private val region: () -> NormalizedRect? = { null },
    private val onText: (String) -> Unit,
) : ImageAnalysis.Analyzer {
    private val reader = QRCodeReader()
    private val hints = mapOf(
        DecodeHintType.POSSIBLE_FORMATS to listOf(BarcodeFormat.QR_CODE),
        DecodeHintType.TRY_HARDER to true,
    )
    private var bytes = ByteArray(0)

    override fun analyze(image: ImageProxy) {
        try {
            // The Y plane is the luminance ZXing needs; its pixel stride is always 1.
            val plane = image.planes[0]
            val buffer = plane.buffer
            val size = buffer.remaining()
            if (bytes.size < size) bytes = ByteArray(size)
            buffer.get(bytes, 0, size)

            val frame = PixelRect(0, 0, image.width, image.height)
            val crop = image.cropRect
            val visible = PixelRect(crop.left, crop.top, crop.right, crop.bottom)
                .takeIf { it.width > 0 && it.height > 0 && it.left >= 0 && it.top >= 0 && it.right <= frame.right && it.bottom <= frame.bottom }
                ?: frame
            val area = region()?.let { ScanGeometry.toBuffer(it, visible, image.imageInfo.rotationDegrees) } ?: visible

            val source = PlanarYUVLuminanceSource(
                bytes, plane.rowStride, image.height,
                area.left, area.top, area.width, area.height, false,
            )
            val result = reader.decode(BinaryBitmap(HybridBinarizer(source)), hints)
            onText(result.text)
        } catch (_: ReaderException) {
            // No QR code in this frame.
        } catch (_: RuntimeException) {
            // An odd frame format from some cameras; this runs on CameraX's thread, so throwing would crash the app.
        } finally {
            reader.reset()
            image.close()
        }
    }
}
