# SMS providers

Bridge sends SMS through your paired Android phones. A project can also use an SMS provider
(MSG91, Twilio, Vonage or Plivo) as a fallback when no phone can send, or instead of phones
altogether. Your application keeps calling the same API; only what delivers the SMS changes.

Typical reasons to add one:

- **Fallback.** A phone is offline, out of signal, or at its send limit or daily cap, and a sign-in
  code must still go out within a minute.
- **No phone at all.** You want Bridge's API, Verify, webhooks and logs, with a provider doing the
  sending.
- **Rules a SIM cannot meet.** In India, application-to-person SMS must use DLT-registered templates
  and sender IDs; MSG91 sends those.

Providers charge per message at their own prices. Bridge adds nothing and sees no bill.

## Routing

Each project has one routing mode. Set it in the dashboard under **Providers**, or with
`PUT /v1/projects/{projectId}/routing`.

| Mode | Dashboard | What happens |
| --- | --- | --- |
| `phones` (default) | Phones only | Providers are never used, even when configured. |
| `phones_then_providers` | Phones, then providers | Phones send first. A provider takes a message no phone can send (see below). |
| `providers` | Providers only | Providers send every live message. Phones are not used for messages without a `device_id`. |

| Setting | Default | Range | Notes |
| --- | --- | --- | --- |
| `fallback_after_seconds` | 60 | 0 to 3600 | With `phones_then_providers`: how long a message may wait for a phone before a provider takes it. `0` hands it over as soon as no phone can take it. |

A mode other than `phones` only takes effect while the project has at least one **enabled**
provider. With none, Bridge behaves as in `phones` mode.

### When a message goes to a provider

With `phones_then_providers`, a message moves to the providers in these cases. The timeline shows
a `provider_fallback` event with the `reason`:

| `reason` | When |
| --- | --- |
| `no_paired_phone` | The project has no paired phones. |
| `no_phone_available` | Phones are paired, but none could take the message (offline, or at its send limit or daily cap, `daily_send_limit`) within `fallback_after_seconds`. |
| `no_phone_in_time` | The message has waited longer than the queue timeout (1 hour). |
| `phone_failed` | A phone reported a failure that will not be retried, or retries ran out (3 attempts). Not used for `invalid_destination`, which a provider would refuse too. |
| `phone_unresponsive` | Phones did not accept the job after 3 assignments. |

With `providers`, every eligible message goes straight to the providers with reason
`providers_only`.

A message **never** goes to a provider when:

- it was sent with a test key (`bk_test_…`). Test messages always go to the simulator, so tests
  never cost money;
- it was sent with a `device_id`. You asked for one phone, so Bridge uses that phone or fails;
- the routing mode is `phones`, or the project has no enabled provider.

Without a fallback, these cases fail as before (`no_device`, `no_device_available`,
`device_unresponsive` or the phone's error); see [Sending messages](../messages/README.md).

### How a provider sends

1. Bridge tries the project's enabled providers in **priority** order (lower first, 0 to 100; ties
   go to the one added first). The first one that accepts the message wins.
2. When a provider accepts it, the message moves to `sent`, as when a phone reports it sent. The
   timeline shows `provider_accepted` and `sent`, and `provider` becomes the provider's name
   (`msg91`, `twilio`, `vonage` or `plivo`).
3. If the provider later reports delivery, the message moves to `delivered`, or to `failed` with the
   provider's error code. Without a report, `sent` is final.

While a message waits for a provider to accept it, its `provider` is `fallback` and its status is
`queued`.

Errors are either **retryable** (the provider could not be reached, answered `429` or a `5xx`, or,
for Vonage, status `1` throttled or `5` internal error) or **permanent** (anything else, such as a
bad number, missing credit or a rejected sender). When every provider fails and at least one error
was retryable, Bridge tries the whole list again later, up to 6 attempts in total. Otherwise, or
after the last attempt, the message fails with the error of the last provider tried. If no provider
is enabled by the time the job runs, it fails with `no_provider`.

## Before you start

**`BRIDGE_SECRET_KEY` must be set.** Provider credentials are encrypted with it, and without it
Bridge refuses to save them (`409`, "This server cannot store provider credentials yet"). Generate
one and restart Bridge:

```bash
openssl rand -base64 32     # put the output in BRIDGE_SECRET_KEY
```

