package dev.bridge.gateway.account

import dev.bridge.gateway.net.BridgeApiException
import kotlinx.serialization.SerializationException
import java.io.IOException

/** What the user was doing when a request failed; it decides how errors are worded. */
enum class AccountAction { SignIn, PairPhone, Send, Load, ManagePhone }

/** Turns API and network failures into messages for people, not developers. */
object AccountErrors {
    fun describe(error: Throwable, host: String, action: AccountAction): String = when (error) {
        is BridgeApiException -> describeApi(error, host, action)
        is SerializationException -> "$host did not answer like a Bridge server. Check the server address."
        is IOException -> "Could not reach $host. Check the server address and this phone's internet connection."
        else -> error.message ?: "Something went wrong. Try again."
    }

    private fun describeApi(e: BridgeApiException, host: String, action: AccountAction): String = when {
        e.status == 401 && action == AccountAction.SignIn -> "Email or password is incorrect."
        e.status == 401 -> "Your session has expired. Sign in again."
        e.status == 429 && action == AccountAction.SignIn ->
            "Too many sign-in attempts. Try again ${retryIn(e.retryAfterSeconds) ?: "in a few minutes"}."
        e.status == 429 -> "Too many requests. Try again ${retryIn(e.retryAfterSeconds) ?: "shortly"}."
        e.status == 422 && action == AccountAction.SignIn -> "Enter a valid email address and your password."
        e.status == 404 && action == AccountAction.SignIn -> "$host is not a Bridge server, or it runs an older version without sign-in."
        e.status == 403 && action == AccountAction.PairPhone ->
            "Only organization admins and owners can pair phones. Ask an admin to pair this phone or to change your role."
        e.status == 403 && action == AccountAction.Send && e.code == "forbidden" ->
            "Only organization admins and owners can send live messages. Send a test message instead, or ask an admin."
        e.status == 403 && action == AccountAction.ManagePhone ->
            "Only organization admins and owners can change phones."
        e.isPlanLimit -> e.message
        e.status >= 500 -> "$host had a problem handling the request. Try again in a moment." + (e.requestId?.let { " (Request $it)" } ?: "")
        else -> e.message
    }

    private fun retryIn(seconds: Int?): String? = when {
        seconds == null -> null
        seconds < 60 -> "in $seconds seconds"
        else -> "in ${(seconds + 59) / 60} minutes"
    }
}
