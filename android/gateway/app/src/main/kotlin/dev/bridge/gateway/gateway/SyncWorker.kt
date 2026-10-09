package dev.bridge.gateway.gateway

import android.content.Context
import androidx.work.Constraints
import androidx.work.CoroutineWorker
import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.ExistingWorkPolicy
import androidx.work.NetworkType
import androidx.work.OneTimeWorkRequestBuilder
import androidx.work.OutOfQuotaPolicy
import androidx.work.PeriodicWorkRequestBuilder
import androidx.work.WorkManager
import androidx.work.WorkerParameters
import dev.bridge.gateway.container
import dev.bridge.gateway.diagnostics.EventLog
import dev.bridge.gateway.net.BridgeApiException
import dev.bridge.gateway.net.HeartbeatRequest
import dev.bridge.gateway.push.flavorPush
import java.io.IOException
import java.util.concurrent.TimeUnit

/**
 * Background safety net and watchdog, run every 15 minutes (Android's minimum)
 * when a network is available, and shortly after Android stops the service:
 * restarts the connection and the gateway service if either stopped, reports
 * status over HTTP, and refreshes the push registration.
 */
class SyncWorker(context: Context, params: WorkerParameters) : CoroutineWorker(context, params) {
    override suspend fun doWork(): Result {
        val c = applicationContext.container
        val pairing = c.store.pairing() ?: return Result.success()
        val credential = c.store.credential() ?: return Result.success()

        // Logged only when something was down, so the log is not one line every 15 minutes.
        if (!c.connection.isRunning || !GatewayService.isRunning) {
            c.events.record(
                EventLog.WORKER,
                "Watchdog: ${if (!c.connection.isRunning) "connection" else "gateway service"} was not running; restarting",
                warn = true,
            )
        }
        c.connection.start("watchdog")
        GatewayService.start(applicationContext, nudge = true)

        val snap = c.statusReader.snapshot()
        val interval = HeartbeatSchedule.from(pairing.heartbeat).intervalSeconds(snap.charging, snap.powerSave)
        try {
            val response = c.api.heartbeat(pairing.apiUrl, credential, HeartbeatRequest(nextIn = interval, status = snap.status))
            response.forwardInbound?.let { c.store.setForwardInbound(it) }
        } catch (e: BridgeApiException) {
            c.events.record(EventLog.WORKER, "Check-in over HTTP rejected: HTTP ${e.status} ${e.code}", warn = true)
            if (e.isCredentialRejected) c.forget(e.message)
            return Result.success()
        } catch (e: IOException) {
            c.events.record(EventLog.WORKER, "Check-in over HTTP failed: ${e.javaClass.simpleName}: ${e.message}")
            return Result.retry()
        }
        runCatching { flavorPush.refresh(applicationContext, pairing) }
        c.reports.flush()
        c.outbox.prune(System.currentTimeMillis() - 7L * 24 * 60 * 60 * 1000)
        return Result.success()
    }

    companion object {
        private const val PERIODIC = "gateway-sync"
        private const val NOW = "gateway-sync-now"
        private const val RESTART = "gateway-restart"

        private val online = Constraints.Builder().setRequiredNetworkType(NetworkType.CONNECTED).build()

        fun schedule(context: Context) {
            val request = PeriodicWorkRequestBuilder<SyncWorker>(15, TimeUnit.MINUTES)
                .setConstraints(online)
                .build()
            WorkManager.getInstance(context).enqueueUniquePeriodicWork(PERIODIC, ExistingPeriodicWorkPolicy.KEEP, request)
        }

        /** Runs a sync as soon as possible, e.g. after a push wake-up the service could not handle. */
        fun runNow(context: Context) {
            val request = OneTimeWorkRequestBuilder<SyncWorker>()
                .setConstraints(online)
                .setExpedited(OutOfQuotaPolicy.RUN_AS_NON_EXPEDITED_WORK_REQUEST)
                .build()
            WorkManager.getInstance(context).enqueueUniqueWork(NOW, ExistingWorkPolicy.REPLACE, request)
        }

        /** Checks again in a few seconds, after Android stopped the service or the app was swiped away. */
        fun restartSoon(context: Context) {
            val request = OneTimeWorkRequestBuilder<SyncWorker>()
                .setInitialDelay(10, TimeUnit.SECONDS)
                .build()
            WorkManager.getInstance(context).enqueueUniqueWork(RESTART, ExistingWorkPolicy.REPLACE, request)
        }

        fun cancel(context: Context) {
            WorkManager.getInstance(context).run {
                cancelUniqueWork(PERIODIC)
                cancelUniqueWork(NOW)
                cancelUniqueWork(RESTART)
            }
        }
    }
}
