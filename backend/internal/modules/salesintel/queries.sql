-- Sales Intelligence queries. It reads only its own schema: everything else arrives as events.

-- name: InsertLeadFact :execrows
INSERT INTO sales_intelligence.fact_lead_events (tenant_id, source_event_id, event_type, prospect_ref, source_key, crm_company_ref, score, occurred_at)
VALUES (@tenant_id, @source_event_id, @event_type, sqlc.narg(prospect_ref), @source_key, sqlc.narg(crm_company_ref), sqlc.narg(score), @occurred_at)
ON CONFLICT (tenant_id, source_event_id) DO NOTHING;

-- name: LeadSource :one
SELECT source_key FROM sales_intelligence.fact_lead_events
WHERE tenant_id = @tenant_id AND prospect_ref = @prospect_ref AND event_type = 'lead_generation.prospect.created' LIMIT 1;

-- name: InsertOutreachFact :execrows
INSERT INTO sales_intelligence.fact_outreach_events (tenant_id, source_event_id, event_type, campaign_ref, company_ref, subtype, occurred_at)
VALUES (@tenant_id, @source_event_id, @event_type, @campaign_ref, sqlc.narg(company_ref), sqlc.narg(subtype), @occurred_at)
ON CONFLICT (tenant_id, source_event_id) DO NOTHING;

-- name: InsertDealFact :execrows
INSERT INTO sales_intelligence.fact_deal_events (tenant_id, source_event_id, event_type, deal_ref, proposal_ref, owner_user_ref, crm_company_ref, company_ref,
  pipeline_ref, stage_key, status, source_module, amount_minor, mrr_minor, currency, probability, expected_close, occurred_at)
VALUES (@tenant_id, @source_event_id, @event_type, sqlc.narg(deal_ref), sqlc.narg(proposal_ref), sqlc.narg(owner_user_ref), sqlc.narg(crm_company_ref),
  sqlc.narg(company_ref), sqlc.narg(pipeline_ref), sqlc.narg(stage_key), sqlc.narg(status), sqlc.narg(source_module), sqlc.narg(amount_minor),
  sqlc.narg(mrr_minor), sqlc.narg(currency), sqlc.narg(probability), sqlc.narg(expected_close), @occurred_at)
ON CONFLICT (tenant_id, source_event_id) DO NOTHING;

-- name: InsertRevenueFact :execrows
INSERT INTO sales_intelligence.fact_revenue_events (tenant_id, source_event_id, movement, contract_ref, customer_ref, mrr_delta_minor, contract_mrr_minor,
  currency, reporting_mrr_delta_minor, reporting_currency, fx_rate, contract_end_date, auto_renew, occurred_at)
VALUES (@tenant_id, @source_event_id, @movement, @contract_ref, sqlc.narg(customer_ref), @mrr_delta_minor, @contract_mrr_minor, @currency,
  sqlc.narg(reporting_mrr_delta_minor), @reporting_currency, sqlc.narg(fx_rate), sqlc.narg(contract_end_date), sqlc.narg(auto_renew), @occurred_at)
ON CONFLICT (tenant_id, source_event_id) DO NOTHING;

-- name: UpsertLabel :exec
INSERT INTO sales_intelligence.dimension_labels (tenant_id, kind, ref, label) VALUES (@tenant_id, @kind, @ref, @label)
ON CONFLICT (tenant_id, kind, ref) DO UPDATE SET label = EXCLUDED.label;

-- name: ListLabels :many
SELECT kind, ref, label FROM sales_intelligence.dimension_labels WHERE tenant_id = @tenant_id;

-- name: MarkDirty :exec
INSERT INTO sales_intelligence.rollup_state (tenant_id, dirty_since, facts_as_of) VALUES (@tenant_id, now(), @facts_as_of)
ON CONFLICT (tenant_id) DO UPDATE SET dirty_since = COALESCE(sales_intelligence.rollup_state.dirty_since, now()),
  facts_as_of = GREATEST(sales_intelligence.rollup_state.facts_as_of, EXCLUDED.facts_as_of);

