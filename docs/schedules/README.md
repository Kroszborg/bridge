# Scheduled messages

A schedule sends one message to one number at set times: once, or every day, on chosen weekdays,
or once a month, at a wall-clock time in a time zone you name. Each run creates an ordinary
message with `metadata.schedule_id`, so it has the usual timeline, webhooks and retries.

```bash
curl "$BRIDGE_URL/v1/schedules" \
  -H "Authorization: Bearer $BRIDGE_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Standup reminder",
    "to": "+919876543210",
    "message": "Standup starts in 15 minutes.",
    "schedule": { "kind": "weekly", "days": ["mon", "wed", "fri"], "at": "09:45", "time_zone": "Asia/Kolkata" }
  }'
```

```ts
const s = await bridge.schedules.create({
  to: '+919876543210',
  message: 'Your rent is due tomorrow.',
  schedule: { kind: 'monthly', day_of_month: 31, at: '10:00', time_zone: 'Europe/London' },
});
console.log(s.description, s.next_run_at); // "Monthly on day 31 at 10:00 (Europe/London)" …
```

`bridgectl schedules` lists them with their next run and last error.

To send one message to many people at once, use a [broadcast](../broadcasts/README.md); it can
also start at a set time.

## Request

`POST /v1/schedules` returns `201` with the schedule.

| Field | Required | Notes |
| --- | --- | --- |
| `to` | yes | E.164 with country code. |
| `message` | yes | Up to 1600 characters. |
| `schedule` | yes | When to send. See below. |
| `name` | no | Up to 100 characters. |
| `device_id` | no | Send through this phone. By default Bridge picks one at each run. |
| `ends_at` | no | RFC 3339 time. No runs after it. |
| `paused` | no | `true` creates it paused. |

## Kinds

| `kind` | Also needs | Runs |
| --- | --- | --- |
| `once` | `date` (`YYYY-MM-DD`) | Once, on that date. |
| `daily` | | Every day. |
| `weekly` | `days`: one or more of `mon`, `tue`, `wed`, `thu`, `fri`, `sat`, `sun` | On those weekdays. Full names such as `monday` are accepted and shortened. |
| `monthly` | `day_of_month`: 1 to 31 | On that day each month. Months without that day use their last day, so `31` runs on 30 April and 28 or 29 February. |

Every kind also needs `at`, a 24-hour time (`HH:MM`, for example `09:45`), and `time_zone`, an
IANA name such as `Asia/Kolkata`, `Europe/London` or `America/New_York`. `UTC` works too. Fields
that do not apply to the kind are ignored. A `once` schedule whose date and time have already
passed in its time zone is refused.

## Time zones and daylight saving

Times are wall-clock times in the schedule's zone, so a 09:00 daily schedule stays at 09:00 local
time when the clocks change. Two cases need a rule:

| Case | What Bridge does | Example (`America/New_York`) |
| --- | --- | --- |
| The time does not exist that day (clocks go forward) | Runs as far past the gap as the time was into it. | 02:30 on 8 March 2026 runs at 03:30. |
| The time happens twice (clocks go back) | Runs once, at the first occurrence. | 01:30 on 1 November 2026 runs at 01:30 EDT, not again at 01:30 EST. |

Zones without daylight saving, such as `Asia/Kolkata`, never hit either case.

## The schedule object

```json
{
  "id": "sch_06gj7k2m4p6r8t0v2x4z6b8d0f",
  "name": "Standup reminder",
  "environment": "live",
  "to": "+919876543210",
  "message": "Standup starts in 15 minutes.",
  "device_id": null,
  "schedule": { "kind": "weekly", "at": "09:45", "days": ["mon", "wed", "fri"], "day_of_month": null, "date": null, "time_zone": "Asia/Kolkata" },
  "description": "Every Mon, Wed, Fri at 09:45 (Asia/Kolkata)",
  "status": "active",
  "paused": false,
  "ends_at": null,
  "next_run_at": "2026-10-09T04:15:00Z",
  "last_run_at": "2026-10-07T04:15:00Z",
  "last_message_id": "msg_06gj7q0c2e4g6j8l0n2q4s6u8w",
  "last_error": null,
  "run_count": 12,
  "created_at": "2026-09-14T08:00:00Z",
  "updated_at": "2026-10-07T04:15:01Z"
}
```

| `status` | Meaning |
| --- | --- |
| `active` | Will run at `next_run_at`. |
| `paused` | Sends nothing until resumed. |
| `completed` | Nothing left to run: a `once` schedule that ran, or the next run would be after `ends_at`. `next_run_at` is `null`. |

## When runs happen

Bridge checks for due schedules once a minute, so a run goes out within about a minute of
`next_run_at`. Each run then goes through the normal pipeline: it waits for a phone like any other
message, and the project's hourly message limits apply.

**Missed runs are not replayed.** If Bridge's worker was down when runs were due, the schedule
sends once when the worker is back, then continues from the next regular time. A daily reminder
missed for three days sends one message, not three.

## Run now, pause and resume

| Request | Effect |
| --- | --- |
| `POST /v1/schedules/{id}/run` | Sends the message once, now. The next regular run does not change. Works while paused. |
| `POST /v1/schedules/{id}/pause` | Nothing is sent until it is resumed. |
| `POST /v1/schedules/{id}/resume` | Runs missed while paused are skipped. The next run is computed from now. |

Pausing a paused schedule or resuming an active one changes nothing. Each of them returns the
schedule.

## Changing and deleting

`PATCH /v1/schedules/{id}` changes the fields you send and leaves the rest:

* `schedule` replaces the whole timing.
* Changing the timing, `ends_at` or `paused` computes the next run again from now. Changing only
  `to`, `message`, `name` or `device_id` keeps the next run.
* `ends_at: ""` removes the end; `device_id: ""` lets Bridge pick the phone again.

A change that would leave an active schedule with nothing to run (for example an `ends_at` in the
past) is refused with `422`.

`DELETE /v1/schedules/{id}` removes the schedule (`204`). Messages it already sent are kept.

`GET /v1/schedules` lists schedules in the key's environment, newest first (`limit` up to 100,
`starting_after`).

## `ends_at`

A repeating schedule stops after `ends_at`: once the next run would fall after it, the schedule
becomes `completed`. A new schedule whose `ends_at` has already passed, or that would never run
before it, is refused with `422`.

## Idempotency

Each regular run has its own idempotency key, made from the schedule and the time it was due. If
the job that sends a run is retried (a worker restart, a database hiccup), Bridge returns the
message it already created instead of sending a second one. A run started with **run now** has a
key of its own, so it never collides with a regular run.

## Errors and `last_error`

When a run sends nothing, Bridge records why in `last_error` and carries on with the next run.
The next run that creates a message clears it.

| `last_error` starts with | Why |
| --- | --- |
| `Not sent: +91… opted out of messages from this project.` | The number is on the [opt-out list](../automation/README.md#opt-out-list). |
| `Not sent: the project's hourly sending limit was reached.` | The run was retried for a while and the limit still applied. |
| `Not sent: ` and a validation message | For example the chosen phone was removed (`No active device with this ID in the project.`). |
| `Not sent: an internal error occurred.` | Check the server logs. |

`last_run_at` and `run_count` cover every run, including **run now** and runs that sent nothing.
`last_message_id` is the newest message a run created. A run whose message then fails to send is
not an error here: look at that message's `error_code`.

## Limits and permissions

Messages from schedules count against the usual per-message limits (1,000 per project and 20 per
destination number per hour). In the dashboard, members can manage test schedules and owners and
admins live ones; changes there are recorded in the audit log.
