# Verify: one-time passwords

Bridge generates a code, sends it by SMS through your phones, and checks it. Your app makes two
calls and never stores, compares or expires a code itself.

```ts
import { Bridge } from '@kroszborg/bridge';

const bridge = new Bridge({ apiKey: process.env.BRIDGE_API_KEY, baseUrl: 'https://api.sms.example.com' });

// When the user asks for a code
await bridge.otp.send({ to: '+919876543210' });

// When they type it in
const { valid } = await bridge.otp.verify({ to: '+919876543210', code: '482913' });
```

The same with curl:

```bash
curl "$BRIDGE_URL/v1/otp" -H "Authorization: Bearer $BRIDGE_API_KEY" \
  -H "Content-Type: application/json" -d '{"to": "+919876543210"}'

curl "$BRIDGE_URL/v1/otp/verify" -H "Authorization: Bearer $BRIDGE_API_KEY" \
  -H "Content-Type: application/json" -d '{"to": "+919876543210", "code": "482913"}'
```

Do not want to build the form? The [drop-in widget](#drop-in-widget) and hosted page run the whole
flow in the browser and give your server a signed [token](#tokens).

## Sending: `POST /v1/otp`

| Field | Required | Notes |
| --- | --- | --- |
| `to` | yes | E.164 with country code. |
| `app` | no | The [Verify app](#verify-apps): its ID (`vap_…`) or slug. Leave it out for the default app. |
| `client_ip` | no | Your end user's IP address (IPv4 or IPv6). Turns on the app's [per-IP limit](#fraud-protection). |
| `android_app_hash` | no | Your app's 11-character SMS Retriever hash. Added as the last line so the app reads the code without SMS permission. |
| `metadata` | no | Your own JSON object (32 keys, 4 KB), returned with the verification. |

The response is `201` with a verification:

```json
{
  "id": "otp_06ghatc07nghtrbq1yj7nwyjtm",
  "status": "pending",
  "to": "+919876543210",
  "environment": "live",
  "app_id": "vap_06ghaq3z0k2v7c9e4r2n0w6yb",
  "attempts": 0,
  "attempts_remaining": 5,
  "expires_at": "2026-10-07T08:19:58Z",
  "resend_available_at": "2026-10-07T08:10:28Z",
  "verified_at": null,
  "message_id": "msg_06ghatc07skf5f2dgf8q4078mm",
  "message_status": "queued",
  "failover_message_id": null,
  "failover_message_status": null,
  "metadata": {},
  "created_at": "2026-10-07T08:09:58Z"
}
```

Sending a new code to the same number through the same app **cancels** the previous one (status
`canceled`), so only the latest code works. `message_status` follows the SMS that carries the code.
`failover_message_id` and `failover_message_status` are set when the code was resent through
another route (see [Delivery failover](#delivery-failover)).

## Checking: `POST /v1/otp/verify`

Pass the `code` and either `to` (checks the latest pending code for that number) or `id`. `app`
(ID or slug) is optional: it limits the check to that app's codes, which matters when several apps
have a code pending for the same number.

```json
{ "valid": true, "verification": { "id": "otp_…", "status": "verified", "attempts": 1, "…": "…" } }
```

`valid` is `true` only when this request's code is right. Otherwise read `verification.status`:

| Status | Meaning | What to tell the user |
| --- | --- | --- |
| `pending` | Wrong code; `attempts_remaining` are left | "Wrong code, try again" |
| `failed` | No attempts left | "Too many attempts, request a new code" |
| `expired` | The code lapsed | "The code expired, request a new one" |
| `verified` | Already used | Treat a repeat as not valid |
| `canceled` | A newer code was sent | "Use the latest code" |

A wrong code is a normal `200` with `valid: false`, not an error. `404` means there is no such
verification, or (with `to`) no code is pending for that number. Checking a finished verification
does not use an attempt.

`GET /v1/otp/{id}` returns a verification at any time.

## Verify apps

A project can have up to 50 Verify apps, one per product, brand or customer. Each app has its own:

| Setting | Default |
| --- | --- |
| Name and slug | The slug is derived from the name and cannot change later |
| `{app}` name in the SMS, message template | The app's name (the project name for the default app), the default template |
| Code length, lifetime, attempts, WebOTP domain | 6 digits, 10 minutes, 5 attempts, none |
| Delivery failover (`failover_after_seconds`) | 30 seconds |
| Fraud protection | All countries, 10 codes per IP and 20 per number range per hour, no country cap |
| Widget: publishable key, allowed origins, redirect URIs, environment, Turnstile | New key, none, none, `live`, off |
| Token signing secret (`bvs_…`) | Created with the app |
| Statistics and blocked-attempt report | Last 30 days |

Every project has a **default app** (slug `default`). It holds the settings the project had before
apps existed, it is used whenever a request names no app, and it cannot be deleted. Manage apps
under **Verify** in the dashboard; owners and admins can change them.

Name an app with `app` when sending, checking (optional) or listing:

```ts
await bridge.otp.send({ to: '+919876543210', app: 'checkout' }); // slug
await bridge.otp.send({ to: '+919876543210', app: 'vap_06ghaq3z0k2v7c9e4r2n0w6yb' }); // or ID
```

The resend cooldown and the hourly cap per number apply per app, and a new code cancels only the
same app's pending code. Deleting an app stops its widget and its tokens at once; its past
verifications are kept, with `app_id: null`.

## Delivery failover

A code that does not arrive is a lost sign-up. When a live code's SMS has not left within the app's
`failover_after_seconds` (default 30; 0 turns it off; at most 600), Bridge sends the same code once
more through another route:

1. the project's enabled [SMS providers](../providers/README.md), if the first SMS was meant for a
   phone and the project's [routing](../providers/README.md) allows providers (`phones_then_providers`
   or `providers`). With routing set to phones only, failover never uses a provider, so it never
   costs money you did not opt into;
2. otherwise another online phone with capacity, never the original one.

A failure the carrier reports after sending (`delivery_failed`) also fails over: that SMS did not
arrive, so a second one cannot be a duplicate.

It also fails over at once, without waiting, when the first SMS fails in a way that proves it was
not sent.

| Situation | Failover? |
| --- | --- |
| The SMS is still queued after N seconds (no phone accepted it) | Yes. The original is marked `failed` with `superseded_by_failover` first, so it cannot go out later. |
| The SMS failed, and the failure is not ambiguous (for example `no_service` or `radio_off` on the phone) | Yes, immediately |
| The SMS is with a phone or on its way (`sending`, `sent` or `delivered`) | No. Resending could deliver the code twice. |
| The failure is ambiguous: Android's `generic_failure` or an unknown Android result, a provider timeout (`…_unreachable`) or a provider `5xx` | No. The carrier may already have the SMS, and a second one would duplicate the code. |
| The verification is no longer pending, or has expired | No |
| A failover already happened | No. Each verification fails over at most once. |
| Test keys | Never |

The verification shows the second SMS in `failover_message_id` and `failover_message_status`. The
new message carries `failover_of` in its metadata and a `failover` timeline event naming the
original message and the route. App statistics count failovers.

To make this possible, the text of a Verify SMS (which contains the code) is kept until its
verification finishes, at most 1 hour, instead of being erased as soon as the phone is done with
it. It is masked everywhere in the meantime.

## Fraud protection

SMS pumping and enumeration attacks request codes to many numbers you pay for. Each app checks
every send attempt, in this order:

| Check | Default | Refused with |
| --- | --- | --- |
| Cloudflare Turnstile (widget and hosted page only, when set up) | Off | `403 otp_blocked`, reason `captcha_failed` |
| Allowed countries (ISO codes, such as `IN`, `US`) | Every country | `403 otp_blocked`, reason `country_not_allowed` |
| Codes per end-user IP address per hour | 10 (0 turns it off) | `429 otp_blocked`, reason `ip_limit` |
| Codes per number range per hour (numbers that differ only in their last 3 digits) | 20 (0 turns it off) | `429 otp_blocked`, reason `range_burst` |
| Codes per country per hour | No cap | `429 otp_blocked`, reason `country_limit` |

The per-IP limit needs the user's address: pass `client_ip` with `POST /v1/otp` (from your own
server, the API cannot see it), while the widget and hosted page use the caller's address
automatically. The number-range limit catches attackers walking through a block of premium
numbers. Limits are counted per app and per environment, and an attempt counts toward each limit it
reaches even when a later check refuses it.

`429` responses carry `Retry-After`:

```json
{
  "error": {
    "code": "otp_blocked",
    "message": "Too many codes were requested from this IP address in the last hour. Retry after 1712 seconds.",
    "request_id": "req_06ghb2m4x8k1v7c9e4r2n0w6yb"
  }
}
```

The usual per-number limits (30 seconds between codes, 5 per hour) still apply after these checks
and return `429 rate_limited`.

**Blocked-attempt report.** Every refused attempt is stored for 30 days with the number, country,
IP address and reason. The app's page in the dashboard lists them, filtered by reason and
environment, and its statistics count them by reason.

**`otp.blocked` event.** Each block is also announced to [webhooks](../webhooks/README.md) and the
event stream, so you can alert on attacks or ban an IP:

```json
{
  "type": "otp.blocked",
  "timestamp": "2026-10-07T08:12:40Z",
  "data": {
    "id": "blk_06ghb2m4x8k1v7c9e4r2n0w6yb",
    "app_id": "vap_06ghaq3z0k2v7c9e4r2n0w6yb",
    "environment": "live",
    "to": "+882351234567",
    "client_ip": "203.0.113.7",
    "country": null,
    "reason": "country_not_allowed",
    "created_at": "2026-10-07T08:12:40Z"
  }
}
```

`client_ip` and `country` are `null` when unknown.

## Drop-in widget

The widget and the hosted page ask for the number, send the code, check it, and give you a signed
[token](#tokens) that proves which number was verified. Your server checks the token; it never
handles the code.

### Set up the app

On the app's page in the dashboard, under the widget settings:

| Setting | Notes |
| --- | --- |
| Publishable key (`bpk_…`) | Identifies the app in the browser. Safe to put in your pages. |
| Allowed origins | Sites that may embed the widget, such as `https://shop.example.com`: `https` only (`http` for `localhost`), no path. Up to 20. The hosted page is always allowed. |
| Redirect URIs | Where the hosted page may send users back, such as `https://shop.example.com/auth/phone/callback`. Absolute `https` URLs (`http` for `localhost`), no `#fragment`, matched exactly. Up to 20. |
| Environment | `live` sends real SMS. `test` sends nothing and shows the code in the widget, for development. Its tokens say `env: test`. |
| Turnstile site key and secret | Optional [Cloudflare Turnstile](https://developers.cloudflare.com/turnstile/). When set, every send needs a passed challenge. Set both or neither; the secret is write-only. |
| Signing secret (`bvs_…`) | Signs the tokens. Keep it on your server. Shown when the app is created; reveal it again (audited) or rotate it, which invalidates earlier tokens at once. |

Signing tokens needs `BRIDGE_SECRET_KEY` on the server (see [self-hosting](../self-hosting/README.md)).

In the examples below, `https://sms.example.com` is your Bridge dashboard and
`https://api.sms.example.com` is your Bridge API.

### Hosted page

Send the user to the dashboard's verification page and get them back with a token. Nothing to
embed:

```text
https://sms.example.com/verify/{publishableKey}?redirect_uri=<url-encoded>&state=<opaque>
```

| Result | Redirect to `redirect_uri` with |
| --- | --- |
| Verified | `bridge_token=<jwt>&state=<state>` |
| The user cancelled | `error=cancelled&state=<state>` |

The `redirect_uri` must be one of the app's redirect URIs, character for character. Use `state` to
tie the callback to the session that started it.

```ts
import { randomBytes } from 'node:crypto';
import express from 'express';
import { BridgeTokenError, verifyWidgetToken } from '@kroszborg/bridge';

const PUBLISHABLE_KEY = 'bpk_9fK2mQ7xR4tV8wY1zA3bC5dE6gH0jL2n';
const APP_ID = 'vap_06ghaq3z0k2v7c9e4r2n0w6yb';
const CALLBACK = 'https://shop.example.com/auth/phone/callback'; // a registered redirect URI

const app = express(); // with a session middleware, such as express-session

app.get('/auth/phone', (req, res) => {
  const state = randomBytes(16).toString('hex');
  req.session.phoneState = state;
  const url = new URL(`https://sms.example.com/verify/${PUBLISHABLE_KEY}`);
  url.searchParams.set('redirect_uri', CALLBACK);
  url.searchParams.set('state', state);
  res.redirect(url.toString());
});

app.get('/auth/phone/callback', async (req, res) => {
  const { bridge_token, state, error } = req.query;
  if (!state || state !== req.session.phoneState) return res.status(400).send('Start again.');
  delete req.session.phoneState;
  if (error === 'cancelled') return res.redirect('/account');
  try {
    const { phone, tokenId } = await verifyWidgetToken(String(bridge_token), {
      secret: process.env.BRIDGE_VERIFY_SECRET!, // the app's bvs_… secret
      appId: APP_ID,
      issuer: 'https://api.sms.example.com',
    });
    if (!(await rememberToken(tokenId))) return res.status(400).send('This link was already used.');
    await savePhone(req.session.userId, phone);
    res.redirect('/account?phone=verified');
  } catch (err) {
    if (err instanceof BridgeTokenError) return res.status(400).send(`Not verified (${err.reason}).`);
    throw err;
  }
});
```

`rememberToken` stores the token ID and returns `false` if it was already there, so a token works
only once.

### Script widget

Embed the widget in your own page. The page's origin must be one of the app's allowed origins.

```html
<script type="module" src="https://sms.example.com/widget.js"></script>

<bridge-verify publishable-key="bpk_9fK2mQ7xR4tV8wY1zA3bC5dE6gH0jL2n" label="Verify your phone"></bridge-verify>

<script type="module">
  const widget = document.querySelector('bridge-verify');

  widget.addEventListener('bridge-verified', async (event) => {
    const { token } = event.detail; // also phone and verificationId
    const res = await fetch('/api/phone/verify', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ token }),
    });
    if (res.ok) location.assign('/account?phone=verified');
  });
  widget.addEventListener('bridge-error', (event) => {
    console.warn(event.detail.code, event.detail.message);
  });
  widget.addEventListener('bridge-cancel', () => {
    // the user closed the widget
  });
</script>
```

| Attribute | Required | Notes |
| --- | --- | --- |
| `publishable-key` | yes | The app's `bpk_…` key. |
| `phone` | no | Prefills the number, in E.164 (`+919876543210`). |
| `label` | no | The button text. |

| Event | `detail` |
| --- | --- |
| `bridge-verified` | `{ token, phone, verificationId }` |
| `bridge-error` | `{ code, message }` |
| `bridge-cancel` | none |

To open it from your own button instead, call `window.BridgeVerify.open()`. It returns a promise of
`{ token, phone, verificationId }`:

```js
document.querySelector('#verify-phone').addEventListener('click', async () => {
  try {
    const { token } = await window.BridgeVerify.open({
      publishableKey: 'bpk_9fK2mQ7xR4tV8wY1zA3bC5dE6gH0jL2n',
      phone: '+919876543210', // optional
    });
    await fetch('/api/phone/verify', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ token }),
    });
  } catch {
    // not verified
  }
});
```

Then check the token on your server. The `phone` in the browser event is for display only; trust
only what the token says:

```ts
app.post('/api/phone/verify', express.json(), async (req, res) => {
  try {
    const { phone, tokenId } = await verifyWidgetToken(req.body.token, {
      secret: process.env.BRIDGE_VERIFY_SECRET!,
      appId: 'vap_06ghaq3z0k2v7c9e4r2n0w6yb',
      issuer: 'https://api.sms.example.com',
    });
    if (!(await rememberToken(tokenId))) return res.status(409).json({ error: 'token_used' });
    await savePhone(req.session.userId, phone);
    res.json({ phone });
  } catch (err) {
    if (err instanceof BridgeTokenError) return res.status(400).json({ error: err.reason });
    throw err;
  }
});
```

### Widget endpoints

The widget and hosted page use these public endpoints, which you can also call to build your own
UI. They take no API key: the publishable key identifies the app, browsers are held to its allowed
origins, and each IP address is rate limited.

| Endpoint | Body | Returns | Per IP |
| --- | --- | --- | --- |
| `GET /v1/widget/{key}` | | `app_name`, `code_length`, `ttl_seconds`, `turnstile_site_key`, `environment` | 120 per minute |
| `POST /v1/widget/{key}/send` | `to`, `turnstile_token` | `verification_id`, `expires_at`, `resend_available_at`, and `code` for test widgets | 10 per minute |
| `POST /v1/widget/{key}/verify` | `verification_id`, `code` | `valid`, `status`, `attempts_remaining`, and when valid `token` and `token_expires_at` | 30 per minute |
| `GET /v1/widget/{key}/redirect-check?redirect_uri=` | | `200` when the URI is registered, `400` otherwise | 60 per minute |

Sends go through the app's fraud protection with the caller's IP address, and through Turnstile
when it is set up.

## Tokens

A token is a compact JWT signed with HS256. The key is the app's signing secret, the whole
`bvs_…` string as UTF-8 bytes (it is not base64). The header is `{"alg":"HS256","typ":"JWT"}`.

| Claim | Value |
| --- | --- |
| `iss` | The Bridge API URL that issued it (`BRIDGE_PUBLIC_URL`, without a trailing slash) |
| `aud` | The app ID, `vap_…` |
| `sub` | The verified phone number, E.164 |
| `vid` | The verification ID, `otp_…` |
| `env` | `live` or `test`: the widget's environment |
| `iat` | When the number was verified, Unix seconds |
| `exp` | `iat` + 600: tokens are valid for 10 minutes |
| `jti` | A random ID, unique per token |

### Verify it yourself

When you check a token locally, check all of these. Skipping any one lets a forged or misused
token through:

1. `alg` is `HS256`. Reject `none` and every other algorithm.
2. The signature matches, compared in constant time.
3. `aud` is your app's ID.
4. `iss` is your Bridge API URL.
5. `exp` is in the future (allow a little clock difference, Bridge uses 30 seconds).
6. `env` is `live`. A test-environment widget sends no SMS and returns the code to the browser,
   so its tokens prove nothing about a real phone. Accept `test` only in development.

Then accept each token once (store `jti` or `vid`): a token stays valid for 10 minutes.

In TypeScript, `verifyWidgetToken` from the [SDK](../../packages/sdk/README.md) does all of this
with WebCrypto (Node.js 20+, Bun, Deno and edge runtimes) and throws `BridgeTokenError` with a
`reason`. Any JWT library works too:

```ts
import jwt from 'jsonwebtoken';

