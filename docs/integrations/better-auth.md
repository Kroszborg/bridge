# Better Auth

Better Auth's [phone number plugin](https://www.better-auth.com/docs/plugins/phone-number) calls a
`sendOTP` function you write whenever a code must go out. Point it at Bridge with the
[SDK](../../packages/sdk/README.md):

```bash
npm i @kroszborg/bridge
```

Set `BRIDGE_API_KEY` (a `bk_live_` key, or `bk_test_` while developing) and `BRIDGE_URL` (your
Bridge server) in the environment. Bridge expects numbers in E.164 (`+919876543210`); use the
plugin's `phoneNumberValidator` option to reject anything else early.

There are two ways to do it.

## Option 1: Better Auth makes the code, Bridge sends it

Better Auth generates, stores and checks the code, as it does by default. Bridge only delivers the
SMS.

```ts
import { betterAuth } from 'better-auth';
import { phoneNumber } from 'better-auth/plugins';
import { Bridge } from '@kroszborg/bridge';

const bridge = new Bridge(); // reads BRIDGE_API_KEY and BRIDGE_URL

export const auth = betterAuth({
  plugins: [
    phoneNumber({
      sendOTP: ({ phoneNumber, code }) => {
        // Not awaited, as Better Auth recommends, so the response time does not depend on SMS.
        bridge.messages
          .send({ to: phoneNumber, message: `${code} is your Acme code. Do not share it.` })
          .catch((err) => console.error('Bridge could not queue the code', err));
      },
    }),
  ],
});
```

Keep the text short: one SMS segment is 160 GSM-7 characters. Because Bridge sees an ordinary
message, the code is stored and shown in full in Bridge until message retention removes it. Use
option 2 if that matters to you.

## Option 2: Bridge Verify makes and checks the code

The plugin's `verifyOTP` option replaces Better Auth's own check. Combined with `sendOTP`, Bridge
[Verify](../otp/README.md) generates, sends and checks the code, and Bridge's message shows it
masked.

```ts
import { betterAuth } from 'better-auth';
import { phoneNumber } from 'better-auth/plugins';
import { Bridge, BridgeApiError } from '@kroszborg/bridge';

const bridge = new Bridge();

export const auth = betterAuth({
  plugins: [
    phoneNumber({
      // Better Auth's code is ignored: Bridge makes its own and sends it.
      sendOTP: ({ phoneNumber }) => {
        bridge.otp
          .send({ to: phoneNumber })
          .catch((err) => console.error('Bridge could not send the code', err));
      },
      verifyOTP: async ({ phoneNumber, code }) => {
        try {
          const { valid } = await bridge.otp.verify({ to: phoneNumber, code });
          return valid;
        } catch (err) {
          // 404: no code is pending for this number. 422: the input is not 4 to 10 digits.
          if (err instanceof BridgeApiError && (err.status === 404 || err.status === 422)) return false;
          throw err;
        }
      },
    }),
  ],
});
```

With option 2:

- The code length, lifetime, attempts and message text come from Bridge (**Verify → Message and
  limits**), not from the plugin's `otpLength`, `expiresIn` and `allowedAttempts`. Better Auth's
  documentation does not say whether it still generates and stores its own code when `verifyOTP`
  is set; either way, only Bridge's code reaches the user.
- Bridge's resend rules apply: a new code to the same number at most every 30 seconds and 5 times an
  hour. `bridge.otp.send` then fails with `429`, which the example logs. Tell users to wait before
  asking again.
- With a `bk_test_` key nothing is sent. The response includes the code
  (`(await bridge.otp.send(...)).code`) for automated tests.

Both options route the SMS like any other Bridge message: through your phones, or an
[SMS provider](../providers/README.md) if routing says so.