The value is 32 random bytes as base64 (standard or URL-safe) or hex. Keep it with your other
secrets and back it up: if it is lost or changed, stored credentials cannot be read, those providers
are skipped (their `last_error` says why), and you must enter the credentials again. See
[Self-hosting](../self-hosting/README.md#configuration).

**`BRIDGE_PUBLIC_URL` must be reachable from the internet** for delivery reports. Providers post
them to `<BRIDGE_PUBLIC_URL>/v1/provider-callbacks/{id}/{token}`. On `http://localhost:8080` the
provider still sends your messages, but reports never arrive, so messages stay `sent`.

## Add a provider

In the dashboard, open **Providers → Add a provider**, choose the provider and fill in its form. Only owners and
admins can add, change, check or remove providers, or change routing. A project can have one
account of each provider.

With the API (session-authenticated, as the dashboard uses it):

```http
POST /v1/projects/{projectId}/providers
Content-Type: application/json

{
  "kind": "twilio",
  "name": "Twilio",
  "priority": 0,
  "credentials": { "account_sid": "AC…", "auth_token": "…" },
  "config": { "messaging_service_sid": "MG…" }
}
```

`credentials` are write-only: they are encrypted and never returned. The response shows a
`credential_hint` instead (the Twilio account SID, the Vonage API key, the Plivo Auth ID, or the
last 4 characters of the MSG91 auth key). Updating `credentials` replaces all of them; updating
`config` replaces the whole config. Unknown settings are rejected.

### MSG91

MSG91 sends DLT-registered templates through its Flow API (`POST /api/v5/flow`). Bridge cannot send
free text through MSG91: every message uses one of your templates.

| Setting | Required | Notes |
| --- | --- | --- |
| `credentials.auth_key` | yes | MSG91 → API → Auth key. |
| `config.otp_template_id` | one of the two | MSG91 template ID for one-time passwords. |
| `config.otp_variable` | no | The template variable that receives the code. Default `OTP`. |
| `config.message_template_id` | one of the two | MSG91 template ID for every other message. |
| `config.message_variable` | no | The template variable that receives the whole message text. Default `message`. |
| `config.sender_id` | no | Your DLT-approved 6-character sender ID (header), if the template does not set one. |

How a message picks a template:

- **One-time passwords** (Verify codes, and codes from the [Supabase hook](../integrations/supabase.md))
  use the OTP template. Only the code is sent, as the variable `otp_variable`. The rest of the
  text comes from your MSG91 template, not from Bridge's Verify template.
- **Everything else** uses the message template, with the whole text as `message_variable`. Your
  DLT template must allow that text (DLT operators check content against the registered template).
- With no fitting template, the message fails with `msg91_no_template`.

MSG91 template variable names are case-sensitive. If your template contains `##OTP##`, the variable
is `OTP`.

**Delivery reports must be set up in MSG91**, because MSG91 does not accept a callback URL per
message. In MSG91, open **SMS → Webhook (New) → Create Webhook**, choose the SMS event **On Report
Received**, and paste the provider's **callback URL** from Bridge (shown on the provider in the
dashboard, and as `callback_url` in the API). Keep at least `requestId`, `status` and
`failureReason` in the payload.

Bridge reads MSG91's numeric statuses: `1` is delivered; `2` (failed), `9` (NDNC), `16` and `25`
(rejected), `17` (blocked number) and `20` (country code blocked) are failures, reported as
`msg91_<status>`. Others, such as `0` (sent), are ignored. Bridge accepts one report, a JSON array
of reports, or an array under `data`.

### Twilio

Uses Twilio's Messages API.

| Setting | Required | Notes |
| --- | --- | --- |
| `credentials.account_sid` | yes | Starts with `AC`. Twilio Console → Account info. |
| `credentials.auth_token` | yes | |
| `config.from` | one of the two | A Twilio number in E.164, or an alphanumeric sender ID where the destination allows it. |
| `config.messaging_service_sid` | one of the two | Starts with `MG`. Used instead of `from` when both are set. |

With a Messaging Service, Twilio picks the sender from the service's senders; `from` always sends
from that one number or sender ID. Bridge passes its callback URL with
each message as `StatusCallback`, so delivery reports need no setup in Twilio.

### Vonage

Uses Vonage's SMS API (`/sms/json`).

| Setting | Required | Notes |
| --- | --- | --- |
| `credentials.api_key` | yes | |
| `credentials.api_secret` | yes | |
| `config.from` | yes | A Vonage number, or an alphanumeric sender ID where allowed. |

Bridge sends the message ID as `client-ref`, marks messages that need Unicode as `type=unicode`,
and passes its callback URL as `callback` with each message.

### Plivo

Uses Plivo's Message API.

| Setting | Required | Notes |
| --- | --- | --- |
| `credentials.auth_id` | yes | |
| `credentials.auth_token` | yes | |
| `config.from` | yes | A Plivo number in E.164, or a sender ID where allowed. |

Bridge passes its callback URL with each message as `url` (method `POST`).

## Delivery reports

