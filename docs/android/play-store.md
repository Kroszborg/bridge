# Google Play (prepared, not submitted)

Bridge ships on GitHub Releases and F-Droid. This page records what a Google Play submission
needs, so it can be done later without rediscovering the policies. Nothing here has been submitted.

The candidate build is the **`gms`** flavor: it adds Firebase Cloud Messaging for wake-ups and is
otherwise identical to `foss`. Build it with `./gradlew :app:bundleGmsRelease` (Play wants an
Android App Bundle) and the signing environment from [Releasing](../releasing.md).

## The blocker: SMS permissions

Google Play restricts `SEND_SMS`, `RECEIVE_SMS`, `READ_SMS`, `RECEIVE_MMS`, `RECEIVE_WAP_PUSH` and
the Call Log permissions to the user's **default SMS (or Phone) handler**, or to apps whose core
feature matches one of the [permitted exceptions](https://support.google.com/googleplay/android-developer/answer/10208820)
and that are approved through the **Permissions Declaration Form** in Play Console.

What the app requests:

| Permission | Restricted on Play | Used for |
| --- | --- | --- |
| `SEND_SMS` | Yes | The gateway's core function: sending the SMS the server queues. |
| `RECEIVE_SMS` | Yes | Forwarding incoming SMS, only after the user turns it on in the dashboard. |
| `READ_SMS`, `READ_CALL_LOG`, `PROCESS_OUTGOING_CALLS` | Yes | **Not requested.** The app never reads the SMS inbox or call log. |
| `READ_PHONE_STATE` | No | Lists SIM slots for dual-SIM sending. The phone number is never read. |
| `FOREGROUND_SERVICE_SPECIAL_USE` | Declaration | Holds the connection to the server. Play asks for a description and a video. |
| `REQUEST_IGNORE_BATTERY_OPTIMIZATIONS` | Review | Doze would otherwise cut the connection. Play allows it only when the core function needs it; explain this in the listing. |
| `CAMERA` | No | Scanning the pairing QR code. |

Bridge is not, and should not become, a default SMS app. None of the listed exceptions describes
"an SMS gateway that sends messages for a server". The closest is *Device automation*, and Play
reviewers have historically rejected SMS gateway apps under it. Plan for a rejection.

If submitting anyway, the declaration form needs:

* **Core functionality:** "Bridge turns the user's phone into an SMS gateway for their own Bridge
  server (self-hosted or hosted). The user's applications send SMS through the server; the phone
  delivers them through its SIM and reports delivery status. Sending SMS is the only purpose of
  the app."
* **Why `RECEIVE_SMS`:** optional forwarding of SMS the phone receives (replies, opt-outs) to the
  user's server, off by default, requested only when the user enables it.
* **Video:** pairing the phone, sending a message from the dashboard, the phone sending it, the
  delivered status in the dashboard, and turning on forwarding.
* **Privacy policy URL** (below).

## Options

1. **Submit `gms` as is** with the Permissions Declaration Form. No code changes. Most likely
   outcome is rejection; a rejected declaration does not affect GitHub or F-Droid distribution.
2. **Companion-only variant for Play** (recommended if Play matters). A build without `SEND_SMS`,
   `RECEIVE_SMS`, the gateway service, the SMS receivers and the boot receiver. It keeps sign-in,
   Messages, Send (which sends through the project's other phones or SMS providers, not this
   phone), Phones and Plan & usage. Phones are paired as gateways with the GitHub or F-Droid build.
   Work needed:
   * a second flavor dimension (for example `role`: `gateway`, `companion`), or a `play` flavor;
   * a `companion` manifest overlay that removes the restricted permissions, the service and the
     receivers with `tools:node="remove"`;
   * a `BuildConfig` flag that hides the Gateway tab and the pairing entry points, and opens on
     sign-in;
   * a different `applicationId` (for example `dev.bridge.companion`) so it can sit beside the
     gateway build on the same phone.

   The signed-in screens already work without a pairing, so this is mostly build configuration.

## Data safety form

Answers for the `gms` build talking to hosted Bridge (`api.bridge.kroszborg.co`). For a
self-hosted server the data goes to that server instead, but the form describes the worst case.

| Question | Answer |
| --- | --- |
| Does the app collect or share user data? | Collects. Nothing is shared with third parties. |
| Encrypted in transit? | Yes (HTTPS). Self-hosters can choose plain HTTP; the app warns about it. |
| Can users request deletion? | Yes: deleting the account in the dashboard (`DELETE /v1/me`), or removing the phone, which revokes its credential. |
| Personal info: email address, name | Collected when signing in. Required for the account features. Purpose: account management. |
| Messages: SMS | Collected. Outgoing message text and recipient numbers come from the server and their status is reported back; incoming SMS are sent to the server only when forwarding is turned on. Purpose: app functionality. |
| Device or other IDs | Collected: a random installation ID created by the app (no hardware IDs, no advertising ID). Purpose: app functionality. |
| App info and performance: diagnostics | Collected: battery level, charging state, network type, carrier name, device model, Android and app version, SIM slots (no numbers). Purpose: app functionality (choosing a phone that can send). |
| Location, contacts, photos, files, calendar, health, financial info, web history | Not collected. |
| Analytics, advertising, crash reporting SDKs | None. Firebase is used only for Cloud Messaging wake-ups, with analytics collection turned off in the manifest. |

Wake-up pushes (FCM in `gms`, UnifiedPush in `foss`) carry no message data, only "wake".

## Store listing

* **Privacy policy:** `https://bridge.kroszborg.co/privacy` (placeholder until the page exists).
* **Source:** `https://github.com/kroszborg/bridge`.
* Title and descriptions can reuse `android/gateway/app/fastlane/metadata/android/en-US/`.
* **Content rating:** utility, no user-generated content shown publicly.
* **Target audience:** 18+ (it sends SMS that can cost money).
* **Account deletion:** Play requires a web link for deleting accounts created in an app. The app
  only signs in to accounts created on the web, but link the dashboard's account page anyway.

## Signing and updates

With Play App Signing, Google re-signs the app with its own key. Play installs then cannot update
from, or be updated by, the GitHub or F-Droid APKs (different signatures), and switching means
uninstalling and pairing again. A separate `applicationId` for a Play build (option 2) avoids
confusing the two.
