# Broadcasts

A broadcast sends one message template to up to 10,000 people, filling `{placeholders}` from each
recipient's own values. Bridge creates the messages gradually, paced to what your phones can send,
and tells you when the last one is done.

```bash
curl "$BRIDGE_URL/v1/broadcasts" \
  -H "Authorization: Bearer $BRIDGE_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Shipping update",
    "template": "Hi {name}, your order {order} has shipped.",
    "recipients": [
      {"to": "+919876543210", "vars": {"name": "Asha", "order": "A-1042"}},
      {"to": "+919812345678", "vars": {"name": "Ravi", "order": "A-1043"}}
    ]
  }'
```

The response is `201 Created` with the broadcast in status `sending`. Each message goes through
the normal pipeline (routing, phones, [SMS providers](../providers/README.md), retries, webhooks)
and carries `metadata.broadcast_id`, so you can find it in the message list and in
`message.*` webhooks.

With the [TypeScript SDK](../../packages/sdk/README.md):

```ts
const broadcast = await bridge.broadcasts.create({
  name: 'Shipping update',
  template: 'Hi {name}, your order {order} has shipped.',
  recipients: orders.map((o) => ({ to: o.phone, vars: { name: o.firstName, order: o.id } })),
});
const done = await bridge.broadcasts.waitFor(broadcast.id, { timeoutMs: 3_600_000 });
console.log(done.counts); // { recipients, queued, sent, delivered, failed, canceled, skipped, duplicates }
```

