package dev.bridge.gateway.ui

import android.util.Size
import androidx.activity.compose.LocalActivity
import androidx.camera.core.Camera
import androidx.camera.core.CameraSelector
import androidx.camera.core.ImageAnalysis
import androidx.camera.core.Preview
import androidx.camera.core.TorchState
import androidx.camera.core.UseCaseGroup
import androidx.camera.core.resolutionselector.ResolutionSelector
import androidx.camera.core.resolutionselector.ResolutionStrategy
import androidx.camera.lifecycle.ProcessCameraProvider
import androidx.camera.view.PreviewView
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LocalContentColor
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Rect
import androidx.compose.ui.graphics.BlendMode
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.CompositingStrategy
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.layout.boundsInRoot
import androidx.compose.ui.layout.onGloballyPositioned
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.role
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.core.content.ContextCompat
import androidx.core.view.WindowCompat
import androidx.core.view.doOnLayout
import androidx.lifecycle.Observer
import androidx.lifecycle.compose.LocalLifecycleOwner
import dev.bridge.gateway.R
import dev.bridge.gateway.pairing.NormalizedRect
import dev.bridge.gateway.pairing.PairingRequest
import dev.bridge.gateway.pairing.PairingUri
import dev.bridge.gateway.pairing.QrAnalyzer
import dev.bridge.gateway.pairing.ScanGeometry
import dev.bridge.gateway.ui.theme.Bridge
import dev.bridge.gateway.ui.theme.BridgeTheme
import kotlinx.coroutines.delay
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicReference

/** Whether the scanner may use the camera. */
enum class CameraAccess {
    Granted,

    /** Not granted yet; Android will still show its permission dialog. */
    Ask,

    /** Denied with "don't ask again" (or twice): only the app's settings page can grant it now. */
    Blocked,
}

@Composable
fun ScanScreen(
    access: CameraAccess,
    onRequestPermission: () -> Unit,
    onOpenSettings: () -> Unit,
    onResult: (PairingRequest) -> Unit,
    onCancel: () -> Unit,
) {
    if (access == CameraAccess.Granted) {
        CameraScanner(onResult = onResult, onCancel = onCancel)
    } else {
        CameraPermission(blocked = access == CameraAccess.Blocked, onRequestPermission, onOpenSettings, onCancel)
    }
}

