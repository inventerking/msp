-- Lead Generation queries. Every query filters on tenant_id, under RLS as well.

-- name: GetSearchJob :one
SELECT id, saved_search_id, criteria_snapshot, status, progress_percent, result_count, provider_keys, credits_reserved, credits_used, job_id, created_by, created_at, updated_at
FROM lead_generation.search_jobs WHERE tenant_id = @tenant_id AND id = @id;

-- name: InsertSearchJob :execrows
INSERT INTO lead_generation.search_jobs (id, tenant_id, saved_search_id, criteria_snapshot, status, progress_percent, result_count, credits_reserved, credits_used, created_by)
VALUES (@id, @tenant_id, sqlc.narg(saved_search_id), @criteria_snapshot, 'queued', 0, 0, @credits_reserved, 0, sqlc.narg(created_by))
ON CONFLICT (id) DO NOTHING;

-- name: SetSearchJobID :exec
UPDATE lead_generation.search_jobs SET job_id = @job_id, updated_at = now() WHERE tenant_id = @tenant_id AND id = @id;

-- name: ListSearchJobs :many
SELECT id, saved_search_id, criteria_snapshot, status, progress_percent, result_count, provider_keys, credits_reserved, credits_used, job_id, created_by, created_at, updated_at
FROM lead_generation.search_jobs WHERE tenant_id = @tenant_id AND (sqlc.narg(before)::uuid IS NULL OR id < sqlc.narg(before)::uuid)
ORDER BY id DESC LIMIT @lim;

-- name: StartSearchJob :execrows
UPDATE lead_generation.search_jobs SET status = 'running', progress_percent = 10, updated_at = now()
WHERE tenant_id = @tenant_id AND id = @id AND status = 'queued';

-- name: FinishSearchJob :exec
UPDATE lead_generation.search_jobs SET status = @status, progress_percent = 100, result_count = @result_count, credits_used = @credits_used, provider_keys = @provider_keys, updated_at = now()
WHERE tenant_id = @tenant_id AND id = @id;

-- name: CancelSearchJob :one
UPDATE lead_generation.search_jobs SET status = 'cancelled', updated_at = now()
WHERE tenant_id = @tenant_id AND id = @id AND status IN ('queued', 'running') RETURNING job_id;

-- name: CancelOpenSearches :many
UPDATE lead_generation.search_jobs SET status = 'cancelled', updated_at = now()
WHERE tenant_id = @tenant_id AND status IN ('queued', 'running') RETURNING job_id;

-- name: InsertSearchResult :exec
INSERT INTO lead_generation.search_results (tenant_id, search_job_id, company_id, rank, source_key, preview)
VALUES (@tenant_id, @search_job_id, @company_id, @rank, @source_key, @preview)
ON CONFLICT (tenant_id, search_job_id, company_id) DO NOTHING;

-- name: ListSearchResults :many
SELECT id, company_id, rank, source_key, preview, promoted_prospect_id FROM lead_generation.search_results
WHERE tenant_id = @tenant_id AND search_job_id = @search_job_id ORDER BY rank;

-- name: GetSearchResultByCompany :one
SELECT id, company_id, source_key, preview, promoted_prospect_id FROM lead_generation.search_results
WHERE tenant_id = @tenant_id AND search_job_id = @search_job_id AND company_id = @company_id;

-- name: MarkResultPromoted :exec
UPDATE lead_generation.search_results SET promoted_prospect_id = @prospect_id WHERE tenant_id = @tenant_id AND id = @id;

-- name: ListSavedSearches :many
SELECT id, name, criteria, created_at, updated_at FROM lead_generation.saved_searches
WHERE tenant_id = @tenant_id AND deleted_at IS NULL ORDER BY name;

-- name: GetSavedSearch :one
SELECT id, name, criteria FROM lead_generation.saved_searches WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: InsertSavedSearch :one
INSERT INTO lead_generation.saved_searches (tenant_id, name, criteria, created_by) VALUES (@tenant_id, @name, @criteria, sqlc.narg(created_by))
RETURNING id, name, criteria, created_at, updated_at;

