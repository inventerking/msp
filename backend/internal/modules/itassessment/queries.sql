-- IT Assessment queries. Every query filters on tenant_id, under RLS as well.

-- name: ListTemplates :many
SELECT id, name, tier, check_keys, scoring_version, updated_at FROM it_assessment.templates WHERE tenant_id = @tenant_id AND deleted_at IS NULL ORDER BY tier, name;

-- name: GetTemplate :one
SELECT id, name, tier, check_keys, scoring_version FROM it_assessment.templates WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: InsertTemplate :one
INSERT INTO it_assessment.templates (tenant_id, name, tier, check_keys, scoring_version) VALUES (@tenant_id, @name, @tier, @check_keys, @scoring_version) RETURNING id;

-- name: UpdateTemplate :execrows
UPDATE it_assessment.templates SET name = coalesce(sqlc.narg(name), name), check_keys = coalesce(sqlc.narg(check_keys), check_keys)
WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: CountTemplates :one
SELECT count(*)::int FROM it_assessment.templates WHERE tenant_id = @tenant_id;

-- name: LockSeed :one
SELECT pg_advisory_xact_lock(hashtextextended(@key::text, 0))::text AS locked;

-- name: InsertVerification :one
INSERT INTO it_assessment.domain_verifications (tenant_id, domain, method, token_hash, status, expires_at)
VALUES (@tenant_id, @domain, @method, @token_hash, 'pending', @expires_at) RETURNING id;

-- name: GetVerification :one
SELECT id, domain, method, token_hash, status, verified_at, expires_at, created_at FROM it_assessment.domain_verifications WHERE tenant_id = @tenant_id AND id = @id;

-- name: SetVerification :exec
UPDATE it_assessment.domain_verifications SET status = @status, verified_at = CASE WHEN @status = 'verified' THEN now() ELSE verified_at END WHERE tenant_id = @tenant_id AND id = @id;

-- name: ListVerifications :many
SELECT id, domain, method, status, verified_at, expires_at, created_at FROM it_assessment.domain_verifications WHERE tenant_id = @tenant_id ORDER BY created_at DESC LIMIT 200;

-- name: InsertAuthorization :one
INSERT INTO it_assessment.authorizations (tenant_id, domain, tier, method, authorizer_name, authorizer_email, authorizer_role, terms_version, domain_verification_id, ip_hash, status, granted_at, expires_at)
VALUES (@tenant_id, @domain, @tier, @method, @authorizer_name, @authorizer_email, sqlc.narg(authorizer_role), @terms_version, sqlc.narg(domain_verification_id), sqlc.narg(ip_hash), 'active', now(), @expires_at)
RETURNING id;

-- name: GetAuthorization :one
SELECT id, domain, tier, method, authorizer_name, authorizer_email, authorizer_role, terms_version, domain_verification_id, status, granted_at, expires_at, revoked_at, created_at
FROM it_assessment.authorizations WHERE tenant_id = @tenant_id AND id = @id;

-- name: ListAuthorizations :many
SELECT id, domain, tier, method, authorizer_name, authorizer_email, authorizer_role, terms_version, domain_verification_id, status, granted_at, expires_at, revoked_at, created_at
FROM it_assessment.authorizations WHERE tenant_id = @tenant_id ORDER BY created_at DESC LIMIT 200;

-- name: RevokeAuthorization :execrows
UPDATE it_assessment.authorizations SET status = 'revoked', revoked_at = now() WHERE tenant_id = @tenant_id AND id = @id AND status = 'active';

-- name: RecentAssessment :one
SELECT EXISTS (SELECT 1 FROM it_assessment.assessments WHERE tenant_id = @tenant_id AND domain = @domain AND created_at > now() - interval '10 minutes' AND status <> 'failed');

-- name: InsertAssessment :one
INSERT INTO it_assessment.assessments (tenant_id, authorization_id, template_id, domain, company_id, crm_company_id, origin, status, created_by)
VALUES (@tenant_id, @authorization_id, @template_id, @domain, sqlc.narg(company_id), sqlc.narg(crm_company_id), 'internal', 'queued', sqlc.narg(created_by)) RETURNING id;

