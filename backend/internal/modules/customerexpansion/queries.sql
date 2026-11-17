-- Customer Expansion queries. Customer identity stays with CRM: a profile only overlays a CRM company.

-- name: EnsureProfile :one
INSERT INTO customer_expansion.customer_profiles (tenant_id, crm_company_id, company_id, health_status)
VALUES (@tenant_id, @crm_company_id, sqlc.narg(company_id), 'unknown')
ON CONFLICT (tenant_id, crm_company_id) WHERE deleted_at IS NULL DO UPDATE SET updated_at = now()
RETURNING id;

-- name: ListProfiles :many
SELECT p.id, p.crm_company_id, p.company_id, p.health_status, p.health_as_of, p.health_source, p.last_analyzed_at, p.created_at,
  COALESCE((SELECT sum(c.mrr_minor) FROM customer_expansion.contracts c
            WHERE c.tenant_id = p.tenant_id AND c.customer_profile_id = p.id AND c.status IN ('active','renewing')), 0)::bigint AS active_mrr_minor,
  COALESCE((SELECT min(c.currency) FROM customer_expansion.contracts c
   WHERE c.tenant_id = p.tenant_id AND c.customer_profile_id = p.id AND c.status IN ('active','renewing')), '')::text AS mrr_currency,
  (SELECT count(DISTINCT c.currency) FROM customer_expansion.contracts c
   WHERE c.tenant_id = p.tenant_id AND c.customer_profile_id = p.id AND c.status IN ('active','renewing'))::int AS mrr_currencies,
  COALESCE((SELECT to_char(min(c.end_date), 'YYYY-MM-DD') FROM customer_expansion.contracts c
   WHERE c.tenant_id = p.tenant_id AND c.customer_profile_id = p.id AND c.status IN ('active','renewing') AND c.end_date >= current_date), '')::text AS renewal_next_date,
  (SELECT count(*) FROM customer_expansion.service_inventory s WHERE s.tenant_id = p.tenant_id AND s.customer_profile_id = p.id AND s.deleted_at IS NULL AND s.coverage = 'active')::int AS covered_active,
  (SELECT count(*) FROM customer_expansion.service_inventory s WHERE s.tenant_id = p.tenant_id AND s.customer_profile_id = p.id AND s.deleted_at IS NULL AND s.coverage = 'absent')::int AS covered_absent,
  (SELECT count(*) FROM customer_expansion.service_inventory s WHERE s.tenant_id = p.tenant_id AND s.customer_profile_id = p.id AND s.deleted_at IS NULL AND s.coverage = 'declined')::int AS covered_declined
FROM customer_expansion.customer_profiles p
WHERE p.tenant_id = @tenant_id AND p.deleted_at IS NULL
  AND (sqlc.narg(health)::text IS NULL OR p.health_status = sqlc.narg(health))
  AND (sqlc.narg(only_id)::uuid IS NULL OR p.id = sqlc.narg(only_id))
ORDER BY p.created_at DESC, p.id DESC
LIMIT 500;

-- name: GetProfile :one
SELECT id, crm_company_id, company_id, health_status, health_as_of, health_source, last_analyzed_at, created_at
FROM customer_expansion.customer_profiles WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: ProfileByCompany :one
SELECT id FROM customer_expansion.customer_profiles WHERE tenant_id = @tenant_id AND crm_company_id = @crm_company_id AND deleted_at IS NULL;

-- name: SetHealth :exec
UPDATE customer_expansion.customer_profiles SET health_status = @health_status, health_source = @health_source, health_as_of = now()
WHERE tenant_id = @tenant_id AND id = @id;

-- name: SetComputedHealth :exec
-- Analysis never overwrites a status someone set by hand.
UPDATE customer_expansion.customer_profiles SET
  health_status = CASE WHEN health_source = 'manual' THEN health_status ELSE @health_status END,
  health_as_of = CASE WHEN health_source = 'manual' THEN health_as_of ELSE now() END,
  last_analyzed_at = now()
