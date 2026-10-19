-- +goose Up
-- The Dodo mode a subscription was bought in. Test and live subscriptions,
-- customers and products are separate, so only subscriptions of the mode
-- Bridge runs in (BRIDGE_DODO_ENVIRONMENT) grant a plan or count as revenue.
-- Rows from before this column are sorted out by the API at startup, which
-- asks Dodo whether each one exists in the current mode.
ALTER TABLE subscriptions
    ADD COLUMN environment text CHECK (environment IN ('test_mode', 'live_mode'));

-- +goose Down
ALTER TABLE subscriptions DROP COLUMN environment;
