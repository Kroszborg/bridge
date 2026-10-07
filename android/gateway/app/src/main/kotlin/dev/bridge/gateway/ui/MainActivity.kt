package dev.bridge.gateway.ui

import android.Manifest
import android.annotation.SuppressLint
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import android.os.Bundle
import android.os.PowerManager
import android.provider.Settings
import androidx.activity.ComponentActivity
import androidx.activity.compose.BackHandler
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.activity.viewModels
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.Surface
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.core.content.ContextCompat
import androidx.core.net.toUri
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.bridge.gateway.container
import dev.bridge.gateway.pairing.PairingUri
import dev.bridge.gateway.push.flavorPush
import dev.bridge.gateway.ui.theme.BridgeTheme
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.lifecycleScope
import androidx.lifecycle.repeatOnLifecycle
import dev.bridge.gateway.data.Pairing
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

class MainActivity : ComponentActivity() {
    private val vm: MainViewModel by viewModels()
    private var checks by mutableStateOf(ReliabilityChecks())
    private var cameraGranted by mutableStateOf(false)
    private var askedReceiveSms = false

    private val notificationPermission = registerForActivityResult(ActivityResultContracts.RequestPermission()) { refreshChecks() }
    private val cameraPermission = registerForActivityResult(ActivityResultContracts.RequestPermission()) { cameraGranted = it }
    private val smsPermission = registerForActivityResult(ActivityResultContracts.RequestPermission()) { refreshChecks() }
    private val phoneStatePermission = registerForActivityResult(ActivityResultContracts.RequestPermission()) { refreshChecks() }
    private val receiveSmsPermission = registerForActivityResult(ActivityResultContracts.RequestPermission()) { refreshChecks() }

    override fun onCreate(savedInstanceState: Bundle?) {
        enableEdgeToEdge()
        super.onCreate(savedInstanceState)
        handleIntent(intent)
        lifecycleScope.launch {
            repeatOnLifecycle(Lifecycle.State.STARTED) {
                combine(container.store.pairing, container.store.pushRegistered, container.store.forwardInbound) { p, r, f ->
                    Triple(p, r != null, f)
                }.collect { (p, r, f) ->
                    updateChecks(p, r, f)
                    // Forwarding was just turned on in the dashboard: ask for the permission it needs.
                    if (f && p != null && !hasPermission(Manifest.permission.RECEIVE_SMS) && !askedReceiveSms) {
                        askedReceiveSms = true
                        receiveSmsPermission.launch(Manifest.permission.RECEIVE_SMS)
                    }
                }
            }
        }

        setContent {
            BridgeTheme {
                Surface(Modifier.fillMaxSize()) {
                    val state by vm.state.collectAsStateWithLifecycle()
                    var scanning by rememberSaveable { mutableStateOf(false) }
                    val pairing = state.pairing

                    when {
                        state.loading -> Unit
                        pairing != null -> StatusScreen(
                            state = state,
                            pairing = pairing,
                            checks = checks,
                            actions = StatusActions(
                                allowSms = { smsPermission.launch(Manifest.permission.SEND_SMS) },
                                allowPhoneState = { phoneStatePermission.launch(Manifest.permission.READ_PHONE_STATE) },
                                allowReceiveSms = { receiveSmsPermission.launch(Manifest.permission.RECEIVE_SMS) },
                                allowNotifications = ::requestNotifications,
                                allowBackground = ::requestUnrestrictedBattery,
                                setupPush = { flavorPush.setup(this, pairing) },
                                reconnect = vm::reconnectNow,
                                unpair = vm::unpair,
                            ),
                        )
                        scanning -> {
                            BackHandler { scanning = false }
                            ScanScreen(
                                hasCameraPermission = cameraGranted,
                                onRequestPermission = { cameraPermission.launch(Manifest.permission.CAMERA) },
                                onResult = { scanning = false; vm.requestPairing(it) },
                                onCancel = { scanning = false },
                            )
                        }
                        else -> WelcomeScreen(
                            state = state,
                            onScan = {
                                cameraGranted = hasPermission(Manifest.permission.CAMERA)
                                if (!cameraGranted) cameraPermission.launch(Manifest.permission.CAMERA)
                                scanning = true
                            },
                            onManual = vm::requestPairing,
                            onManualError = vm::showError,
                            onDismissReason = vm::dismissUnpairedReason,
                        )
                    }

                    state.confirm?.let { request ->
                        ConfirmPairingDialog(request, onConfirm = vm::confirmPairing, onCancel = vm::cancelPairing)
                    }
                }
            }
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        handleIntent(intent)
    }

    override fun onResume() {
        super.onResume()
        refreshChecks()
    }

    private fun handleIntent(intent: Intent?) {
        val data = intent?.data?.toString() ?: return
        PairingUri.parse(data)?.let(vm::requestPairing)
        intent.data = null // do not re-handle the link on configuration changes
    }

    private fun refreshChecks() {
        lifecycleScope.launch {
            updateChecks(container.store.pairing.first(), container.store.pushRegistered.first() != null, container.store.forwardInbound())
        }
    }

    private suspend fun updateChecks(pairing: Pairing?, registered: Boolean, forwardInbound: Boolean) {
        val midnight = java.util.Calendar.getInstance().apply {
            set(java.util.Calendar.HOUR_OF_DAY, 0)
            set(java.util.Calendar.MINUTE, 0)
            set(java.util.Calendar.SECOND, 0)
            set(java.util.Calendar.MILLISECOND, 0)
        }.timeInMillis
        val (sent, failed) = withContext(Dispatchers.IO) { container.outbox.countsSince(midnight) }
        checks = ReliabilityChecks(
            smsAllowed = hasPermission(Manifest.permission.SEND_SMS),
            phoneStateAllowed = hasPermission(Manifest.permission.READ_PHONE_STATE),
            forwardInbound = forwardInbound,
            receiveSmsAllowed = hasPermission(Manifest.permission.RECEIVE_SMS),
            sentToday = sent,
            failedToday = failed,
            notificationsAllowed = Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU || hasPermission(Manifest.permission.POST_NOTIFICATIONS),
            batteryUnrestricted = getSystemService(PowerManager::class.java).isIgnoringBatteryOptimizations(packageName),
            push = runCatching { flavorPush.status(this, pairing, registered) }.getOrNull(),
        )
    }

    private fun requestNotifications() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            notificationPermission.launch(Manifest.permission.POST_NOTIFICATIONS)
        }
    }

    // The gateway is a long-running server-like app the user installs for this purpose;
    // asking for the exemption directly is the documented approach for such apps.
    @SuppressLint("BatteryLife")
    private fun requestUnrestrictedBattery() {
        val direct = Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS, "package:$packageName".toUri())
        runCatching { startActivity(direct) }.onFailure {
            startActivity(Intent(Settings.ACTION_IGNORE_BATTERY_OPTIMIZATION_SETTINGS))
        }
    }

    private fun hasPermission(p: String) = ContextCompat.checkSelfPermission(this, p) == PackageManager.PERMISSION_GRANTED
}
