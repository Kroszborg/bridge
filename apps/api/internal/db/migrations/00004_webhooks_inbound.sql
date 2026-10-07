-- +goose Up

-- Incoming SMS are forwarded only from devices whose owner turned it on.
ALTER TABLE devices
    ADD COLUMN forward_inbound boolean NOT NULL DEFAULT false,
    -- Last presence sent as a device.online/device.offline webhook, so brief
    -- reconnects do not produce a burst of events.
    ADD COLUMN notified_presence text CHECK (notified_presence IN ('online', 'offline'));

-- Inbound messages store the sender; recipient is empty (Bridge never reads the phone's own number).
CREATE INDEX messages_project_direction_idx ON messages (project_id, direction, created_at DESC);

CREATE TABLE webhook_endpoints (
    id              text PRIMARY KEY,
    project_id      text NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    url             text NOT NULL,
    description     text NOT NULL DEFAULT '',
    -- Subscribed event types; empty means every event.
    events          text[] NOT NULL DEFAULT '{}',
    -- Standard Webhooks signing secret (whsec_…). Needed in plaintext to sign.
    secret          text NOT NULL,
    enabled         boolean NOT NULL DEFAULT true,
    disabled_reason text,
    failing_since   timestamptz,
    last_success_at timestamptz,
    last_failure_at timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX webhook_endpoints_project_idx ON webhook_endpoints (project_id);

CREATE TABLE webhook_events (
    id         text PRIMARY KEY,
    project_id text NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    type       text NOT NULL,
    payload    jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX webhook_events_project_idx ON webhook_events (project_id, created_at DESC);

CREATE TABLE webhook_deliveries (
    id              text PRIMARY KEY,
    endpoint_id     text NOT NULL REFERENCES webhook_endpoints (id) ON DELETE CASCADE,
    event_id        text NOT NULL REFERENCES webhook_events (id) ON DELETE CASCADE,
    attempt         integer NOT NULL,
    succeeded       boolean NOT NULL,
    response_status integer,
    response_body   text,
    error           text,
    duration_ms     integer NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX webhook_deliveries_endpoint_idx ON webhook_deliveries (endpoint_id, created_at DESC);

-- +goose Down
DROP TABLE webhook_deliveries;
DROP TABLE webhook_events;
DROP TABLE webhook_endpoints;
DROP INDEX messages_project_direction_idx;
ALTER TABLE devices DROP COLUMN forward_inbound, DROP COLUMN notified_presence;
