package dev.bridge.gateway

import android.content.Context
import dev.bridge.gateway.account.AccountRepository
import dev.bridge.gateway.account.AccountStore
import dev.bridge.gateway.account.SessionCookieJar
import dev.bridge.gateway.data.CredentialCipher
import dev.bridge.gateway.data.GatewayStore
import dev.bridge.gateway.data.Pairing
import dev.bridge.gateway.diagnostics.EventLog
import dev.bridge.gateway.gateway.ConnectionManager
import dev.bridge.gateway.gateway.Frames
import dev.bridge.gateway.gateway.ServerFrame
import dev.bridge.gateway.sms.Outbox
import dev.bridge.gateway.sms.ReportPump
import dev.bridge.gateway.sms.SendJob
import dev.bridge.gateway.sms.SmsSender
import dev.bridge.gateway.gateway.DeviceStatusReader
import dev.bridge.gateway.gateway.GatewayService
import dev.bridge.gateway.gateway.GatewaySession
import dev.bridge.gateway.gateway.HeartbeatSchedule
import dev.bridge.gateway.gateway.NetworkMonitor
import dev.bridge.gateway.gateway.OkHttpSocketOpener
import dev.bridge.gateway.gateway.SyncWorker
import dev.bridge.gateway.net.AccountApi
import dev.bridge.gateway.net.BridgeApi
import dev.bridge.gateway.net.PairRequest
import dev.bridge.gateway.net.PushRegistration
import dev.bridge.gateway.pairing.PairingRequest
import dev.bridge.gateway.push.flavorPush
import dev.bridge.gateway.widget.WidgetUpdater
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineExceptionHandler
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch
import kotlinx.coroutines.withTimeout
import okhttp3.OkHttpClient
import java.security.MessageDigest
import java.util.concurrent.TimeUnit
import kotlin.time.Duration.Companion.seconds

/** Wires the app's dependencies by hand; there are few enough that a DI framework would only add weight. */
class AppContainer(private val context: Context) {
    /** What the gateway did and why, kept across restarts: Status → Connection log. */
    val events = EventLog(context)

    // A failure in background work is logged; it must not take the whole gateway down with it.
    val scope = CoroutineScope(
        SupervisorJob() + Dispatchers.Default + CoroutineExceptionHandler { _, e ->
            events.record(EventLog.APP, "Background task failed: ${EventLog.describe(e)}", warn = true)
        },
    )
    val userAgent = "BridgeGateway/${BuildConfig.VERSION_NAME} (${BuildConfig.FLAVOR}; Android ${android.os.Build.VERSION.RELEASE})"

    private val http = OkHttpClient.Builder()
        .connectTimeout(15, TimeUnit.SECONDS)
        .readTimeout(30, TimeUnit.SECONDS)
        .callTimeout(60, TimeUnit.SECONDS)
        .addInterceptor { chain -> chain.proceed(chain.request().newBuilder().header("User-Agent", userAgent).build()) }
        .build()

    // WebSockets stay open indefinitely; liveness comes from app heartbeats.
    private val wsHttp = http.newBuilder().readTimeout(0, TimeUnit.MILLISECONDS).callTimeout(0, TimeUnit.MILLISECONDS).build()

    val api = BridgeApi(http)
    val store = GatewayStore(context, CredentialCipher())

    // The signed-in side has its own cookie jar and Keystore key, so it never touches the device credential.
    private val accountStore = AccountStore(context, CredentialCipher("bridge_account_session"))
    private val sessionCookies = SessionCookieJar(accountStore::cookies, accountStore::saveCookies)
    val account = AccountRepository(AccountApi(http.newBuilder().cookieJar(sessionCookies).build()), accountStore, sessionCookies)
    val statusReader = DeviceStatusReader(context)
    val network = NetworkMonitor(context) { events.record(EventLog.NETWORK, it) }

    val outbox = Outbox(context)
    val reports = ReportPump(outbox) { text -> connection.sendRaw(text) }
    val smsSender = SmsSender(context, outbox, reports)

    val connection: ConnectionManager = ConnectionManager(
        scope = scope,
        opener = OkHttpSocketOpener(wsHttp, userAgent),
        networkAvailable = network.available,
        session = ::session,
        snapshot = { statusReader.snapshot() },
        onRevoked = { message -> forget(message) },
        onConnected = { reports.flush() },
        onFrame = { frame -> handleFrame(frame) },
        networkChanges = network.changes,
        log = { events.record(EventLog.CONNECTION, it) },
    )

    /** Keeps home-screen widgets in step with the connection and the outbox. */
    val widgets = WidgetUpdater(context, this)

    private fun handleFrame(frame: ServerFrame) {
        when (frame.type) {
            Frames.SEND_SMS -> {
                val id = frame.messageId ?: return
                val to = frame.to ?: return
                val body = frame.body ?: return
                smsSender.handle(SendJob(id, frame.attempt ?: 1, to, body, frame.simSlot))
            }
            Frames.REPORT_ACK -> {
                val id = frame.messageId ?: return
                reports.acknowledged(id, frame.report ?: return)
            }
            Frames.WELCOME, Frames.CONFIG -> frame.forwardInbound?.let { forward ->
                launch { store.setForwardInbound(forward) }
            }
        }
    }