WHERE tenant_id = @tenant_id AND id = @id;

-- name: RepointProfiles :execrows
-- Runs as the system role for a platform-wide company merge.
UPDATE customer_expansion.customer_profiles SET company_id = @target WHERE company_id = @source;

-- name: ContractByDealForUpdate :one
SELECT id, customer_profile_id, status, mrr_minor, currency, setup_minor, term_months, proposal_version_id, review_reason, end_date, auto_renew
FROM customer_expansion.contracts WHERE tenant_id = @tenant_id AND deal_id = @deal_id FOR UPDATE;

-- name: InsertContract :one
INSERT INTO customer_expansion.contracts (tenant_id, customer_profile_id, deal_id, proposal_version_id, status, start_date, end_date, term_months, auto_renew,
  notice_days, currency, mrr_minor, setup_minor, source, owner_user_id, review_reason)
VALUES (@tenant_id, @customer_profile_id, sqlc.narg(deal_id), sqlc.narg(proposal_version_id), @status, @start_date, sqlc.narg(end_date), sqlc.narg(term_months),
  sqlc.narg(auto_renew), sqlc.narg(notice_days), @currency, @mrr_minor, @setup_minor, @source, sqlc.narg(owner_user_id), sqlc.narg(review_reason))
RETURNING id;

-- name: ActivateContract :exec
UPDATE customer_expansion.contracts SET status = 'active', end_date = sqlc.narg(end_date), start_date = @start_date, currency = @currency, mrr_minor = @mrr_minor,
  setup_minor = @setup_minor, owner_user_id = COALESCE(sqlc.narg(owner_user_id), owner_user_id), review_reason = sqlc.narg(review_reason)
WHERE tenant_id = @tenant_id AND id = @id;

-- name: ApplyProposal :exec
UPDATE customer_expansion.contracts SET proposal_version_id = @proposal_version_id, mrr_minor = @mrr_minor, setup_minor = @setup_minor,
  term_months = COALESCE(sqlc.narg(term_months), term_months), currency = @currency, review_reason = sqlc.narg(review_reason)
WHERE tenant_id = @tenant_id AND id = @id;

-- name: SetContractStatus :exec
UPDATE customer_expansion.contracts SET status = @status, end_date = COALESCE(sqlc.narg(end_date), end_date), review_reason = COALESCE(sqlc.narg(review_reason), review_reason)
WHERE tenant_id = @tenant_id AND id = @id;

-- name: GetContractForUpdate :one
SELECT c.id, c.customer_profile_id, c.deal_id, c.proposal_version_id, c.status, c.start_date, c.end_date, c.term_months, c.auto_renew, c.notice_days,
  c.currency, c.mrr_minor, c.setup_minor, c.source, c.owner_user_id, c.review_reason, p.crm_company_id
FROM customer_expansion.contracts c JOIN customer_expansion.customer_profiles p ON p.tenant_id = c.tenant_id AND p.id = c.customer_profile_id
WHERE c.tenant_id = @tenant_id AND c.id = @id FOR UPDATE OF c;

-- name: UpdateContract :exec
UPDATE customer_expansion.contracts SET status = @status, start_date = @start_date, end_date = sqlc.narg(end_date), term_months = sqlc.narg(term_months),
  auto_renew = sqlc.narg(auto_renew), notice_days = sqlc.narg(notice_days), currency = @currency, mrr_minor = @mrr_minor, setup_minor = @setup_minor,
  review_reason = sqlc.narg(review_reason)
WHERE tenant_id = @tenant_id AND id = @id;

-- name: ListContracts :many
SELECT id, customer_profile_id, deal_id, proposal_version_id, status, start_date, end_date, term_months, auto_renew, notice_days, currency, mrr_minor,
  setup_minor, source, owner_user_id, review_reason, created_at, updated_at
