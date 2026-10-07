-- +goose Up

-- Single-use invite links. The token is shown once; only its hash is stored.
CREATE TABLE organization_invites (
    id              text PRIMARY KEY,
    organization_id text NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    token_hash      bytea NOT NULL UNIQUE,
    role            member_role NOT NULL,
    -- Who the link is meant for; informational, the link works for any account.
    email           text NOT NULL DEFAULT '',
    invited_by      text REFERENCES users (id) ON DELETE SET NULL,
    expires_at      timestamptz NOT NULL,
    accepted_at     timestamptz,
    accepted_by     text REFERENCES users (id) ON DELETE SET NULL,
    revoked_at      timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX organization_invites_org_idx ON organization_invites (organization_id, created_at DESC);

CREATE INDEX audit_logs_org_action_idx ON audit_logs (organization_id, action, created_at DESC);

-- +goose Down
DROP INDEX audit_logs_org_action_idx;
DROP TABLE organization_invites;