-- name: UpdateSavedSearch :one
UPDATE lead_generation.saved_searches SET name = coalesce(sqlc.narg(name), name), criteria = coalesce(sqlc.narg(criteria), criteria), updated_at = now()
WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL RETURNING id, name, criteria, created_at, updated_at;

-- name: DeleteSavedSearch :execrows
UPDATE lead_generation.saved_searches SET deleted_at = now() WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: InsertProspect :one
INSERT INTO lead_generation.prospects (tenant_id, company_id, display_name, domain, status, source_key, source_ref, data_quality, created_by)
VALUES (@tenant_id, @company_id, @display_name, sqlc.narg(domain), 'new', @source_key, sqlc.narg(source_ref), 'unknown', sqlc.narg(created_by))
ON CONFLICT (tenant_id, company_id) WHERE deleted_at IS NULL DO NOTHING
RETURNING id;

-- name: FindProspectByCompany :one
SELECT id FROM lead_generation.prospects WHERE tenant_id = @tenant_id AND company_id = @company_id AND deleted_at IS NULL;

-- name: GetProspect :one
SELECT id, company_id, display_name, domain, status, source_key, data_quality, latest_score, latest_coverage, crm_company_id, owner_user_id, created_at, updated_at
FROM lead_generation.prospects WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: ListProspects :many
SELECT p.id, p.company_id, p.display_name, p.domain, p.status, p.source_key, p.data_quality, p.latest_score, p.latest_coverage, p.crm_company_id, p.owner_user_id, p.created_at, p.updated_at
FROM lead_generation.prospects p
WHERE p.tenant_id = @tenant_id AND p.deleted_at IS NULL
  AND (sqlc.narg(status)::text IS NULL OR p.status = sqlc.narg(status)::text)
  AND (sqlc.narg(min_score)::int IS NULL OR p.latest_score >= sqlc.narg(min_score)::int)
  AND (sqlc.narg(list_id)::uuid IS NULL OR EXISTS (SELECT 1 FROM lead_generation.prospect_list_members m WHERE m.tenant_id = p.tenant_id AND m.list_id = sqlc.narg(list_id)::uuid AND m.prospect_id = p.id))
  AND (sqlc.narg(q)::text IS NULL OR p.display_name ILIKE '%' || sqlc.narg(q)::text || '%' OR p.domain::text ILIKE '%' || sqlc.narg(q)::text || '%')
  AND (sqlc.narg(before)::uuid IS NULL OR p.id < sqlc.narg(before)::uuid)
ORDER BY p.id DESC LIMIT @lim;

-- name: UpdateProspect :one
UPDATE lead_generation.prospects SET status = coalesce(sqlc.narg(status), status), owner_user_id = CASE WHEN @set_owner::bool THEN sqlc.narg(owner_user_id) ELSE owner_user_id END, updated_at = now()
WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL RETURNING status;

-- name: DeleteProspect :execrows
UPDATE lead_generation.prospects SET deleted_at = now() WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: SetProspectScore :exec
UPDATE lead_generation.prospects SET latest_score = @score, latest_coverage = @coverage, data_quality = @data_quality, updated_at = now()
WHERE tenant_id = @tenant_id AND id = @id;

-- name: SetProspectName :exec
UPDATE lead_generation.prospects SET display_name = @display_name, updated_at = now() WHERE tenant_id = @tenant_id AND id = @id;

-- name: MarkConverted :exec
UPDATE lead_generation.prospects SET status = 'converted', crm_company_id = @crm_company_id, updated_at = now() WHERE tenant_id = @tenant_id AND id = @id;

-- name: InsertAttribute :exec
INSERT INTO lead_generation.prospect_attributes (tenant_id, prospect_id, attribute, value, source_key, source_url, licence_class, confidence, observed_at)
VALUES (@tenant_id, @prospect_id, @attribute, @value, @source_key, sqlc.narg(source_url), @licence_class, sqlc.narg(confidence), now());

-- name: LatestAttributes :many
SELECT DISTINCT ON (attribute) attribute, value, source_key, licence_class, confidence, observed_at
FROM lead_generation.prospect_attributes WHERE tenant_id = @tenant_id AND prospect_id = @prospect_id
  AND (expires_at IS NULL OR expires_at > now())
ORDER BY attribute, observed_at DESC, id DESC;

