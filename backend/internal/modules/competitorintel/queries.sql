-- Competitor Intelligence queries. Every query filters on tenant_id, under RLS as well.

-- name: InsertCompetitor :one
INSERT INTO competitor_intelligence.competitors (tenant_id, company_id, display_name, domain, status, tier, notes, monitoring_enabled, created_by)
VALUES (@tenant_id, @company_id, @display_name, sqlc.narg(domain), 'tracked', sqlc.narg(tier), sqlc.narg(notes), @monitoring_enabled, sqlc.narg(created_by))
ON CONFLICT (tenant_id, company_id) WHERE deleted_at IS NULL DO NOTHING RETURNING id;

-- name: GetCompetitor :one
SELECT id, company_id, display_name, domain, status, tier, notes, monitoring_enabled, created_at, updated_at
FROM competitor_intelligence.competitors WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: ListCompetitors :many
SELECT c.id, c.company_id, c.display_name, c.domain, c.status, c.tier, c.notes, c.monitoring_enabled, c.created_at, c.updated_at,
  lr.finished_at AS last_researched_at,
  (SELECT count(*) FROM competitor_intelligence.change_events e WHERE e.tenant_id = c.tenant_id AND e.competitor_id = c.id AND e.acknowledged_at IS NULL)::int AS open_changes
FROM competitor_intelligence.competitors c
LEFT JOIN LATERAL (SELECT r.finished_at FROM competitor_intelligence.research_runs r
  WHERE r.tenant_id = c.tenant_id AND r.competitor_id = c.id AND r.finished_at IS NOT NULL ORDER BY r.finished_at DESC LIMIT 1) lr ON true
WHERE c.tenant_id = @tenant_id AND c.deleted_at IS NULL ORDER BY c.display_name;

-- name: UpdateCompetitor :one
UPDATE competitor_intelligence.competitors SET status = coalesce(sqlc.narg(status), status), tier = coalesce(sqlc.narg(tier), tier), notes = coalesce(sqlc.narg(notes), notes),
  monitoring_enabled = coalesce(sqlc.narg(monitoring_enabled), monitoring_enabled), display_name = coalesce(sqlc.narg(display_name), display_name), updated_at = now()
WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL RETURNING id;

-- name: DeleteCompetitor :execrows
UPDATE competitor_intelligence.competitors SET deleted_at = now(), updated_at = now() WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: IsCompetitor :one
SELECT EXISTS (SELECT 1 FROM competitor_intelligence.competitors WHERE tenant_id = @tenant_id AND company_id = @company_id AND deleted_at IS NULL AND status <> 'archived');

-- name: InsertSource :one
INSERT INTO competitor_intelligence.sources (tenant_id, competitor_id, url, kind, access_status) VALUES (@tenant_id, @competitor_id, @url, @kind, 'unchecked')
ON CONFLICT (tenant_id, competitor_id, url) WHERE deleted_at IS NULL DO NOTHING
RETURNING id, url, kind, access_status, last_fetched_at, created_at;

-- name: ListSources :many
SELECT id, url, kind, access_status, last_fetched_at, created_at FROM competitor_intelligence.sources
WHERE tenant_id = @tenant_id AND competitor_id = @competitor_id AND deleted_at IS NULL ORDER BY created_at;

-- name: DeleteSource :execrows
UPDATE competitor_intelligence.sources SET deleted_at = now(), updated_at = now() WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: SetSourceStatus :exec
UPDATE competitor_intelligence.sources SET access_status = @access_status, last_fetched_at = CASE WHEN @fetched::bool THEN now() ELSE last_fetched_at END, updated_at = now()
WHERE tenant_id = @tenant_id AND id = @id;

-- name: GetConfig :one
SELECT id, competitor_id, cadence, scope, enabled, next_run_at, updated_at FROM competitor_intelligence.research_configs
WHERE tenant_id = @tenant_id AND competitor_id = @competitor_id AND deleted_at IS NULL;

