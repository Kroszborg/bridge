# Sending messages

```bash
curl "$BRIDGE_URL/v1/messages" \
  -H "Authorization: Bearer $BRIDGE_API_KEY" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: order-1042-shipped" \
  -d '{"to": "+919876543210", "message": "Your order has shipped.", "metadata": {"order_id": "1042"}}'
```

The response is `202 Accepted` with the message in status `queued`. Bridge then delivers it
through a paired phone (or an [SMS provider](../providers/README.md), if the project routes to one)
and records every step.

| Field | Required | Notes |
| --- | --- | --- |
| `to` | yes | E.164 with country code. Spaces and dashes are removed. |
| `message` | yes | Up to 1600 characters (about 10 SMS segments). |
| `device_id` | no | Send through this phone only. By default Bridge picks one. |
| `sim_slot` | no | `1` or `2`. By default the device's chosen SIM, else the phone's default SMS SIM. |
| `metadata` | no | Your own JSON object (32 keys, 4 KB), returned with the message. |

`Idempotency-Key` (header, optional): retrying with the same key returns the original message
(`200` and `Idempotent-Replayed: true`) instead of sending twice. The same key with a different
body is rejected with `409`.

A number on the project's [opt-out list](../automation/README.md#opt-out-list) (for example
because it replied `STOP`) is refused with `409` and code `opted_out`, and nothing is queued. Only
one-time passwords still go to it. Do not retry.

To send one template to many numbers, use a [broadcast](../broadcasts/README.md); to send at set
times, a [schedule](../schedules/README.md). Their messages are ordinary messages with
`metadata.broadcast_id` or `metadata.schedule_id`.

## Lifecycle

```text
queued --> sending --> sent --> delivered
   |          |          |
   +----------+----------+--> failed
```

| Status | Meaning |
| --- | --- |
| `queued` | Accepted; waiting for a phone, or assigned and waiting for it to accept |
| `sending` | The phone accepted the job and handed it to Android |
| `sent` | Android sent every segment. Carrier delivery not yet confirmed |
| `delivered` | The carrier's delivery report confirmed it |
| `failed` | See `error_code` and `error_message` |

Bridge never reports `delivered` without the carrier's delivery report. Some carriers never send
one, so `sent` can be final. `GET /v1/messages/{id}` returns the full timeline:

```text
14:01:02.114  Created
14:01:02.131  Queued
14:01:02.402  Assigned to Pixel 7
14:01:03.050  Phone accepted
14:01:05.880  Sent · 1 segment
14:01:07.215  Delivered
```

## How Bridge picks a phone

Bridge considers phones in the project that are online, under their send limit and under their
daily cap. It prefers a phone that is charging and on Wi-Fi, then the one that has used the least of
its allowance, then the one used least recently.

If no phone can take the message, it waits in the queue. Offline phones with a push registration are
woken. The message fails with `no_device_available` after an hour (`daily_limit_reached` when every
online phone is at its daily cap), or straight away with `no_device` when the project has no phones.

A phone that does not accept a job within 2 minutes loses it, and the job goes to another phone or is
retried. Failures the phone marks as retryable (no service, radio off) are retried up to 3 attempts.
Phones de-duplicate jobs by message and attempt, so a redelivered job is never sent twice.

## Phones and providers

Every message has a `provider` field saying what sends it:

| `provider` | Meaning |
| --- | --- |
| `android` | A paired phone. |
| `simulator` | The test-mode simulator (`bk_test_` keys). |
| `fallback` | Handed to the project's SMS providers; waiting for one to accept it. |
| `msg91`, `twilio`, `vonage`, `plivo` | That SMS provider accepted it. |

By default projects use only their phones. A project can route to an SMS provider when no phone can
send a message (`phones_then_providers`), or send everything through providers (`providers`). The
timeline then shows `provider_fallback` with a `reason` (such as `no_phone_available` or
`phone_failed`) and `provider_accepted`. A provider accepting a message counts as `sent`; its
delivery report, if any, moves it to `delivered` or `failed`. Messages sent with a `device_id` or a
test key never go to a provider. See [SMS providers](../providers/README.md) for setup, exactly when
fallback happens, and provider error codes (`twilio_21211`, `msg91_no_template`, …).

## Android's sending limit

Android asks the phone's owner to approve each SMS beyond about **30 per 30 minutes** from one
app. Until someone taps *Allow*, the message waits. Bridge therefore paces each phone under its
**send limit** (30 per 30 minutes by default) and queues the rest. More phones mean more capacity.