    private suspend fun session(): GatewaySession? {
        val pairing = store.pairing() ?: return null
        val credential = store.credential() ?: return null
        return GatewaySession(pairing.websocketUrl, credential, HeartbeatSchedule.from(pairing.heartbeat))
    }

    /**
     * Exchanges a pairing token for a credential, then starts the gateway. If the
     * phone is still paired (say the dashboard removed it while it was offline),
     * the new pairing replaces the old one, but only once the server accepted the
     * code, so a failed attempt leaves the working pairing alone.
     */
    suspend fun pair(request: PairingRequest): Pairing {
        val previous = store.pairing()
        events.record(EventLog.PAIRING, "Pairing with ${request.host}${previous?.let { " (replacing ${it.projectName})" } ?: ""}")
        val response = try {
            api.pair(
                request.apiUrl,
                PairRequest(
                    token = request.token,
                    installationId = store.installationId(),
                    deviceModel = DeviceStatusReader.deviceModel(),
                    androidVersion = android.os.Build.VERSION.RELEASE,
                    appVersion = BuildConfig.VERSION_NAME,
                    appFlavor = BuildConfig.FLAVOR,
                ),
            )
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            events.record(EventLog.PAIRING, "Pairing with ${request.host} failed: ${e.javaClass.simpleName}: ${e.message}", warn = true)
            throw e
        }
        val pairing = Pairing(
            deviceId = response.deviceId,
            projectId = response.projectId,
            projectName = response.projectName,
            apiUrl = response.apiUrl.trimEnd('/'),
            websocketUrl = response.websocketUrl,
            heartbeat = response.heartbeat,
            push = response.push,
            pairedAt = System.currentTimeMillis(),
        )
        if (previous != null) {
            val previousCredential = store.credential()
            connection.stop("replaced by a new pairing")
            if (previous.deviceId != pairing.deviceId || previous.apiUrl != pairing.apiUrl) {
                // A different device record: release the old one so it does not linger in that dashboard.
                if (previousCredential != null) {
                    runCatching { withTimeout(10.seconds) { api.unpair(previous.apiUrl, previousCredential) } }
                }
                runCatching { flavorPush.reset(context) }
                outbox.clear()
            }
        }
        store.savePairing(pairing, response.credential)
        events.record(EventLog.PAIRING, "Paired with ${pairing.projectName} as ${pairing.deviceId}")
        startGateway("paired")
        return pairing
    }

    /**
     * Starts the connection, the foreground service that keeps it alive and the
     * periodic watchdog. The connection starts first and does not depend on the
     * service: Android refuses a foreground service started from the background
     * unless Bridge is exempt from battery optimisation, and the watchdog then
     * tries again shortly.
     */
    fun startGateway(reason: String) {
        connection.start(reason)
        if (!GatewayService.start(context)) SyncWorker.runNow(context)
        SyncWorker.schedule(context)
    }

    /** Starts whatever part of the gateway is not running, if the phone is paired. */
    suspend fun ensureRunning(reason: String) {
        if (store.pairing() == null) return
        if (connection.isRunning && GatewayService.isRunning) return
        startGateway(reason)
    }

    /** Sends a push registration to the server unless it is unchanged. */
    suspend fun registerPush(registration: PushRegistration) {
        val pairing = store.pairing() ?: return
        val credential = store.credential() ?: return
        val fingerprint = sha256("${registration.provider}|${registration.endpoint}|${registration.p256dh}|${registration.auth}")
        if (store.pushFingerprint() == fingerprint) return
        runCatching { api.registerPush(pairing.apiUrl, credential, registration) }
            .onSuccess {
                store.setPushRegistered(registration.provider, fingerprint)
                events.record(EventLog.PUSH, "Wake-ups registered with the server (${registration.provider})")
            }
            .onFailure { events.record(EventLog.PUSH, "Push registration failed: ${it.message}", warn = true) }
    }

    /** Disconnects from the server at the user's request. */
    suspend fun unpair() {
        val pairing = store.pairing()
        val credential = store.credential()
        if (pairing != null && credential != null) {
            runCatching { api.unpair(pairing.apiUrl, credential) }
        }
        forget(null)
    }

    /** Drops the pairing locally and stops all background work. */
    suspend fun forget(reason: String?) {
        events.record(EventLog.PAIRING, "Forgetting the pairing${reason?.let { ": $it" } ?: " at the user's request"}", warn = reason != null)
        connection.stop("unpaired")
        GatewayService.stop(context)
        SyncWorker.cancel(context)
        runCatching { flavorPush.reset(context) }
        outbox.clear()
        store.clearPairing(reason)
    }

    fun launch(block: suspend CoroutineScope.() -> Unit) = scope.launch(block = block)

    private fun sha256(s: String): String =
        MessageDigest.getInstance("SHA-256").digest(s.toByteArray()).joinToString("") { "%02x".format(it) }

    companion object {
        const val TAG = EventLog.TAG
    }
}
