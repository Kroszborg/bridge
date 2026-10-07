package dev.bridge.gateway.gateway

import dev.bridge.gateway.net.HeartbeatPolicyDto
import kotlin.math.min
import kotlin.math.pow
import kotlin.random.Random
import kotlin.time.Duration
import kotlin.time.Duration.Companion.milliseconds
import kotlin.time.Duration.Companion.minutes
import kotlin.time.Duration.Companion.seconds

/**
 * Capped exponential backoff with ±20% jitter, so a fleet of phones that lost
 * the server at the same moment does not reconnect in lockstep.
 */
class Backoff(
    private val base: Duration = 1.seconds,
    private val max: Duration = 5.minutes,
    private val random: Random = Random.Default,
) {
    fun delayFor(attempt: Int): Duration {
        val exp = base.inWholeMilliseconds * 2.0.pow(attempt.coerceIn(0, 20))
        val capped = min(exp, max.inWholeMilliseconds.toDouble())
        val jitter = 0.8 + random.nextDouble() * 0.4
        return (capped * jitter).toLong().milliseconds
    }
}

/**
 * How often to check in. Charging phones check in often; on battery the
 * interval stretches, and further in battery saver, to keep radio wake-ups rare.
 */
data class HeartbeatSchedule(
    val minSeconds: Int,
    val maxSeconds: Int,
    val chargingSeconds: Int,
    val onBatterySeconds: Int,
) {
    fun intervalSeconds(charging: Boolean, powerSave: Boolean): Int {
        val wanted = when {
            charging -> chargingSeconds
            powerSave -> onBatterySeconds * 2
            else -> onBatterySeconds
        }
        return wanted.coerceIn(minSeconds, maxSeconds)
    }

    companion object {
        fun from(dto: HeartbeatPolicyDto) =
            HeartbeatSchedule(dto.minSeconds, dto.maxSeconds, dto.chargingSeconds, dto.onBatterySeconds)
    }
}
