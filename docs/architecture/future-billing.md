# Billing design

How hosted Bridge's plans and payments are built. The code is in `apps/api/internal/billing`;
prices, limits and how to set up Dodo Payments are in [plans and billing](../hosted/billing.md).

## Principles

* **Never cripple the open-source build.** Plans apply only with `BRIDGE_CLOUD=true`. Self-hosted
  servers have no plans, limits or payment settings, and `GET /v1/plans` returns an empty list.
* **Pricing is configuration, not code.** Plans, prices and limits live in the `plans` table, so
  they change with an `UPDATE`, not a deploy.
* **Bridge's own state decides access**, and it is always read from the payment provider, never
  taken from a webhook's body.
* **Do not subsidise provider costs.** SMS providers (MSG91, Twilio and others) bill their own
  accounts directly; plans only cover what Bridge itself runs.

## Provider: Dodo Payments

Dodo Payments is the merchant of record: it takes payment, issues invoices and handles tax. Bridge
calls Dodo's REST API directly from Go (`internal/billing/dodo.go`, no SDK) for checkout sessions,
the customer portal, plan changes, reading a subscription and scheduling or withdrawing
cancellation.

Configuration: `BRIDGE_DODO_API_KEY`, `BRIDGE_DODO_WEBHOOK_SECRET`, `BRIDGE_DODO_ENVIRONMENT`
(`test_mode` or `live_mode`) and `BRIDGE_DODO_PRODUCTS` (`pro=pdt_…,business=pdt_…`). Without them,
`BRIDGE_CLOUD=true` still enforces plans and the Billing page says payments are not set up.

## Data model

| Table | Holds |
| --- | --- |
| `plans` | `free`, `pro` and `business` (shown as Team): name, price, currency, and the limits on phones, live SMS a month, projects and members (`NULL` is unlimited). |
| `subscriptions` | One row per workspace: plan, status, Dodo customer and subscription IDs, period end, `cancel_at_period_end`. |
| `billing_webhook_events` | The `webhook-id` of every Dodo delivery already applied. |
| `organization_usage` | Live SMS per workspace per calendar month (UTC). |

A workspace without a subscription that grants access is on Free. Statuses are Bridge's own:
`active`, `past_due` (Dodo `on_hold`: access continues while Dodo retries the payment),
`cancelled` (also Dodo `paused`), `expired` and `incomplete`. Any status Dodo adds later maps to
`incomplete`, which grants nothing.

## Flows

1. **Checkout.** An owner picks a plan. A workspace without a paying subscription gets a Dodo
   checkout session with `product_cart`, the customer (the stored Dodo customer when there is one,
   so cards and invoices stay together), `return_url`, `cancel_url` and
   `metadata {organization_id, plan}`. A workspace that already pays is moved in place instead
   (`change-plan`, prorated immediately, refused if the charge fails).
2. **Return from checkout.** Dodo appends `subscription_id` to the return URL. The dashboard calls
   `POST /v1/organizations/{id}/billing/sync`, which reads that subscription from Dodo and stores it,
   so the plan changes without waiting for the webhook. A subscription that belongs to another
   workspace is refused.
3. **Webhooks.** `POST /v1/billing/dodo/webhook` verifies the Standard Webhooks signature
   (`webhook-id`, `webhook-timestamp`, `webhook-signature`) over the **raw** body, within 5 minutes,
   and answers `401` on failure. A `subscription.*` event for one of Bridge's products is only a
   signal: Bridge reads the subscription's current state from Dodo and applies that.
4. **Portal and cancellation.** Owners open Dodo's customer portal for invoices, payment methods and
   cancelling. A cancellation scheduled for the period end can be withdrawn with
   `POST /v1/organizations/{id}/billing/resume`.

## Decisions worth keeping

* **Idempotency is atomic.** The webhook claims its `webhook-id` with an insert that does nothing on
  conflict, in the same transaction that applies the change. Checking "already processed?" and
  processing as separate steps would let concurrent deliveries through.
* **Re-read instead of trusting the event.** Because each webhook triggers a fresh read from Dodo,
  late, repeated or out-of-order deliveries cannot undo a newer change, and an event about an older
  subscription that no longer grants access never replaces a newer, active one.
* **Matching a subscription to a workspace.** The first webhook can arrive before the checkout
  redirect returns, so the workspace is found from checkout metadata first, then the stored
  subscription ID, then the stored customer ID. A subscription for one of Bridge's products that
  matches no workspace fails the webhook, so Dodo delivers it again. Events for other products on
  the same Dodo account are acknowledged and ignored.
* **Limits are enforced where the data is written.** Live SMS are counted in the transaction that
  creates each message, so concurrent sends cannot overshoot; phones, projects and members are
  checked in the transaction that adds them. Test messages are never counted.
* **Status mapping has no SDK or environment imports**, so it stays unit-testable
  (`internal/billing/webhook_test.go`).
