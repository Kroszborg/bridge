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

/** Decodes QR codes from camera frames with ZXing (Apache-2.0, no Google Play services). */
class QrAnalyzer(private val onText: (String) -> Unit) : ImageAnalysis.Analyzer {
    private val reader = QRCodeReader()
    private val hints = mapOf(DecodeHintType.POSSIBLE_FORMATS to listOf(BarcodeFormat.QR_CODE))

    override fun analyze(image: ImageProxy) {
        try {
            val plane = image.planes[0]
            val buffer = plane.buffer
            val bytes = ByteArray(buffer.remaining()).also { buffer.get(it) }
            val source = PlanarYUVLuminanceSource(bytes, plane.rowStride, image.height, 0, 0, image.width, image.height, false)
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
