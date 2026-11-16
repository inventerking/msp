-- name: ListProviders :many
SELECT id, key, kind, name, tenant_connectable, status FROM integration.providers ORDER BY kind, key;

-- name: GetProviderByKey :one
SELECT id, key, kind, status FROM integration.providers WHERE key = @key;

-- name: SetProviderStatus :execrows
UPDATE integration.providers SET status = @status WHERE id = @id;

-- name: UpsertCredential :exec
INSERT INTO integration.platform_credentials (provider_id, label, secret_ciphertext, key_id, rotated_at)
VALUES (@provider_id, @label, @secret_ciphertext, @key_id, now())
ON CONFLICT (provider_id, label) DO UPDATE SET secret_ciphertext = EXCLUDED.secret_ciphertext, key_id = EXCLUDED.key_id, rotated_at = now();

-- name: GetCredential :one
SELECT secret_ciphertext, key_id FROM integration.platform_credentials WHERE provider_id = @provider_id AND label = @label;

-- name: HasCredential :one
SELECT count(*)::int FROM integration.platform_credentials WHERE provider_id = @provider_id;

-- name: InsertCallLog :exec
INSERT INTO integration.provider_call_logs (tenant_id, provider_id, operation, outcome, status_code, latency_ms, correlation_id)
VALUES (sqlc.narg(tenant_id), @provider_id, @operation, @outcome, sqlc.narg(status_code), @latency_ms, sqlc.narg(correlation_id));

-- name: ProviderHealth :many
SELECT provider_id, outcome, count(*)::bigint AS calls FROM integration.provider_call_logs WHERE created_at > @since GROUP BY provider_id, outcome;

-- name: CreateWebhookEndpoint :one
INSERT INTO integration.webhook_endpoints (tenant_id, provider_id, path_token, secret_ciphertext, key_id, status)
VALUES (sqlc.narg(tenant_id), @provider_id, @path_token, @secret_ciphertext, @key_id, 'active') RETURNING id;

-- name: GetWebhookEndpoint :one
SELECT e.id, e.tenant_id, e.secret_ciphertext, p.key AS provider_key, p.kind AS provider_kind
FROM integration.webhook_endpoints e JOIN integration.providers p ON p.id = e.provider_id
WHERE e.path_token = @path_token AND e.status = 'active';

-- name: InsertWebhookDelivery :one
INSERT INTO integration.webhook_deliveries (tenant_id, endpoint_id, provider_event_id, signature_verified, payload_hash, payload, status, attempts, received_at)
VALUES (sqlc.narg(tenant_id), @endpoint_id, sqlc.narg(provider_event_id), true, @payload_hash, @payload, 'received', 0, now())
ON CONFLICT (endpoint_id, provider_event_id) DO NOTHING RETURNING id;

-- name: GetWebhookDelivery :one
SELECT d.id, d.tenant_id, d.payload, p.key AS provider_key, d.status
FROM integration.webhook_deliveries d
JOIN integration.webhook_endpoints e ON e.id = d.endpoint_id
JOIN integration.providers p ON p.id = e.provider_id
WHERE d.id = @id;

-- name: SetWebhookDeliveryStatus :exec
UPDATE integration.webhook_deliveries SET status = @status, attempts = attempts + 1, last_error = sqlc.narg(last_error) WHERE id = @id;

-- name: GetActiveRoute :one
SELECT r.id, r.model, r.max_input_tokens, r.max_output_tokens, r.fallback_route_id, p.id AS provider_id, p.key AS provider_key, p.status AS provider_status
FROM integration.ai_model_routes r JOIN integration.providers p ON p.id = r.provider_id
WHERE r.task_key = @task_key AND r.status = 'active' ORDER BY r.created_at LIMIT 1;

-- name: GetRouteByID :one
SELECT r.id, r.model, r.max_input_tokens, r.max_output_tokens, r.fallback_route_id, p.id AS provider_id, p.key AS provider_key, p.status AS provider_status
FROM integration.ai_model_routes r JOIN integration.providers p ON p.id = r.provider_id WHERE r.id = @id AND r.status = 'active';