-- name: GetRollupState :one
SELECT dirty_since, last_rolled_at, facts_as_of FROM sales_intelligence.rollup_state WHERE tenant_id = @tenant_id;

-- name: FinishRollup :exec
-- Clears the dirty mark only if nothing new arrived while the rollup ran.
INSERT INTO sales_intelligence.rollup_state (tenant_id, last_rolled_at) VALUES (@tenant_id, @computed_at)
ON CONFLICT (tenant_id) DO UPDATE SET last_rolled_at = @computed_at,
  dirty_since = CASE WHEN sales_intelligence.rollup_state.dirty_since <= @started_at THEN NULL ELSE sales_intelligence.rollup_state.dirty_since END;

-- name: DueRollups :many
-- Runs as the system role: tenants with new facts, or whose metrics have not been computed this month.
SELECT tenant_id FROM sales_intelligence.rollup_state
WHERE dirty_since IS NOT NULL OR last_rolled_at IS NULL OR last_rolled_at < date_trunc('month', now())
ORDER BY dirty_since NULLS LAST LIMIT 1000;

-- name: ListLeadFacts :many
SELECT event_type, prospect_ref, source_key, occurred_at FROM sales_intelligence.fact_lead_events
WHERE tenant_id = @tenant_id ORDER BY occurred_at, created_at;

-- name: ListOutreachFacts :many
SELECT event_type, campaign_ref, company_ref, occurred_at FROM sales_intelligence.fact_outreach_events
WHERE tenant_id = @tenant_id ORDER BY occurred_at, created_at;

-- name: ListDealFacts :many
SELECT event_type, deal_ref, proposal_ref, owner_user_ref, company_ref, stage_key, status, source_module, amount_minor, mrr_minor, currency,
  probability, expected_close, occurred_at
FROM sales_intelligence.fact_deal_events WHERE tenant_id = @tenant_id ORDER BY occurred_at, created_at, id;

-- name: ListRevenueFacts :many
SELECT movement, contract_ref, mrr_delta_minor, contract_mrr_minor, currency, reporting_mrr_delta_minor, reporting_currency, contract_end_date, auto_renew, occurred_at
FROM sales_intelligence.fact_revenue_events WHERE tenant_id = @tenant_id ORDER BY occurred_at, created_at;

-- name: FirstFactAt :one
-- With no facts yet, now: the range is then just this month.
SELECT COALESCE(LEAST(
  (SELECT min(l.occurred_at) FROM sales_intelligence.fact_lead_events l WHERE l.tenant_id = sqlc.arg(tenant_id)::uuid),
  (SELECT min(o.occurred_at) FROM sales_intelligence.fact_outreach_events o WHERE o.tenant_id = sqlc.arg(tenant_id)::uuid),
  (SELECT min(d.occurred_at) FROM sales_intelligence.fact_deal_events d WHERE d.tenant_id = sqlc.arg(tenant_id)::uuid),
  (SELECT min(r.occurred_at) FROM sales_intelligence.fact_revenue_events r WHERE r.tenant_id = sqlc.arg(tenant_id)::uuid),
  (SELECT min(c.period_start)::timestamptz FROM sales_intelligence.cost_entries c WHERE c.tenant_id = sqlc.arg(tenant_id)::uuid AND c.deleted_at IS NULL)), now())::timestamptz AS first_at;

-- name: FactPresence :one
SELECT
  EXISTS (SELECT 1 FROM sales_intelligence.fact_lead_events l WHERE l.tenant_id = sqlc.arg(tenant_id)::uuid)::boolean AS leads,
  EXISTS (SELECT 1 FROM sales_intelligence.fact_outreach_events o WHERE o.tenant_id = sqlc.arg(tenant_id)::uuid)::boolean AS outreach,
  EXISTS (SELECT 1 FROM sales_intelligence.fact_deal_events d WHERE d.tenant_id = sqlc.arg(tenant_id)::uuid)::boolean AS deals,
  EXISTS (SELECT 1 FROM sales_intelligence.fact_revenue_events r WHERE r.tenant_id = sqlc.arg(tenant_id)::uuid)::boolean AS revenue,
  EXISTS (SELECT 1 FROM sales_intelligence.cost_entries c WHERE c.tenant_id = sqlc.arg(tenant_id)::uuid AND c.deleted_at IS NULL)::boolean AS costs;

