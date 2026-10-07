package dev.bridge.gateway

import android.content.Context
import android.util.Log
import dev.bridge.gateway.data.CredentialCipher
import dev.bridge.gateway.data.GatewayStore
import dev.bridge.gateway.data.Pairing
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
import dev.bridge.gateway.net.BridgeApi
import dev.bridge.gateway.net.PairRequest
import dev.bridge.gateway.net.PushRegistration
import dev.bridge.gateway.pairing.PairingRequest
import dev.bridge.gateway.push.flavorPush
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch
import okhttp3.OkHttpClient
import java.security.MessageDigest
import java.util.concurrent.TimeUnit

/** Wires the app's dependencies by hand; there are few enough that a DI framework would only add weight. */
class AppContainer(private val context: Context) {
    val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
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
    val statusReader = DeviceStatusReader(context)
    val network = NetworkMonitor(context)

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
    )

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

    /** Exchanges a pairing token for a credential, then starts the gateway. */
    suspend fun pair(request: PairingRequest): Pairing {
        val response = api.pair(
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
        store.savePairing(pairing, response.credential)
        startGateway()
        return pairing
    }

    /** Starts the foreground service and the periodic background check. */
    fun startGateway() {
        GatewayService.start(context)
        SyncWorker.schedule(context)
    }

    /** Sends a push registration to the server unless it is unchanged. */
    suspend fun registerPush(registration: PushRegistration) {
        val pairing = store.pairing() ?: return
        val credential = store.credential() ?: return
        val fingerprint = sha256("${registration.provider}|${registration.endpoint}|${registration.p256dh}|${registration.auth}")
        if (store.pushFingerprint() == fingerprint) return
        runCatching { api.registerPush(pairing.apiUrl, credential, registration) }
            .onSuccess { store.setPushRegistered(registration.provider, fingerprint) }
            .onFailure { Log.w(TAG, "Push registration failed: ${it.message}") }
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
        connection.stop()
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
        const val TAG = "BridgeGateway"
    }
}
