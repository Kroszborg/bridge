-- +goose Up
-- SMS providers (MSG91, Twilio, Vonage, Plivo) as a fallback for, or instead
-- of, paired phones; and integrations such as the Supabase Send SMS hook.

CREATE TABLE provider_accounts (
    id               text PRIMARY KEY,
    project_id       text NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    kind             text NOT NULL CHECK (kind IN ('msg91', 'twilio', 'vonage', 'plivo')),
    name             text NOT NULL,
    enabled          boolean NOT NULL DEFAULT true,
    -- Lower runs first when several providers are configured.
    priority         integer NOT NULL DEFAULT 0,
    -- AES-256-GCM under BRIDGE_SECRET_KEY, bound to the row ID (internal/secretbox).
    credentials      bytea NOT NULL,
    -- Non-secret settings: sender, templates. Shown in the dashboard.
    config           jsonb NOT NULL DEFAULT '{}',
    -- What the dashboard shows of the credentials, e.g. the Twilio account SID.
    credential_hint  text NOT NULL DEFAULT '',
    -- Delivery reports arrive at /v1/provider-callbacks/{id}/{callback_token}.
    callback_token   text NOT NULL,
    last_used_at     timestamptz,
    last_error       text,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    UNIQUE (project_id, kind)
);
CREATE INDEX provider_accounts_project_idx ON provider_accounts (project_id, priority, created_at);

-- How a project chooses between its phones and its providers.
CREATE TABLE project_routing (
    project_id             text PRIMARY KEY REFERENCES projects (id) ON DELETE CASCADE,
    mode                   text NOT NULL DEFAULT 'phones' CHECK (mode IN ('phones', 'phones_then_providers', 'providers')),
    -- With phones_then_providers: how long a message may wait for a phone.
    fallback_after_seconds integer NOT NULL DEFAULT 60 CHECK (fallback_after_seconds BETWEEN 0 AND 3600),
    updated_at             timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE messages
    ADD COLUMN provider_account_id text REFERENCES provider_accounts (id) ON DELETE SET NULL,
    -- Template variables for providers that send registered templates (MSG91 and India's DLT),
    -- such as the one-time code. Redacted together with the body.
    ADD COLUMN body_vars jsonb;
CREATE INDEX messages_provider_message_idx ON messages (provider_account_id, provider_message_id)
    WHERE provider_message_id IS NOT NULL;

CREATE TABLE integrations (
    id           text PRIMARY KEY,
    project_id   text NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    kind         text NOT NULL CHECK (kind IN ('supabase_send_sms')),
    environment  api_environment NOT NULL DEFAULT 'live',
    -- The signing secret the other service generated, sealed like provider credentials.
    secret       bytea,
    last_used_at timestamptz,
    last_error   text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX integrations_project_idx ON integrations (project_id, created_at);

-- +goose Down
DROP TABLE integrations;
DROP INDEX messages_provider_message_idx;
ALTER TABLE messages DROP COLUMN body_vars, DROP COLUMN provider_account_id;
DROP TABLE project_routing;
DROP TABLE provider_accounts;
