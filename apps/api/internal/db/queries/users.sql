-- name: CreateUser :one
INSERT INTO users (id, email, name, password_hash)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE lower(email) = lower(@email);

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: CountUsers :one
SELECT count(*) FROM users;

-- name: MarkUserEmailVerified :one
-- Only while the address is still the one the code was sent to.
UPDATE users SET email_verified_at = now(), updated_at = now()
WHERE id = @id AND lower(email) = lower(@email)
RETURNING *;

-- name: PhoneVerifiedByOtherUser :one
SELECT EXISTS (
    SELECT 1 FROM users WHERE phone = @phone AND phone_verified_at IS NOT NULL AND id <> @user_id
);

-- name: SetUserPhoneVerified :one
UPDATE users SET phone = @phone, phone_verified_at = now(), updated_at = now()
WHERE id = @id
RETURNING *;

-- name: ClearUserPhone :one
UPDATE users SET phone = NULL, phone_verified_at = NULL, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ChangeUserEmail :one
-- The new address was proven with a code, so it is verified.
UPDATE users SET email = @email, email_verified_at = now(), updated_at = now()
WHERE id = @id
RETURNING *;
