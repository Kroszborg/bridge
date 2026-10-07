package dev.bridge.gateway.sms

/** Why Android could not send, in Bridge's terms. Retryable failures may succeed on another try or phone. */
data class SendFailure(val code: String, val message: String, val retryable: Boolean)

/**
 * Maps SmsManager result codes (RESULT_ERROR_*, RESULT_RIL_*) to stable error codes.
 * Values are spelled out so this stays testable on the JVM.
 */
fun sendFailure(resultCode: Int): SendFailure = when (resultCode) {
    1 -> SendFailure("generic_failure", "Android reported a generic send failure.", retryable = true)
    2 -> SendFailure("radio_off", "The phone's radio is off (airplane mode?).", retryable = true)
    3 -> SendFailure("null_pdu", "Android could not encode the message.", retryable = false)
    4 -> SendFailure("no_service", "The SIM has no mobile service right now.", retryable = true)
    5 -> SendFailure("limit_exceeded", "Android's SMS sending limit was reached; approve sending on the phone or lower the device's send limit.", retryable = true)
    6 -> SendFailure("fdn_check_failure", "Fixed dialing on this SIM blocks the number.", retryable = false)
    7 -> SendFailure("short_code_not_allowed", "The user declined sending to this short code.", retryable = false)
    8 -> SendFailure("short_code_never_allowed", "Sending to this short code is blocked on the phone.", retryable = false)
    9 -> SendFailure("radio_not_available", "The phone's radio is not available.", retryable = true)
    10 -> SendFailure("network_reject", "The mobile network rejected the message.", retryable = false)
    11 -> SendFailure("invalid_arguments", "Android rejected the message parameters.", retryable = false)
    12 -> SendFailure("invalid_state", "The phone's SMS service was not ready.", retryable = true)
    14 -> SendFailure("invalid_sms_format", "The message format is invalid.", retryable = false)
    17 -> SendFailure("network_error", "A mobile network error occurred.", retryable = true)
    19 -> SendFailure("invalid_smsc_address", "The SIM's SMS centre address is invalid.", retryable = false)
    20 -> SendFailure("operation_not_allowed", "The carrier does not allow this SIM to send SMS.", retryable = false)
    else -> SendFailure("android_error_$resultCode", "Android could not send the message (code $resultCode).", retryable = true)
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
