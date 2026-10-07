-- name: HitRateLimit :one
INSERT INTO rate_limit_counters AS c (key, window_start, count)
VALUES (@key, @window_start, 1)
ON CONFLICT (key) DO UPDATE SET
    count = CASE WHEN c.window_start >= EXCLUDED.window_start THEN c.count + 1 ELSE 1 END,
    window_start = GREATEST(c.window_start, EXCLUDED.window_start)
RETURNING count;

-- name: DeleteStaleRateLimits :execrows
DELETE FROM rate_limit_counters WHERE window_start < @before;

-- name: DeleteExpiredPairingTokens :execrows
DELETE FROM device_pairing_tokens WHERE expires_at < @before;