FROM customer_expansion.contracts WHERE tenant_id = @tenant_id AND customer_profile_id = @customer_profile_id
ORDER BY start_date DESC, id DESC;

-- name: OtherActiveMRR :one
-- The customer's MRR from other active contracts in the same currency, which decides whether a
-- change is new revenue, expansion or churn.
SELECT COALESCE(sum(mrr_minor), 0)::bigint FROM customer_expansion.contracts
WHERE tenant_id = @tenant_id AND customer_profile_id = @customer_profile_id AND id <> @exclude_id AND status IN ('active','renewing');

-- name: HadRevenue :one
SELECT EXISTS (SELECT 1 FROM customer_expansion.contracts WHERE tenant_id = @tenant_id AND customer_profile_id = @customer_profile_id AND id <> @exclude_id
  AND status IN ('active','renewing','ended','cancelled') AND mrr_minor > 0)::boolean;

-- name: ContractsEndingSoon :many
-- Runs as the system role, which may only see active contracts with an end date, to find renewals.
SELECT id, tenant_id FROM customer_expansion.contracts
WHERE status IN ('active','renewing') AND end_date IS NOT NULL AND end_date BETWEEN current_date AND current_date + 120
ORDER BY end_date LIMIT 2000;

-- name: InsertAlert :execrows
INSERT INTO customer_expansion.renewal_alerts (tenant_id, contract_id, window_days, due_date, kind, status)
VALUES (@tenant_id, @contract_id, @window_days, @due_date, @kind, 'open')
ON CONFLICT (tenant_id, contract_id, due_date, window_days) DO NOTHING;

-- name: ListAlerts :many
SELECT a.id, a.contract_id, a.window_days, a.due_date, a.kind, a.status, a.acknowledged_by, a.acknowledged_at, a.created_at,
  c.customer_profile_id, c.mrr_minor, c.currency, c.auto_renew, p.crm_company_id
FROM customer_expansion.renewal_alerts a
JOIN customer_expansion.contracts c ON c.tenant_id = a.tenant_id AND c.id = a.contract_id
JOIN customer_expansion.customer_profiles p ON p.tenant_id = c.tenant_id AND p.id = c.customer_profile_id
WHERE a.tenant_id = @tenant_id AND (sqlc.narg(status)::text IS NULL OR a.status = sqlc.narg(status))
  AND a.due_date <= current_date + @within_days::int
ORDER BY a.due_date, a.id LIMIT 500;

-- name: AcknowledgeAlert :execrows
UPDATE customer_expansion.renewal_alerts SET status = 'acknowledged', acknowledged_by = @user_id, acknowledged_at = now()
WHERE tenant_id = @tenant_id AND id = @id AND status = 'open';

-- name: GetAlert :one
SELECT id, contract_id, status FROM customer_expansion.renewal_alerts WHERE tenant_id = @tenant_id AND id = @id;

-- name: ResolveAlertsForContract :exec
-- A contract that ends, renews or changes dates no longer needs its old alerts.
UPDATE customer_expansion.renewal_alerts SET status = 'resolved' WHERE tenant_id = @tenant_id AND contract_id = @contract_id AND status <> 'resolved';

-- name: ListCoverage :many
SELECT id, tenant_service_id, contract_id, coverage, quantity, mrr_minor, currency, source, updated_at
FROM customer_expansion.service_inventory WHERE tenant_id = @tenant_id AND customer_profile_id = @customer_profile_id AND deleted_at IS NULL;

-- name: UpsertCoverage :exec
INSERT INTO customer_expansion.service_inventory (tenant_id, customer_profile_id, tenant_service_id, coverage, quantity, mrr_minor, currency, source)
VALUES (@tenant_id, @customer_profile_id, @tenant_service_id, @coverage, sqlc.narg(quantity), sqlc.narg(mrr_minor), sqlc.narg(currency), @source)
ON CONFLICT (tenant_id, customer_profile_id, tenant_service_id) WHERE deleted_at IS NULL DO UPDATE SET
  coverage = EXCLUDED.coverage, quantity = EXCLUDED.quantity, mrr_minor = EXCLUDED.mrr_minor, currency = EXCLUDED.currency, source = EXCLUDED.source;