To raise a phone's limit, first lift Android's limit over USB with
[adb](https://developer.android.com/tools/adb):

```bash
adb shell settings put global sms_outgoing_check_max_count 1000
adb shell settings put global sms_outgoing_check_interval_ms 1800000
```

Then raise the limit in **Phones → ⋯ → Settings**. Carriers may still throttle or block bulk
sending from a SIM; Bridge is not a way around carrier rules.

### Daily cap

Operators also limit how many SMS a SIM may send a day, about 100 on most Indian plans, and can
block SIMs that send far more. Each phone therefore has a **daily cap** (`daily_send_limit`, 100 in
any rolling 24 hours by default, 1 to 10,000), set by an admin or owner under **Phones → ⋯ →
Settings → Messages per day**. A phone at its cap gets no more messages until its oldest send in the
window is 24 hours old. `GET /v1/devices` returns each phone's `daily_send_limit` and `day_sends`.
Keep the cap within your SIM plan's allowance; to send more, pair more phones or route to an
[SMS provider](../providers/README.md).

## Test mode

`bk_test_` keys run the same API, validation and lifecycle with a simulated phone. Nothing is
sent and it costs nothing. These numbers produce specific outcomes:

| Number | Outcome |
| --- | --- |
| `+15550000002` | `failed`: `invalid_destination` |
| `+15550000003` | `sent`, with no delivery report |
| `+15550000004` | `failed`: `send_timeout` |
| `+15550000005` | `failed`: `delivery_failed` after sending |
| any other | `delivered` |

## Error codes

| `error_code` | Meaning |
| --- | --- |
| `no_device` | The project has no paired phones |
| `no_device_available` | No phone could take the message within an hour |
| `daily_limit_reached` | Every online phone had already sent its daily cap, and none had room within an hour |
| `device_not_found` | The requested `device_id` was removed |
| `device_unresponsive` | Phones did not accept the job after 3 attempts |
| `permission_denied` | The Bridge app lacks the SMS permission |
| `sim_unavailable` | The requested SIM is missing, or the app lacks Phone access |
| `no_service`, `radio_off`, `network_error` | The phone could not reach the mobile network |
| `limit_exceeded` | Android's sending limit was hit; see above |
| `delivery_failed` | The carrier reported the message undelivered |
| `generic_failure`, `android_error_<n>` | Android could not confirm sending. Not retried, because the SMS may have gone out |
| `<provider>_<code>` | An SMS provider refused or failed the message; see [SMS providers](../providers/README.md#error-codes) |
| `canceled` | Canceled while still waiting in the queue, because its [broadcast](../broadcasts/README.md#cancel) was canceled |

## Timeline entries from messaging tools

Besides status changes, a message's timeline can show:

| Entry | On | Detail |
| --- | --- | --- |
| `canceled` | Outgoing | The message was canceled before any phone or provider took it. `reason`: `broadcast_canceled`. The message is `failed` with `error_code: canceled`. |
| `auto_reply` | Incoming | An [auto-reply rule](../automation/README.md#auto-reply-rules) matched: `rule_id`, `rule_name`, `keyword`, `action`, and `reply_message_id`, `reply_skipped` (`loop_protection`, `device_removed`) or `reply_error`. When the sender is not a phone number: `skipped: sender_not_a_phone_number`. |
| `automation_skipped` | Incoming | Auto-replies and forwarding were not run. `reason: forwarded_by_bridge`: the SMS is one of Bridge's own forwards arriving at another of your phones. |

## Incoming messages

Phones with forwarding turned on store the SMS they receive as messages with `direction: inbound`
and `status: received`. They are listed with outgoing ones; filter with `?direction=inbound` and
`?from=<sender>`. They are always in the live environment. To be told as they arrive, add a
`message.received` [webhook](../webhooks/README.md#incoming-sms).

## Privacy and retention

Message bodies are kept for `BRIDGE_MESSAGE_RETENTION` (default 30 days, i.e. `720h`) and then
removed. Status, timeline and metadata stay. Bodies never appear in logs, and recipients are logged
with only their last three digits. Phones drop a message's text as soon as Android takes it.

## Rate limits

| Limit | Default |
| --- | --- |
| Requests per API key | 300 per minute |
| Messages per project | 1,000 per hour |
| Messages per destination number | 20 per hour |

[Broadcast](../broadcasts/README.md#pacing-and-limits) messages do not count against the two
message limits; a project may create 20 broadcasts an hour instead.
