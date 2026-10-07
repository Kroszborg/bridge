# Future billing (Era 1+) — reference notes

**Not implemented.** Billing is explicitly out of scope until Bridge Cloud exists. These notes
record the payment pattern already proven in a sibling project so Era 1 does not start from zero.

## Provider: Dodo Payments

* SDK: `dodopayments` (TypeScript). For a Go backend, either call the REST API directly or keep
  billing in a small TS edge function. Decide in Era 1.
* Env: `DODO_API_KEY`, `DODO_WEBHOOK_SECRET`, `DODO_ENVIRONMENT` (`test_mode` | `live_mode`),
  plus one product ID per plan. Billing switches on only when key and webhook secret are both set,
  so self-hosted installs never need them.

## Flow

1. **Checkout:** create a checkout session with `product_cart`, `customer {email, name}`,
   `return_url` and `metadata {organization_id}`, then redirect to `checkout_url`.
2. **Portal:** create a customer-portal session from the stored provider customer ID.
3. **Webhooks:** Standard Webhooks format (`webhook-id`, `webhook-signature`,
   `webhook-timestamp`). Verify against the **raw** body, and return 401 on failure.

## Lessons to carry over

* Map provider statuses to our own: `active`, `past_due` (Dodo `on_hold`, which keeps access
  during payment retries), `cancelled`, `expired`, `incomplete`. Unknown statuses become `incomplete`.
* **Idempotency must be atomic.** Claim the event first with
  `INSERT INTO billing_webhook_events (id) … ON CONFLICT DO NOTHING RETURNING id`, then process it
  in the same transaction. Checking "already processed?" and processing as separate steps lets
  concurrent deliveries through.
* The first webhook can arrive before the checkout redirect returns, so resolve the customer by
  `metadata`, then the provider customer ID, then email.
* Side effects such as emails run after commit and never fail the webhook response; otherwise
  the provider retries.
* Keep normalization code free of SDK/env imports so it stays unit-testable.
* Pricing is configuration, never code: plan limits and prices belong in the database, not in constants.

## Bridge-specific constraints

* Never cripple the open-source build. Billing only gates hosted conveniences.
* Do not subsidize external provider (MSG91/Twilio) costs indefinitely; pass them through transparently.