-- name: DeleteCoverageExcept :exec
UPDATE customer_expansion.service_inventory SET deleted_at = now()
WHERE tenant_id = @tenant_id AND customer_profile_id = @customer_profile_id AND deleted_at IS NULL AND NOT (tenant_service_id = ANY(@keep::uuid[]));

-- name: PeerPrices :many
-- What the tenant's customers pay for each service, as the basis for estimates. Excludes one customer.
SELECT tenant_service_id, currency::text AS currency, count(*)::int AS customers,
  percentile_disc(0.5) WITHIN GROUP (ORDER BY mrr_minor)::bigint AS median_mrr_minor
FROM customer_expansion.service_inventory
WHERE tenant_id = @tenant_id AND customer_profile_id <> @exclude_profile AND deleted_at IS NULL AND coverage = 'active' AND mrr_minor IS NOT NULL
GROUP BY tenant_service_id, currency;

-- name: InsertIndicator :exec
INSERT INTO customer_expansion.health_indicators (tenant_id, customer_profile_id, indicator_key, value, polarity, evidence, as_of)
VALUES (@tenant_id, @customer_profile_id, @indicator_key, @value, @polarity, @evidence, @as_of);

-- name: LatestIndicators :many
SELECT DISTINCT ON (indicator_key) indicator_key, value, polarity, evidence, as_of
FROM customer_expansion.health_indicators WHERE tenant_id = @tenant_id AND customer_profile_id = @customer_profile_id
ORDER BY indicator_key, as_of DESC, id DESC;

-- name: LatestAssessment :one
SELECT value, evidence, as_of FROM customer_expansion.health_indicators
WHERE tenant_id = @tenant_id AND customer_profile_id = @customer_profile_id AND indicator_key = 'assessment_risk' AND evidence ? 'assessment_id'
ORDER BY as_of DESC, id DESC LIMIT 1;

-- name: InsertGap :exec
INSERT INTO customer_expansion.service_gaps (tenant_id, customer_profile_id, tenant_service_id, gap_state, analysis_run_id)
VALUES (@tenant_id, @customer_profile_id, @tenant_service_id, @gap_state, @analysis_run_id) ON CONFLICT DO NOTHING;

-- name: OpenRecommendationKeys :many
SELECT id, dedupe_key FROM customer_expansion.recommendations WHERE tenant_id = @tenant_id AND customer_profile_id = @customer_profile_id AND status = 'open';

-- name: DismissedKeys :many
-- A dismissed suggestion stays dismissed for 90 days, so analysis does not repeat it at once.
SELECT dedupe_key FROM customer_expansion.recommendations
WHERE tenant_id = @tenant_id AND customer_profile_id = @customer_profile_id AND status = 'dismissed' AND decided_at > now() - interval '90 days';

-- name: InsertRecommendation :one
INSERT INTO customer_expansion.recommendations (tenant_id, customer_profile_id, tenant_service_id, contract_id, kind, rationale, rule_rationale,
  estimated_mrr_minor, currency, evidence_class, status, dedupe_key)
VALUES (@tenant_id, @customer_profile_id, sqlc.narg(tenant_service_id), sqlc.narg(contract_id), @kind, @rationale, @rationale, sqlc.narg(estimated_mrr_minor),
  sqlc.narg(currency), @evidence_class, 'open', @dedupe_key)
RETURNING id;

-- name: RefreshRecommendation :exec
-- Same suggestion, fresh facts: the rule wording and estimate follow the data, and AI wording is redone.
UPDATE customer_expansion.recommendations SET rationale = @rationale, rule_rationale = @rationale, estimated_mrr_minor = sqlc.narg(estimated_mrr_minor),
  currency = sqlc.narg(currency), evidence_class = @evidence_class, ai_request_id = NULL