-- name: DeleteSnapshots :exec
DELETE FROM sales_intelligence.metric_snapshots WHERE tenant_id = @tenant_id;

-- name: InsertSnapshots :exec
-- One statement for a whole rollup. COPY cannot be used: PostgreSQL refuses it under row-level
-- security. An empty value means none; flags are comma-separated.
INSERT INTO sales_intelligence.metric_snapshots (tenant_id, metric_key, period_grain, period_start, dimension, dimension_value, value, value_kind, currency, flags, computed_at)
SELECT @tenant_id::uuid, unnest(@keys::text[]), 'month', unnest(@periods::date[]), unnest(@dims::text[]), unnest(@dim_values::text[]),
  NULLIF(unnest(@vals::text[]), '')::numeric, unnest(@kinds::text[]), NULLIF(unnest(@currencies::text[]), ''), string_to_array(unnest(@flags::text[]), ','), @computed_at::timestamptz;

-- name: ListSnapshots :many
SELECT id, metric_key, period_start, dimension, dimension_value, value, value_kind, currency, flags, computed_at
FROM sales_intelligence.metric_snapshots
WHERE tenant_id = @tenant_id AND metric_key = ANY(@keys::text[]) AND period_grain = 'month'
  AND period_start BETWEEN @from_month AND @to_month
  AND (sqlc.narg(dimension)::text IS NULL OR dimension = sqlc.narg(dimension))
ORDER BY metric_key, dimension, dimension_value, period_start;

-- name: SnapshotsByIDs :many
SELECT id, metric_key, period_start, dimension, dimension_value, value, value_kind, currency, flags
FROM sales_intelligence.metric_snapshots WHERE tenant_id = @tenant_id AND id = ANY(@ids::uuid[]);

-- name: ListCosts :many
SELECT id, period_start, period_end, category, campaign_ref, amount_minor, currency, note, created_by, created_at, updated_at
FROM sales_intelligence.cost_entries WHERE tenant_id = @tenant_id AND deleted_at IS NULL ORDER BY period_start DESC, id DESC LIMIT 1000;

-- name: GetCost :one
SELECT id, period_start, period_end, category, campaign_ref, amount_minor, currency, note, created_by, created_at, updated_at
FROM sales_intelligence.cost_entries WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: InsertCost :one
INSERT INTO sales_intelligence.cost_entries (tenant_id, period_start, period_end, category, campaign_ref, amount_minor, currency, note, created_by)
VALUES (@tenant_id, @period_start, @period_end, @category, sqlc.narg(campaign_ref), @amount_minor, @currency, sqlc.narg(note), sqlc.narg(created_by))
RETURNING id;

-- name: UpdateCost :exec
UPDATE sales_intelligence.cost_entries SET period_start = @period_start, period_end = @period_end, category = @category, campaign_ref = sqlc.narg(campaign_ref),
  amount_minor = @amount_minor, currency = @currency, note = sqlc.narg(note)
WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: DeleteCost :execrows
UPDATE sales_intelligence.cost_entries SET deleted_at = now() WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: FindLiveRun :one
SELECT id FROM sales_intelligence.forecast_runs
WHERE tenant_id = @tenant_id AND kind = @kind AND as_of = @as_of AND horizon_months = @horizon_months AND status <> 'failed';

-- name: InsertRun :one
INSERT INTO sales_intelligence.forecast_runs (tenant_id, kind, method_version, horizon_months, status, as_of, requested_by)
VALUES (@tenant_id, @kind, @method_version, @horizon_months, 'queued', @as_of, sqlc.narg(requested_by))
RETURNING id;

-- name: SetRunJob :exec
UPDATE sales_intelligence.forecast_runs SET job_id = @job_id WHERE tenant_id = @tenant_id AND id = @id;