/** Explains why the camera is needed; in the app's own theme, scrollable so it fits any window. */
@Composable
private fun CameraPermission(blocked: Boolean, onRequestPermission: () -> Unit, onOpenSettings: () -> Unit, onCancel: () -> Unit) {
    Column(
        Modifier
            .fillMaxSize()
            .safeDrawingPadding()
            .verticalScroll(rememberScrollState())
            .padding(horizontal = 24.dp, vertical = 16.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        TextButton(onClick = onCancel, contentPadding = PaddingValues(0.dp)) { Text("Back") }
        BridgeTile(48.dp)
        Text("Scan pairing code", style = MaterialTheme.typography.headlineMedium)
        Text(
            "Bridge needs the camera only to read the pairing QR code. Nothing is recorded or uploaded.",
            style = MaterialTheme.typography.bodyLarge,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        if (blocked) {
            Notice(
                "Camera access is turned off for Bridge. Open its settings, choose Permissions › Camera › Allow, then come back.",
                color = Bridge.colors.warning,
            )
            Button(onClick = onOpenSettings, modifier = Modifier.fillMaxWidth().height(52.dp)) { Text("Open settings") }
            TextButton(onClick = onRequestPermission) { Text("Ask again") }
        } else {
            Button(onClick = onRequestPermission, modifier = Modifier.fillMaxWidth().height(52.dp)) { Text("Allow camera") }
        }
        Text(
            "No camera? Go back and choose to enter the server address and pairing code by hand.",
            style = MaterialTheme.typography.bodySmall,
            color = Bridge.colors.faint,
        )
    }
}

/**
 * The full-window scanner. The preview fills the window edge to edge, behind the system
 * bars, and is cropped (never stretched) to fill it; the viewfinder, title, hint and buttons
 * stay inside the safe area (status and navigation bars, display cutouts). A camera view is
 * dark in either app theme, so this part always uses the dark palette.
 */
@Composable
private fun CameraScanner(onResult: (PairingRequest) -> Unit, onCancel: () -> Unit) {
    var previewBounds by remember { mutableStateOf(Rect.Zero) }
    var finderBounds by remember { mutableStateOf(Rect.Zero) }
    var hint by remember { mutableStateOf<String?>(null) }
    var failure by remember { mutableStateOf<String?>(null) }
    var camera by remember { mutableStateOf<Camera?>(null) }
    var torchOn by remember { mutableStateOf(false) }
    // Read by the analyzer on its own thread.
    val region = remember { AtomicReference<NormalizedRect?>(null) }

    LaunchedEffect(previewBounds, finderBounds) {
        region.set(
            NormalizedRect.of(
                finderBounds.left - previewBounds.left, finderBounds.top - previewBounds.top,
                finderBounds.right - previewBounds.left, finderBounds.bottom - previewBounds.top,
                previewBounds.width, previewBounds.height,
            ),
        )
    }
    LaunchedEffect(hint) {
        if (hint != null) {
            delay(2_500)
            hint = null
        }
    }
    LightSystemBarIcons()

    BridgeTheme(dark = true) {
        CompositionLocalProvider(LocalContentColor provides Color.White) {
            Box(Modifier.fillMaxSize().background(Color.Black).onGloballyPositioned { previewBounds = it.boundsInRoot() }) {
                CameraPreview(
                    region = region,
                    onCamera = { camera = it },
                    onTorch = { torchOn = it },
                    onWrongCode = { hint = "That QR code is not a Bridge pairing code." },
                    onFailure = { failure = it },
                    onResult = onResult,
                )
                Scrim(finder = finderBounds.translate(-previewBounds.left, -previewBounds.top))
                ScannerControls(
                    hint = failure ?: hint,
                    hintIsProblem = failure != null || hint != null,
                    torchOn = torchOn.takeIf { camera?.cameraInfo?.hasFlashUnit() == true },
                    onTorch = { camera?.cameraControl?.enableTorch(!torchOn) },
                    onCancel = onCancel,
                    onFinder = { finderBounds = it },
                )
            }
        }
    }
}

@Composable
private fun CameraPreview(
    region: AtomicReference<NormalizedRect?>,
    onCamera: (Camera?) -> Unit,
    onTorch: (Boolean) -> Unit,
    onWrongCode: () -> Unit,
    onFailure: (String) -> Unit,
    onResult: (PairingRequest) -> Unit,
) {
    val context = LocalContext.current
    val lifecycleOwner = LocalLifecycleOwner.current
    val previewView = remember {
        PreviewView(context).apply {
            // A TextureView follows Compose's layout and drawing order exactly; a SurfaceView can show through overlays.
            implementationMode = PreviewView.ImplementationMode.COMPATIBLE
            // Crop to fill the view, keeping the aspect ratio: no stretching, no letterbox bars.
            scaleType = PreviewView.ScaleType.FILL_CENTER
        }
    }

    AndroidView(factory = { previewView }, modifier = Modifier.fillMaxSize())

    DisposableEffect(lifecycleOwner, previewView) {
        val main = ContextCompat.getMainExecutor(context)
        val executor = Executors.newSingleThreadExecutor()
        val handled = AtomicBoolean(false)
        val torchObserver = Observer<Int> { onTorch(it == TorchState.ON) }
        var disposed = false
        var provider: ProcessCameraProvider? = null
        var bound: Camera? = null
        val preview = Preview.Builder().build()
        val analysis = ImageAnalysis.Builder()
            .setBackpressureStrategy(ImageAnalysis.STRATEGY_KEEP_ONLY_LATEST)
            .setResolutionSelector(
                ResolutionSelector.Builder()
                    .setResolutionStrategy(ResolutionStrategy(Size(1280, 720), ResolutionStrategy.FALLBACK_RULE_CLOSEST_LOWER_THEN_HIGHER))
                    .build(),
            )
            .build()
        analysis.setAnalyzer(
            executor,
            QrAnalyzer(region = region::get) { text ->
                val request = PairingUri.parse(text)
                main.execute {
                    if (disposed) return@execute
                    if (request == null) onWrongCode() else if (handled.compareAndSet(false, true)) onResult(request)
                }
            },
        )

        val future = ProcessCameraProvider.getInstance(context)
        future.addListener({
            if (disposed) return@addListener
            val cameraProvider = runCatching { future.get() }.getOrElse {
                onFailure("The camera could not be started. Go back and enter the pairing code by hand.")
                return@addListener
            }
            provider = cameraProvider
            // The view port (what the preview shows) is known once the view is laid out. Binding with it
            // gives the analyzer frames whose crop rect is exactly the visible area, in any orientation.
            previewView.doOnLayout {
                if (disposed) return@doOnLayout
                preview.surfaceProvider = previewView.surfaceProvider
                previewView.display?.rotation?.let { rotation ->
                    preview.targetRotation = rotation
                    analysis.targetRotation = rotation
                }
                val group = UseCaseGroup.Builder().addUseCase(preview).addUseCase(analysis)
                previewView.viewPort?.let(group::setViewPort)
                try {
                    cameraProvider.unbindAll()
                    val cam = cameraProvider.bindToLifecycle(lifecycleOwner, CameraSelector.DEFAULT_BACK_CAMERA, group.build())
                    bound = cam
                    cam.cameraInfo.torchState.observe(lifecycleOwner, torchObserver)
                    onCamera(cam)
                } catch (_: Exception) {
                    // No back camera, or it is in use by another app.
                    onFailure("This phone's camera could not be opened. Close other camera apps, or go back and enter the pairing code by hand.")
                }
            }
        }, main)

        onDispose {
            disposed = true
            bound?.cameraInfo?.torchState?.removeObserver(torchObserver)
            // Unbinding closes the camera and turns the torch off.
            provider?.unbind(preview, analysis)
            analysis.clearAnalyzer()
            executor.shutdown()
            onCamera(null)
        }
    }
}

/** Dims everything but the viewfinder, which is outlined with rounded corners. */
@Composable
private fun Scrim(finder: Rect) {
    val outline = MaterialTheme.colorScheme.primary
    Canvas(Modifier.fillMaxSize().graphicsLayer(compositingStrategy = CompositingStrategy.Offscreen)) {
        drawRect(Color.Black.copy(alpha = 0.6f))
        if (finder.width > 0f && finder.height > 0f) {
            val radius = CornerRadius(20.dp.toPx())
            drawRoundRect(Color.Transparent, finder.topLeft, finder.size, radius, blendMode = BlendMode.Clear)
            val stroke = 3.dp.toPx()
            drawRoundRect(
                outline,
                finder.topLeft - Offset(stroke / 2, stroke / 2),
                androidx.compose.ui.geometry.Size(finder.width + stroke, finder.height + stroke),
                CornerRadius(radius.x + stroke / 2),
                style = Stroke(stroke),
            )
        }
    }
}

/**
 * Title, close and torch buttons, the viewfinder's slot and the hint, laid out inside the safe
 * area: stacked in a tall window, side by side in a wide one, so nothing runs off the screen.
 */
@Composable
private fun ScannerControls(
    hint: String?,
    hintIsProblem: Boolean,
    /** Null when the camera has no flash unit. */
    torchOn: Boolean?,
    onTorch: () -> Unit,
    onCancel: () -> Unit,
    onFinder: (Rect) -> Unit,
) {
    val hintText = hint ?: "Point the camera at the QR code on the dashboard's Phones page."
    val hintColor = if (hintIsProblem) Bridge.colors.warning else Color.White.copy(alpha = 0.85f)
    val close: @Composable () -> Unit = {
        IconButton(onClick = onCancel) { Icon(painterResource(R.drawable.ic_close), contentDescription = "Close scanner") }
    }
    val torchButton: @Composable () -> Unit = {
        if (torchOn != null) {
            val on: Boolean = torchOn
            IconButton(onClick = onTorch, modifier = Modifier.semantics { role = Role.Switch }) {
                Icon(
                    painterResource(if (on) R.drawable.ic_flash_on else R.drawable.ic_flash_off),
                    contentDescription = if (on) "Turn the light off" else "Turn the light on",
                )
            }
        } else {
            Spacer(Modifier.size(48.dp))
        }
    }

    BoxWithConstraints(Modifier.fillMaxSize().windowInsetsPadding(WindowInsets.safeDrawing)) {
        val portrait = maxHeight >= maxWidth
        val reserved = if (portrait) PORTRAIT_RESERVED else LANDSCAPE_RESERVED
        val side = ScanGeometry.finderSide(maxWidth.value, maxHeight.value, reserved.value).dp
        val finder = Modifier.size(side).onGloballyPositioned { onFinder(it.boundsInRoot()) }

        if (portrait) {
            Column(Modifier.fillMaxSize(), horizontalAlignment = Alignment.CenterHorizontally) {
                Row(Modifier.fillMaxWidth().padding(4.dp), verticalAlignment = Alignment.CenterVertically) {
                    close()
                    Text(
                        "Scan pairing code",
                        style = MaterialTheme.typography.titleLarge,
                        textAlign = TextAlign.Center,
                        modifier = Modifier.weight(1f),
                    )
                    torchButton()
                }
                Box(Modifier.weight(1f).fillMaxWidth(), contentAlignment = Alignment.Center) { Box(finder) }
                Text(
                    hintText,
                    style = MaterialTheme.typography.bodyLarge,
                    color = hintColor,
                    textAlign = TextAlign.Center,
                    modifier = Modifier.fillMaxWidth().padding(horizontal = 32.dp, vertical = 24.dp),
                )
            }
        } else {
            Row(Modifier.fillMaxSize(), verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f).fillMaxHeight().padding(4.dp)) {
                    close()
                    Text(
                        "Scan pairing code",
                        style = MaterialTheme.typography.titleLarge,
                        modifier = Modifier.padding(horizontal = 12.dp, vertical = 8.dp),
                    )
                }
                Box(Modifier.fillMaxHeight().width(side + 32.dp), contentAlignment = Alignment.Center) { Box(finder) }
                Column(Modifier.weight(1f).fillMaxHeight().padding(4.dp), horizontalAlignment = Alignment.End) {
                    torchButton()
                    Spacer(Modifier.weight(1f))
                    Text(
                        hintText,
                        style = MaterialTheme.typography.bodyMedium,
                        color = hintColor,
                        modifier = Modifier.fillMaxWidth().padding(12.dp),
                    )
                }
            }
        }
    }
}

/** White status and navigation bar icons over the camera, whatever the app theme; restored on leaving. */
@Composable
private fun LightSystemBarIcons() {
    val window = LocalActivity.current?.window ?: return
    DisposableEffect(window) {
        val controller = WindowCompat.getInsetsController(window, window.decorView)
        val status = controller.isAppearanceLightStatusBars
        val navigation = controller.isAppearanceLightNavigationBars
        controller.isAppearanceLightStatusBars = false
        controller.isAppearanceLightNavigationBars = false
        onDispose {
            controller.isAppearanceLightStatusBars = status
            controller.isAppearanceLightNavigationBars = navigation
        }
    }
}

// Room for the title bar and the hint (tall windows), or the two side columns (wide windows).
private val PORTRAIT_RESERVED = 64.dp + 120.dp
private val LANDSCAPE_RESERVED = 300.dp