-- name: LatestAttributesFor :many
SELECT DISTINCT ON (prospect_id, attribute) prospect_id, attribute, value, source_key, licence_class
FROM lead_generation.prospect_attributes WHERE tenant_id = @tenant_id AND prospect_id = ANY(@prospect_ids::uuid[])
  AND (expires_at IS NULL OR expires_at > now())
ORDER BY prospect_id, attribute, observed_at DESC, id DESC;

-- name: InsertProspectContact :exec
INSERT INTO lead_generation.prospect_contacts (tenant_id, prospect_id, contact_id, role_title, is_decision_maker, confidence, source_key)
VALUES (@tenant_id, @prospect_id, @contact_id, sqlc.narg(role_title), sqlc.narg(is_decision_maker), sqlc.narg(confidence), @source_key)
ON CONFLICT (tenant_id, prospect_id, contact_id) DO UPDATE SET role_title = EXCLUDED.role_title, is_decision_maker = EXCLUDED.is_decision_maker, confidence = EXCLUDED.confidence, updated_at = now();

-- name: ListProspectContacts :many
SELECT id, contact_id, role_title, is_decision_maker, confidence, source_key, created_at FROM lead_generation.prospect_contacts
WHERE tenant_id = @tenant_id AND prospect_id = @prospect_id ORDER BY is_decision_maker DESC NULLS LAST, created_at;

-- name: ContactLinks :many
SELECT pc.prospect_id, pc.role_title, pc.is_decision_maker, pc.source_key, pc.created_at FROM lead_generation.prospect_contacts pc
WHERE pc.tenant_id = @tenant_id AND pc.contact_id = @contact_id;

-- name: DeleteContactLinks :exec
DELETE FROM lead_generation.prospect_contacts WHERE tenant_id = @tenant_id AND contact_id = @contact_id;

-- name: InsertEnrichmentRun :one
INSERT INTO lead_generation.enrichment_runs (tenant_id, prospect_id, kind, provider_key, status, credits_used)
VALUES (@tenant_id, @prospect_id, @kind, @provider_key, 'queued', 0) RETURNING id;

-- name: SetEnrichmentJob :exec
UPDATE lead_generation.enrichment_runs SET job_id = @job_id WHERE tenant_id = @tenant_id AND id = @id;

-- name: GetEnrichmentRun :one
SELECT id, prospect_id, kind, provider_key, status FROM lead_generation.enrichment_runs WHERE tenant_id = @tenant_id AND id = @id;

-- name: FinishEnrichmentRun :exec
UPDATE lead_generation.enrichment_runs SET status = @status, credits_used = @credits_used, error_code = sqlc.narg(error_code), finished_at = now(), updated_at = now()
WHERE tenant_id = @tenant_id AND id = @id;

-- name: ListEnrichmentRuns :many
SELECT id, kind, provider_key, status, credits_used, error_code, created_at, finished_at FROM lead_generation.enrichment_runs
WHERE tenant_id = @tenant_id AND prospect_id = @prospect_id ORDER BY id DESC LIMIT 20;

-- name: CancelOpenEnrichments :many
UPDATE lead_generation.enrichment_runs SET status = 'cancelled', finished_at = now(), updated_at = now()
WHERE tenant_id = @tenant_id AND status IN ('queued', 'running') RETURNING job_id;

-- name: ActiveScoringModel :one
SELECT id, name, version, weights FROM lead_generation.scoring_models WHERE tenant_id = @tenant_id AND status = 'active' ORDER BY version DESC LIMIT 1;

-- name: ListScoringModels :many
SELECT id, name, version, weights, status, created_at FROM lead_generation.scoring_models WHERE tenant_id = @tenant_id ORDER BY name, version DESC;

-- name: GetScoringModel :one
SELECT id, name, version, weights, status, created_at FROM lead_generation.scoring_models WHERE tenant_id = @tenant_id AND id = @id;

-- name: InsertScoringModel :one
INSERT INTO lead_generation.scoring_models (tenant_id, name, version, weights, status, created_by)
VALUES (@tenant_id, @name, (SELECT coalesce(max(version), 0) + 1 FROM lead_generation.scoring_models m WHERE m.tenant_id = @tenant_id AND m.name = @name), @weights, @status, sqlc.narg(created_by))
RETURNING id, name, version, weights, status, created_at;

