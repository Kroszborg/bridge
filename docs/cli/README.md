# bridgectl, the Bridge CLI

`bridgectl` sends messages, follows them, watches events as they happen, and forwards webhooks to a
server on your laptop. It is one static binary with no runtime.

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
bridgectl otp verify +919876543210 482913          # exit status 1 when the code is not valid
bridgectl otp get otp_06gh…                        # a verification and its SMS

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
