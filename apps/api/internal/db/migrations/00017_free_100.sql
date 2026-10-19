-- +goose Up
-- Free covers trying Bridge out: 100 live SMS a month, about a day of one
-- phone's daily cap. Test messages stay unlimited on every plan.
UPDATE plans SET max_live_messages = 100, updated_at = now() WHERE id = 'free';

-- +goose Down
UPDATE plans SET max_live_messages = 300, updated_at = now() WHERE id = 'free';
