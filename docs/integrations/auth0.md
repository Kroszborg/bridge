# Auth0

Auth0 can send its SMS (passwordless codes, MFA codes, phone verification) through any gateway with
a **custom phone provider**: an Action on the `custom-phone-provider` trigger that Auth0 runs for
each message. Auth0 generates and checks the code and writes the text; the Action hands the text
to Bridge.

See Auth0's guide,
[Configure a custom phone provider](https://auth0.com/docs/customize/phone-messages/configure-phone-messaging-providers/configure-a-custom-phone-provider),
for the current screens.

## Set it up

1. In Bridge, create a live API key (`bk_live_…`) for the project that should send. Use a test key
   (`bk_test_…`) first if you want to try the flow without sending anything.
2. In the Auth0 Dashboard, open **Branding → Phone Provider**, choose **Custom**, and choose
   **Text** as the delivery method. Bridge sends SMS only, not voice.
3. Under the provider configuration, add two secrets: `BRIDGE_URL` (your Bridge API, e.g.
   `https://api.sms.example.com`) and `BRIDGE_API_KEY`.
4. Replace the Action code with the code below and save. Auth0 deploys it on save.
5. Use **Send Test Message**, then follow the message under **Messages** in Bridge.

```js
/**
 * Sends Auth0's phone messages through Bridge.
 * @param {Event} event
 * @param {CustomPhoneProviderAPI} api
 */
exports.onExecuteCustomPhoneProvider = async (event, api) => {
  const n = event.notification;
  if (n.delivery_method !== 'text') {
    throw new Error('Bridge sends SMS only; voice messages are not supported.');
  }

  const res = await fetch(`${event.secrets.BRIDGE_URL}/v1/messages`, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${event.secrets.BRIDGE_API_KEY}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({
      to: n.recipient,
      message: n.as_text, // the text Auth0 rendered, including the code
      metadata: { source: 'auth0', message_type: n.message_type },
    }),
  });

  if (!res.ok) {
    throw new Error(`Bridge answered ${res.status}: ${await res.text()}`);
  }
};
```

The fields come from Auth0's `event.notification`: `recipient` (the phone number), `as_text` (the
rendered message), `delivery_method` (`text` or `voice`) and `message_type` (such as `otp_verify`,
`otp_enroll`, `blocked_account`, `change_password` or `password_breach`). It also carries `code`,
`from` and `locale` if you would rather write your own text.

Notes:

- Bridge answers `202` once the message is queued; delivery happens after the Action returns.
  Follow it in Bridge or with a [webhook](../webhooks/README.md).
- Bridge needs the number in E.164 (`+919876543210`). If your tenant passes numbers without the
  leading `+`, add it before sending; otherwise Bridge answers `422`.
- Auth0's documentation does not describe what the user sees when the Action throws. Check it with
  **Send Test Message** and a deliberately wrong API key before going live.
- Because Bridge sees an ordinary message, the code is stored and shown in full in Bridge until
  message retention removes it. Auth0 cannot use Bridge Verify for its own codes, since Auth0
  checks them itself.
- Auth0 may apply its own limits to phone messages; Bridge's apply too (20 messages per number and
  1,000 per project per hour). Over them, Bridge answers `429` and the Action throws.

## Send Phone Message trigger (MFA only)

Auth0 also has a separate `send-phone-message` trigger (`onExecuteSendPhoneMessage`) that runs
when SMS or voice is used as an **MFA** factor. Its message is in `event.message_options`
(`recipient`, `text`, `message_type`, `action`). If you use it, the Action body is the same as
above with `event.message_options.recipient` and `event.message_options.text`. Auth0's
documentation says not to use this trigger to configure a custom phone provider; the two are set up
differently.