-- name: GetActivePrompt :one
SELECT t.id AS template_id, t.action_class, t.output_schema, v.id AS version_id, v.version, v.system_text, v.user_text
FROM integration.prompt_templates t JOIN integration.prompt_versions v ON v.template_id = t.id
WHERE t.task_key = @task_key AND t.status = 'active' AND v.status = 'active' ORDER BY v.version DESC LIMIT 1;

-- name: FindAIRequestByKey :one
SELECT id, outcome FROM integration.ai_requests WHERE tenant_id = @tenant_id AND idempotency_key = @idempotency_key;

-- name: InsertAIRequest :one
INSERT INTO integration.ai_requests (tenant_id, module_key, task_key, prompt_version_id, provider_id, model, input_tokens, output_tokens, cost_estimate_minor, credits_charged, outcome, validation_passed, input_hash, latency_ms, correlation_id, idempotency_key)
VALUES (@tenant_id, @module_key, @task_key, sqlc.narg(prompt_version_id), sqlc.narg(provider_id), sqlc.narg(model), sqlc.narg(input_tokens), sqlc.narg(output_tokens), sqlc.narg(cost_estimate_minor), sqlc.narg(credits_charged), @outcome, sqlc.narg(validation_passed), sqlc.narg(input_hash), sqlc.narg(latency_ms), sqlc.narg(correlation_id), sqlc.narg(idempotency_key))
RETURNING id;

-- name: AIUsageByModule :many
SELECT module_key, count(*)::bigint AS requests, COALESCE(SUM(credits_charged), 0)::bigint AS credits, COALESCE(SUM(input_tokens + output_tokens), 0)::bigint AS tokens
FROM integration.ai_requests WHERE tenant_id = @tenant_id AND created_at > @since GROUP BY module_key ORDER BY module_key;

-- name: ListRoutes :many
SELECT r.id, r.task_key, r.model, r.max_input_tokens, r.max_output_tokens, r.status, p.key AS provider_key
FROM integration.ai_model_routes r JOIN integration.providers p ON p.id = r.provider_id ORDER BY r.task_key;

-- name: UpdateRoute :execrows
UPDATE integration.ai_model_routes SET model = @model, max_input_tokens = @max_input_tokens, max_output_tokens = @max_output_tokens, status = @status WHERE id = @id;

-- name: ListPrompts :many
SELECT t.id, t.key, t.task_key, t.action_class, t.status,
       COALESCE((SELECT v.version FROM integration.prompt_versions v WHERE v.template_id = t.id AND v.status = 'active' ORDER BY v.version DESC LIMIT 1), 0)::int AS active_version,
       (SELECT count(*) FROM integration.prompt_versions v WHERE v.template_id = t.id)::int AS versions
FROM integration.prompt_templates t ORDER BY t.key;

-- name: NextPromptVersion :one
SELECT COALESCE(MAX(version), 0)::int + 1 FROM integration.prompt_versions WHERE template_id = @template_id;

-- name: InsertPromptVersion :one
INSERT INTO integration.prompt_versions (template_id, version, system_text, user_text, status) VALUES (@template_id, @version, @system_text, @user_text, 'draft') RETURNING id;

-- name: ActivatePromptVersion :execrows
UPDATE integration.prompt_versions AS target
SET status = CASE WHEN target.id = sqlc.arg(version_id)::uuid THEN 'active' WHEN target.status = 'active' THEN 'retired' ELSE target.status END
WHERE target.template_id = (SELECT v.template_id FROM integration.prompt_versions v WHERE v.id = sqlc.arg(version_id)::uuid);

-- name: InsertConnection :one
INSERT INTO integration.connections (tenant_id, provider_id, display_name, account_ref, status, created_by)
VALUES (@tenant_id, @provider_id, @display_name, sqlc.narg(account_ref), 'connected', sqlc.narg(created_by)) RETURNING id;
