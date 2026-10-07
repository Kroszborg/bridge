-- +goose Up

-- Push wake-up registration. provider is 'unifiedpush' (WebPush to a distributor
-- endpoint) or 'fcm' (Firebase registration token in push_endpoint).
ALTER TABLE devices DROP COLUMN push_token;
ALTER TABLE devices
    ADD COLUMN push_provider      text CHECK (push_provider IN ('unifiedpush', 'fcm')),
    ADD COLUMN push_endpoint      text,
    ADD COLUMN push_p256dh        text,
    ADD COLUMN push_auth          text,
    ADD COLUMN push_updated_at    timestamptz,
    -- The live WebSocket session holding this device, so a stale instance
    -- cannot mark a reconnected device offline.
    ADD COLUMN connection_id      text,
    ADD COLUMN connected_at       timestamptz,
    -- Seconds until the device's next heartbeat, as declared by the device.
    ADD COLUMN heartbeat_interval integer NOT NULL DEFAULT 60,
    ADD COLUMN app_flavor         text;

CREATE INDEX devices_online_idx ON devices (last_seen_at) WHERE status = 'online';

-- Server-wide key material generated on first use (e.g. the VAPID key for WebPush).
CREATE TABLE server_keys (
    name        text PRIMARY KEY,
    private_key bytea NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE server_keys;
DROP INDEX devices_online_idx;
ALTER TABLE devices
    DROP COLUMN push_provider,
    DROP COLUMN push_endpoint,
    DROP COLUMN push_p256dh,
    DROP COLUMN push_auth,
    DROP COLUMN push_updated_at,
    DROP COLUMN connection_id,
    DROP COLUMN connected_at,
    DROP COLUMN heartbeat_interval,
    DROP COLUMN app_flavor;
ALTER TABLE devices ADD COLUMN push_token text;