WHERE tenant_id = @tenant_id AND id = @id AND status = 'open';

-- name: DeleteEvidence :exec
DELETE FROM customer_expansion.recommendation_evidence WHERE tenant_id = @tenant_id AND recommendation_id = @recommendation_id;

-- name: InsertEvidence :exec
INSERT INTO customer_expansion.recommendation_evidence (tenant_id, recommendation_id, kind, reference_type, reference_id, detail)
VALUES (@tenant_id, @recommendation_id, @kind, sqlc.narg(reference_type), sqlc.narg(reference_id), @detail);

-- name: ExpireRecommendations :execrows
-- Open suggestions the latest analysis no longer supports.
UPDATE customer_expansion.recommendations SET status = 'expired'
WHERE tenant_id = @tenant_id AND customer_profile_id = @customer_profile_id AND status = 'open' AND NOT (id = ANY(@keep::uuid[]));

-- name: SetAIWording :execrows
-- Applies AI wording only if the rule wording has not changed since it was requested.
UPDATE customer_expansion.recommendations SET rationale = @rationale, ai_request_id = @ai_request_id
WHERE tenant_id = @tenant_id AND id = @id AND status = 'open' AND rule_rationale = @rule_rationale;

-- name: ListRecommendations :many
SELECT r.id, r.customer_profile_id, r.tenant_service_id, r.contract_id, r.kind, r.rationale, r.estimated_mrr_minor, r.currency, r.evidence_class,
  r.status, r.ai_request_id, r.decided_by, r.decided_at, r.created_at, p.crm_company_id
FROM customer_expansion.recommendations r
JOIN customer_expansion.customer_profiles p ON p.tenant_id = r.tenant_id AND p.id = r.customer_profile_id
WHERE r.tenant_id = @tenant_id AND (sqlc.narg(status)::text IS NULL OR r.status = sqlc.narg(status))
  AND (sqlc.narg(customer_profile_id)::uuid IS NULL OR r.customer_profile_id = sqlc.narg(customer_profile_id))
ORDER BY r.created_at DESC, r.id DESC LIMIT 500;

-- name: GetRecommendation :one
SELECT r.id, r.customer_profile_id, r.tenant_service_id, r.contract_id, r.kind, r.rationale, r.rule_rationale, r.estimated_mrr_minor, r.currency,
  r.evidence_class, r.status, r.ai_request_id, r.decided_by, r.decided_at, r.created_at, p.crm_company_id
FROM customer_expansion.recommendations r
JOIN customer_expansion.customer_profiles p ON p.tenant_id = r.tenant_id AND p.id = r.customer_profile_id
WHERE r.tenant_id = @tenant_id AND r.id = @id;

-- name: EvidenceFor :many
SELECT recommendation_id, kind, reference_type, reference_id, detail FROM customer_expansion.recommendation_evidence
WHERE tenant_id = @tenant_id AND recommendation_id = ANY(@ids::uuid[]) ORDER BY created_at, id;

-- name: DecideRecommendation :execrows
UPDATE customer_expansion.recommendations SET status = @status, decided_by = @decided_by, decided_at = now()
WHERE tenant_id = @tenant_id AND id = @id AND status = 'open';

-- name: SetRecommendationStatus :exec
UPDATE customer_expansion.recommendations SET status = @status WHERE tenant_id = @tenant_id AND id = @id;

-- name: InsertOpportunity :one
INSERT INTO customer_expansion.revenue_opportunities (tenant_id, recommendation_id, estimated_mrr_minor, currency, status)
VALUES (@tenant_id, @recommendation_id, sqlc.narg(estimated_mrr_minor), sqlc.narg(currency), 'open')
ON CONFLICT (tenant_id, recommendation_id) DO UPDATE SET updated_at = now()
RETURNING id, status, deal_id;

