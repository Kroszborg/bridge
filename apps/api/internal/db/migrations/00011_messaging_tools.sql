-- +goose Up
-- Messaging tools: bulk sends (broadcasts), scheduled and repeating messages,
-- an opt-out list with keyword auto-replies, and forwarding of incoming SMS.

-- ---------------------------------------------------------------------------
-- Broadcasts
-- ---------------------------------------------------------------------------

-- One template sent to many recipients. Messages are created gradually by a
-- background job (internal/broadcast) through the normal sending pipeline.
CREATE TABLE broadcasts (
    id                text PRIMARY KEY,
    project_id        text NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    environment       api_environment NOT NULL,
    name              text NOT NULL DEFAULT '',
    template          text NOT NULL,
    -- Send through this phone only; NULL lets Bridge pick, as for single messages.
    device_id         text REFERENCES devices (id) ON DELETE SET NULL,
    status            text NOT NULL CHECK (status IN ('scheduled', 'sending', 'completed', 'canceled')),
    scheduled_at      timestamptz,
    -- Counted when the broadcast was created; recipients are unique numbers
    -- that were not opted out.
    total_recipients  integer NOT NULL,
    skipped_opted_out integer NOT NULL DEFAULT 0,
    duplicates        integer NOT NULL DEFAULT 0,
    total_segments    integer NOT NULL DEFAULT 0,
    api_key_id        text REFERENCES api_keys (id) ON DELETE SET NULL,
    created_by        text REFERENCES users (id) ON DELETE SET NULL,
    started_at        timestamptz,
    completed_at      timestamptz,
    canceled_at       timestamptz,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX broadcasts_project_idx ON broadcasts (project_id, environment, created_at DESC, id DESC);

CREATE TABLE broadcast_recipients (
    broadcast_id text NOT NULL REFERENCES broadcasts (id) ON DELETE CASCADE,
    position     integer NOT NULL,
    recipient    text NOT NULL,
    -- Template variables; cleared once the message exists (its body follows
    -- the message retention period).
    vars         jsonb,
    -- pending: no message yet; queued: message created; skipped: refused when
    -- its turn came (for example opted out meanwhile); canceled: the broadcast
    -- was canceled first.
    status       text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'queued', 'skipped', 'canceled')),
    skip_reason  text,
    message_id   text REFERENCES messages (id) ON DELETE SET NULL,
    PRIMARY KEY (broadcast_id, position)
);
CREATE INDEX broadcast_recipients_pending_idx ON broadcast_recipients (broadcast_id, position) WHERE status = 'pending';

-- ---------------------------------------------------------------------------
-- Scheduled and repeating messages
-- ---------------------------------------------------------------------------

