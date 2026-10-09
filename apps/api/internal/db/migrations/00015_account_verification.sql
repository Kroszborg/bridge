-- +goose Up
-- Account verification. An emailed code proves a user's address; a phone
-- number is verified with a code sent through the operator's own Bridge
-- Verify (BRIDGE_ACCOUNT_VERIFY_API_KEY).
ALTER TABLE users
    ADD COLUMN phone             text,
    ADD COLUMN phone_verified_at timestamptz;
-- A verified number belongs to one account.
CREATE UNIQUE INDEX users_phone_verified_key ON users (phone) WHERE phone_verified_at IS NOT NULL;

-- Six-digit codes sent by email. Only a hash is stored; a code expires after
-- 15 minutes or 5 wrong attempts, and a new code replaces the pending one of
-- the same purpose. verify proves the account's address; change proves a new
-- address the user wants to switch to. email is where the code was sent.
CREATE TABLE email_verifications (
    id          text PRIMARY KEY,
    user_id     text NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    purpose     text NOT NULL DEFAULT 'verify' CHECK (purpose IN ('verify', 'change')),
    email       text NOT NULL,
    code_hash   bytea NOT NULL,
    attempts    integer NOT NULL DEFAULT 0,
    expires_at  timestamptz NOT NULL,
    consumed_at timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX email_verifications_user_idx ON email_verifications (user_id);

-- +goose Down
DROP TABLE email_verifications;
DROP INDEX users_phone_verified_key;
ALTER TABLE users
    DROP COLUMN phone_verified_at,
    DROP COLUMN phone;
