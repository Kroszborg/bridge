# Supabase Auth

Supabase Auth can send phone sign-in codes through Bridge with its
[Send SMS hook](https://supabase.com/docs/guides/auth/auth-hooks/send-sms-hook). Supabase still
generates the code and checks it when the user types it in; Bridge only delivers the SMS, through
your phones or an [SMS provider](../providers/README.md). Your app keeps calling Supabase as before.

```text
supabase.auth.signInWithOtp({ phone })
     │
     ▼
Supabase Auth ── Send SMS hook (signed) ──► Bridge ──► phone or provider ──► SMS
     ▲
     │
supabase.auth.verifyOtp({ phone, token, type: 'sms' })
```

## What you need

- Bridge reachable from Supabase over the internet. For Supabase's hosted platform this means a
  public HTTPS URL in `BRIDGE_PUBLIC_URL`, because the hook URL is built from it.
- `BRIDGE_SECRET_KEY` set on the Bridge server. Bridge stores Supabase's signing secret encrypted
  with it, and refuses to create the integration without it. See
  [Self-hosting](../self-hosting/README.md#configuration).

## Set it up

1. **Bridge:** open the project, then **Integrations**. Under **Supabase Auth**, choose the
   environment (**Live** sends real SMS; **Test** sends nothing while you wire things up, and
   messages go to the simulator) and **Connect Supabase**. Copy the **hook URL**. It looks like
   `https://api.sms.example.com/v1/hooks/supabase/int_06ghc2…`.
2. **Supabase:** open **Authentication → Hooks**, add a new hook and choose **Send SMS hook**.
   Pick **HTTPS** as the hook type and paste the hook URL.
3. **Supabase:** choose **Generate secret** and copy the whole secret, including the `v1,whsec_`
   prefix. Save the hook.
4. **Bridge:** paste the secret into **Hook secret** and save. Until you do, Bridge answers
   Supabase with `503` and sends nothing.
5. **Supabase:** enable the Phone provider under Authentication if it is not on yet. Its own SMS
   provider settings are not used while the hook is on.
6. Sign in with a phone number from your app:

```ts
await supabase.auth.signInWithOtp({ phone: '+919876543210' });
// the user receives the SMS and types the code
await supabase.auth.verifyOtp({ phone: '+919876543210', token: '482913', type: 'sms' });
```

The message appears under **Messages** in Bridge with `purpose: otp`, the code masked, and metadata
`source: supabase`, `integration_id` and `supabase_user_id`.

The same with the API instead of the dashboard (session-authenticated):

```http
POST /v1/projects/{projectId}/integrations
{ "kind": "supabase_send_sms", "environment": "live" }

PATCH /v1/projects/{projectId}/integrations/{integrationId}
{ "secret": "v1,whsec_…" }
```

## The message

Bridge writes the SMS with the project's Verify template (**Verify → Message and limits**), so it
reads like Bridge's own codes: `482913 is your Acme code. It expires in 10 minutes. Do not share
it.` The placeholders are filled as follows:

| Placeholder | Becomes |
| --- | --- |
| `{code}` | The code Supabase generated |
| `{app}` | Your Verify app name, or the project name |
| `{minutes}` | **Bridge's** Verify lifetime setting, not Supabase's |

Set Bridge's **Valid for** to match the SMS code expiry in Supabase, or leave `{minutes}` out of
the template, so the SMS does not promise a lifetime Supabase does not honour. If you set a WebOTP
domain in Verify, the `@domain #code` line is added for browser and iOS autofill.

Bridge's Verify limits (attempts, the 30-second resend wait, 5 codes per number per hour) do not
apply here: Supabase enforces its own. Bridge's message limits do (1,000 per project and 20 per
number per hour).

## How the request is checked

Supabase signs each request with [Standard Webhooks](https://www.standardwebhooks.com) using the
secret it generated. Bridge verifies the signature against the stored secret and rejects
timestamps more than 5 minutes from its clock. It then reads `user.phone` and `sms.otp` from the
body (adding a leading `+` to the number if Supabase sent it without one), and queues the SMS.

| Bridge answers | Body | When |
| --- | --- | --- |
| `200` | `{}` | The SMS is queued. Supabase treats this as sent. |
| `4xx`, `5xx` | `{"error": {"http_code": 401, "message": "…"}}` | See below. Supabase shows the message to its caller. |

`200` means queued, not delivered. Follow delivery in Bridge (**Messages**, or a
[webhook](../webhooks/README.md)).

## Local development with the Supabase CLI

The CLI configures hooks in `supabase/config.toml`
([CLI config reference](https://supabase.com/docs/guides/local-development/cli/config)):

```toml
[auth.hook.send_sms]
enabled = true
uri = "http://host.docker.internal:8080/v1/hooks/supabase/int_06ghc2…"
secrets = "env(BRIDGE_SEND_SMS_HOOK_SECRET)"
```

```bash
export BRIDGE_SEND_SMS_HOOK_SECRET='v1,whsec_<base64>'   # then restart Supabase
```

(Supabase's own guide keeps such values in `supabase/functions/.env`; any way the CLI can read the
variable works.)

- Local Supabase Auth runs in Docker, so `localhost` there is the container, not your machine. Use
  `host.docker.internal` to reach a Bridge that publishes port 8080 on the host. (Supabase documents
  `host.docker.internal` for hooks; on Linux it may need extra Docker configuration.) Keep the path
  from the hook URL Bridge shows you and change only the host.
- With the CLI there is no **Generate secret** button. Make your own and paste the same value into
  Bridge (**Hook secret**) and `BRIDGE_SEND_SMS_HOOK_SECRET`:

  ```bash
  echo "v1,whsec_$(openssl rand -base64 32)"
  ```

- Plain `http://` is fine locally; the CLI accepts http and https hook URIs.
- Use a **Test** integration so local sign-ins send nothing.

## Troubleshooting

Bridge records the last error on the integration (`last_error`), shown in the dashboard.

| Status | Message | Fix |
| --- | --- | --- |
| `401` | The signature does not match the secret stored in Bridge. | Paste the secret again, exactly as Supabase shows it. Check that the Bridge server's clock is right (5-minute tolerance). |
| `503` | Bridge is not set up yet: paste Supabase's hook secret into the Bridge dashboard. | Do step 4. |
| `429` | Too many messages to this number. Try again later. | Bridge's message limits were hit (20 per number or 1,000 per project per hour). |
| `400` | Expected user.phone and sms.otp. | The request was not a Send SMS hook payload. |
| `400` | A validation message, e.g. about the number | The phone number is not valid E.164, or the code is not 4 to 12 letters or digits. |
| `404` | Unknown Bridge hook URL. | The integration was deleted, or the URL is wrong. |
| `500` | Bridge cannot read the stored hook secret. Paste it again in the dashboard. | `BRIDGE_SECRET_KEY` changed since the secret was saved. |

Supabase retries `429` and `503` a few times within its 5-second budget for a hook, and treats
other `4xx` and `5xx` answers as errors. If Supabase cannot reach Bridge at all, nothing shows up
in Bridge: check that the hook URL is reachable from where Supabase Auth runs.
