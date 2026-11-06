-- name: InsertEvent :exec
INSERT INTO audit.events (tenant_id, actor_type, actor_id, action, resource_type, resource_id, outcome, ip_hash, user_agent, correlation_id, before, after)
VALUES (sqlc.narg(tenant_id), @actor_type, sqlc.narg(actor_id), @action, @resource_type, sqlc.narg(resource_id), @outcome, sqlc.narg(ip_hash), sqlc.narg(user_agent), sqlc.narg(correlation_id), sqlc.narg(before), sqlc.narg(after));

-- name: InsertSecurityEvent :exec
INSERT INTO audit.security_events (tenant_id, event_type, severity, actor_ref, ip_hash, detail, correlation_id)
VALUES (sqlc.narg(tenant_id), @event_type, @severity, sqlc.narg(actor_ref), sqlc.narg(ip_hash), @detail, sqlc.narg(correlation_id));

-- name: ListEvents :many
SELECT id, tenant_id, actor_type, actor_id, action, resource_type, resource_id, outcome, occurred_at
FROM audit.events
WHERE (sqlc.narg(before_id)::uuid IS NULL OR id < sqlc.narg(before_id)::uuid)
ORDER BY id DESC
LIMIT @row_limit;
