-- +goose Up
-- A daily cap per phone, on top of the short burst window. Mobile operators
-- limit how many SMS a SIM sends a day (in India about 100 on most unlimited
-- packs) and may block SIMs that go far beyond it, so Bridge never assigns a
-- phone more than this in any rolling 24 hours.
ALTER TABLE devices
    ADD COLUMN daily_send_limit integer NOT NULL DEFAULT 100 CHECK (daily_send_limit BETWEEN 1 AND 10000);

-- Dispatch counts each phone's sends in the last 24 hours with
-- messages_device_window_idx (device_id, assigned_at).

-- Hosted plans: cheap and small. Free is enough to try Bridge on one phone;
-- teams and more projects start at Pro. Allowances stay below what the plan's
-- phones can send under the daily cap (phones x 100 x 30).
UPDATE plans SET name = 'Free', price_cents = 0,
    max_phones = 1, max_live_messages = 300, max_projects = 1, max_members = 1, updated_at = now()
WHERE id = 'free';
UPDATE plans SET name = 'Pro', price_cents = 500,
    max_phones = 3, max_live_messages = 5000, max_projects = 3, max_members = 3, updated_at = now()
WHERE id = 'pro';
UPDATE plans SET name = 'Team', price_cents = 1500,
    max_phones = 10, max_live_messages = 25000, max_projects = NULL, max_members = 10, updated_at = now()
WHERE id = 'business';

-- +goose Down
UPDATE plans SET name = 'Business', price_cents = 2900,
    max_phones = 25, max_live_messages = 100000, max_projects = NULL, max_members = 20, updated_at = now()
WHERE id = 'business';
UPDATE plans SET price_cents = 900, max_phones = 5, max_live_messages = 10000, max_projects = 5, max_members = 5, updated_at = now()
WHERE id = 'pro';
UPDATE plans SET max_phones = 1, max_live_messages = 300, max_projects = 1, max_members = 2, updated_at = now()
WHERE id = 'free';
ALTER TABLE devices DROP COLUMN daily_send_limit;