const claims = jwt.verify(token, process.env.BRIDGE_VERIFY_SECRET, {
  algorithms: ['HS256'],
  audience: 'vap_06ghaq3z0k2v7c9e4r2n0w6yb',
  issuer: 'https://api.sms.example.com',
  clockTolerance: 30,
});
if (claims.env !== 'live') throw new Error('Test widget token');
```

```python
import jwt  # PyJWT

claims = jwt.decode(token, secret, algorithms=["HS256"], audience=app_id,
                    issuer="https://api.sms.example.com", leeway=30,
                    options={"require": ["exp", "iat", "aud", "iss", "sub"]})
if claims.get("env") != "live":
    raise ValueError("test widget token")
```

### Or ask Bridge: `POST /v1/otp/tokens/verify`

With an API key of the same project. Bridge checks the signature with the app's current secret,
the issuer, the expiry, that the token's environment matches the key's (a test token needs a test
key), and that the verification it names exists and was verified for that number.

```bash
curl "$BRIDGE_URL/v1/otp/tokens/verify" -H "Authorization: Bearer $BRIDGE_API_KEY" \
  -H "Content-Type: application/json" -d '{"token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9…"}'
```

```json
{
  "valid": true,
  "reason": null,
  "phone": "+919876543210",
  "verification_id": "otp_06ghatc07nghtrbq1yj7nwyjtm",
  "app_id": "vap_06ghaq3z0k2v7c9e4r2n0w6yb",
  "environment": "live",
  "expires_at": "2026-10-07T08:20:12Z"
}
```

An invalid token is a normal `200` with `valid: false` and the other fields `null`:

| `reason` | Meaning |
| --- | --- |
| `malformed` | Not a well-formed HS256 JWT |
| `unknown_app` | No app with this ID in the key's project (deleted, or another project). Locally: `aud` is not your app. |
| `bad_signature` | Not signed with the app's current secret (forged, or the secret was rotated) |
| `wrong_issuer` | Issued by another Bridge server |
| `expired` | Older than 10 minutes, or issued in the future |
| `environment_mismatch` | A test token checked with a live key, or the reverse. Locally: not the expected `env`. |
| `verification_mismatch` | The verification does not exist, belongs to another app, or was not verified for this number |

In the SDK: `await bridge.otp.verifyToken(token)`.

## Events

Subscribe a [webhook](../webhooks/README.md) (or `bridgectl messages tail`) to `otp.verified`,
`otp.failed` and `otp.expired` to track sign-ups without polling. Each event's `data` is the
verification, without the code. A verification replaced by a newer code is not announced.
`otp.blocked` reports attempts that [fraud protection](#fraud-protection) refused.

## Limits

| | Default | Configurable (per app) |
| --- | --- | --- |
| Code length | 6 digits | 4 to 10 |
| Valid for | 10 minutes | 1 to 60 minutes |
| Attempts | 5 | 1 to 10 |
| New code to the same number | after 30 seconds | no |
| Codes to the same number | 5 per hour | no |

The cooldown and the hourly cap count per app. Limits that are hit return `429 rate_limited` with
`Retry-After`. The per-number limits also bound guessing: at most 5 codes × 5 attempts per number
per hour and app. Regular message limits (per project and per destination) apply too, because each
code is an SMS. [Fraud protection](#fraud-protection) adds per-IP, number-range and country limits.

## Message text

Change it on the app's page under **Verify** in the dashboard. Each app has its own. Placeholders:

| Placeholder | Becomes |
| --- | --- |
| `{code}` | The code (required, exactly once) |
| `{app}` | The app name you set, or the project name |
| `{minutes}` | How long the code is valid, rounded up |

The default is `{code} is your {app} code. It expires in {minutes} minutes. Do not share it.`,
which fits one SMS segment. Starting with the code helps phones that show only the first line of a
notification.

**Autofill.** Two optional additions put the code where the user needs it:

- **Android (SMS Retriever):** pass `android_app_hash` when sending. Bridge adds the hash as the
  last line, and your app receives the code without the SMS permission.
- **Browsers (WebOTP) and iOS:** set a domain such as `example.com` on the app. Bridge adds
  `@example.com #482913` as the last line, which Chrome's WebOTP API and Safari use to offer the
  code above the keyboard on your site.

