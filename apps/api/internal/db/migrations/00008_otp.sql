-- +goose Up
-- One-time passwords: Bridge generates a code, sends it as an SMS and checks it.

-- Messages carry the code in their body, so they are marked and shown masked.
ALTER TABLE messages
    ADD COLUMN purpose      text NOT NULL DEFAULT 'message' CHECK (purpose IN ('message', 'otp')),
    ADD COLUMN display_body text;

COMMENT ON COLUMN messages.display_body IS
    'Shown instead of body by the API and webhooks. Set for OTP messages, whose body contains the code.';

CREATE TYPE otp_status AS ENUM ('pending', 'verified', 'expired', 'failed', 'canceled');

CREATE TABLE otp_verifications (
    id            text PRIMARY KEY,
    project_id    text NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    environment   api_environment NOT NULL,
    api_key_id    text REFERENCES api_keys (id) ON DELETE SET NULL,
    recipient     text NOT NULL,
    status        otp_status NOT NULL DEFAULT 'pending',
    -- HMAC-SHA256 of the code under the installation's "otp" key in server_keys.
    -- Cleared once the verification is finished, so finished codes cannot be recovered.
    code_hash     bytea,
    -- Only for test keys, where the code is returned to the caller anyway.
    test_code     text CHECK (test_code IS NULL OR environment = 'test'),
    code_length   smallint NOT NULL,
    attempts      smallint NOT NULL DEFAULT 0,
    max_attempts  smallint NOT NULL,
    message_id    text REFERENCES messages (id) ON DELETE SET NULL,
    metadata      jsonb NOT NULL DEFAULT '{}',
    expires_at    timestamptz NOT NULL,
    verified_at   timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX otp_verifications_project_idx ON otp_verifications (project_id, created_at DESC);
CREATE INDEX otp_verifications_recipient_idx ON otp_verifications (project_id, environment, recipient, created_at DESC);
CREATE INDEX otp_verifications_pending_idx ON otp_verifications (expires_at) WHERE status = 'pending';

CREATE TABLE otp_settings (
    project_id     text PRIMARY KEY REFERENCES projects (id) ON DELETE CASCADE,
    app_name       text,
    template       text,
    code_length    smallint NOT NULL DEFAULT 6 CHECK (code_length BETWEEN 4 AND 10),
    ttl_seconds    integer NOT NULL DEFAULT 600 CHECK (ttl_seconds BETWEEN 60 AND 3600),
    max_attempts   smallint NOT NULL DEFAULT 5 CHECK (max_attempts BETWEEN 1 AND 10),
    web_otp_domain text,
    updated_at     timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE otp_settings;
DROP TABLE otp_verifications;
DROP TYPE otp_status;
ALTER TABLE messages DROP COLUMN display_body, DROP COLUMN purpose;