-- name: UpsertConfig :one
INSERT INTO competitor_intelligence.research_configs (tenant_id, competitor_id, cadence, scope, enabled, next_run_at)
VALUES (@tenant_id, @competitor_id, @cadence, '{}', @enabled, sqlc.narg(next_run_at))
ON CONFLICT (tenant_id, competitor_id) WHERE deleted_at IS NULL
DO UPDATE SET cadence = EXCLUDED.cadence, enabled = EXCLUDED.enabled, next_run_at = EXCLUDED.next_run_at, updated_at = now()
RETURNING id, competitor_id, cadence, scope, enabled, next_run_at, updated_at;

-- name: DueConfigs :many
-- Runs as the system role, which may only read configs, to find work across tenants.
SELECT id, tenant_id, competitor_id, cadence FROM competitor_intelligence.research_configs
WHERE enabled AND deleted_at IS NULL AND cadence <> 'manual' AND next_run_at IS NOT NULL AND next_run_at <= now()
ORDER BY next_run_at LIMIT 200;

-- name: AdvanceConfig :execrows
UPDATE competitor_intelligence.research_configs SET next_run_at = @next_run_at, updated_at = now()
WHERE tenant_id = @tenant_id AND id = @id AND enabled AND next_run_at <= now();

-- name: InsertRun :one
INSERT INTO competitor_intelligence.research_runs (tenant_id, competitor_id, config_id, status, pages_fetched, credits_used)
VALUES (@tenant_id, @competitor_id, sqlc.narg(config_id), 'queued', 0, 0) RETURNING id;

-- name: SetRunJob :exec
UPDATE competitor_intelligence.research_runs SET job_id = @job_id WHERE tenant_id = @tenant_id AND id = @id;

-- name: GetRun :one
SELECT id, competitor_id, config_id, status, pages_fetched, credits_used, job_id, created_at, finished_at FROM competitor_intelligence.research_runs
WHERE tenant_id = @tenant_id AND id = @id;

-- name: ListRuns :many
SELECT id, competitor_id, config_id, status, pages_fetched, credits_used, job_id, created_at, finished_at FROM competitor_intelligence.research_runs
WHERE tenant_id = @tenant_id AND competitor_id = @competitor_id ORDER BY id DESC LIMIT 20;

-- name: StartRun :execrows
UPDATE competitor_intelligence.research_runs SET status = 'running', updated_at = now() WHERE tenant_id = @tenant_id AND id = @id AND status = 'queued';

-- name: FinishRun :exec
UPDATE competitor_intelligence.research_runs SET status = @status, pages_fetched = @pages_fetched, finished_at = now(), updated_at = now()
WHERE tenant_id = @tenant_id AND id = @id AND status = 'running';

-- name: CancelRun :one
UPDATE competitor_intelligence.research_runs SET status = 'cancelled', finished_at = now(), updated_at = now()
WHERE tenant_id = @tenant_id AND id = @id AND status IN ('queued', 'running') RETURNING job_id;

-- name: CancelOpenRuns :many
UPDATE competitor_intelligence.research_runs SET status = 'cancelled', finished_at = now(), updated_at = now()
WHERE tenant_id = @tenant_id AND status IN ('queued', 'running') RETURNING job_id;

-- name: LatestSnapshotHash :one
SELECT content_hash FROM competitor_intelligence.source_snapshots WHERE tenant_id = @tenant_id AND source_id = @source_id ORDER BY fetched_at DESC, id DESC LIMIT 1;

-- name: InsertSnapshot :one
INSERT INTO competitor_intelligence.source_snapshots (tenant_id, source_id, run_id, content_hash, file_id, fetched_at, http_status)
VALUES (@tenant_id, @source_id, @run_id, @content_hash, sqlc.narg(file_id), now(), sqlc.narg(http_status)) RETURNING id, fetched_at;

-- name: CurrentObservations :many
SELECT id, attribute_type, value, evidence_class FROM competitor_intelligence.observations
WHERE tenant_id = @tenant_id AND competitor_id = @competitor_id AND superseded_by_id IS NULL ORDER BY attribute_type, id;

