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
lets default SMS apps send SMS, and a gateway is not one.

## Pairing

1. In the dashboard, open **Devices → Pair device**. A QR code appears, valid for 10 minutes and
   usable once.
2. In the app, tap **Scan pairing code** (or **Enter code manually**, then paste the server URL and
   code from *Cannot scan?*).
3. Confirm the server. The app always asks before trusting a server, because a pairing link could
   come from anyone, and a paired phone sends whatever that server asks it to.
4. The dashboard closes the dialog once the phone connects.

Pairing the same phone again (same app install) keeps its device ID and rotates its credential.
**Disconnect this phone** in the app, or **Remove** in the dashboard, revokes the credential at once.

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

## Keeping it online

Android aggressively stops background apps. The app's status screen has a reliability checklist;
work through it:

| Item | Why |
| --- | --- |
| Status notification | Android requires a visible notification for the foreground service that holds the connection. |
| Background use: Unrestricted | Exempts the app from battery optimisation so Doze does not cut the connection. |
| Wake-ups | Lets the server wake the app with a push if its connection drops. |

Vendor battery savers (Xiaomi, Samsung, OnePlus, Oppo, Vivo, Huawei) add their own app killers.
Allow Bridge to autostart and run in the background; [dontkillmyapp.com](https://dontkillmyapp.com)
has steps for each brand. A phone on a charger and Wi-Fi is the most reliable gateway.

## How the connection works

```text
App ──WebSocket (Authorization: Bearer bd_…)──► /v1/device/connect
    ◄── welcome
    ──► heartbeat {seq, next_in, status}        every 60 s charging · 5 min on battery · 10 min in battery saver
    ◄── heartbeat_ack {seq}                     no ack within 20 s → reconnect
    ◄── sync                                    "check in now" (dashboard Wake, queued work)
    ◄── config {forward_inbound}                a setting changed in the dashboard
    ──► sms_received {inbound_id, from, body}   only while forwarding is on; kept until report_ack
    ◄── unpaired                                credential revoked → app forgets the server
```

* **Battery:** one small heartbeat per interval, and no WebSocket pings. When the network drops,
  the app waits for Android's network callback instead of retrying blindly. Reconnects use
  exponential backoff with jitter.
* **Background refresh:** WorkManager runs every 15 minutes on a network. It restarts the
  foreground service if Android stopped it, checks in over HTTP (`POST /v1/device/heartbeat`), and
  refreshes the push registration. The gateway also restarts after a reboot or an app update.
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

With **Forward incoming SMS** turned on for a phone (dashboard → Devices → ⋯ → Settings), the app
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
