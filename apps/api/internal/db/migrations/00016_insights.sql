-- +goose Up
-- Operator Insights (GET /v1/system/insights) counts messages and Verify
-- codes across every project by creation time. Without these indexes each
-- request would read the whole messages and otp_verifications tables.
CREATE INDEX messages_created_idx ON messages (created_at);
CREATE INDEX otp_verifications_created_idx ON otp_verifications (created_at);

-- +goose Down
DROP INDEX otp_verifications_created_idx;
DROP INDEX messages_created_idx;
