# Verify: one-time passwords

Bridge generates a code, sends it by SMS through your phones, and checks it. Your app makes two
calls and never stores, compares or expires a code itself.

```ts
import { Bridge } from '@kroszborg/bridge';

const bridge = new Bridge({ apiKey: process.env.BRIDGE_API_KEY, baseUrl: 'https://api.sms.example.com' });

// When the user asks for a code
await bridge.otp.send({ to: '+919876543210' });

// When they type it in
const { valid } = await bridge.otp.verify({ to: '+919876543210', code: '482913' });
```

The same with curl:

```bash
curl "$BRIDGE_URL/v1/otp" -H "Authorization: Bearer $BRIDGE_API_KEY" \
  -H "Content-Type: application/json" -d '{"to": "+919876543210"}'

curl "$BRIDGE_URL/v1/otp/verify" -H "Authorization: Bearer $BRIDGE_API_KEY" \
  -H "Content-Type: application/json" -d '{"to": "+919876543210", "code": "482913"}'
```

## Sending: `POST /v1/otp`

| Field | Required | Notes |
| --- | --- | --- |
| `to` | yes | E.164 with country code. |
| `android_app_hash` | no | Your app's 11-character SMS Retriever hash. Added as the last line so the app reads the code without SMS permission. |
| `metadata` | no | Your own JSON object (32 keys, 4 KB), returned with the verification. |

The response is `201` with a verification:

```json
{
  "id": "otp_06ghatc07nghtrbq1yj7nwyjtm",
  "status": "pending",
  "to": "+919876543210",
  "environment": "live",
  "attempts": 0,
  "attempts_remaining": 5,
  "expires_at": "2026-10-07T08:19:58Z",
  "resend_available_at": "2026-10-07T08:10:28Z",
  "verified_at": null,
  "message_id": "msg_06ghatc07skf5f2dgf8q4078mm",
  "message_status": "queued",
  "metadata": {},
  "created_at": "2026-10-07T08:09:58Z"
}
```

Sending a new code to the same number **cancels** the previous one (status `canceled`), so only the
latest code works. `message_status` follows the SMS that carries the code.

## Checking: `POST /v1/otp/verify`

Pass the `code` and either `to` (checks the latest pending code for that number) or `id`.

```json
{ "valid": true, "verification": { "id": "otp_…", "status": "verified", "attempts": 1, "…": "…" } }
```

`valid` is `true` only when this request's code is right. Otherwise read `verification.status`:

| Status | Meaning | What to tell the user |
| --- | --- | --- |
| `pending` | Wrong code; `attempts_remaining` are left | "Wrong code, try again" |
| `failed` | No attempts left | "Too many attempts, request a new code" |
| `expired` | The code lapsed | "The code expired, request a new one" |
| `verified` | Already used | Treat a repeat as not valid |
| `canceled` | A newer code was sent | "Use the latest code" |

A wrong code is a normal `200` with `valid: false`, not an error. `404` means there is no such
verification, or (with `to`) no code is pending for that number. Checking a finished verification
does not use an attempt.

`GET /v1/otp/{id}` returns a verification at any time.

## Events

Subscribe a [webhook](../webhooks/README.md) (or `bridgectl messages tail`) to `otp.verified`,
`otp.failed` and `otp.expired` to track sign-ups and spot abuse without polling. Each event's
`data` is the verification, without the code. A verification replaced by a newer code is not
announced.

## Limits

| | Default | Configurable |
| --- | --- | --- |
| Code length | 6 digits | 4 to 10 |
| Valid for | 10 minutes | 1 to 60 minutes |
| Attempts | 5 | 1 to 10 |
| New code to the same number | after 30 seconds | no |
| Codes to the same number | 5 per hour | no |

Limits that are hit return `429 rate_limited` with `Retry-After`. The per-number limits also bound
guessing: at most 5 codes × 5 attempts per number per hour. Regular message limits (per project
and per destination) apply too, because each code is an SMS.

## Message text

Change it under **Verify → Message and limits** in the dashboard. Placeholders:

| Placeholder | Becomes |
| --- | --- |
| `{code}` | The code (required, exactly once) |
| `{app}` | The app name you set, or the project name |
| `{minutes}` | How long the code is valid, rounded up |

The default is `{code} is your {app} code. It expires in {minutes} minutes. Do not share it.`,
which fits one SMS segment. Starting with the code helps phones that show only the first line of a
notification.

**Autofill.** Two optional additions put the code where the user needs it:

- **Android (SMS Retriever):** pass `android_app_hash` when sending. Bridge adds the hash as the
  last line, and your app receives the code without the SMS permission.
- **Browsers (WebOTP) and iOS:** set a domain such as `example.com`. Bridge adds
  `@example.com #482913` as the last line, which Chrome's WebOTP API and Safari use to offer the
  code above the keyboard on your site.

Both must be the last line, so when you pass an app hash, the domain line is left out of that SMS.

## iPhones

An iPhone cannot be a Bridge gateway: iOS does not let apps send SMS in the background, only through
a compose screen the user confirms. Pair Android phones to send, or use an
[SMS provider](../providers/README.md).

iPhones are fine on the receiving end. iOS reads the code from the SMS and offers it above the
keyboard:

- **Native apps:** iOS suggests codes from incoming SMS in fields marked for one-time codes
  (`textContentType = .oneTimeCode` in UIKit). Nothing to configure in Bridge.
- **Websites:** set the WebOTP domain under **Verify → Message and limits**. The `@example.com
  #482913` line lets Safari offer the code on that domain only. Mark the input with
  `autocomplete="one-time-code"`.

## Testing without a phone

With a `bk_test_` key nothing is sent, the SMS goes through the simulator, and the response
includes the code:

```ts
const sent = await bridge.otp.send({ to: '+15550000001' });
const { valid } = await bridge.otp.verify({ id: sent.id, code: sent.code! });
```

Live keys never return the code. Test and live verifications are separate: a test key cannot see
or check a live code. The simulator's [test numbers](../messages/README.md) behave as for messages,
so `+15550000002` gives you a code whose SMS fails.

From a terminal:

```bash
bridgectl otp send +15550000001        # prints the code with a test key
bridgectl otp verify +15550000001 482913
```

`bridgectl otp verify` exits with status 1 when the code is not valid, so it works in scripts.

## How codes are protected

- Codes come from the operating system's secure random generator.
- Bridge stores only an HMAC-SHA256 of each code, keyed per installation and bound to the
  verification ID. The hash is erased as soon as the verification finishes.
- The SMS body contains the code, so the API, the dashboard and webhooks always show it masked
  (`•••••• is your Acme code…`), and the stored text is erased once the code is used or the SMS
  has left the phone.
- Checks compare in constant time and lock the verification row, so two concurrent checks cannot
  both use the last attempt.
- Finished verifications are kept for 30 days for the dashboard, then deleted.
