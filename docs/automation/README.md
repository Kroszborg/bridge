# Opt-outs, auto-replies and forwarding

Three tools react to the SMS your phones receive and keep you on the right side of the people you
message:

* The **opt-out list** holds numbers that asked not to be messaged. Bridge refuses ordinary
  messages to them.
* **Auto-reply rules** answer keywords such as `STOP`, `START` and `HELP`, and can add the sender
  to the opt-out list or take them off it. They work out of the box.
* **Forwarding rules** copy incoming SMS to another phone number, a Telegram chat, a webhook
  (your own, Slack or Discord) or an email address.

Auto-replies and forwarding need incoming SMS, so turn on **Forward incoming SMS** for at least one
phone (see [Incoming SMS](../webhooks/README.md#incoming-sms)). The opt-out list works without it.

The opt-out list has an API for your application (below). Auto-reply and forwarding rules are
managed in the dashboard, on the project's **Automation** page. Members can see them; owners and
admins change them, and every change is recorded in the audit log.

## Opt-out list

Each project has one list, shared by live and test keys. Numbers are stored in E.164 form.

| Sent to an opted-out number | What happens |
| --- | --- |
| A message (`POST /v1/messages`, the SDK, the CLI, MCP) | Refused with `409 opted_out`. Nothing is queued. |
| A [broadcast](../broadcasts/README.md) | The number is left out (`skipped`). The rest of the broadcast goes out. |
| A [schedule](../schedules/README.md) run | Not sent; the reason is in the schedule's `last_error`. |
| A forwarding rule's phone destination | The delivery fails: Bridge will not forward SMS to an opted-out number. |
| A one-time password ([Verify](../otp/README.md), Supabase hook) | **Sent.** People who opted out of marketing must still be able to sign in. |
| An auto-reply | **Sent**, so the person who texted `STOP` gets the confirmation. |

The list does not stop anything coming **in**: SMS from opted-out numbers are still received,
announced as `message.received`, answered by auto-reply rules (so `START` works) and forwarded.

```json
{
  "error": {
    "code": "opted_out",
    "message": "+919876543210 opted out of messages from this project (for example by replying STOP). Only one-time passwords can still be sent to it. Remove it from the opt-out list if the person asked to receive messages again.",
    "request_id": "req_06gj9a…"
  }
}
```

Treat `opted_out` as final for that number. Do not retry, and do not work around it with another
project or channel.

### How numbers get on the list

| `source` | How |
| --- | --- |
| `keyword` | The person texted a keyword of a rule whose action is `opt_out` (by default `STOP`, `UNSUBSCRIBE`, `CANCEL`, `END` or `QUIT`). `keyword` holds the word. |
| `manual` | Added in the dashboard. |
| `api` | Added with an API key, for example when someone unsubscribes in your app. |

A number leaves the list when it texts an `opt_in` keyword (`START` or `UNSTOP` by default), or when
you remove it. Only remove a number when the person asked to receive messages again.

### API

| Request | Result |
| --- | --- |
| `GET /v1/opt-outs` | Newest first. `limit` (1 to 100, default 50), `starting_after` (an entry ID) and `source`. |
| `POST /v1/opt-outs` with `{"number": "+919876543210"}` | `201` with the new entry, or `200` with the existing one if the number was already listed. |
| `GET /v1/opt-outs/{number}` | `200` with the entry when the number opted out; **`404` when it may be messaged**. |
| `DELETE /v1/opt-outs/{number}` | `204`, or `404` if the number was not listed. |

Write the `+` in a path as `%2B` (or leave it as is):

```bash
curl "$BRIDGE_URL/v1/opt-outs/%2B919876543210" -H "Authorization: Bearer $BRIDGE_API_KEY"
```

```json
{
  "id": "uns_06gj9c4e6g8j0l2n4q6s8u0w2y",
  "number": "+919876543210",
  "source": "keyword",
  "keyword": "STOP",
  "created_at": "2026-10-07T09:31:02Z"
}
```

```ts
if (await bridge.optOuts.isOptedOut('+919876543210')) return; // 404 becomes false
await bridge.optOuts.add('+919876543210');
for await (const o of bridge.optOuts.listAll({ source: 'keyword' })) console.log(o.number, o.keyword);
await bridge.optOuts.remove('+919876543210');
```

From the command line: `bridgectl optouts`, `optouts add NUMBER`, `optouts check NUMBER` and
`optouts remove NUMBER`. The dashboard can also export the list as CSV (`number`, `source`,
`keyword`, `created_at`).

## Auto-reply rules

When a phone receives an SMS, Bridge tries the project's enabled rules in order and runs the first
one that matches. Only one rule runs per SMS.

### Default rules

Every project starts with three rules, created the first time an SMS arrives or the rules are
opened. You can edit, disable or delete them; deleted defaults are not created again.

| Priority | Name | Keywords (exact) | Action | Reply |
| --- | --- | --- | --- | --- |
| 10 | Unsubscribe | `STOP`, `UNSUBSCRIBE`, `CANCEL`, `END`, `QUIT` | `opt_out` | You are unsubscribed. Reply START to subscribe again. |
| 20 | Subscribe again | `START`, `UNSTOP` | `opt_in` | You are subscribed again. |
| 30 | Help | `HELP` | `none` | Reply STOP to unsubscribe. |

Change the replies to name your service, for example `Acme alerts: you are unsubscribed.`

### Matching

| Setting | Values |
| --- | --- |
| `match` | `exact` (the whole message is the keyword), `starts_with` or `contains`. Default `exact`. |
| `keywords` | 1 to 20, up to 50 characters each. |
| `priority` | 0 to 10,000, lower first. Default 100. Rules with the same priority are tried oldest first. |
| `enabled` | Disabled rules are skipped. |

The message is trimmed and compared ignoring case, so ` stop ` and `Stop` match `STOP`. With
`exact`, `STOP please` does not match; use `starts_with` for that. Be careful with `contains`:
`END` would also match "weekend".

A project can have up to 50 rules.

### Actions and replies

| `action` | Effect |
| --- | --- |
| `none` | Reply only. |
| `opt_out` | Adds the sender to the opt-out list (`source: keyword`). |
| `opt_in` | Removes the sender from the opt-out list. |

The reply (up to 480 characters) is optional, but a rule needs a reply, an action or both. The
action runs first, then the reply is sent **through the phone that received the SMS**, as an
ordinary message with `metadata.auto_reply_rule_id` and `metadata.in_reply_to`. It is sent even to
an opted-out number, and the project's hourly message limits apply to it.

### Loop protection

Two automated senders could answer each other forever. Bridge prevents it:

* A rule replies to the same number **at most once every 10 minutes**. The action still runs; the
  reply is skipped (`reply_skipped: loop_protection`).
* Bridge never replies to, opts out or opts in **alphanumeric sender IDs** (`AX-HDFCBK`), **short
  codes** (fewer than 7 digits) or **numbers not in international format** (without `+` and a
  country code). The rule is recorded on the timeline with `skipped: sender_not_a_phone_number`
  and nothing else happens.
* An incoming SMS whose text matches a message Bridge **forwarded** in the last hour is one of its
  own forwards arriving at another of your phones. Bridge ignores it: no auto-reply and no
  forwarding (timeline: `automation_skipped`, `reason: forwarded_by_bridge`).
* Each incoming SMS is answered at most once, even if its processing is retried.

If the phone that received the SMS was removed meanwhile, the reply is skipped with
`reply_skipped: device_removed`.

### Events

The incoming message's timeline gets an `auto_reply` entry with `rule_id`, `rule_name`, `keyword`,
`action`, and either `reply_message_id`, `reply_skipped` or `reply_error`.

When a rule runs for a phone number, Bridge sends a `message.auto_replied`
[webhook](../webhooks/README.md#messageauto_replied):

```json
{
  "type": "message.auto_replied",
  "timestamp": "2026-10-07T09:31:03Z",
  "data": {
    "environment": "live",
    "message": { "id": "msg_06gj9b…", "direction": "inbound", "status": "received", "from": "+919876543210", "body": "STOP", "…": "…" },
    "rule_id": "arr_06gj8z…",
    "rule_name": "Unsubscribe",
    "keyword": "STOP",
    "action": "opt_out",
    "reply_message_id": "msg_06gj9d…"
  }
}
```

`reply_message_id` is `null` when the rule has no reply or the reply was skipped. Use this event to
mirror opt-outs into your own database.

## Forwarding rules

A forwarding rule copies matching incoming SMS to up to 5 destinations. Unlike auto-replies,
**every** matching enabled rule runs, so one SMS can go to several rules' destinations. A project
can have up to 20 rules. SMS from opted-out numbers are forwarded too.

### Match

| Setting | Notes |
| --- | --- |
| `senders` | Up to 20. An exact sender (`+919876543210`, `AX-HDFCBK`) or a prefix ending in `*` (`+9198*`, `AX-*`). Empty matches every sender. |
| `contains` | Only messages containing this text (up to 100 characters). Empty matches every message. |

Both compare ignoring case, and both must match. Leave both empty to forward everything.

### Destinations

| `type` | Settings | What arrives |
| --- | --- | --- |
| `phone` | `to`: an E.164 number | An SMS `From <sender>: <text>`, cut to 3 segments with `...`. |
| `telegram` | `bot_token`, `chat_id` | A message `SMS from <sender>`, a blank line and the text (up to 4,000 characters). |
| `webhook` | `url`, `format`: `json`, `slack` or `discord` | A signed `POST`. See below. |
| `email` | `to`: an address | A plain-text email with the subject `SMS from <sender>`. Needs SMTP on the server. |

**Phone.** The forward is an ordinary outgoing message (Bridge picks the phone), with
`metadata.forwarded_from` and `metadata.forwarding_rule_id`, and the hourly message limits apply.
It is skipped when the destination is the sender itself, and fails if the destination is on the
opt-out list.

### Telegram

1. In Telegram, open [@BotFather](https://t.me/BotFather), send `/newbot`, and pick a name and a
   username. BotFather answers with the bot token, such as `123456789:AAH…`.
2. Start a chat with your new bot and send it any message. For a group, add the bot to the group
   and send a message there (or `/start@yourbot`). For a channel, add the bot as an administrator.
3. Open `https://api.telegram.org/bot<token>/getUpdates` in a browser. Find `"chat":{"id":…}`: that
   number is the chat ID. Private chats have positive IDs, groups negative ones, and supergroups and
   channels start with `-100`. A public channel can also use its `@username`.
4. Add a `telegram` destination with the token and chat ID.

Telegram tokens are stored encrypted, so the server needs `BRIDGE_SECRET_KEY` (see
[self-hosting](../self-hosting/README.md#configuration)). The API never returns them;
`bot_token_set` says whether one is stored. When you edit a rule, keep a destination's `id` and
leave `bot_token` out to keep its stored token. If Telegram answers `400`, `401`, `403` or `404`
(a wrong token or chat, or the bot was removed from the chat), the delivery fails at once.

### Webhooks, Slack and Discord

| `format` | Body |
| --- | --- |
| `json` (default) | The incoming message, as `GET /v1/messages/{id}` returns it without the timeline. |
| `slack` | `{"text": "SMS from <sender>\n\n<text>"}` (up to 3,000 characters). |
| `discord` | `{"content": "SMS from <sender>\n\n<text>"}` (up to 2,000 characters). |

**Slack:** create an app at [api.slack.com/apps](https://api.slack.com/apps), turn on **Incoming
Webhooks**, choose **Add New Webhook to Workspace**, pick a channel, and copy the
`https://hooks.slack.com/services/…` URL into a `webhook` destination with format `slack`.

**Discord:** open the channel's **Edit Channel → Integrations → Webhooks → New Webhook**, then
**Copy Webhook URL** (`https://discord.com/api/webhooks/…`) into a `webhook` destination with
format `discord`.

Like [webhooks](../webhooks/README.md#self-hosting-endpoints-on-your-network), URLs must point at
public addresses unless the server sets `BRIDGE_WEBHOOK_ALLOW_PRIVATE_ENDPOINTS=true`, and
redirects are not followed.

#### Signing

Every request is signed with [Standard Webhooks](https://www.standardwebhooks.com) using the
**rule's** signing secret (`whsec_…`), shown when you create the rule and revealable later (each
reveal is audit-logged). It is not the secret of your webhook endpoints.

| Header | Value |
| --- | --- |
| `webhook-id` | The delivery ID (`fdl_…`). The same on every retry, so de-duplicate on it. |
| `webhook-timestamp` | Unix seconds. |
| `webhook-signature` | `v1,` and a base64 HMAC-SHA256 of `id.timestamp.body`. |

`User-Agent` is `Bridge-Forwarding/1`. Slack and Discord ignore the headers. Your own endpoint
should verify them, exactly as for [webhooks](../webhooks/README.md#verify-the-signature):

```js
import { Webhook } from "standardwebhooks";

const wh = new Webhook(process.env.BRIDGE_FORWARDING_SECRET);

app.post("/sms-forward", express.raw({ type: "application/json" }), (req, res) => {
  let sms;
  try {
    sms = wh.verify(req.body, req.headers); // the message itself: id, from, body, device_id, …
  } catch {
    return res.status(400).end();
  }
  res.status(204).end();
});
```

The `json` body is the message object, not an event envelope: it has no `type` field.

Answer with a `2xx` within 15 seconds. `400`, `401`, `403`, `404` and `410` fail the delivery at
once; other statuses, timeouts and connection errors are retried.

### Email

Email destinations need an SMTP server, set on the API and worker:

| Variable | Default | Notes |
| --- | --- | --- |
| `BRIDGE_SMTP_HOST` | | Turns email on. |
| `BRIDGE_SMTP_PORT` | `587` | |
| `BRIDGE_SMTP_TLS` | `starttls` | `starttls` (usually port 587), `tls` (TLS from the first byte, usually 465) or `none`. |
| `BRIDGE_SMTP_USERNAME`, `BRIDGE_SMTP_PASSWORD` | | When the server needs a login. |
| `BRIDGE_SMTP_FROM` | | Required with a host, for example `Bridge <sms@example.com>`. |

Restart Bridge after setting them. The rules list in the dashboard shows whether email is
available (`email_available`). Without SMTP, adding an email destination is refused with
`409 conflict`. See [Security](../security/README.md#forwarding-and-auto-replies) for how TLS is
handled.

The email body is the SMS text, then the sender, when it was received, the rule's name and the
message ID. It carries `Auto-Submitted: auto-generated`, so well-behaved autoresponders do not
answer it. A `5xx` answer from the mail server fails the delivery at once; other errors are
retried.

### Retries and the delivery log

Each incoming SMS gets one delivery per destination. Failed deliveries are retried for about four
hours: 8 attempts, waiting 10 seconds, 1 minute, 5 minutes, 15 minutes, 30 minutes, 1 hour and
2 hours between them (plus up to 10% jitter).

The rule's **delivery log** (`GET /v1/projects/{projectId}/forwarding-rules/{ruleId}/deliveries`,
newest first, up to 100) shows one entry per SMS and destination, updated on every attempt:

| `status` | Meaning |
| --- | --- |
| `pending` | Queued, not tried yet. |
| `retrying` | The last attempt failed; another is scheduled. |
| `succeeded` | Delivered. A phone forward has `forwarded_message_id`. |
| `failed` | A permanent error, or the last attempt failed. `error` and `response_status` say why. |
| `skipped` | Nothing to do: the rule was disabled before delivery, or the phone destination is the sender. |

A delivery also fails if the message text was removed by the retention policy before it could be
sent. Deleting a rule drops its pending deliveries; removing a destination from a rule deletes its
log. Delivery logs are kept for the message retention period (`BRIDGE_MESSAGE_RETENTION`).