-- name: SetAssessmentJob :exec
UPDATE it_assessment.assessments SET job_id = @job_id WHERE tenant_id = @tenant_id AND id = @id;

-- name: GetAssessment :one
SELECT id, authorization_id, template_id, domain, company_id, crm_company_id, origin, status, score, risk_level, job_id, started_at, completed_at, created_by, created_at
FROM it_assessment.assessments WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: ListAssessments :many
SELECT id, domain, crm_company_id, status, score, risk_level, created_at, completed_at FROM it_assessment.assessments
WHERE tenant_id = @tenant_id AND deleted_at IS NULL ORDER BY created_at DESC LIMIT 200;

-- name: StartAssessment :execrows
UPDATE it_assessment.assessments SET status = 'running', started_at = now() WHERE tenant_id = @tenant_id AND id = @id AND status = 'queued';

-- name: FinishAssessment :exec
UPDATE it_assessment.assessments SET status = @status, score = sqlc.narg(score), risk_level = sqlc.narg(risk_level), completed_at = now() WHERE tenant_id = @tenant_id AND id = @id;

-- name: CancelAssessment :one
UPDATE it_assessment.assessments SET status = 'cancelled', completed_at = now() WHERE tenant_id = @tenant_id AND id = @id AND status IN ('queued', 'running') RETURNING job_id;

-- name: UpsertCheck :one
INSERT INTO it_assessment.checks (tenant_id, assessment_id, check_key, status, target, raw_result, duration_ms)
VALUES (@tenant_id, @assessment_id, @check_key, @status, @target, sqlc.narg(raw_result), sqlc.narg(duration_ms))
ON CONFLICT (tenant_id, assessment_id, check_key, target) DO UPDATE SET status = EXCLUDED.status, raw_result = EXCLUDED.raw_result
RETURNING id;

-- name: ListChecks :many
SELECT id, check_key, status, target, raw_result FROM it_assessment.checks WHERE tenant_id = @tenant_id AND assessment_id = @assessment_id ORDER BY check_key;

-- name: DeleteFindings :exec
DELETE FROM it_assessment.findings WHERE tenant_id = @tenant_id AND assessment_id = @assessment_id;

-- name: InsertFinding :exec
INSERT INTO it_assessment.findings (tenant_id, assessment_id, check_id, severity, classification, title, description, recommendation, evidence)
VALUES (@tenant_id, @assessment_id, @check_id, @severity, @classification, @title, @description, @recommendation, sqlc.narg(evidence));

-- name: ListFindings :many
SELECT f.id, f.severity, f.classification, f.title, f.description, f.recommendation, f.evidence, c.check_key
FROM it_assessment.findings f JOIN it_assessment.checks c ON c.tenant_id = f.tenant_id AND c.id = f.check_id
WHERE f.tenant_id = @tenant_id AND f.assessment_id = @assessment_id
ORDER BY CASE f.severity WHEN 'critical' THEN 0 WHEN 'high' THEN 1 WHEN 'medium' THEN 2 WHEN 'low' THEN 3 ELSE 4 END, f.title;

-- name: InsertEgressDecision :exec
INSERT INTO it_assessment.egress_decisions (tenant_id, assessment_id, check_key, requested_host, resolved_ip, decision, reason)
VALUES (@tenant_id, @assessment_id, @check_key, @requested_host, sqlc.narg(resolved_ip), @decision, sqlc.narg(reason));

-- name: InsertReport :one
INSERT INTO it_assessment.reports (tenant_id, assessment_id, version, status)
VALUES (@tenant_id, @assessment_id, (SELECT coalesce(max(version), 0) + 1 FROM it_assessment.reports r WHERE r.tenant_id = @tenant_id AND r.assessment_id = @assessment_id), 'queued')
RETURNING id;

-- name: GetReport :one
SELECT id, assessment_id, version, file_id, status, created_at FROM it_assessment.reports WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: ListReports :many
SELECT id, version, file_id, status, created_at FROM it_assessment.reports WHERE tenant_id = @tenant_id AND assessment_id = @assessment_id AND deleted_at IS NULL ORDER BY version DESC;

-- name: FinishReport :exec
UPDATE it_assessment.reports SET status = @status, file_id = sqlc.narg(file_id) WHERE tenant_id = @tenant_id AND id = @id;
