-- name: CreateObject :one
INSERT INTO files.objects (id, tenant_id, bucket, object_key, content_type, original_name, status, classification, owner_module, size_bytes, uploaded_by)
VALUES (@id, @tenant_id, @bucket, @object_key, @content_type, sqlc.narg(original_name), 'pending_upload', 'tenant_private', @owner_module, @size_bytes, sqlc.narg(uploaded_by))
RETURNING id;

-- name: GetObject :one
SELECT id, tenant_id, bucket, object_key, sha256, size_bytes, content_type, original_name, status, classification, owner_module, created_at
FROM files.objects WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: MarkUploaded :execrows
UPDATE files.objects SET status = 'scanning', sha256 = @sha256, size_bytes = @size_bytes, content_type = @content_type
WHERE tenant_id = @tenant_id AND id = @id AND status = 'pending_upload';

-- name: SetObjectStatus :execrows
UPDATE files.objects SET status = @status WHERE tenant_id = @tenant_id AND id = @id AND status = 'scanning';

-- name: AddReference :exec
INSERT INTO files.object_references (object_id, tenant_id, owner_module, owner_type, owner_id) VALUES (@object_id, @tenant_id, @owner_module, @owner_type, @owner_id)
ON CONFLICT (object_id, owner_module, owner_type, owner_id) DO NOTHING;

-- name: GetPolicyID :one
SELECT id FROM files.access_policies WHERE key = @key;

-- name: CreateGrant :exec
INSERT INTO files.access_grants (tenant_id, object_id, policy_id, token_hash, expires_at, max_downloads, download_count)
VALUES (@tenant_id, @object_id, @policy_id, @token_hash, @expires_at, sqlc.narg(max_downloads), 0);

-- name: UseGrant :one
-- Atomic, so concurrent downloads cannot exceed the limit.
UPDATE files.access_grants SET download_count = download_count + 1
WHERE tenant_id = @tenant_id AND token_hash = @token_hash AND expires_at > now() AND (max_downloads IS NULL OR download_count < max_downloads)
RETURNING object_id;