With [bridgectl](../cli/README.md#broadcasts-from-a-csv-file), from a CSV file:

```bash
bridgectl broadcast send --csv customers.csv --template "Hi {name}, your order {order} has shipped."
```

## Request

`POST /v1/broadcasts`

| Field | Required | Notes |
| --- | --- | --- |
| `template` | yes | The message, up to 1600 characters, with `{placeholders}`. |
| `recipients` | yes | 1 to 10,000 rows of `{ "to": "+91…", "vars": { … } }`. |
| `name` | no | Up to 100 characters, to find the broadcast later. |
| `device_id` | no | Send every message through this phone. By default Bridge picks per message. |
| `scheduled_at` | no | Start later, at this RFC 3339 time, at most one year ahead. See [Scheduling a broadcast](#scheduling-a-broadcast). |
| `dry_run` | no | `true` validates and previews without creating anything. See [Dry run](#dry-run). |

Broadcasts take no `Idempotency-Key`. A request that failed on the network may still have created
the broadcast, so list recent broadcasts (`GET /v1/broadcasts`) before sending again. The SDK does
not retry `create` for the same reason.

## Template variables

A placeholder is `{` + a name + `}`. The name starts with a letter or underscore, followed by up to
39 letters, digits or underscores. Any other braces are ordinary text, so `{not a var}` and `{}`
are sent as they are.

| Rule | Limit |
| --- | --- |
| Every placeholder needs a value | for every recipient. An empty string counts as a value. |
| Variables per recipient | 20 |
| Length of one value | 500 characters |
| Length of the rendered message | 1600 characters, and not empty |

Bridge renders every recipient when you create the broadcast. If any row breaks a rule, the whole
request fails with `422 validation_failed`, naming the row:

```json
{
  "error": {
    "code": "validation_failed",
    "message": "validation failed",
    "details": [
      {
        "location": "body.recipients[41].vars",
        "message": "Row 42 (+919812345678): the template uses {order} but this recipient has no value for it."
      }
    ]
  }
}
```

Numbers are normalised to E.164 (`+` and the country code). A row whose number cannot be read also
fails the request with its row number.

## CSV files

The API takes JSON. The CLI and the dashboard read CSV files for you:

```text
to,name,order
+919876543210,Asha,A-1042
+919812345678,"Ravi, Jr.",A-1043
```

* The first row names the columns. The first column, `to` or `phone`, holds the numbers.
* Every other column is a variable named by its header, so `{name}` reads the `name` column.
* Quote values that contain commas. Blank lines are skipped.
* `bridgectl` sends only the columns the template uses, and stops with an error before sending if
  the template names a column the file does not have.

## Dry run

Send the same request with `"dry_run": true` to check it first. Nothing is created and nothing
counts against the hourly broadcast limit. The response is `200 OK`:

```json
{
  "dry_run": true,
  "recipients": 2,
  "skipped_opted_out": 1,
  "duplicates": 1,
  "total_segments": 2,
  "samples": [
    { "to": "+919876543210", "text": "Hi Asha, your order A-1042 has shipped.", "segments": 1, "encoding": "gsm7" },
    { "to": "+919812345678", "text": "Hi Ravi, your order A-1043 has shipped.", "segments": 1, "encoding": "gsm7" }
  ]
}
```

| Field | Meaning |
| --- | --- |
| `recipients` | Unique numbers that would receive the message. |
| `skipped_opted_out` | Numbers left out because they are on the [opt-out list](../automation/README.md#opt-out-list). |
| `duplicates` | Repeated numbers removed. The first row of each number is kept. |
| `total_segments` | SMS segments for every recipient together. Carriers and providers bill per segment. |
| `samples` | The first 5 rendered messages, with segments and encoding (`gsm7` or `ucs2`). |

Validation errors are the same as for a real broadcast. `bridge.broadcasts.preview(params)` in the
SDK and `bridgectl broadcast send … --dry-run` do the same.

## Scheduling a broadcast

Pass `scheduled_at` to start later:

```json
{ "template": "Our sale starts now.", "recipients": [ … ], "scheduled_at": "2026-11-01T09:00:00+05:30" }
```

The broadcast is created with status `scheduled` and starts sending at that time. Recipients,
duplicates and opted-out numbers are counted when you create it, and each number is checked against
the opt-out list again when its turn comes. A time up to one minute in the past is treated as now;
anything earlier is refused. Cancel a scheduled broadcast to stop it before it starts.

For a message that repeats (every day, every Monday, the first of the month), use a
[schedule](../schedules/README.md) instead.

## Pacing and limits

A broadcast never floods the queue. Bridge keeps a **window** of the broadcast's messages waiting
to be sent at once, and creates more only as earlier ones finish:

| Case | Window |
| --- | --- |
| Test key (`bk_test_…`) | 500 |
| No `device_id`, routing is not `phones`, and the project has an enabled SMS provider | 500 |
| Otherwise | The sum of the send limits of the project's online phones (or of the chosen phone), each no more than what is left of its daily cap; at least 10 and at most 500 |

Messages are created in batches of up to 100. When the window is full, Bridge checks again every
10 seconds. Phones then send at their own pace, under their **send limit** (30 per 30 minutes by
default, see [Android's sending limit](../messages/README.md#androids-sending-limit)) and their
**daily cap** (100 in any 24 hours by default, see [Daily cap](../messages/README.md#daily-cap)).
The window counts only what each online phone may still send today, so it is roughly what your
phones can send in one limit period and no message waits long enough to hit the one-hour queue
timeout.

How long a broadcast takes is set by your phones. One phone at the default daily cap sends 100
messages a day. While every phone is at its cap, Bridge still keeps a few messages waiting, and
those that no phone can take within an hour fail with `daily_limit_reached`, so part of a broadcast
larger than your phones' daily caps together fails. Size broadcasts to your phones, pair more
phones, raise a cap only if the SIM plan allows it, or route to a provider. If no phone is online
for an hour, waiting messages fail with `no_device_available`.

| Limit | Value |
| --- | --- |
| Recipients per broadcast | 10,000 rows (duplicates included) |
| Broadcasts per project | 20 per hour, counted when created; dry runs do not count |
| Request body | 4 MB |
| Template and rendered message | 1600 characters each |
| `scheduled_at` | at most one year ahead |

Broadcast messages do **not** count against the per-message hourly limits (1,000 messages per
project and 20 per destination number); a long list would otherwise stop after the first thousand.
The broadcast is admitted as a whole instead, under the limits above. Over the hourly broadcast
limit the request fails with `429 rate_limited` and `Retry-After`. The API key's request limit
(300 per minute) applies to the request itself as usual.

## Status and counts

`GET /v1/broadcasts/{id}` returns the broadcast with live counts:

```json
{
  "id": "brd_06gj4m8c2v0b4n6m8q0s2u4w6y",
  "name": "Shipping update",
  "environment": "live",
  "status": "sending",
  "template": "Hi {name}, your order {order} has shipped.",
  "device_id": null,
  "scheduled_at": null,
  "counts": {
    "recipients": 2, "queued": 1, "sent": 0, "delivered": 1,
    "failed": 0, "canceled": 0, "skipped": 1, "duplicates": 1
  },
  "total_segments": 2,
  "created_at": "2026-10-07T09:00:00Z",
  "started_at": "2026-10-07T09:00:00Z",
  "completed_at": null,
  "canceled_at": null
}
```

| Status | Meaning |
| --- | --- |
| `scheduled` | Waiting for `scheduled_at`. |
| `sending` | Creating and sending messages. |
| `completed` | Every message was sent or failed. Delivery reports may still arrive afterwards. |
| `canceled` | Canceled; see below. |

| Count | Meaning |
| --- | --- |
| `recipients` | Unique numbers the broadcast sends to, after removing duplicates and opted-out numbers. |
| `queued` | Not sent yet: waiting for their turn, waiting for a phone, or being sent. |
| `sent` | Sent, with no delivery report yet. |
| `delivered` | The carrier confirmed delivery. |
| `failed` | Failed, see each message's `error_code`. |
| `canceled` | Not sent because the broadcast was canceled. |
| `skipped` | Opted-out numbers left out at creation, plus rows refused when their turn came (opted out meanwhile, or the chosen phone was removed). |
| `duplicates` | Repeated numbers removed at creation. |

`GET /v1/broadcasts` lists broadcasts in the key's environment, newest first (`limit` up to 100,
`starting_after`, and `status`). In the SDK: `bridge.broadcasts.get`, `list` and `listAll`.

## Opted-out numbers

Numbers on the project's [opt-out list](../automation/README.md#opt-out-list) never receive a
broadcast. They are left out when the broadcast is created (`skipped_opted_out` in the preview,
`skipped` in the counts), and a number that opts out while a long broadcast is running is skipped
when its turn comes. The broadcast itself does not fail.

## Cancel

```bash
curl -X POST "$BRIDGE_URL/v1/broadcasts/brd_06gj4m8c…/cancel" -H "Authorization: Bearer $BRIDGE_API_KEY"
```

Canceling a `scheduled` or `sending` broadcast:

* marks every recipient without a message yet as canceled;
* cancels its messages that are still waiting in the queue and not yet assigned to a phone. They
  become `failed` with `error_code: canceled`, a `canceled` timeline entry (`reason:
  broadcast_canceled`), and a `message.failed` webhook;
* leaves alone messages already assigned to a phone or handed to an SMS provider. They finish
  normally and keep their counts.

The response is the broadcast with status `canceled`. Canceling one that already completed or was
canceled returns `409 conflict`. A canceled broadcast does not send `broadcast.completed`.

## The `broadcast.completed` event

When every message of a broadcast has been sent or failed, Bridge sends a `broadcast.completed`
[webhook](../webhooks/README.md) whose `data` is the broadcast, as `GET /v1/broadcasts/{id}`
returns it, with final counts. Subscribe to it instead of polling. Delivery reports that arrive
later still produce `message.delivered` and `message.failed` events.

## Test mode

With a test key, broadcasts run against the simulator: nothing is sent, the window is 500, and the
[test numbers](../messages/README.md#test-mode) produce their usual outcomes, so you can watch a
broadcast complete with a mix of delivered and failed messages.

## Dashboard

In the dashboard, owners and admins can send and cancel live broadcasts; members can send test
broadcasts. Creating and canceling a broadcast there is recorded in the audit log.