-- name: LinkOpportunityDeal :exec
UPDATE customer_expansion.revenue_opportunities SET deal_id = @deal_id, status = 'in_deal' WHERE tenant_id = @tenant_id AND id = @id AND status = 'open';

-- name: CountDealAttempt :one
UPDATE customer_expansion.revenue_opportunities SET deal_attempts = deal_attempts + 1 WHERE tenant_id = @tenant_id AND id = @id RETURNING deal_attempts;

-- name: OpportunityByDealForUpdate :one
SELECT id, recommendation_id, status, estimated_mrr_minor, currency FROM customer_expansion.revenue_opportunities
WHERE tenant_id = @tenant_id AND deal_id = @deal_id FOR UPDATE;

-- name: CloseOpportunity :exec
UPDATE customer_expansion.revenue_opportunities SET status = @status, realised_mrr_minor = sqlc.narg(realised_mrr_minor),
  currency = COALESCE(sqlc.narg(currency), currency)
WHERE tenant_id = @tenant_id AND id = @id;

-- name: ReopenOpportunity :exec
UPDATE customer_expansion.revenue_opportunities SET status = 'in_deal', realised_mrr_minor = NULL WHERE tenant_id = @tenant_id AND id = @id;

-- name: GetOpportunity :one
SELECT o.id, o.recommendation_id, o.deal_id, o.estimated_mrr_minor, o.realised_mrr_minor, o.currency, o.status, o.created_at, o.updated_at,
  r.customer_profile_id, r.kind, r.tenant_service_id, p.crm_company_id
FROM customer_expansion.revenue_opportunities o
JOIN customer_expansion.recommendations r ON r.tenant_id = o.tenant_id AND r.id = o.recommendation_id
JOIN customer_expansion.customer_profiles p ON p.tenant_id = r.tenant_id AND p.id = r.customer_profile_id
WHERE o.tenant_id = @tenant_id AND o.id = @id;

-- name: ListOpportunities :many
SELECT o.id, o.recommendation_id, o.deal_id, o.estimated_mrr_minor, o.realised_mrr_minor, o.currency, o.status, o.created_at, o.updated_at,
  r.customer_profile_id, r.kind, r.tenant_service_id, p.crm_company_id
FROM customer_expansion.revenue_opportunities o
JOIN customer_expansion.recommendations r ON r.tenant_id = o.tenant_id AND r.id = o.recommendation_id
JOIN customer_expansion.customer_profiles p ON p.tenant_id = r.tenant_id AND p.id = r.customer_profile_id
WHERE o.tenant_id = @tenant_id AND (sqlc.narg(status)::text IS NULL OR o.status = sqlc.narg(status))
ORDER BY o.created_at DESC, o.id DESC LIMIT 500;

-- name: OpportunitySummary :many
-- Estimated and realised amounts are kept apart: an estimate is never revenue.
SELECT status, COALESCE(currency, '')::text AS currency, count(*)::int AS count,
  COALESCE(sum(estimated_mrr_minor), 0)::bigint AS estimated_mrr_minor, COALESCE(sum(realised_mrr_minor), 0)::bigint AS realised_mrr_minor
FROM customer_expansion.revenue_opportunities WHERE tenant_id = @tenant_id
GROUP BY status, currency ORDER BY status, currency;

-- name: OpportunitiesNeedingDeal :many
SELECT id FROM customer_expansion.revenue_opportunities WHERE tenant_id = @tenant_id AND status = 'open' AND deal_id IS NULL AND deal_attempts < 10;

-- name: ProfilesForTenant :many
SELECT id FROM customer_expansion.customer_profiles WHERE tenant_id = @tenant_id AND deleted_at IS NULL;

-- name: CustomerCurrency :one
-- The currency the customer pays in now, for a deal opened without an estimate.
SELECT currency::text FROM customer_expansion.contracts
WHERE tenant_id = @tenant_id AND customer_profile_id = @customer_profile_id AND status IN ('active','renewing')
ORDER BY mrr_minor DESC, start_date DESC LIMIT 1;