Both must be the last line, so when you pass an app hash, the domain line is left out of that SMS.

## iPhones

An iPhone cannot be a Bridge gateway: iOS does not let apps send SMS in the background, only through
a compose screen the user confirms. Pair Android phones to send, or use an
[SMS provider](../providers/README.md).

iPhones are fine on the receiving end. iOS reads the code from the SMS and offers it above the
keyboard:

- **Native apps:** iOS suggests codes from incoming SMS in fields marked for one-time codes
  (`textContentType = .oneTimeCode` in UIKit). Nothing to configure in Bridge.
- **Websites:** set the app's WebOTP domain in the dashboard. The `@example.com #482913` line lets
  Safari offer the code on that domain only. Mark the input with `autocomplete="one-time-code"`.

## Testing without a phone

With a `bk_test_` key nothing is sent, the SMS goes through the simulator, and the response
includes the code:

```ts
const sent = await bridge.otp.send({ to: '+15550000001' });
const { valid } = await bridge.otp.verify({ id: sent.id, code: sent.code! });
```

Live keys never return the code. Test and live verifications are separate: a test key cannot see
or check a live code. The simulator's [test numbers](../messages/README.md) behave as for messages,
so `+15550000002` gives you a code whose SMS fails.

For the widget and hosted page, set the app's widget environment to `test`: nothing is sent, the
widget shows the code, and tokens carry `env: test`. Check them with
`verifyWidgetToken(token, { …, environment: 'test' })` or a test key, and switch the app to `live`
before launch.

From a terminal:

```bash
bridgectl otp send +15550000001        # prints the code with a test key
bridgectl otp verify +15550000001 482913
```

`bridgectl otp verify` exits with status 1 when the code is not valid, so it works in scripts.

## How codes are protected

- Codes come from the operating system's secure random generator.
- Bridge stores only an HMAC-SHA256 of each code, keyed per installation and bound to the
  verification ID. The hash is erased as soon as the verification finishes.
- The SMS body contains the code, so the API, the dashboard and webhooks always show it masked
  (`•••••• is your Acme code…`). The stored text is erased when the verification finishes and
  the SMS has left the phone, and at the latest 1 hour after sending, so that
  [failover](#delivery-failover) can resend it until then.
- Checks compare in constant time and lock the verification row, so two concurrent checks cannot
  both use the last attempt.
- Finished verifications are kept for 30 days for the dashboard, then deleted. Blocked attempts
  are kept for 30 days too.

See the [security model](../security/README.md#verify-widget-and-tokens) for the widget's design.
