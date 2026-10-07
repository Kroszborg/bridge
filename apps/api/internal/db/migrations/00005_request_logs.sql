-- +goose Up

-- One row per developer API request (API-key authenticated). Metadata only:
-- no headers, query strings or bodies, which may hold phone numbers and text.
CREATE TABLE api_request_logs (
    id           text PRIMARY KEY,
    request_id   text NOT NULL,
    project_id   text NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    api_key_id   text REFERENCES api_keys (id) ON DELETE SET NULL,
    environment  text NOT NULL CHECK (environment IN ('live', 'test')),
    method       text NOT NULL,
    path         text NOT NULL,
    status       integer NOT NULL,
    error_code   text,
    duration_ms  integer NOT NULL,
    ip           inet,
    user_agent   text,
    -- The resource the request created or read, e.g. a message ID.
    resource_id  text,
    created_at   timestamptz NOT NULL
);
CREATE INDEX api_request_logs_project_idx ON api_request_logs (project_id, created_at DESC, id DESC);
CREATE INDEX api_request_logs_created_idx ON api_request_logs (created_at);

-- +goose Down
DROP TABLE api_request_logs;
