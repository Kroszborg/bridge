-- +goose Up
-- Verify Pro: several Verify apps per project (own template, limits and
-- widget), fraud blocks, and resending a code through another route when the
-- first SMS was not sent in time.

CREATE TABLE verify_apps (
    id                     text PRIMARY KEY,
    project_id             text NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    -- 'default' is the project's default app, used when a request names none.
    slug                   text NOT NULL CHECK (slug ~ '^[a-z0-9][a-z0-9-]{0,39}$'),
    name                   text NOT NULL,
    -- Code settings. app_name replaces {app}; NULL uses the project name.
    app_name               text,
    template               text,
    code_length            smallint NOT NULL DEFAULT 6 CHECK (code_length BETWEEN 4 AND 10),
    ttl_seconds            integer NOT NULL DEFAULT 600 CHECK (ttl_seconds BETWEEN 60 AND 3600),
    max_attempts           smallint NOT NULL DEFAULT 5 CHECK (max_attempts BETWEEN 1 AND 10),
    web_otp_domain         text,
    -- Resend the code through another route when the SMS was not sent within
    -- this many seconds. 0 turns failover off.
    failover_after_seconds integer NOT NULL DEFAULT 30 CHECK (failover_after_seconds BETWEEN 0 AND 600),
    -- Fraud protection. Empty allowed_countries allows every country; a limit of
    -- 0 turns that limit off; a NULL country cap means none.
    allowed_countries      text[] NOT NULL DEFAULT '{}',
    ip_hourly_limit        integer NOT NULL DEFAULT 10 CHECK (ip_hourly_limit BETWEEN 0 AND 100000),
    range_hourly_limit     integer NOT NULL DEFAULT 20 CHECK (range_hourly_limit BETWEEN 0 AND 100000),
    country_hourly_limit   integer CHECK (country_hourly_limit BETWEEN 1 AND 1000000),
    -- Drop-in widget and hosted page. The publishable key is safe to expose.
    publishable_key        text NOT NULL UNIQUE,
    allowed_origins        text[] NOT NULL DEFAULT '{}',
    redirect_uris          text[] NOT NULL DEFAULT '{}',
    widget_environment     api_environment NOT NULL DEFAULT 'live',
    turnstile_site_key     text,
    -- Sealed under BRIDGE_SECRET_KEY (internal/secretbox), bound to "<id>:turnstile".
    turnstile_secret       bytea,
    -- Signs verification tokens (HS256). Sealed, bound to the row ID. NULL until
    -- first needed when the server had no BRIDGE_SECRET_KEY at creation.
    secret                 bytea,
    created_at             timestamptz NOT NULL DEFAULT now(),
    updated_at             timestamptz NOT NULL DEFAULT now(),
    UNIQUE (project_id, slug)
);
CREATE INDEX verify_apps_project_idx ON verify_apps (project_id, created_at);

-- Every project that has settings or verifications gets its default app now;
-- others get one the first time they send a code.
-- +goose StatementBegin
DO $$
DECLARE
    alphabet constant text := '0123456789abcdefghjkmnpqrstvwxyz';
    p record;
    new_id text;
BEGIN
    FOR p IN
        SELECT pr.id AS project_id, s.app_name, s.template, s.code_length, s.ttl_seconds, s.max_attempts, s.web_otp_domain
        FROM projects pr
        LEFT JOIN otp_settings s ON s.project_id = pr.id
        WHERE s.project_id IS NOT NULL OR EXISTS (SELECT 1 FROM otp_verifications o WHERE o.project_id = pr.id)
    LOOP
        new_id := 'vap_' || substr(alphabet, 1 + floor(random() * 8)::int, 1);
        FOR i IN 1..25 LOOP
            new_id := new_id || substr(alphabet, 1 + floor(random() * 32)::int, 1);
        END LOOP;
        INSERT INTO verify_apps (id, project_id, slug, name, app_name, template, code_length, ttl_seconds,
                                 max_attempts, web_otp_domain, publishable_key)
        VALUES (new_id, p.project_id, 'default', 'Default', p.app_name, p.template, COALESCE(p.code_length, 6),
                COALESCE(p.ttl_seconds, 600), COALESCE(p.max_attempts, 5), p.web_otp_domain,
                'bpk_' || replace(gen_random_uuid()::text, '-', ''));
    END LOOP;
END
$$;
-- +goose StatementEnd

DROP TABLE otp_settings;

ALTER TABLE otp_verifications
    ADD COLUMN app_id              text REFERENCES verify_apps (id) ON DELETE SET NULL,
    -- The second SMS sent when the first was not sent in time.
    ADD COLUMN failover_message_id text REFERENCES messages (id) ON DELETE SET NULL;

UPDATE otp_verifications o SET app_id = a.id
FROM verify_apps a
WHERE a.project_id = o.project_id AND a.slug = 'default';

CREATE INDEX otp_verifications_app_idx ON otp_verifications (app_id, created_at DESC);

-- Send attempts refused by fraud protection. Kept 30 days.
CREATE TABLE otp_blocks (
    id          text PRIMARY KEY,
    project_id  text NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    app_id      text NOT NULL REFERENCES verify_apps (id) ON DELETE CASCADE,
    environment api_environment NOT NULL,
    recipient   text NOT NULL,
    client_ip   inet,
    country     text,
    reason      text NOT NULL CHECK (reason IN ('country_not_allowed', 'ip_limit', 'range_burst', 'country_limit', 'captcha_failed')),
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX otp_blocks_app_idx ON otp_blocks (app_id, created_at DESC);
CREATE INDEX otp_blocks_project_idx ON otp_blocks (project_id, environment, created_at);
CREATE INDEX otp_blocks_created_idx ON otp_blocks (created_at);

-- +goose Down
DROP TABLE otp_blocks;
DROP INDEX otp_verifications_app_idx;
ALTER TABLE otp_verifications DROP COLUMN failover_message_id, DROP COLUMN app_id;

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
INSERT INTO otp_settings (project_id, app_name, template, code_length, ttl_seconds, max_attempts, web_otp_domain, updated_at)
SELECT project_id, app_name, template, code_length, ttl_seconds, max_attempts, web_otp_domain, updated_at
FROM verify_apps WHERE slug = 'default';

DROP TABLE verify_apps;