-- name: StartRun :execrows
UPDATE sales_intelligence.forecast_runs SET status = 'running' WHERE tenant_id = @tenant_id AND id = @id AND status IN ('queued','running');

-- name: CompleteRun :exec
UPDATE sales_intelligence.forecast_runs SET status = 'completed', assumptions = @assumptions, currency = @currency, completed_at = now()
WHERE tenant_id = @tenant_id AND id = @id;

-- name: FailRun :exec
UPDATE sales_intelligence.forecast_runs SET status = 'failed', error_code = @error_code, assumptions = @assumptions, completed_at = now()
WHERE tenant_id = @tenant_id AND id = @id;

-- name: DeletePoints :exec
DELETE FROM sales_intelligence.forecast_points WHERE tenant_id = @tenant_id AND run_id = @run_id;

-- name: InsertPoint :exec
INSERT INTO sales_intelligence.forecast_points (tenant_id, run_id, period_start, expected_minor, low_minor, high_minor, currency)
VALUES (@tenant_id, @run_id, @period_start, @expected_minor, @low_minor, @high_minor, @currency);

-- name: GetRun :one
SELECT id, kind, method_version, horizon_months, assumptions, status, error_code, as_of, currency, requested_by, created_at, completed_at
FROM sales_intelligence.forecast_runs WHERE tenant_id = @tenant_id AND id = @id;

-- name: ListRuns :many
SELECT id, kind, method_version, horizon_months, assumptions, status, error_code, as_of, currency, requested_by, created_at, completed_at
FROM sales_intelligence.forecast_runs WHERE tenant_id = @tenant_id AND (sqlc.narg(kind)::text IS NULL OR kind = sqlc.narg(kind))
ORDER BY created_at DESC, id DESC LIMIT 50;

-- name: PointsFor :many
SELECT run_id, period_start, expected_minor, low_minor, high_minor, currency FROM sales_intelligence.forecast_points
WHERE tenant_id = @tenant_id AND run_id = ANY(@run_ids::uuid[]) ORDER BY run_id, period_start;

-- name: InsertDefinition :one
INSERT INTO sales_intelligence.report_definitions (tenant_id, name, definition, created_by) VALUES (@tenant_id, @name, @definition, sqlc.narg(created_by))
RETURNING id, name, definition, created_by, created_at;

-- name: ListDefinitions :many
SELECT id, name, definition, created_by, created_at FROM sales_intelligence.report_definitions
WHERE tenant_id = @tenant_id AND deleted_at IS NULL ORDER BY created_at DESC, id DESC LIMIT 200;

-- name: GetDefinition :one
SELECT id, name, definition, created_by, created_at FROM sales_intelligence.report_definitions WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: InsertExport :one
INSERT INTO sales_intelligence.report_exports (tenant_id, definition_id, format, status, created_by) VALUES (@tenant_id, @definition_id, 'csv', 'queued', sqlc.narg(created_by))
RETURNING id;

-- name: GetExport :one
SELECT id, definition_id, file_id, format, status, error_code, created_by, created_at, updated_at FROM sales_intelligence.report_exports
WHERE tenant_id = @tenant_id AND id = @id;

-- name: FinishExport :exec
UPDATE sales_intelligence.report_exports SET status = @status, file_id = sqlc.narg(file_id), error_code = sqlc.narg(error_code) WHERE tenant_id = @tenant_id AND id = @id;

-- name: InsertInsight :one
INSERT INTO sales_intelligence.ai_insights (tenant_id, scope, content, cited_metric_ids, ai_request_id, created_by)
VALUES (@tenant_id, @scope, @content, @cited_metric_ids, sqlc.narg(ai_request_id), sqlc.narg(created_by))
RETURNING id, scope, content, cited_metric_ids, created_at;

-- name: ListInsights :many
SELECT id, scope, content, cited_metric_ids, created_at FROM sales_intelligence.ai_insights
WHERE tenant_id = @tenant_id ORDER BY created_at DESC, id DESC LIMIT 20;