-- name: InsertObservation :one
INSERT INTO competitor_intelligence.observations (tenant_id, competitor_id, attribute_type, value, evidence_class, confidence, source_snapshot_id, ai_request_id, observed_at)
VALUES (@tenant_id, @competitor_id, @attribute_type, @value, @evidence_class, sqlc.narg(confidence), sqlc.narg(source_snapshot_id), sqlc.narg(ai_request_id), now()) RETURNING id;

-- name: SupersedeObservations :exec
UPDATE competitor_intelligence.observations SET superseded_by_id = @new_id
WHERE tenant_id = @tenant_id AND competitor_id = @competitor_id AND attribute_type = @attribute_type AND superseded_by_id IS NULL AND id <> ALL(@keep::uuid[]);

-- name: ListObservations :many
SELECT o.id, o.attribute_type, o.value, o.evidence_class, o.confidence, o.observed_at, o.superseded_by_id, o.ai_request_id, s.url AS source_url, n.fetched_at AS source_fetched_at
FROM competitor_intelligence.observations o
LEFT JOIN competitor_intelligence.source_snapshots n ON n.tenant_id = o.tenant_id AND n.id = o.source_snapshot_id
LEFT JOIN competitor_intelligence.sources s ON s.tenant_id = n.tenant_id AND s.id = n.source_id
WHERE o.tenant_id = @tenant_id AND o.competitor_id = @competitor_id AND (@include_history::bool OR o.superseded_by_id IS NULL)
ORDER BY o.attribute_type, o.observed_at DESC, o.id DESC LIMIT 500;

-- name: CurrentObservationsFor :many
SELECT o.id, o.competitor_id, o.attribute_type, o.value, o.evidence_class FROM competitor_intelligence.observations o
WHERE o.tenant_id = @tenant_id AND o.competitor_id = ANY(@competitor_ids::uuid[]) AND o.superseded_by_id IS NULL ORDER BY o.competitor_id, o.attribute_type, o.id;

-- name: InsertChange :one
INSERT INTO competitor_intelligence.change_events (tenant_id, competitor_id, attribute_type, previous_observation_id, new_observation_id, summary)
VALUES (@tenant_id, @competitor_id, @attribute_type, sqlc.narg(previous_observation_id), @new_observation_id, @summary) RETURNING id;

-- name: ListChanges :many
SELECT id, attribute_type, summary, created_at, acknowledged_at FROM competitor_intelligence.change_events
WHERE tenant_id = @tenant_id AND competitor_id = @competitor_id ORDER BY id DESC LIMIT 200;

-- name: AcknowledgeChange :execrows
UPDATE competitor_intelligence.change_events SET acknowledged_at = now() WHERE tenant_id = @tenant_id AND id = @id AND acknowledged_at IS NULL;

-- name: ChangeExists :one
SELECT EXISTS (SELECT 1 FROM competitor_intelligence.change_events WHERE tenant_id = @tenant_id AND id = @id);

-- name: InsertOpportunity :one
INSERT INTO competitor_intelligence.market_opportunities (tenant_id, geography, industry_id, title, rationale, score, evidence_class, status, ai_request_id)
VALUES (@tenant_id, sqlc.narg(geography), sqlc.narg(industry_id), @title, @rationale, sqlc.narg(score), @evidence_class, 'new', sqlc.narg(ai_request_id)) RETURNING id;

-- name: InsertOpportunityEvidence :exec
INSERT INTO competitor_intelligence.opportunity_evidence (tenant_id, opportunity_id, observation_id, note) VALUES (@tenant_id, @opportunity_id, sqlc.narg(observation_id), sqlc.narg(note));

-- name: ListOpportunities :many
SELECT id, geography, industry_id, title, rationale, score, evidence_class, status, ai_request_id, created_at, updated_at FROM competitor_intelligence.market_opportunities
WHERE tenant_id = @tenant_id ORDER BY id DESC LIMIT 200;

