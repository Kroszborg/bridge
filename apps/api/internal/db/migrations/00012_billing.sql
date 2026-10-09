-- +goose Up
-- Hosted Bridge billing (BRIDGE_CLOUD=true). Self-hosted installs never read
-- these tables: without the flag every organization is unlimited.

-- Plans and their limits. Pricing is configuration, not code: change a row to
-- change a plan. NULL limits are unlimited. The Dodo product for each paid
-- plan is configured with BRIDGE_DODO_PRODUCTS, since test and live mode
-- products have different IDs.
CREATE TABLE plans (
    id                text PRIMARY KEY,
    name              text NOT NULL,
    price_cents       integer NOT NULL CHECK (price_cents >= 0),
    currency          text NOT NULL DEFAULT 'USD',
    max_phones        integer CHECK (max_phones >= 0),
    max_live_messages integer CHECK (max_live_messages >= 0),
    max_projects      integer CHECK (max_projects >= 0),
    max_members       integer CHECK (max_members >= 0),
    sort_order        integer NOT NULL DEFAULT 0,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now()
);

INSERT INTO plans (id, name, price_cents, max_phones, max_live_messages, max_projects, max_members, sort_order) VALUES
    ('free',     'Free',     0,    1,  300,    1,    2,  0),
    ('pro',      'Pro',      900,  5,  10000,  5,    5,  1),
    ('business', 'Business', 2900, 25, 100000, NULL, 20, 2);

-- An organization's paid subscription. No row (or an ended one) means Free.
CREATE TABLE subscriptions (
    organization_id          text PRIMARY KEY REFERENCES organizations (id) ON DELETE CASCADE,
    plan_id                  text NOT NULL REFERENCES plans (id),
    -- Bridge's own statuses; provider statuses are mapped in internal/billing.
    status                   text NOT NULL CHECK (status IN ('active', 'past_due', 'cancelled', 'expired', 'incomplete')),
    provider_customer_id     text,
    provider_subscription_id text UNIQUE,
    current_period_end       timestamptz,
    cancel_at_period_end     boolean NOT NULL DEFAULT false,
    created_at               timestamptz NOT NULL DEFAULT now(),
    updated_at               timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX subscriptions_customer_idx ON subscriptions (provider_customer_id);

-- Webhook deliveries already processed, claimed atomically so concurrent
-- retries of the same event are applied once.
CREATE TABLE billing_webhook_events (
    id          text PRIMARY KEY,
    type        text NOT NULL,
    received_at timestamptz NOT NULL DEFAULT now()
);

-- Live messages accepted per organization per calendar month (UTC). Counted
-- in the transaction that creates each message, so the limit cannot be raced.
CREATE TABLE organization_usage (
    organization_id text NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    period          date NOT NULL,
    live_messages   integer NOT NULL DEFAULT 0,
    PRIMARY KEY (organization_id, period)
);

-- +goose Down
DROP TABLE organization_usage;
DROP TABLE billing_webhook_events;
DROP TABLE subscriptions;
DROP TABLE plans;