-- name: RetireActiveModels :exec
UPDATE lead_generation.scoring_models SET status = 'retired', updated_at = now() WHERE tenant_id = @tenant_id AND status = 'active';

-- name: ActivateModel :execrows
UPDATE lead_generation.scoring_models SET status = 'active', updated_at = now() WHERE tenant_id = @tenant_id AND id = @id AND status = 'draft';

-- name: InsertScore :one
INSERT INTO lead_generation.prospect_scores (tenant_id, prospect_id, model_id, score, coverage) VALUES (@tenant_id, @prospect_id, @model_id, @score, @coverage) RETURNING id, created_at;

-- name: InsertScoreFactor :exec
INSERT INTO lead_generation.prospect_score_factors (tenant_id, score_id, factor_key, state, weight, earned, evidence)
VALUES (@tenant_id, @score_id, @factor_key, @state, @weight, sqlc.narg(earned), @evidence);

-- name: LatestScore :one
SELECT s.id, s.score, s.coverage, s.explanation, s.ai_request_id, s.created_at, m.version AS model_version, m.name AS model_name
FROM lead_generation.prospect_scores s JOIN lead_generation.scoring_models m ON m.tenant_id = s.tenant_id AND m.id = s.model_id
WHERE s.tenant_id = @tenant_id AND s.prospect_id = @prospect_id ORDER BY s.created_at DESC, s.id DESC LIMIT 1;

-- name: ScoreFactors :many
SELECT factor_key, state, weight, earned, evidence FROM lead_generation.prospect_score_factors WHERE tenant_id = @tenant_id AND score_id = @score_id ORDER BY factor_key;

-- name: SetExplanation :exec
UPDATE lead_generation.prospect_scores SET explanation = @explanation, ai_request_id = @ai_request_id WHERE tenant_id = @tenant_id AND id = @id;

-- name: ListLists :many
SELECT l.id, l.name, l.description, l.created_at, l.updated_at,
  (SELECT count(*) FROM lead_generation.prospect_list_members m JOIN lead_generation.prospects p ON p.tenant_id = m.tenant_id AND p.id = m.prospect_id AND p.deleted_at IS NULL
   WHERE m.tenant_id = l.tenant_id AND m.list_id = l.id)::int AS member_count
FROM lead_generation.prospect_lists l WHERE l.tenant_id = @tenant_id AND l.deleted_at IS NULL ORDER BY l.name;

-- name: GetList :one
SELECT id, name, description, created_at, updated_at FROM lead_generation.prospect_lists WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: InsertList :one
INSERT INTO lead_generation.prospect_lists (tenant_id, name, description, created_by) VALUES (@tenant_id, @name, sqlc.narg(description), sqlc.narg(created_by))
RETURNING id, name, description, created_at, updated_at;

-- name: UpdateList :one
UPDATE lead_generation.prospect_lists SET name = coalesce(sqlc.narg(name), name), description = coalesce(sqlc.narg(description), description), updated_at = now()
WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL RETURNING id, name, description, created_at, updated_at;

-- name: DeleteList :execrows
UPDATE lead_generation.prospect_lists SET deleted_at = now() WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: AddListMember :exec
INSERT INTO lead_generation.prospect_list_members (tenant_id, list_id, prospect_id, added_by) VALUES (@tenant_id, @list_id, @prospect_id, sqlc.narg(added_by))
ON CONFLICT (tenant_id, list_id, prospect_id) DO NOTHING;

-- name: RemoveListMember :execrows
DELETE FROM lead_generation.prospect_list_members WHERE tenant_id = @tenant_id AND list_id = @list_id AND prospect_id = @prospect_id;

-- name: CountLiveProspects :one
SELECT count(*)::int FROM lead_generation.prospects WHERE tenant_id = @tenant_id AND id = ANY(@ids::uuid[]) AND deleted_at IS NULL;

-- name: InsertImportJob :one
INSERT INTO lead_generation.import_jobs (tenant_id, source_file_id, mapping, lawful_source_attested, status, created_by)
VALUES (@tenant_id, @source_file_id, @mapping, true, 'uploaded', sqlc.narg(created_by)) RETURNING id;