-- name: GetOpportunity :one
SELECT id, geography, industry_id, title, rationale, score, evidence_class, status, ai_request_id, created_at, updated_at FROM competitor_intelligence.market_opportunities
WHERE tenant_id = @tenant_id AND id = @id;

-- name: SetOpportunityStatus :execrows
UPDATE competitor_intelligence.market_opportunities SET status = @status, updated_at = now() WHERE tenant_id = @tenant_id AND id = @id;

-- name: OpportunityEvidence :many
SELECT observation_id, note FROM competitor_intelligence.opportunity_evidence WHERE tenant_id = @tenant_id AND opportunity_id = @opportunity_id;

-- name: InsertComparison :one
INSERT INTO competitor_intelligence.comparison_sets (tenant_id, name, dimensions, created_by) VALUES (@tenant_id, @name, @dimensions, sqlc.narg(created_by)) RETURNING id;

-- name: InsertComparisonMember :exec
INSERT INTO competitor_intelligence.comparison_set_members (tenant_id, comparison_set_id, competitor_id) VALUES (@tenant_id, @comparison_set_id, @competitor_id);

-- name: ListComparisons :many
SELECT s.id, s.name, s.dimensions, s.created_at,
  (SELECT count(*) FROM competitor_intelligence.comparison_set_members m WHERE m.tenant_id = s.tenant_id AND m.comparison_set_id = s.id)::int AS member_count
FROM competitor_intelligence.comparison_sets s WHERE s.tenant_id = @tenant_id AND s.deleted_at IS NULL ORDER BY s.name;

-- name: GetComparison :one
SELECT id, name, dimensions, created_at FROM competitor_intelligence.comparison_sets WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: ComparisonMembers :many
SELECT c.id, c.display_name FROM competitor_intelligence.comparison_set_members m
JOIN competitor_intelligence.competitors c ON c.tenant_id = m.tenant_id AND c.id = m.competitor_id AND c.deleted_at IS NULL
WHERE m.tenant_id = @tenant_id AND m.comparison_set_id = @comparison_set_id ORDER BY c.display_name;

-- name: CountLiveCompetitors :one
SELECT count(*)::int FROM competitor_intelligence.competitors WHERE tenant_id = @tenant_id AND id = ANY(@ids::uuid[]) AND deleted_at IS NULL;

-- name: InsertReport :one
INSERT INTO competitor_intelligence.reports (tenant_id, title, kind, status, content, created_by) VALUES (@tenant_id, @title, @kind, 'queued', @content, sqlc.narg(created_by)) RETURNING id;

-- name: GetReport :one
SELECT id, title, kind, status, content, file_id, ai_request_id, created_at, updated_at FROM competitor_intelligence.reports WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: ListReports :many
SELECT id, title, kind, status, file_id, created_at, updated_at FROM competitor_intelligence.reports WHERE tenant_id = @tenant_id AND deleted_at IS NULL ORDER BY id DESC LIMIT 100;

-- name: SetReportStatus :exec
UPDATE competitor_intelligence.reports SET status = @status, updated_at = now() WHERE tenant_id = @tenant_id AND id = @id;

-- name: FinishReport :exec
UPDATE competitor_intelligence.reports SET status = @status, content = @content, file_id = sqlc.narg(file_id), ai_request_id = sqlc.narg(ai_request_id), updated_at = now()
WHERE tenant_id = @tenant_id AND id = @id;

-- Merge repointing runs as the system role, across tenants, because a merge is platform-wide.

-- name: RepointCompetitors :execrows
UPDATE competitor_intelligence.competitors c SET company_id = @target, updated_at = now()
WHERE c.company_id = @source AND NOT EXISTS (
  SELECT 1 FROM competitor_intelligence.competitors o WHERE o.tenant_id = c.tenant_id AND o.company_id = @target AND o.deleted_at IS NULL);

-- name: RetireDuplicateCompetitors :execrows
UPDATE competitor_intelligence.competitors SET deleted_at = now(), updated_at = now() WHERE company_id = @source AND deleted_at IS NULL;