| Provider | How the callback URL reaches the provider | Bridge acts on |
| --- | --- | --- |
| MSG91 | You set it once in MSG91 (see above) | `1` delivered; `2`, `9`, `16`, `17`, `20`, `25` failed |
| Twilio | With each message (`StatusCallback`) | `delivered`; `undelivered`, `failed` |
| Vonage | With each message (`callback`) | `delivered`; `failed`, `rejected`, `expired` |
| Plivo | With each message (`url`) | `delivered`; `failed`, `undelivered`, `rejected` |

Each provider account has its own callback URL with a random token:
`<BRIDGE_PUBLIC_URL>/v1/provider-callbacks/{id}/{token}`. Bridge accepts `GET` and `POST`, reads
form, query-string or JSON bodies, and answers `200 ok`. A wrong token gets `404`. Treat the URL
as a secret: anyone who has it can report your messages delivered or failed.

A report moves a `sent` message to `delivered`, or a `sent` or `sending` message to `failed`. Other
statuses (such as Twilio's `sent` or MSG91's `0`) are ignored, and so are reports for messages
Bridge does not know.

## Error codes

Provider errors appear as the message's `error_code`, prefixed with the provider's name:

| `error_code` | Meaning |
| --- | --- |
| `twilio_<code>` | Twilio's error code, e.g. `twilio_21211` (invalid `To` number). |
| `vonage_<status>` | Vonage's status when sending, or its `err-code` in a delivery report. |
| `plivo_<http status>` | Plivo refused the message, e.g. `plivo_400`. In delivery reports, Plivo's `ErrorCode`. |
| `msg91_<http status>` | MSG91 refused the message. In delivery reports, MSG91's status, e.g. `msg91_17`. |
| `msg91_no_template` | No MSG91 template fits the message. |
| `<provider>_unreachable` | Bridge could not reach the provider (retried). |
| `<provider>_http_<status>` | The provider answered `429` or a `5xx` (retried). |
| `vonage_empty` | Vonage returned no message status (retried). |
| `no_provider` | No provider was enabled when the message was handed over. |

`error_message` carries the provider's own text where it gave one. The provider's last refusal is
also shown on the provider in the dashboard (`last_error`), and cleared by its next success.

## Checking credentials

**Check credentials** on a provider (or `POST /v1/projects/{projectId}/providers/{providerId}/check`) asks the
provider about the account without sending anything:

| Provider | What Bridge asks |
| --- | --- |
| Twilio | The account's name and status. |
| Vonage | The account balance. |
| Plivo | The account's name and credit. |
| MSG91 | The transactional balance (best effort). |

The answer is `{"ok": true, "detail": "…"}` or `{"ok": false, "detail": "<reason>"}`. A check only
proves the credentials work; it does not check your sender ID, templates or DLT registration. Send
a real message to confirm those.

## API

All routes are session-authenticated (the dashboard's API), not API-key routes.

| Method and path | Purpose |
| --- | --- |
| `GET /v1/provider-kinds` | Supported providers and the settings each needs. |
| `GET`, `PUT /v1/projects/{projectId}/routing` | Read or change `mode` and `fallback_after_seconds`. `secret_key_set` says whether `BRIDGE_SECRET_KEY` is set. |
| `GET`, `POST /v1/projects/{projectId}/providers` | List or add providers. |
| `PATCH`, `DELETE /v1/projects/{projectId}/providers/{providerId}` | Change (`name`, `enabled`, `priority`, `credentials`, `config`) or remove a provider. |
| `POST /v1/projects/{projectId}/providers/{providerId}/check` | Check the credentials. |

Adding, changing and removing providers and changing routing are written to the audit log
(`provider.added`, `provider.updated`, `provider.removed`, `routing.updated`). Credentials never
appear in it.

## Limitations

- **MSG91 sends templates only.** One-time passwords need an OTP template; any other message needs a
  message template whose registered text matches what you send.
- **MSG91's Check is best effort.** When MSG91's answer is neither a balance nor an auth error, Check
  succeeds with "the key could not be confirmed". Send a test message to be sure.
- **MSG91's delivery-report format** is read as MSG91 documents it (`requestId`, numeric `status`,
  `failureReason`). It has not yet been confirmed against a live MSG91 account.
- **Providers cost money**, at their own prices, for every message they accept.
- **Test keys never use providers.** Use a live key and a real number to try a provider.
- **Possible duplicates.** A message is handed to a provider after a phone failure that is not
  retried. Android's ambiguous failures (such as `generic_failure`) are among those, and the phone
  may in fact have sent the SMS, so the recipient can receive it twice. Likewise, a timeout talking
  to a provider is retried, and the provider may have accepted the first attempt.
- One account per provider per project.
