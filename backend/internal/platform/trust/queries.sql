-- Legal documents and abuse reports. The documents' text comes from counsel; this only versions it.

-- name: ListDocuments :many
SELECT id, kind, version, title, status, requires_acceptance, counsel_reviewed, published_at, created_at, updated_at
FROM platform.legal_documents ORDER BY kind, version DESC;

-- name: GetDocument :one
SELECT id, kind, version, title, body, status, requires_acceptance, counsel_reviewed, published_at, created_at, updated_at
FROM platform.legal_documents WHERE id = @id;

-- name: PublishedDocuments :many
SELECT id, kind, version, title, body, requires_acceptance, counsel_reviewed, published_at
FROM platform.legal_documents WHERE status = 'published' ORDER BY kind;

-- name: NextDocumentVersion :one
SELECT COALESCE(max(version), 0)::int + 1 FROM platform.legal_documents WHERE kind = @kind;

-- name: InsertDocument :one
INSERT INTO platform.legal_documents (kind, version, title, body, status, requires_acceptance, created_by)
VALUES (@kind, @version, @title, @body, 'draft', @requires_acceptance, sqlc.narg(created_by)) RETURNING id;

-- name: UpdateDraft :execrows
UPDATE platform.legal_documents SET title = @title, body = @body, requires_acceptance = @requires_acceptance
WHERE id = @id AND status = 'draft';

-- name: RetirePublished :exec
UPDATE platform.legal_documents SET status = 'retired' WHERE kind = @kind AND status = 'published';

-- name: PublishDocument :execrows
UPDATE platform.legal_documents SET status = 'published', published_at = now(), published_by = @published_by, counsel_reviewed = @counsel_reviewed
WHERE id = @id AND status = 'draft';

-- name: InsertAbuseReport :one
INSERT INTO platform.abuse_reports (category, subject_url, details, reporter_email) VALUES (@category, sqlc.narg(subject_url), @details, sqlc.narg(reporter_email))
RETURNING id;

-- name: ListAbuseReports :many
SELECT id, category, subject_url, details, reporter_email, target_tenant_id, status, decision, reviewed_by, reviewed_at, created_at
FROM platform.abuse_reports WHERE (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status)) ORDER BY created_at DESC LIMIT 200;

-- name: GetAbuseReport :one
SELECT id, category, subject_url, details, reporter_email, target_tenant_id, status, decision, reviewed_by, reviewed_at, created_at
FROM platform.abuse_reports WHERE id = @id;

-- name: ReviewAbuseReport :execrows
UPDATE platform.abuse_reports SET status = @status, decision = sqlc.narg(decision), target_tenant_id = sqlc.narg(target_tenant_id),
  reviewed_by = @reviewed_by, reviewed_at = now()
WHERE id = @id;

-- name: OpenAbuseReports :one
SELECT count(*)::int FROM platform.abuse_reports WHERE status IN ('new','reviewing');
