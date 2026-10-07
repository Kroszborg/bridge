# Firebase and Clerk

## Firebase Authentication

Firebase Phone Auth sends its verification SMS itself. Its documentation offers no way to plug in
your own SMS sender for that flow, so Bridge cannot deliver Firebase's codes.

You can still sign users in to Firebase with a phone number verified by Bridge: check the number
with Bridge [Verify](../otp/README.md) on your server, then hand the client a Firebase **custom
token**.

```ts
// Your server (Node.js), with firebase-admin and @kroszborg/bridge
import { Bridge } from '@kroszborg/bridge';
import { getAuth } from 'firebase-admin/auth';

const bridge = new Bridge();

// POST /auth/phone/start  { phone }
export async function start(phone: string) {
  await bridge.otp.send({ to: phone });
}

// POST /auth/phone/finish  { phone, code }  ->  { token } or 401
export async function finish(phone: string, code: string) {
  const { valid } = await bridge.otp.verify({ to: phone, code });
  if (!valid) return null;
  const auth = getAuth();
  const user = await auth.getUserByPhoneNumber(phone).catch(() => auth.createUser({ phoneNumber: phone }));
  return auth.createCustomToken(user.uid);
}
```

```ts
// The client
import { getAuth, signInWithCustomToken } from 'firebase/auth';

const { token } = await (await fetch('/auth/phone/finish', { method: 'POST', body: JSON.stringify({ phone, code }) })).json();
await signInWithCustomToken(getAuth(), token);
```

The user ends up signed in to Firebase with the phone number on their account, and Firebase never
sends an SMS. `bridge.otp.verify` throws for `404` when no code is pending for the number; handle
it like a wrong code.

## Clerk

Clerk sends SMS through its own gateway by default, but its
[SMS templates](https://clerk.com/docs/guides/customizing-clerk/email-sms-templates) have a
**Delivered by Clerk** setting. Turn it off for a template and Clerk stops sending that SMS; your
app delivers it instead, from the `sms.created` webhook. Clerk's templates page does not say which
plans include this; check your Clerk dashboard.

To deliver with Bridge:

1. In Clerk, turn off **Delivered by Clerk** for the SMS templates Bridge should send (for example
   the verification code).
2. Add a Clerk webhook endpoint in your app subscribed to `sms.created`, and verify each request as
   Clerk's webhook guide describes (Clerk signs webhooks through Svix).
3. Send the message Clerk rendered:

```ts
// After verifying the webhook request with Clerk's tooling
if (evt.type === 'sms.created' && !evt.data.delivered_by_clerk) {
  await bridge.messages.send(
    { to: evt.data.to_phone_number, message: evt.data.message },
    { idempotencyKey: evt.data.id }, // a retried webhook does not send the SMS twice
  );
}
```

`to_phone_number`, `message` and `delivered_by_clerk` are fields of Clerk's SMS message object.
Clerk still generates and checks the code. Because Bridge sees an ordinary message, the code is
stored and shown in full in Bridge until message retention removes it.

If you would rather not depend on Clerk's SMS at all, use the Firebase pattern above: verify the
number with Bridge Verify on your server, then create or sign in the Clerk user with Clerk's
backend API.
