package dev.bridge.gateway.sms

/**
 * Why Android could not send, in Bridge's terms.
 *
 * [retryable] is true only when Android is sure nothing reached the network
 * (radio off, no service, modem not ready, or an explicit "retry"). Ambiguous
 * failures are final: on many phones, especially over IMS/VoLTE, a "generic
 * failure" can arrive after the carrier already accepted the SMS, and a retry
 * would send the recipient a duplicate.
 */
data class SendFailure(val code: String, val message: String, val retryable: Boolean)

private const val MAYBE_SENT = " The SMS may still have been sent, so Bridge does not retry it automatically."

/**
 * Maps SmsManager result codes (RESULT_ERROR_*, RESULT_*, RESULT_RIL_*) to
 * stable error codes. [modemError] is the radio's own cause code from the
 * "errorCode" extra, when the phone provides one. Values are spelled out so
 * this stays testable on the JVM.
 */
fun sendFailure(resultCode: Int, modemError: Int = -1): SendFailure {
    val f = when (resultCode) {
        // Nothing left the phone: safe to try again, here or on another phone.
        2 -> SendFailure("radio_off", "The phone's radio is off (airplane mode?).", true)
        4 -> SendFailure("no_service", "The SIM has no mobile service right now.", true)
        5 -> SendFailure("limit_exceeded", "Android's SMS sending limit was reached. Approve sending on the phone, or lower the device's send limit.", true)
        9, 100 -> SendFailure("radio_not_available", "The phone's radio is not available.", true)
        12, 103 -> SendFailure("invalid_state", "The phone's SMS service was not ready.", true)
        13, 105 -> SendFailure("no_memory", "The phone ran out of memory for SMS.", true)
        22, 118 -> SendFailure("no_resources", "The modem was busy.", true)
        101 -> SendFailure("send_fail_retry", "The network asked the phone to retry.", true)
        106 -> SendFailure("rate_limited", "The modem is rate limiting SMS.", true)
        115 -> SendFailure("invalid_modem_state", "The modem was not ready.", true)
        116 -> SendFailure("network_not_ready", "The mobile network was not ready.", true)
        123 -> SendFailure("blocked_by_call", "A phone call blocked SMS sending.", true)

        // Refused for good: retrying would fail the same way.
        3 -> SendFailure("null_pdu", "Android could not encode the message.", false)
        6 -> SendFailure("fdn_check_failure", "Fixed dialing on this SIM blocks the number.", false)
        7 -> SendFailure("short_code_not_allowed", "The phone's owner declined sending to this short code.", false)
        8 -> SendFailure("short_code_never_allowed", "Sending to this short code is blocked on the phone.", false)
        10, 102 -> SendFailure("network_reject", "The mobile network rejected the message. Check the number and the SIM's SMS plan.", false)
        11, 104 -> SendFailure("invalid_arguments", "Android rejected the message parameters.", false)
        14, 107 -> SendFailure("invalid_sms_format", "The message format is invalid.", false)
        18, 109 -> SendFailure("encoding_error", "The message could not be encoded.", false)
        19, 110 -> SendFailure("invalid_smsc_address", "The SIM's SMS centre number is invalid. Check it in the phone's messaging settings.", false)
        20, 117 -> SendFailure("operation_not_allowed", "The carrier does not allow this SIM to send SMS.", false)
        24, 114 -> SendFailure("not_supported", "This phone does not support the request.", false)
        29 -> SendFailure("blocked_during_emergency", "SMS is blocked during an emergency call.", false)
        120 -> SendFailure("sim_absent", "The SIM is missing.", false)
        122 -> SendFailure("access_barred", "The network barred this SIM from sending.", false)

        // Ambiguous: the SMS may have gone out.
        1 -> SendFailure("generic_failure", "Android reported a generic send failure." + MAYBE_SENT, false)
        else -> SendFailure("android_error_$resultCode", "Android could not confirm sending (code $resultCode)." + MAYBE_SENT, false)
    }
    return if (modemError > 0) f.copy(message = f.message.removeSuffix(MAYBE_SENT) + " Modem cause $modemError." + if (f.message.endsWith(MAYBE_SENT)) MAYBE_SENT else "") else f
}

enum class DeliveryClass { Delivered, Pending, Failed, Unknown }

/**
 * Classifies a 3GPP TP-Status from a delivery report:
 * 0x00–0x1F completed, 0x20–0x3F temporary error (the SMSC keeps trying),
 * 0x40 and above failed.
 */
fun classifyDelivery(status: Int): DeliveryClass = when {
    status < 0 -> DeliveryClass.Unknown
    status < 0x20 -> DeliveryClass.Delivered
    status < 0x40 -> DeliveryClass.Pending
    else -> DeliveryClass.Failed
}
