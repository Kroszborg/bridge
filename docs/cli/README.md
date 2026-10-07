# bridgectl, the Bridge CLI

`bridgectl` sends messages and broadcasts, follows them, manages the opt-out list, watches events as
they happen, and forwards webhooks to a server on your laptop. It is one static binary with no runtime.

## Install

Download the archive for your platform from [GitHub Releases](https://github.com/kroszborg/bridge/releases) (Linux, macOS and
Windows, amd64 and arm64), unpack it, and put `bridgectl` on your PATH:

```bash
tar -xzf bridgectl_0.3.0_linux_amd64.tar.gz
sudo mv bridgectl /usr/local/bin/
bridgectl version
```

Check the download against `checksums.txt` from the same release, or verify its provenance with
`gh attestation verify bridgectl_0.3.0_linux_amd64.tar.gz --repo kroszborg/bridge`.

Or build it from the repository (Go 1.27+):

```bash
cd apps/api
go build -ldflags "-s -w" -o bridgectl ./cmd/bridgectl   # bridgectl.exe on Windows
```

## Log in

```bash
bridgectl login --url https://api.sms.example.com
# API key (bk_live_… or bk_test_…, input hidden):
```

`login` checks the key, then saves the URL and key to `~/.config/bridge/config.json` (on macOS
`~/Library/Application Support/bridge/`, on Windows `%AppData%\bridge\`), readable only by you. In
scripts and CI, skip `login` and set `BRIDGE_URL` and `BRIDGE_API_KEY` instead. Flags override the
environment, which overrides the saved file:

| Setting | Flag | Environment | Default |
| --- | --- | --- | --- |
| API URL | `--url` | `BRIDGE_URL` | saved, then `http://localhost:8080` |
| API key | `--api-key` | `BRIDGE_API_KEY` | saved |
| Config file | | `BRIDGE_CONFIG` | see above |

Use a test key (`bk_test_…`) while developing: Bridge simulates every message and sends nothing.
`bridgectl logout` deletes the saved file.

## Commands

```bash
bridgectl whoami                                   # project and environment of the key

bridgectl send +919876543210 "Your order has shipped."
bridgectl send +919876543210 "Code 482913" --sim 2 --idempotency-key login-7731 --wait

bridgectl messages                                 # newest 20
bridgectl messages --status failed --limit 50
bridgectl messages --direction inbound --from AX-HDFCBK
bridgectl messages get msg_06ggn…                  # one message and its timeline
bridgectl messages tail                            # events as they happen

bridgectl otp send +919876543210                   # one-time password (test keys print the code)
bridgectl otp send +919876543210 --app checkout    # with a specific Verify app (ID or slug)
bridgectl otp verify +919876543210 482913          # exit status 1 when the code is not valid
bridgectl otp get otp_06gh…                        # a verification and its SMS

bridgectl broadcast send --csv customers.csv --template "Hi {name}, your order {order} has shipped." --dry-run
bridgectl broadcast send --csv customers.csv --template "Hi {name}, your order {order} has shipped." --name "Shipping update"
bridgectl broadcasts                               # recent broadcasts and their counts
bridgectl broadcast get brd_06gj…                  # progress of one broadcast
bridgectl broadcast cancel brd_06gj…               # stop sending to the rest of the list

bridgectl schedules                                # scheduled and repeating messages, next run, last error

bridgectl optouts                                  # numbers that opted out (newest 50)
bridgectl optouts --source keyword                 # only those who texted STOP or similar
bridgectl optouts add +919876543210
bridgectl optouts check +919876543210
bridgectl optouts remove +919876543210             # only when the person asked to be messaged again

bridgectl logs --status error                      # API requests made with this environment's keys
bridgectl logs --path /v1/otp --limit 50

bridgectl mcp                                      # MCP server for AI assistants, see docs/mcp
bridgectl devices                                  # presence, battery, send window, forwarding
bridgectl usage --days 30 --tz Asia/Kolkata        # daily volume and delivery rate
```

`send --wait` follows the message until it is delivered or fails, prints its timeline, and exits
with status 1 if it failed, so you can use it in scripts. Every command accepts `--json` to print
the API's JSON instead of a table.

```text
$ bridgectl send +15550000001 "Hello from bridgectl" --wait
msg_06gh9q17gdw7rgqmgftswexty8  delivered  → +15550000001
  "Hello from bridgectl"
  11:05:34  created                1 segment(s)
  11:05:34  queued
  11:05:34  device_accepted
  11:05:35  sent
  11:05:37  delivered
```

## Broadcasts from a CSV file

`broadcast send` reads a CSV file with a header row. The first column, named `to` or `phone`, holds
the numbers. Every other column is a template variable, named by its header:

```text
to,name,order
+919876543210,Asha,A-1042
+919812345678,"Ravi, Jr.",A-1043
```

```bash
bridgectl broadcast send --csv customers.csv --template "Hi {name}, order {order} has shipped." --dry-run
```

```text
Dry run: nothing was created.
  2 recipient(s) · 2 segment(s) in all · 0 opted out · 0 duplicate(s) removed
  +919876543210 (1 seg) "Hi Asha, order A-1042 has shipped."
  +919812345678 (1 seg) "Hi Ravi, Jr., order A-1043 has shipped."
```

| Flag | Meaning |
| --- | --- |
| `--csv FILE` | The recipients. Blank lines are skipped. At most 10,000 rows. |
| `--template TEXT` | The message, with `{column}` placeholders. Every placeholder needs a column. Only the columns the template uses are sent to Bridge. |
| `--name N` | A name to find the broadcast by. |
| `--at TIME` | Start later, at an RFC 3339 time such as `2026-11-01T09:00:00+05:30`. |
| `--device ID` | Send every message through this phone. |
| `--dry-run` | Validate and preview. Nothing is created or sent. |
| `--test` | Refuse to run unless the key is a test key (`bk_test_…`), so a rehearsal can never send real SMS. |

Without `--dry-run`, the broadcast is created and starts at once (or at `--at`). Repeated numbers
are sent once, and numbers on the opt-out list are skipped. Follow it with
`bridgectl broadcast get ID`, and stop it with `bridgectl broadcast cancel ID`. A failed command is
not retried, so run `bridgectl broadcasts` before sending again to check that the first attempt did
not go through. See [Broadcasts](../broadcasts/README.md) for pacing and limits.

## Develop webhooks locally

`listen` receives your project's events live and forwards each one to a local URL, signed exactly
like a real webhook. You don't need a public URL or a webhook endpoint in the dashboard.

```bash
bridgectl listen --forward-to http://localhost:3000/webhooks/bridge
```

```text
Forwarding events to http://localhost:3000/webhooks/bridge
Signing secret for this session: whsec_Kq3…
11:08:13 message.sent       msg_06gh9qmer7… → +15550000001
  → 204 3ms
11:08:14 message.delivered  msg_06gh9qmer7… → +15550000001
  → 204 2ms
```

Set your app's `BRIDGE_WEBHOOK_SECRET` to the printed secret while testing, or pass
`--secret whsec_…` to reuse one across sessions. `--types message.received,message.failed`
limits which events are forwarded. Without `--forward-to`, `listen` only prints events.

`listen` and `messages tail` use the event stream (`GET /v1/events/stream`, Server-Sent Events,
authenticated with the API key). They see the key's environment, plus device events. They
reconnect on their own after network drops. Events that happen while disconnected are not replayed;
use `bridgectl messages` or the API to catch up.
