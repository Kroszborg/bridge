# Google Play

Step by step, how the gateway app goes onto Google Play. Read [The SMS permissions](#the-sms-permissions)
first: Play restricts the permission the app exists for, approval is not guaranteed, and GitHub
Releases (now) and [F-Droid](f-droid.md) (after inclusion) remain the primary channels whatever Play
decides.

The Play build is the **`gms`** flavor: identical to `foss`, plus Firebase Cloud Messaging for
wake-ups. Play wants an Android App Bundle (AAB).

| | |
| --- | --- |
| Package name | `dev.bridge.gateway` (both flavors) |
| Version | 1.0.0, version code 1000099 (see [Releasing](../releasing.md)) |
| Target / compile SDK | 37 (Play requires at least 35 for new apps and updates) |
| Min SDK | 26 (Android 8.0) |
| Privacy policy | <https://bridge.kroszborg.co/privacy/> |
| Terms | <https://bridge.kroszborg.co/terms/> |
| Account deletion | <https://dashboard.bridge.kroszborg.co/account> (Delete account) |
| Support email | abhimanpanwar6@gmail.com |
| Listing text and graphics | `android/gateway/app/fastlane/metadata/android/en-US/` |

## 1. Keys: Play App Signing and the upload key

With Play App Signing, Google holds the **app signing key** that signs what phones install, and you
sign each upload with an **upload key**. Two keys, two decisions:

**App signing key.** Play Console offers to generate one, or to use your own. Use your own: the
existing GitHub release key (`bridge-release.jks`, see [Releasing](../releasing.md#android-signing-key)).
Then the Play install and the GitHub APKs carry the same signature, so users can move between them
without uninstalling and pairing again. With a Google-generated key they cannot, because both builds
use the package name `dev.bridge.gateway`. Play Console walks you through exporting the key with
its PEPK tool (*Use a different key → Export and upload a key from Java keystore*); the encrypted
export is uploaded, never the keystore itself. This choice is effectively permanent.

**Upload key.** A separate key, used only to sign uploads. If it leaks or is lost, Play support can
reset it; the app signing key is unaffected. Create it once, on your own machine, outside the
repository:

```bash
keytool -genkeypair -v -keystore ~/keys/bridge-upload.jks -storetype PKCS12 \
  -alias upload -keyalg RSA -keysize 4096 -validity 10000 \
  -dname "CN=Bridge Gateway upload"
# Certificate to register in Play Console (Setup → App signing → Upload key):
keytool -export -rfc -keystore ~/keys/bridge-upload.jks -alias upload -file bridge-upload.pem
```

Back up both keystores and their passwords somewhere other than this machine. Never commit them
(`*.jks` and `*.keystore` are git-ignored).

## 2. Build the bundle

Release signing reads, in order, the `BRIDGE_KEYSTORE_*` environment variables (what CI uses) and
a git-ignored `android/gateway/keystore.properties`. With neither, release builds come out unsigned.
For the Play upload, sign with the upload key:

```properties
# android/gateway/keystore.properties (git-ignored). A relative storeFile is resolved from android/gateway/.
storeFile=/home/you/keys/bridge-upload.jks
storePassword=…
keyAlias=upload
keyPassword=…
```

```bash
cd android/gateway
./gradlew :app:bundleGmsRelease
# app/build/outputs/bundle/gmsRelease/app-gms-release.aab
jarsigner -verify -verbose:summary app/build/outputs/bundle/gmsRelease/app-gms-release.aab
```

The bundle is shrunk and obfuscated by R8. It carries its own mapping file
(`BUNDLE-METADATA/com.android.tools.build.obfuscation/proguard.map`), which Play Console uses to
deobfuscate crash and ANR traces. Still archive
`app/build/outputs/mapping/gmsRelease/mapping.txt` from the same build, outside the repository,
for every release. It is the only way to read a trace from that version if the upload's copy is
missing (upload it under *Android vitals → Deobfuscation files*) or for a trace a user sends. See
[Obfuscation and mapping files](README.md#obfuscation-and-mapping-files).

Without `BRIDGE_VERSION_NAME` the version comes from `defaultConfig` in
`android/gateway/app/build.gradle.kts`, which the release commit bumps. Build the bundle from the
release tag so it matches the GitHub APKs. Keep `keystore.properties` pointing at the upload key;
for GitHub APKs CI uses the release key from its secrets.

## 3. Create the app in Play Console

1. [Play Console](https://play.google.com/console) → **Create app**. App name *Bridge SMS Gateway*,
   default language English (United States), **App**, **Free**. Accept the declarations.
2. **Setup → App signing**: choose *Use a different key*, export `bridge-release.jks` with PEPK as
   prompted, and upload the upload key's certificate (`bridge-upload.pem`).
3. Complete **App content** (section 4) and **Main store listing** (section 5). Play will not
   send a release for review until every App content item is done.
4. **Testing → Internal testing → Create new release**: upload `app-gms-release.aab`, release
   name `1.0.0`, notes from `changelogs/1000099.txt`. Add yourself as a tester, install from the
   opt-in link and check pairing, sending, forwarding and wake-ups on a real phone.
5. **Personal developer accounts created after November 2023** must run a **closed test** with at
   least 12 opted-in testers for 14 days in a row before they can apply for production access.
   Organization accounts skip this. Start it early; it can run while the declarations are in review.
6. **Production → Create new release**: promote the tested release, choose countries, roll out.
   The SMS and foreground-service declarations are reviewed with the first release that carries
   the permissions, which can take a week or more.

## 4. App content (Policy → App content)

### Privacy policy

`https://bridge.kroszborg.co/privacy/`. It already describes the Android app, its SMS permissions,
forwarding and Firebase in the gms build.

### App access

Everything past the welcome screen needs a Bridge server, so choose *All or some functionality is
restricted* and give the reviewer an account on hosted Bridge. Create a dedicated one; do not reuse
your own:

1. Sign up at <https://dashboard.bridge.kroszborg.co/signup> with an address you control and use
   only for this (for example a `+play-review` alias), and verify it.
2. Create a project named *Play review*. Leave forwarding off.
3. In Play Console, add the email and password, and these instructions:

   > Tap **Sign in with your account** and sign in with the credentials above (server: the default,
   > hosted Bridge). Choose the project *Play review*. The Messages, Send, Phones and Account tabs
   > work without pairing; on Send, choose **Test** to send a simulated message. To see the gateway,
   > tap **Pair this phone** on the Gateway tab: the phone then sends SMS that the account queues,
   > through its SIM. Live sends need a SIM and are charged by the carrier; Test sends never leave
   > the server.

Keep the account's password current and the account on a plan that allows at least one phone.

### Ads

No ads.

### Content rating

Questionnaire category: *Utility, Productivity, Communication, or Other*. Answer **No** to violence, sexuality,
language, controlled substances, gambling. The app lets users exchange content with others (it sends
and receives SMS), so answer **Yes** to *users can interact or exchange content*; the app has no
public sharing, chat with strangers or location sharing. Expected result: Everyone / PEGI 3 with a
*Users Interact* note.

### Target audience and content

Target age **18 and over** only. It sends SMS that can cost money and serves developers and
businesses. The app is not designed for children; say No to *appeals to children*.

### Data safety

Based on what the app actually sends (see `net/Models.kt`, `net/AccountModels.kt`,
`gateway/DeviceStatusReader.kt` and `gateway/Protocol.kt`) and on the privacy policy. Answers
cover the hosted service, the worst case; with a self-hosted server the data goes to that server.

**Data collection and security**

| Question | Answer |
| --- | --- |
| Does your app collect or share any of the required user data types? | Yes |
| Is all of the user data collected by your app encrypted in transit? | Yes. Hosted Bridge is HTTPS only. The app also accepts an `http://` address for a self-hosted server on a local network, after a warning; that traffic goes to the user's own server, not to the developer. |
| Do you provide a way for users to request that their data is deleted? | Yes. Account settings → Delete account (`https://dashboard.bridge.kroszborg.co/account`), removing a phone (revokes and deletes its pairing), or email abhimanpanwar6@gmail.com. |

**Data types.** Nothing is *shared* (transferred to a third party for its own use). AWS hosting
and Firebase Cloud Messaging act as service providers, which Play does not count as sharing.

| Data type | Collected | Optional? | Purposes | Notes |
| --- | --- | --- | --- | --- |
| Personal info → Email address | Yes | Optional (only when signing in) | Account management, App functionality | Sent at sign-in. The password is sent to log in and never stored on the phone. |
| Personal info → Phone number | No | | | The device's own number is never read. Recipient and sender numbers are part of the SMS data below. |
| Messages → SMS or MMS | Yes | Required | App functionality | Recipient numbers and text of the SMS the server asks the phone to send, with delivery status; messages typed on the Send tab; incoming SMS (sender, text, time) only while forwarding is turned on for the phone. Message text is erased from hosted Bridge after 30 days. |
| Device or other IDs | Yes | Required | App functionality | A random installation ID created by the app. No IMEI, hardware serial or advertising ID. |
| App info and performance → Diagnostics | Yes | Required | App functionality | Battery level, charging state, network type, carrier name, SIM slots and names (never numbers), device model, Android and app version, reported with each heartbeat so the server can route messages to a phone that can send. |
| Location, contacts, calendar, photos, files, audio, health, financial info, web history, app activity, installed apps | No | | | |
| Crash logs | No | | | The connection log stays on the phone; users copy it themselves. |

Not collected for analytics, advertising, personalisation or fraud prevention. The app contains no
analytics, advertising or crash-reporting SDK; Firebase is used only for Cloud Messaging, with
analytics collection disabled in the manifest. Wake-up pushes carry no message data.

### Advertising ID

**No.** Neither flavor declares `com.google.android.gms.permission.AD_ID` (Firebase Messaging does
not add it). Re-check the merged manifest after upgrading Firebase.

### Government, financial, health, news apps

No to all.

### Foreground service permissions

The gateway's foreground service (`GatewayService`) uses type **`specialUse`**
(`FOREGROUND_SERVICE_SPECIAL_USE`). None of the predefined types fit: `dataSync` is capped at six
hours a day from Android 15, `connectedDevice` needs an external device, and `remoteMessaging` is
for continuing a conversation from one of the user's devices on another. Play Console asks for:

* **Type:** Special use.
* **Description:** "The foreground service keeps a connection from the phone to the user's own
  Bridge server, so that SMS the user's applications queue are sent through this phone's SIM
  within seconds, and delivery reports go back. It runs only while the phone is paired, shows a
  persistent notification, and stops when the user disconnects the phone."
* **User impact if deferred or interrupted:** "Messages the user queued, for example one-time
  passwords or appointment reminders, would wait until the system next ran the app, often many
  minutes, or fail after their timeout."
* **Video:** a link (YouTube unlisted or Drive) showing pairing, the status notification appearing,
  and a message sent from the dashboard leaving the phone while the app is in the background.

The manifest's `PROPERTY_SPECIAL_USE_FGS_SUBTYPE` carries the same explanation.

### Sensitive permissions: SMS

See the next section. The **Permissions Declaration Form** appears under App content once a bundle
requesting `SEND_SMS` or `RECEIVE_SMS` is uploaded to any track.

### Battery optimisation

The app asks the user to exempt it from battery optimisation
(`REQUEST_IGNORE_BATTERY_OPTIMIZATIONS`). Play allows this only when the core function breaks
without it. Justification, if a reviewer asks: the gateway must receive send requests from the
user's server at any time; Doze defers network access and push wake-ups alone are throttled for
apps that receive many of them. The app only asks from its reliability checklist, through Android's
own dialog.

### Payments

The app has no in-app purchases. For hosted plans it shows an **Upgrade** button that opens the
organization's billing page in the dashboard. Plans pay for a service used through the API and
dashboard, outside the app, which Play's payments policy does not require Google Play Billing for.
If a reviewer disagrees, the fix is to hide that button in the gms build, not to add Play Billing.

## The SMS permissions

What the app requests (`android/gateway/app/src/main/AndroidManifest.xml`):

| Permission | Restricted on Play | Used for |
| --- | --- | --- |
| `SEND_SMS` | Yes, SMS group | The core function: sending the SMS the server queues. |
| `RECEIVE_SMS` | Yes, SMS group | Forwarding incoming SMS to the server, off by default; requested only when the user turns forwarding on in the dashboard. |
| `READ_SMS`, `RECEIVE_MMS`, `RECEIVE_WAP_PUSH`, Call Log | Yes | **Not requested.** The app never reads the SMS inbox or call log. |
| `READ_PHONE_STATE` | No | Lists SIM slots for dual-SIM sending. Phone numbers are never read. |
| `CAMERA` | No | Scanning the pairing QR code. |

Play only grants the SMS group to the user's **default SMS handler**, or to apps whose core
functionality matches a [permitted exception](https://support.google.com/googleplay/android-developer/answer/10208820)
and whose declaration is approved. Bridge is not, and should not become, a default SMS app.

No exception describes "an SMS gateway for the user's own server". The closest:

1. **Cross-device synchronization or transfer of SMS or calls** (best fit): apps that let the user
   send and receive their phone's SMS from another device, such as a computer. Bridge does that
   between the phone and the user's dashboard, applications and webhooks.
2. **Device automation**: apps that automate actions on the phone on the user's behalf. A weaker
   fit, since the automation runs on a server.

**Approval is not guaranteed.** Reviewers have rejected SMS gateway apps before, and Play can
reject an app that sends SMS in bulk or for third parties. Expect questions, and possibly a
rejection. A rejected declaration only affects Play: GitHub Releases and F-Droid are unaffected.

### Declaration text (draft)

* **Core functionality** (pick *Cross-device synchronization or transfer of SMS or calls*):

  > Bridge turns the user's own Android phone into an SMS gateway for their Bridge account, hosted
  > or self-hosted. The user sends SMS from their computer (the Bridge dashboard) or from their own
  > applications through the Bridge API, and this phone delivers them through its SIM and reports
  > delivery status back. Optionally, the user can have SMS the phone receives shown in their
  > dashboard. Sending SMS is the app's only purpose; without SEND_SMS it does nothing.

* **Why `SEND_SMS` is essential:**

  > Every message the user queues is sent from this phone's SIM with SmsManager. There is no
  > alternative: an intent to the default SMS app needs a tap per message, so queued messages
  > (for example one-time codes) could not be delivered while the user is away from the phone.

* **Why `RECEIVE_SMS` is essential:**

  > Optional forwarding of incoming SMS (replies and opt-outs) to the user's dashboard. It is off
  > by default, enabled per phone by the user, and the permission is requested only at that point.
  > While forwarding is off, the app ignores incoming SMS and stores nothing. The app never reads
  > the SMS inbox (no READ_SMS).

* **Video** (required; YouTube unlisted or Drive link, under 90 seconds, no real phone numbers):
  install, sign in or scan the pairing QR code, the permission prompt for SMS, sending a message
  from the dashboard on a computer, the phone sending it, the delivered status in the dashboard;
  then turning on forwarding, the RECEIVE_SMS prompt, and an incoming SMS appearing in the
  dashboard.

If the declaration is rejected, the option that stays within policy is a Play-only companion build
without `SEND_SMS`, `RECEIVE_SMS`, the gateway service and the SMS receivers: sign-in, Messages,
Send (through the project's other phones or SMS providers), Phones and Account. It needs a second
flavor dimension, a manifest overlay removing those components with `tools:node="remove"`, a
`BuildConfig` flag hiding the Gateway tab and pairing, and its own `applicationId` (for example
`dev.bridge.companion`).

## 5. Main store listing

Play Console → **Grow → Store presence → Main store listing**. Everything comes from
`android/gateway/app/fastlane/metadata/android/en-US/` (also usable with `fastlane supply`):

| Field | Source | Play limit |
| --- | --- | --- |
| App name | `title.txt` | 30 characters |
| Short description | `short_description.txt` | 80 characters |
| Full description | `full_description.txt` | 4,000 characters |
| App icon | `images/icon.png` | 512 × 512 PNG |
| Feature graphic | `images/featureGraphic.png` | 1024 × 500 PNG or JPEG |
| Phone screenshots | `images/phoneScreenshots/` | 2 to 8, see the README there |
| Release notes | `changelogs/<versionCode>.txt` | 500 characters |

`full_description.txt` uses the small HTML subset F-Droid renders (`<p>`, `<b>`, `<ul>`, `<li>`).
Play Console shows `<b>` but not list tags; when pasting by hand, turn each `<li>` into a line
starting with `•` and drop the other tags.

Store settings: category **Tools** (or *Communication*), contact email abhimanpanwar6@gmail.com,
website `https://bridge.kroszborg.co`.

## Updates

Each release: bump `defaultConfig` and add the changelog file in the release commit, tag it, build
`bundleGmsRelease` from the tag with the upload key, archive that build's `mapping.txt`, upload to
Internal testing, then promote to Production. The version code must always increase, which the formula in `build.gradle.kts`
guarantees.
