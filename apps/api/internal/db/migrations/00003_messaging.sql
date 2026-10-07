-- +goose Up

-- Per-device sending settings, chosen in the dashboard.
ALTER TABLE devices
    -- SIM slot to send from (1 or 2). NULL means the phone's default SMS SIM.
    ADD COLUMN preferred_sim_slot smallint CHECK (preferred_sim_slot IN (1, 2)),
    -- Android asks the user to approve sends beyond ~30 per 30 minutes per app;
    -- Bridge paces each device under this limit.
    ADD COLUMN send_limit_count          integer NOT NULL DEFAULT 30 CHECK (send_limit_count BETWEEN 1 AND 10000),
    ADD COLUMN send_limit_window_seconds integer NOT NULL DEFAULT 1800 CHECK (send_limit_window_seconds BETWEEN 60 AND 86400),
    -- SIMs reported by the phone: [{"slot":1,"carrier":"Jio","display_name":"SIM 1"}]. Never phone numbers.
    ADD COLUMN sims jsonb NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE messages
    ADD COLUMN requested_device_id text REFERENCES devices (id) ON DELETE SET NULL,
    ADD COLUMN sim_slot            smallint CHECK (sim_slot IN (1, 2)),
    ADD COLUMN assigned_at         timestamptz,
    ADD COLUMN idempotency_hash    bytea,
    ADD COLUMN body_sha256         bytea,
    ADD COLUMN body_length         integer,
    ADD COLUMN body_redacted_at    timestamptz,
    ADD COLUMN encoding            text CHECK (encoding IN ('gsm7', 'ucs2'));

CREATE INDEX messages_assigned_idx ON messages (assigned_at) WHERE status = 'queued' AND device_id IS NOT NULL;
CREATE INDEX messages_device_window_idx ON messages (device_id, assigned_at) WHERE assigned_at IS NOT NULL;
CREATE INDEX messages_redaction_idx ON messages (created_at) WHERE body_redacted_at IS NULL;
CREATE INDEX messages_project_recipient_idx ON messages (project_id, recipient, created_at DESC);

-- +goose Down
DROP INDEX messages_project_recipient_idx;
DROP INDEX messages_redaction_idx;
DROP INDEX messages_device_window_idx;
DROP INDEX messages_assigned_idx;
ALTER TABLE messages
    DROP COLUMN requested_device_id,
    DROP COLUMN sim_slot,
    DROP COLUMN assigned_at,
    DROP COLUMN idempotency_hash,
    DROP COLUMN body_sha256,
    DROP COLUMN body_length,
    DROP COLUMN body_redacted_at,
    DROP COLUMN encoding;
ALTER TABLE devices
    DROP COLUMN preferred_sim_slot,
    DROP COLUMN send_limit_count,
    DROP COLUMN send_limit_window_seconds,
    DROP COLUMN sims;
