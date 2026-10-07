-- ---- Opt-outs ---------------------------------------------------------------

-- name: IsOptedOut :one
SELECT EXISTS (SELECT 1 FROM opt_outs WHERE project_id = @project_id AND number = @number);

-- name: OptedOutAmong :many
SELECT number FROM opt_outs WHERE project_id = @project_id AND number = ANY(@numbers::text[]);

-- name: InsertOptOut :one
INSERT INTO opt_outs (id, project_id, number, source, keyword)
VALUES (@id, @project_id, @number, @source, sqlc.narg(keyword))
ON CONFLICT (project_id, number) DO NOTHING
RETURNING *;

-- name: GetOptOut :one
SELECT * FROM opt_outs WHERE project_id = @project_id AND number = @number;

-- name: DeleteOptOut :one
DELETE FROM opt_outs WHERE project_id = @project_id AND number = @number
RETURNING *;

-- name: ListOptOuts :many
SELECT * FROM opt_outs
WHERE project_id = @project_id
  AND (sqlc.narg(source)::text IS NULL OR source = sqlc.narg(source))
  AND (sqlc.narg(before_created)::timestamptz IS NULL
       OR (created_at, id) < (sqlc.narg(before_created)::timestamptz, sqlc.narg(before_id)::text))
ORDER BY created_at DESC, id DESC
LIMIT @row_limit;

-- name: GetOptOutByID :one
SELECT * FROM opt_outs WHERE id = @id AND project_id = @project_id;

-- ---- Auto-replies -----------------------------------------------------------

-- name: ClaimAutomationDefaults :execrows
-- Returns 1 the first time for a project, when its default rules should be created.
INSERT INTO automation_settings (project_id) VALUES ($1) ON CONFLICT (project_id) DO NOTHING;

-- name: InsertAutoReplyRule :one
INSERT INTO auto_reply_rules (id, project_id, name, match_type, keywords, reply, action, enabled, priority)
VALUES (@id, @project_id, @name, @match_type, @keywords, sqlc.narg(reply), @action, @enabled, @priority)
RETURNING *;

-- name: ListAutoReplyRules :many
SELECT * FROM auto_reply_rules WHERE project_id = $1 ORDER BY priority, created_at, id;

-- name: GetAutoReplyRule :one
SELECT * FROM auto_reply_rules WHERE id = @id AND project_id = @project_id;

-- name: UpdateAutoReplyRule :one
UPDATE auto_reply_rules SET
    name = @name, match_type = @match_type, keywords = @keywords, reply = sqlc.narg(reply),
    action = @action, enabled = @enabled, priority = @priority, updated_at = now()
WHERE id = @id AND project_id = @project_id
RETURNING *;

-- name: DeleteAutoReplyRule :execrows
DELETE FROM auto_reply_rules WHERE id = @id AND project_id = @project_id;

-- name: HasMessageEvent :one
SELECT EXISTS (SELECT 1 FROM message_events WHERE message_id = @message_id AND type = @type);

-- name: RecentAutoReply :one
-- Whether a rule already replied to a number since a time (loop protection).
SELECT EXISTS (
    SELECT 1 FROM messages
    WHERE project_id = @project_id AND recipient = @recipient AND direction = 'outbound'
      AND created_at > @since AND metadata ->> 'auto_reply_rule_id' = @rule_id::text
);

-- name: RecentForwardWithBody :one
-- Whether Bridge forwarded a message with this exact text recently: an incoming
-- SMS that matches is one of its own forwards arriving back.
SELECT EXISTS (
    SELECT 1 FROM messages
    WHERE project_id = @project_id AND direction = 'outbound' AND body_sha256 = @body_sha256
      AND created_at > @since AND metadata ? 'forwarded_from'
);

-- ---- Forwarding -------------------------------------------------------------

-- name: InsertForwardingRule :one
INSERT INTO forwarding_rules (id, project_id, name, enabled, senders, contains, signing_secret)
VALUES (@id, @project_id, @name, @enabled, @senders, sqlc.narg(contains), @signing_secret)
RETURNING *;

-- name: ListForwardingRules :many
SELECT * FROM forwarding_rules WHERE project_id = $1 ORDER BY created_at, id;

-- name: GetForwardingRule :one
SELECT * FROM forwarding_rules WHERE id = @id AND project_id = @project_id;

-- name: GetForwardingRuleByID :one
SELECT * FROM forwarding_rules WHERE id = $1;

-- name: UpdateForwardingRule :one
UPDATE forwarding_rules SET
    name = @name, enabled = @enabled, senders = @senders, contains = sqlc.narg(contains), updated_at = now()
WHERE id = @id AND project_id = @project_id
RETURNING *;

-- name: DeleteForwardingRule :execrows
DELETE FROM forwarding_rules WHERE id = @id AND project_id = @project_id;

-- name: InsertForwardingDestination :one
INSERT INTO forwarding_destinations (id, rule_id, project_id, type, target, format, secret, position)
VALUES (@id, @rule_id, @project_id, @type, @target, sqlc.narg(format), sqlc.narg(secret), @position)
RETURNING *;

-- name: UpdateForwardingDestination :one
UPDATE forwarding_destinations SET
    type = @type, target = @target, format = sqlc.narg(format), secret = sqlc.narg(secret), position = @position
WHERE id = @id AND rule_id = @rule_id
RETURNING *;

-- name: DeleteForwardingDestination :exec
DELETE FROM forwarding_destinations WHERE id = @id AND rule_id = @rule_id;

-- name: ListForwardingDestinations :many
SELECT * FROM forwarding_destinations WHERE rule_id = ANY(@rule_ids::text[]) ORDER BY rule_id, position, id;

-- name: GetForwardingDestination :one
SELECT * FROM forwarding_destinations WHERE id = $1;

-- name: InsertForwardingDelivery :one
INSERT INTO forwarding_deliveries (id, rule_id, destination_id, project_id, message_id)
VALUES (@id, @rule_id, @destination_id, @project_id, @message_id)
ON CONFLICT (destination_id, message_id) DO NOTHING
RETURNING id;

-- name: GetForwardingDelivery :one
SELECT * FROM forwarding_deliveries WHERE id = $1;

-- name: RecordForwardingAttempt :exec
UPDATE forwarding_deliveries SET
    status = @status, attempts = @attempts, response_status = sqlc.narg(response_status), error = sqlc.narg(error),
    forwarded_message_id = COALESCE(sqlc.narg(forwarded_message_id)::text, forwarded_message_id), updated_at = now()
WHERE id = @id;

-- name: ListForwardingDeliveries :many
SELECT d.*, fd.type AS destination_type FROM forwarding_deliveries d
JOIN forwarding_destinations fd ON fd.id = d.destination_id
WHERE d.rule_id = @rule_id
ORDER BY d.created_at DESC, d.id DESC
LIMIT @row_limit;

-- name: DeleteOldForwardingDeliveries :execrows
DELETE FROM forwarding_deliveries WHERE created_at < @before;
