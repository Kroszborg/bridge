# Plans and billing (hosted Bridge)

Hosted Bridge sells three monthly plans through [Dodo Payments](https://dodopayments.com), the
merchant of record (it takes payment, issues invoices and handles tax). All of it switches on with
`BRIDGE_CLOUD=true`; self-hosted installs never see plans or plan limits.

| | Free | Pro | Team |
| --- | --- | --- | --- |
| Price | $0 | $5/month | $15/month |
| Phones | 1 | 3 | 10 |
| Live SMS a month | 300 | 5,000 | 25,000 |
| Projects | 1 | 3 | Unlimited |
| Members (incl. open invites) | 1 | 3 | 10 |

Free is for one person trying Bridge on one phone; teams and more projects start at Pro. Allowances
stay below what the plan's phones can send under the daily cap (phones x 100 x 30), so a plan never
promises more SMS than its SIMs can safely send. Team is stored as plan ID `business`.

Test messages are never limited. Plans live in the `plans` table, so prices and limits change with an
`UPDATE`, not a deploy:

```sql
UPDATE plans SET max_live_messages = 500, updated_at = now() WHERE id = 'free';
```

To give a workspace a plan without payment (your own, or a partner's), add a subscription with no
provider IDs. It only changes if the workspace later buys a plan itself:

```sql
INSERT INTO subscriptions (organization_id, plan_id, status) VALUES ('org_…', 'business', 'active')
ON CONFLICT (organization_id) DO UPDATE SET plan_id = EXCLUDED.plan_id, status = 'active', updated_at = now();
```

## How limits apply

* **Every phone** also has a daily cap (`daily_send_limit`, default 100 in any 24 hours, set per
  phone under Phones → ⋯ → Settings), on every plan and on self-hosted servers. Operators cap how many
  SMS a SIM may send a day, about 100 on most Indian plans, and can block SIMs that send far more.
  Dispatch skips a phone at its cap; if every phone is full, the message waits and fails after an
  hour with `daily_limit_reached`.

* **Live SMS** are counted per workspace per calendar month (UTC), in the same transaction that creates
  each message, so concurrent sends cannot overshoot. Over the allowance, live sends get
  `402 plan_limit_reached`; broadcast recipients are skipped with `plan_limit`, and schedules record
  the reason. The count resets on the 1st.
* **Phones** are checked when a new phone pairs (re-pairing the same phone is always allowed),
  **projects** when one is created, and **members** when an invite is created or accepted.
* A workspace keeps its plan while Dodo retries a failed payment (`on_hold` → `past_due`). Cancelled,
  expired and failed subscriptions fall back to Free; nothing is deleted.

## Setting up Dodo Payments

1. Create an account at dodopayments.com and stay in **Test mode** until everything works.
2. **Products → Create product**, twice, as **Subscription** products billed monthly:
   * `Bridge Pro`, $5.00
   * `Bridge Team`, $15.00 (plan ID `business`)

   Copy each product ID (`pdt_…`).
3. **Developer → API keys → Create**. Copy the key.
4. **Developer → Webhooks → Add endpoint**:
   * URL: `https://api.bridge.kroszborg.co/v1/billing/dodo/webhook`
   * Events: all `subscription.*` events.

   Copy the signing secret (`whsec_…`).
5. Add to `.env.production` and deploy:

   ```dotenv
   BRIDGE_CLOUD=true
   BRIDGE_DODO_API_KEY=...
   BRIDGE_DODO_WEBHOOK_SECRET=whsec_...
   BRIDGE_DODO_ENVIRONMENT=test_mode
   BRIDGE_DODO_PRODUCTS=pro=pdt_...,business=pdt_...
   ```

6. In the dashboard, open **Billing** from the account menu, upgrade with one of
   the test cards from Dodo's testing docs, and check the plan
   switches to Pro within a few seconds.
7. To go live, repeat steps 2–4 in **Live mode** (live products and keys are different), set
   `BRIDGE_DODO_ENVIRONMENT=live_mode` with the live values, and deploy.

Without the Dodo settings, `BRIDGE_CLOUD=true` still enforces plans; the Billing page then says
payments are not set up.

## API

| Endpoint | |
| --- | --- |
| `GET /v1/plans` | Public plan list (empty when self-hosted). |
| `GET /v1/organizations/{id}/billing` | Plan, status, limits and this month's usage. |
| `POST /v1/organizations/{id}/billing/checkout` | Owners. Checkout URL, or an in-place prorated plan change for paying workspaces. |
| `POST /v1/organizations/{id}/billing/portal` | Owners. Dodo customer portal: invoices, payment method, cancel. |
| `POST /v1/billing/dodo/webhook` | Dodo events, verified with Standard Webhooks and applied once per `webhook-id`. |

The implementation is in `apps/api/internal/billing`; see the
[billing design](../architecture/future-billing.md) for how it works, and the
[security model](../security/README.md#hosted-bridge) for how payments and webhooks are protected.
