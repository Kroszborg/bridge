-- +goose Up

-- Each API and worker process checks in here, for the status page.
CREATE UNLOGGED TABLE instance_heartbeats (
    instance_id text PRIMARY KEY,
    kind        text NOT NULL CHECK (kind IN ('api', 'worker')),
    version     text NOT NULL,
    hostname    text NOT NULL,
    started_at  timestamptz NOT NULL,
    seen_at     timestamptz NOT NULL
);

-- One sample per component per minute, kept 90 days for uptime history.
CREATE TABLE status_samples (
    component  text NOT NULL,
    sampled_at timestamptz NOT NULL,
    status     text NOT NULL CHECK (status IN ('operational', 'degraded', 'outage')),
    value      double precision,
    detail     text NOT NULL DEFAULT '',
    PRIMARY KEY (component, sampled_at)
);
CREATE INDEX status_samples_time_idx ON status_samples (sampled_at);

-- +goose Down
DROP TABLE status_samples;
DROP TABLE instance_heartbeats;