CREATE TABLE scheduled_messages (
    id              text PRIMARY KEY,
    project_id      text NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    environment     api_environment NOT NULL,
    name            text NOT NULL DEFAULT '',
    recipient       text NOT NULL,
    body            text NOT NULL,
    device_id       text REFERENCES devices (id) ON DELETE SET NULL,
    -- When it runs, in time_zone (IANA): once on run_date, every day, on the
    -- given weekdays, or on day_of_month (clamped to the month's last day).
    kind            text NOT NULL CHECK (kind IN ('once', 'daily', 'weekly', 'monthly')),
    at_time         text NOT NULL CHECK (at_time ~ '^([01][0-9]|2[0-3]):[0-5][0-9]$'),
    days            text[] NOT NULL DEFAULT '{}',
    day_of_month    smallint CHECK (day_of_month BETWEEN 1 AND 31),
    run_date        date,
    time_zone       text NOT NULL,
    ends_at         timestamptz,
    paused          boolean NOT NULL DEFAULT false,
    -- NULL once nothing is left to run (a past one-off, or after ends_at).
    next_run_at     timestamptz,
    last_run_at     timestamptz,
    last_message_id text REFERENCES messages (id) ON DELETE SET NULL,
    last_error      text,
    run_count       integer NOT NULL DEFAULT 0,
    api_key_id      text REFERENCES api_keys (id) ON DELETE SET NULL,
    created_by      text REFERENCES users (id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX scheduled_messages_due_idx ON scheduled_messages (next_run_at) WHERE NOT paused AND next_run_at IS NOT NULL;
CREATE INDEX scheduled_messages_project_idx ON scheduled_messages (project_id, environment, created_at DESC, id DESC);

-- ---------------------------------------------------------------------------
-- Opt-outs and auto-replies
-- ---------------------------------------------------------------------------

-- Numbers that asked not to receive messages. Ordinary sends to them are
-- refused; one-time passwords and auto-replies are not.
CREATE TABLE opt_outs (
    id         text PRIMARY KEY,
    project_id text NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    number     text NOT NULL,
    source     text NOT NULL CHECK (source IN ('keyword', 'manual', 'api')),
    keyword    text,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (project_id, number)
);
CREATE INDEX opt_outs_project_idx ON opt_outs (project_id, created_at DESC, id DESC);

CREATE TABLE auto_reply_rules (
    id         text PRIMARY KEY,
    project_id text NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    name       text NOT NULL,
    match_type text NOT NULL CHECK (match_type IN ('exact', 'contains', 'starts_with')),
    -- Compared case-insensitively with the trimmed message.
    keywords   text[] NOT NULL,
    reply      text,
    action     text NOT NULL DEFAULT 'none' CHECK (action IN ('none', 'opt_out', 'opt_in')),
    enabled    boolean NOT NULL DEFAULT true,
    -- Lower runs first; only the first matching enabled rule runs.
    priority   integer NOT NULL DEFAULT 100,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX auto_reply_rules_project_idx ON auto_reply_rules (project_id, priority, created_at);

-- Per-project automation state: the default auto-reply rules are created
-- once, so deleting them sticks.
CREATE TABLE automation_settings (
    project_id          text PRIMARY KEY REFERENCES projects (id) ON DELETE CASCADE,
    defaults_created_at timestamptz NOT NULL DEFAULT now()
);

-- ---------------------------------------------------------------------------
-- Forwarding of incoming SMS
-- ---------------------------------------------------------------------------

CREATE TABLE forwarding_rules (
    id             text PRIMARY KEY,
    project_id     text NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    name           text NOT NULL,
    enabled        boolean NOT NULL DEFAULT true,
    -- Match: senders (exact, or a prefix ending in *) and a keyword the body
    -- contains. Empty matches every message.
    senders        text[] NOT NULL DEFAULT '{}',
    contains       text,
    -- Standard Webhooks signing secret (whsec_…) for webhook destinations.
    signing_secret text NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX forwarding_rules_project_idx ON forwarding_rules (project_id, created_at);

CREATE TABLE forwarding_destinations (
    id         text PRIMARY KEY,
    rule_id    text NOT NULL REFERENCES forwarding_rules (id) ON DELETE CASCADE,
    project_id text NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    type       text NOT NULL CHECK (type IN ('phone', 'telegram', 'webhook', 'email')),
    -- phone: E.164 number; telegram: chat ID; webhook: URL; email: address.
    target     text NOT NULL,
    -- webhook body: json (the message), slack ({"text"}) or discord ({"content"}).
    format     text CHECK (format IN ('json', 'slack', 'discord')),
    -- Telegram bot token, sealed under BRIDGE_SECRET_KEY and bound to the row ID.
    secret     bytea,
    position   integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX forwarding_destinations_rule_idx ON forwarding_destinations (rule_id, position);

-- One row per destination and message, updated on every attempt.
CREATE TABLE forwarding_deliveries (
    id                   text PRIMARY KEY,
    rule_id              text NOT NULL REFERENCES forwarding_rules (id) ON DELETE CASCADE,
    destination_id       text NOT NULL REFERENCES forwarding_destinations (id) ON DELETE CASCADE,
    project_id           text NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    message_id           text NOT NULL REFERENCES messages (id) ON DELETE CASCADE,
    status               text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'retrying', 'succeeded', 'failed', 'skipped')),
    attempts             integer NOT NULL DEFAULT 0,
    response_status      integer,
    error                text,
    forwarded_message_id text REFERENCES messages (id) ON DELETE SET NULL,
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now(),
    UNIQUE (destination_id, message_id)
);
CREATE INDEX forwarding_deliveries_rule_idx ON forwarding_deliveries (rule_id, created_at DESC);
CREATE INDEX forwarding_deliveries_created_idx ON forwarding_deliveries (created_at);

-- Finds Bridge's own forwards when they arrive back as incoming SMS.
CREATE INDEX messages_project_body_idx ON messages (project_id, body_sha256, created_at DESC) WHERE direction = 'outbound';

-- +goose Down
DROP INDEX messages_project_body_idx;
DROP TABLE forwarding_deliveries;
DROP TABLE forwarding_destinations;
DROP TABLE forwarding_rules;
DROP TABLE automation_settings;
DROP TABLE auto_reply_rules;
DROP TABLE opt_outs;
DROP TABLE scheduled_messages;
DROP TABLE broadcast_recipients;
DROP TABLE broadcasts;
