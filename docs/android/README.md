# Android gateway

The Bridge gateway app turns an Android phone with a SIM into a delivery device for your Bridge
server. It keeps a connection to the server, reports its health, sends the SMS your applications
queue, and, if you turn it on, forwards the SMS it receives.

## Requirements

* Android 8.0 (API 26) or newer
* A SIM that can send SMS on your plan
* Network access to your Bridge server's `BRIDGE_PUBLIC_URL`

## Builds

| Build | Wake-ups | For |
| --- | --- | --- |
| `foss` | UnifiedPush (ntfy or another distributor) | Everyone. 100% open source, no Google services. **Use this unless you have a reason not to.** |
| `gms` | Firebase Cloud Messaging | Servers whose operator configured Firebase (see below). Needs Google Play services on the phone. |

Download `bridge-gateway-<version>-foss.apk` (or `-gms.apk`) from
[GitHub Releases](https://github.com/kroszborg/bridge/releases) and check it against `apk-checksums.txt`. Bridge is not on Google Play: Play only
lets default SMS apps send SMS, and a gateway is not one. [Google Play](play-store.md) records what a
submission would need. The F-Droid listing text lives in `android/gateway/app/fastlane/`.

## Pairing

1. In the dashboard, open **Phones → Pair device**. A QR code appears, valid for 10 minutes and
   usable once.
2. In the app, tap **Scan pairing code** (or **Enter code manually**, then paste the server URL and
   code from *Cannot scan?*).
3. Confirm the server. The app always asks before trusting a server, because a pairing link could
   come from anyone, and a paired phone sends whatever that server asks it to.
4. The dashboard closes the dialog once the phone connects.

If you have an account, you can skip the QR code: tap **Sign in with your account**, sign in (the
server defaults to hosted Bridge, `https://api.bridge.kroszborg.co`; enter your own API address if
you self-host), choose a project and tap **Pair this phone**. The app creates the pairing code for
you, which needs the organization's admin or owner role. Members see a message asking an admin.

Pairing the same phone again (same app install) keeps its device ID and rotates its credential.
**Disconnect this phone** in the app, or **Remove** in the dashboard, revokes the credential at once.

A phone that still shows a pairing (for example because the dashboard removed it while it was
offline) can scan a new code from **More → Pair with a new code**, or open a pairing link. The
confirmation says which pairing it replaces, and the old one is only dropped once the server accepts
the new code. If pairing fails, the reason is shown on screen and logged in the connection log.

## Signed in

Signing in is optional. The gateway works the same without it, and signing out never unpairs the
phone unless you tick **Also disconnect this phone**. While signed in, the app has five tabs:

| Tab | What it does |
| --- | --- |
| Gateway | The status and reliability checklist (or pairing, if this phone is not paired). |
| Messages | Recent sent and received messages for the project, with status and failure reasons. Pull to refresh; switch between Live and Test. |
| Send | Send one message, like the dashboard's Playground. Test messages are simulated. Live messages need the admin or owner role, and can name a SIM or go through this phone. The status updates until the message is delivered or failed. |
| Phones | The project's phones, online or offline, with this phone marked. Admins can rename or remove them. |
| Account | Switch project, plan and usage (hosted Bridge; self-hosted servers show no limits), server addresses, about and privacy, sign out. |

When a send or a pairing hits the plan's limit, the app shows the reason and an **Upgrade** button
that opens the organization's billing page in the dashboard. The dashboard address is derived from
the API address (`api.example.com` → `app.example.com`, port 8080 → 3000, otherwise the same
host); change it on the sign-in screen or under Account → Server.

The app uses the same session endpoints as the dashboard (`POST /v1/auth/login`, then the
`bridge_session` cookie). It sends no `Origin` header, which the API's cross-site check allows.

## Permissions

| Permission | Why |
| --- | --- |
| Send SMS | Required. Messages are sent through this phone's SIM. |
| Phone (optional) | Lists SIM slots so you can choose one for dual-SIM phones. Your phone number is never read. |
| Receive SMS (optional) | Asked for only when forwarding of incoming SMS is turned on for this phone in the dashboard. |
| Notifications | Shows the gateway's status while it runs. |
| Camera | Only to scan the pairing QR code. |

Android also limits apps to about 30 SMS per 30 minutes before asking for approval of each one.
Bridge paces each phone under its send limit. See [Sending messages](../messages/README.md#androids-sending-limit)
to raise it.

## Daily send cap

Mobile operators limit how many SMS a SIM may send a day (about 100 on most Indian prepaid and
unlimited plans) and can block SIMs that send far more. Every phone therefore has a daily cap,
`daily_send_limit`, 100 messages in any rolling 24 hours by default. Bridge never assigns a phone
more than that: dispatch skips a phone at its cap, broadcasts pace to what is left, and a message
that no phone can take fails after an hour with `daily_limit_reached` (or goes to an SMS provider,
if the project falls back to one). The cap applies on hosted and self-hosted servers alike.

Change it per phone in the dashboard under **Phones → ⋯ → Settings → Messages per day** (1 to
10,000), and keep it within your SIM plan's daily allowance. The Phones page shows each phone's
sends in the last 24 hours against its cap. To send more, pair more phones or add an
[SMS provider](../providers/README.md).

## Keeping it online

Android aggressively stops background apps. The app's status screen has a reliability checklist;
work through it:

| Item | Why |
| --- | --- |
| Status notification | Android requires a visible notification for the foreground service that holds the connection. |
| Background use: Unrestricted | Exempts the app from battery optimisation so Doze does not cut the connection. **Allow** opens Android's own dialog. |
| *Brand* battery manager | Shown on Realme, Oppo and OnePlus (ColorOS), Xiaomi, Vivo, Samsung and Huawei/Honor phones. **Open** goes to that maker's auto-start or battery page, or to Bridge's app settings when the page has moved. |
| Wake-ups | Lets the server wake the app with a push if its connection drops. |

Vendor battery savers add their own app killers on top of Android's. On ColorOS, allow **Auto
launch** and **Allow background activity** for Bridge (App info → Battery usage); without them the
phone can stop Bridge seconds after you leave it, and blocks Android from restarting it.
[dontkillmyapp.com](https://dontkillmyapp.com) has steps for each brand. A phone on a charger and
Wi-Fi is the most reliable gateway.

## Connection log

Status → **More → Connection log** (or **Connection log** on the pairing screen) lists the last 200
things the gateway did and why: app starts and how the previous run ended (crash, low memory, or
stopped by the system or the phone maker's battery manager, from Android 11), service starts and
stops, connects, closes with their code, retries, network changes, revokes and pairing attempts.
**Copy** puts the whole log, with the app version, phone model and Android version, on the
clipboard for a bug report. The log stays on the phone and survives unpairing.

The same lines go to Logcat under the `BridgeGateway` tag, in release builds too:

```sh
adb logcat -s BridgeGateway
```

## Home-screen widget

Long-press the home screen → Widgets → Bridge: **Gateway status** (2×1) or **Gateway status,
wide** (4×1). Both resize; taller sizes add today's count and the last message. The widget shows
Online, Connecting, Offline or Not paired with the project name, the messages sent today and the
last message's status and time. Tapping it opens the app. It updates when the connection state
changes or a message report arrives, never on a timer of its own, and follows the light or dark
theme.

## How the connection works

```text
App --WebSocket (Authorization: Bearer bd_...)--> /v1/device/connect
    <-- welcome
    --> heartbeat {seq, next_in, status}        every 60 s charging, 5 min on battery, 10 min in battery saver
    <-- heartbeat_ack {seq}                     no ack within 20 s -> reconnect
    <-- sync                                    "check in now" (dashboard Wake, queued work)
    <-- config {forward_inbound}                a setting changed in the dashboard
    --> sms_received {inbound_id, from, body}   only while forwarding is on; kept until report_ack
    <-- unpaired                                credential revoked -> app forgets the server
```

* **Battery:** one small heartbeat per interval, and no WebSocket pings. When the network drops,
  the app waits for Android's network callback instead of retrying blindly, but still tries every
  5 minutes in case that report is wrong. Reconnects use exponential backoff with jitter (1 s up
  to 5 min). A new default network (Wi-Fi to mobile data, say) triggers an immediate retry, or a
  heartbeat that checks a connection still open on the old network.
* **Only a revoke stops it:** the `unpaired` frame, close code 4003 or a rejected credential make
  the app forget the server. Every other close, failure or internal error is retried.
* **Background refresh:** WorkManager runs every 15 minutes on a network as a watchdog. It restarts
  the connection and the foreground service if either stopped, checks in over HTTP
  (`POST /v1/device/heartbeat`), and refreshes the push registration. It also runs 10 seconds after
  Android stops the service or the app is swiped away. The gateway restarts after a reboot or an
  app update, whenever the app's process starts, and when you open the app.
* **Push wake-up:** when the socket is down, the server can send a wake-up through the device's
  push registration. The push carries no data beyond "wake"; the app reconnects and fetches its work.
* **Server side:** a device that misses two heartbeats (plus 30 s grace) is marked offline. A newer
  connection from the same device replaces the old one.

## Wake-ups with UnifiedPush (foss)

1. Install a distributor such as [ntfy](https://ntfy.sh) from F-Droid or Google Play.
2. In the Bridge app, tap **Turn on** next to *Wake-ups*. Pick the distributor if asked.
3. The dashboard shows *UnifiedPush* under the device's **Wake-up** field.

Messages are encrypted end to end (RFC 8291) and authenticated with the server's VAPID key, which
Bridge generates on first start. A self-hosted ntfy on your LAN works, but the Bridge server
refuses private-network push endpoints unless `BRIDGE_PUSH_ALLOW_PRIVATE_ENDPOINTS=true`. This stops a
paired device from making the server call internal hosts.

## Wake-ups with Firebase (gms)

The gms build initialises Firebase at runtime from configuration your server hands out during
pairing. The APK contains no `google-services.json`.

1. In the [Firebase console](https://console.firebase.google.com), create a project and add an
   Android app with package name `dev.bridge.gateway`.
2. Note the app's **App ID**, the project's **Web API key**, **Project ID** and **Sender ID**
   (Project settings → Cloud Messaging).
3. Create a service account with the *Firebase Cloud Messaging API Admin* role and download its JSON key.
4. Configure the Bridge server:

   ```bash
   BRIDGE_FCM_CREDENTIALS_FILE=/run/secrets/fcm.json
   BRIDGE_FCM_PROJECT_ID=my-project
   BRIDGE_FCM_APP_ID=1:1234567890:android:abc123
   BRIDGE_FCM_API_KEY=AIza…
   BRIDGE_FCM_SENDER_ID=1234567890
   ```

5. Pair (or re-pair) the phone with the gms build and tap **Turn on** next to *Wake-ups*.

## Incoming SMS

With **Forward incoming SMS** turned on for a phone (dashboard → Phones → ⋯ → Settings), the app
forwards every SMS the phone receives. They appear on the Messages page under *Incoming* and as
`message.received` webhooks; see [Webhooks](../webhooks/README.md#incoming-sms). The setting reaches
the phone at once over its connection, or at the next background check-in.

Forwarding is off by default. While it is off the app ignores incoming SMS completely: nothing is
stored on the phone or sent anywhere. Bridge does not need to be the default SMS app; your normal
messaging app keeps receiving everything as before. Use a SIM dedicated to Bridge, because
forwarding includes verification codes and personal messages.

## Building

```bash
cd android/gateway
./gradlew testFossDebugUnitTest lintFossDebug   # tests and lint
./gradlew assembleFossDebug                     # app/build/outputs/apk/foss/debug/
./gradlew assembleGmsDebug
```

Requires JDK 17+. The Gradle wrapper (9.8, checksum-pinned) downloads everything else.

### Signed releases

Release signing reads its key from the environment, so no key material is ever committed:

```bash
export BRIDGE_KEYSTORE_FILE=/secure/bridge-release.jks
export BRIDGE_KEYSTORE_PASSWORD=…
export BRIDGE_KEY_ALIAS=bridge
export BRIDGE_KEY_PASSWORD=…
./gradlew assembleFossRelease assembleGmsRelease
```

Keep the keystore safe and backed up. Android only installs updates signed with the same key.

## Privacy

The app reads battery level, charging state, network type, the carrier name and the number of SIM
slots, which are shown on the dashboard. It does not read your phone number, IMEI or contacts, and
it never reads the phone's SMS inbox. Incoming SMS are passed to the server only while you have
forwarding turned on for this phone. The installation ID is a random UUID created on first launch. Backups are disabled, and
the device credential is encrypted with a key that never leaves the Android Keystore.

Signing in stores the server's session cookie, encrypted with a second Keystore key; your password
is never stored. Signing out deletes the session on the server and the cookie and key on the phone.
**About & privacy** in the app links to the [privacy policy](https://bridge.kroszborg.co/privacy)
and the source code.
