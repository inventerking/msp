-- name: InsertNotification :one
INSERT INTO notification.notifications (tenant_id, recipient_type, recipient_id, notification_key, title, body, link, idempotency_key)
VALUES (@tenant_id, 'user', @recipient_id, @notification_key, @title, sqlc.narg(body), sqlc.narg(link), @idempotency_key)
ON CONFLICT (recipient_id, idempotency_key) DO NOTHING RETURNING id;

-- name: GetNotification :one
SELECT id, tenant_id, recipient_id, notification_key, title, body FROM notification.notifications WHERE id = @id;

-- name: ListNotifications :many
SELECT id, notification_key, title, body, link, read_at, created_at FROM notification.notifications
WHERE tenant_id = @tenant_id AND recipient_id = @recipient_id
ORDER BY created_at DESC LIMIT 50;

-- name: CountUnread :one
SELECT count(*)::int FROM notification.notifications WHERE tenant_id = @tenant_id AND recipient_id = @recipient_id AND read_at IS NULL;

-- name: MarkRead :execrows
UPDATE notification.notifications SET read_at = now() WHERE tenant_id = @tenant_id AND recipient_id = @recipient_id AND id = @id AND read_at IS NULL;

-- name: MarkAllRead :exec
UPDATE notification.notifications SET read_at = now() WHERE tenant_id = @tenant_id AND recipient_id = @recipient_id AND read_at IS NULL;

-- name: GetPreference :one
SELECT enabled FROM notification.preferences WHERE tenant_id = @tenant_id AND user_id = @user_id AND notification_key = @notification_key AND channel = @channel;

-- name: ListPreferences :many
SELECT notification_key, channel, enabled FROM notification.preferences WHERE tenant_id = @tenant_id AND user_id = @user_id ORDER BY notification_key, channel;

-- name: UpsertPreference :exec
INSERT INTO notification.preferences (tenant_id, user_id, notification_key, channel, enabled) VALUES (@tenant_id, @user_id, @notification_key, @channel, @enabled)
ON CONFLICT (tenant_id, user_id, notification_key, channel) DO UPDATE SET enabled = EXCLUDED.enabled;

-- name: GetTemplate :one
SELECT subject, body FROM notification.templates WHERE key = @key AND channel = @channel AND locale = 'en' ORDER BY version DESC LIMIT 1;

-- name: InsertAttempt :exec
INSERT INTO notification.delivery_attempts (notification_id, channel, provider_key, status, attempt, error_code) VALUES (@notification_id, @channel, sqlc.narg(provider_key), @status, @attempt, sqlc.narg(error_code));
