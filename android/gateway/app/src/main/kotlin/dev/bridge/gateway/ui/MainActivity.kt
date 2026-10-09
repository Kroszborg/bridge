package dev.bridge.gateway.ui

import android.Manifest
import android.annotation.SuppressLint
import android.content.ActivityNotFoundException
import android.content.ClipData
import android.content.ClipboardManager
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import android.os.Bundle
import android.os.PowerManager
import android.provider.Settings
import android.widget.Toast
import androidx.activity.ComponentActivity
import androidx.activity.compose.BackHandler
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.activity.viewModels
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.Surface
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalUriHandler
import androidx.core.content.ContextCompat
import androidx.core.net.toUri
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.bridge.gateway.account.ServerUrls
import dev.bridge.gateway.container
import dev.bridge.gateway.diagnostics.BackgroundSettings
import dev.bridge.gateway.diagnostics.EventLog
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
    private val accountVm: AccountViewModel by viewModels()
    private val messagesVm: MessagesViewModel by viewModels()
    private val sendVm: SendViewModel by viewModels()
    private val phonesVm: PhonesViewModel by viewModels()
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
                    val events by container.events.events.collectAsStateWithLifecycle()
                    var scanning by rememberSaveable { mutableStateOf(false) }
                    var showLog by rememberSaveable { mutableStateOf(false) }
                    var signingIn by rememberSaveable { mutableStateOf(false) }
                    var picking by rememberSaveable { mutableStateOf(false) }
                    var tab by rememberSaveable { mutableStateOf(HomeTab.Gateway) }
                    var about by rememberSaveable { mutableStateOf(false) }
                    var offerPair by rememberSaveable { mutableStateOf(false) }
                    var confirmDisconnect by rememberSaveable { mutableStateOf(false) }
                    val pairing = state.pairing
                    val account = state.account
                    val uri = LocalUriHandler.current
                    val upgrade: (() -> Unit)? = account?.organization?.let { org ->
                        { uri.openSafely(ServerUrls.billing(account.dashboardUrl, org.id)) }
                    }

                    LaunchedEffect(account != null) { if (account != null) signingIn = false }

                    val startScan = {
                        cameraGranted = hasPermission(Manifest.permission.CAMERA)
                        if (!cameraGranted) cameraPermission.launch(Manifest.permission.CAMERA)
                        scanning = true
                    }
                    val openLog = { showLog = true }
                    val gateway: @Composable () -> Unit = {
                        if (pairing != null) {
                            StatusScreen(
                                state = state,
                                pairing = pairing,
                                checks = checks,
                                actions = statusActions(pairing, { about = true }, signIn = null, openLog = openLog, pairAgain = startScan),
                                recentEvents = events,
                            )
                        } else {
                            WelcomeScreen(
                                state = state,
                                onScan = startScan,
                                onManual = vm::requestPairing,
                                onManualError = vm::showError,
                                onDismissReason = vm::dismissUnpairedReason,
                                onAbout = { about = true },
                                onPairWithAccount = vm::pairWithAccount,
                                onUpgrade = upgrade,
                                onOpenLog = openLog,
                            )
                        }
                    }

                    when {
                        state.loading -> Unit
                        scanning -> {
                            BackHandler { scanning = false }
                            ScanScreen(
                                hasCameraPermission = cameraGranted,
                                onRequestPermission = { cameraPermission.launch(Manifest.permission.CAMERA) },
                                onResult = { scanning = false; vm.requestPairing(it) },
                                onCancel = { scanning = false },
                            )
                        }
                        showLog -> {
                            BackHandler { showLog = false }
                            ConnectionLogScreen(events, onCopy = ::copyLog, onBack = { showLog = false })
                        }
                        account == null && signingIn -> {
                            BackHandler { signingIn = false }
                            SignInScreen(accountVm, onBack = { signingIn = false })
                        }
                        account != null && (account.project == null || picking) -> {
                            if (account.project != null) BackHandler { picking = false }
                            ProjectPickerScreen(
                                vm = accountVm,
                                currentProjectId = account.project?.id,
                                onPicked = { project ->
                                    accountVm.selectProject(project)
                                    picking = false
                                    if (pairing == null) offerPair = true
                                },
                                onBack = if (account.project != null) ({ picking = false }) else null,
                                onSignOut = { accountVm.signOut(alsoDisconnect = false) },
                            )
                        }
                        account != null -> HomeShell(tab, onTab = { tab = it }) { current ->
                            when (current) {
                                HomeTab.Gateway -> gateway()
                                HomeTab.Messages -> MessagesScreen(messagesVm, account)
                                HomeTab.Send -> SendScreen(sendVm, account, pairing, onUpgrade = upgrade ?: {})
                                HomeTab.Phones -> PhonesScreen(phonesVm, account, pairing, onDisconnectThisPhone = { confirmDisconnect = true })
                                HomeTab.Account -> AccountScreen(accountVm, account, pairing, onSwitchProject = { picking = true })
                            }
                        }
                        pairing != null -> StatusScreen(
                            state = state,
                            pairing = pairing,
                            checks = checks,
                            actions = statusActions(pairing, { about = true }, signIn = { signingIn = true }, openLog = openLog, pairAgain = startScan),
                            recentEvents = events,
                        )
                        else -> WelcomeScreen(
                            state = state,
                            onScan = startScan,
                            onManual = vm::requestPairing,
                            onManualError = vm::showError,
                            onDismissReason = vm::dismissUnpairedReason,
                            onAbout = { about = true },
                            onSignIn = { signingIn = true },
                            onOpenLog = openLog,
                        )
                    }

                    state.confirm?.let { confirm ->
                        ConfirmPairingDialog(confirm, onConfirm = vm::confirmPairing, onCancel = vm::cancelPairing)
                    }
                    if (about) AboutDialog(onDismiss = { about = false })
                    val project = account?.project
                    if (offerPair && project != null && pairing == null) {
                        OfferPairingDialog(
                            projectName = project.name,
                            onPair = { offerPair = false; tab = HomeTab.Gateway; vm.pairWithAccount() },
                            onLater = { offerPair = false },
                        )
                    }
                    if (confirmDisconnect && pairing != null) {
                        DisconnectDialog(
                            projectName = pairing.projectName,
                            onConfirm = { confirmDisconnect = false; vm.unpair() },
                            onCancel = { confirmDisconnect = false },
                        )
                    }
                }
            }
        }
    }

    private fun statusActions(
        pairing: Pairing,
        about: () -> Unit,
        signIn: (() -> Unit)?,
        openLog: () -> Unit,
        pairAgain: () -> Unit,
    ) = StatusActions(
        allowSms = { smsPermission.launch(Manifest.permission.SEND_SMS) },
        allowPhoneState = { phoneStatePermission.launch(Manifest.permission.READ_PHONE_STATE) },
        allowReceiveSms = { receiveSmsPermission.launch(Manifest.permission.RECEIVE_SMS) },
        allowNotifications = ::requestNotifications,
        allowBackground = ::requestUnrestrictedBattery,
        setupPush = { flavorPush.setup(this, pairing) },
        reconnect = vm::reconnectNow,
        unpair = vm::unpair,
        about = about,
        signIn = signIn,
        openVendorSettings = ::openVendorSettings,
        openLog = openLog,
        pairAgain = pairAgain,
        dismissError = vm::dismissError,
    )

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        handleIntent(intent)
    }

    override fun onResume() {
        super.onResume()
        refreshChecks()
        vm.ensureRunning()
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
            vendor = BackgroundSettings.skin(),
            vendorSettingsOpened = BackgroundSettings.opened(this),
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
        val attempts = listOf(direct, Intent(Settings.ACTION_IGNORE_BATTERY_OPTIMIZATION_SETTINGS), BackgroundSettings.appDetails(packageName))
        for (intent in attempts) {
            try {
                startActivity(intent)
                return
            } catch (_: ActivityNotFoundException) {
            } catch (_: SecurityException) {
            }
        }
    }

    private fun openVendorSettings() {
        val skin = BackgroundSettings.skin() ?: return
        val opened = BackgroundSettings.open(this, skin)
        container.events.record(EventLog.APP, if (opened) "Opened ${BackgroundSettings.brand(skin)} background settings" else "No background settings page found")
    }

    private fun copyLog() {
        val text = container.events.export()
        getSystemService(ClipboardManager::class.java).setPrimaryClip(ClipData.newPlainText("Bridge connection log", text))
        // Android 13 and later confirm a copy themselves.
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU) Toast.makeText(this, "Connection log copied", Toast.LENGTH_SHORT).show()
    }

    private fun hasPermission(p: String) = ContextCompat.checkSelfPermission(this, p) == PackageManager.PERMISSION_GRANTED
}