-- name: GetImportJob :one
SELECT id, source_file_id, report_file_id, mapping, status, rows_total, rows_imported, rows_duplicate, rows_rejected, created_by, created_at, updated_at
FROM lead_generation.import_jobs WHERE tenant_id = @tenant_id AND id = @id;

-- name: SetImportStatus :exec
UPDATE lead_generation.import_jobs SET status = @status, updated_at = now() WHERE tenant_id = @tenant_id AND id = @id;

-- name: FinishImport :exec
UPDATE lead_generation.import_jobs SET status = @status, rows_total = @rows_total, rows_imported = @rows_imported, rows_duplicate = @rows_duplicate, rows_rejected = @rows_rejected,
  report_file_id = sqlc.narg(report_file_id), updated_at = now() WHERE tenant_id = @tenant_id AND id = @id;

-- name: InsertExportJob :one
INSERT INTO lead_generation.export_jobs (tenant_id, criteria, status, created_by) VALUES (@tenant_id, @criteria, 'queued', sqlc.narg(created_by)) RETURNING id;

-- name: GetExportJob :one
SELECT id, criteria, file_id, status, rows_total, values_withheld, created_by, created_at, updated_at FROM lead_generation.export_jobs WHERE tenant_id = @tenant_id AND id = @id;

-- name: FinishExport :exec
UPDATE lead_generation.export_jobs SET status = @status, file_id = sqlc.narg(file_id), rows_total = @rows_total, values_withheld = @values_withheld, updated_at = now()
WHERE tenant_id = @tenant_id AND id = @id;

-- name: ExportProspects :many
SELECT p.id, p.display_name, p.domain, p.status, p.latest_score, p.data_quality FROM lead_generation.prospects p
WHERE p.tenant_id = @tenant_id AND p.deleted_at IS NULL
  AND (sqlc.narg(status)::text IS NULL OR p.status = sqlc.narg(status)::text)
  AND (sqlc.narg(list_id)::uuid IS NULL OR EXISTS (SELECT 1 FROM lead_generation.prospect_list_members m WHERE m.tenant_id = p.tenant_id AND m.list_id = sqlc.narg(list_id)::uuid AND m.prospect_id = p.id))
ORDER BY p.display_name LIMIT 10000;

-- Merge repointing runs as the system role, across tenants, because a merge is platform-wide.

-- name: RepointProspects :execrows
UPDATE lead_generation.prospects p SET company_id = @target, updated_at = now()
WHERE p.company_id = @source AND NOT EXISTS (
  SELECT 1 FROM lead_generation.prospects o WHERE o.tenant_id = p.tenant_id AND o.company_id = @target AND o.deleted_at IS NULL) ;

-- name: RetireDuplicateProspects :execrows
UPDATE lead_generation.prospects SET deleted_at = now(), updated_at = now() WHERE company_id = @source AND deleted_at IS NULL;

-- name: RepointSearchResults :execrows
UPDATE lead_generation.search_results r SET company_id = @target
WHERE r.company_id = @source AND NOT EXISTS (
  SELECT 1 FROM lead_generation.search_results o WHERE o.tenant_id = r.tenant_id AND o.search_job_id = r.search_job_id AND o.company_id = @target);

-- name: GetScoreByID :one
SELECT s.id, s.score, s.coverage, s.explanation, s.ai_request_id, s.created_at, m.version AS model_version, m.name AS model_name
FROM lead_generation.prospect_scores s JOIN lead_generation.scoring_models m ON m.tenant_id = s.tenant_id AND m.id = s.model_id
WHERE s.tenant_id = @tenant_id AND s.id = @id;

-- name: FailSearchIfOpen :exec
UPDATE lead_generation.search_jobs SET status = 'failed', updated_at = now() WHERE tenant_id = @tenant_id AND id = @id AND status IN ('queued', 'running');

-- name: FailEnrichmentIfOpen :exec
UPDATE lead_generation.enrichment_runs SET status = 'failed', error_code = 'job_failed', finished_at = now(), updated_at = now()
WHERE tenant_id = @tenant_id AND id = @id AND status IN ('queued', 'running');
