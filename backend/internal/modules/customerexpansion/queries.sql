-- Customer Expansion queries. In Phase 4 only the contract record (D-03) is built.

-- name: EnsureProfile :one
INSERT INTO customer_expansion.customer_profiles (tenant_id, crm_company_id, company_id, health_status)
VALUES (@tenant_id, @crm_company_id, sqlc.narg(company_id), 'unknown')
ON CONFLICT (tenant_id, crm_company_id) WHERE deleted_at IS NULL DO UPDATE SET updated_at = now()
RETURNING id;

-- name: InsertContractFromDeal :execrows
INSERT INTO customer_expansion.contracts (tenant_id, customer_profile_id, deal_id, status, start_date, currency, mrr_minor, setup_minor, source)
VALUES (@tenant_id, @customer_profile_id, @deal_id, 'active', @start_date, @currency, @mrr_minor, @setup_minor, 'won_deal')
ON CONFLICT (tenant_id, deal_id) DO UPDATE SET status = 'active', end_date = NULL, start_date = EXCLUDED.start_date,
  currency = EXCLUDED.currency, mrr_minor = EXCLUDED.mrr_minor, setup_minor = EXCLUDED.setup_minor
WHERE customer_expansion.contracts.status <> 'active';

-- name: EndContractForDeal :execrows
UPDATE customer_expansion.contracts SET status = 'cancelled', end_date = current_date
WHERE tenant_id = @tenant_id AND deal_id = @deal_id AND status = 'active';

-- name: ContractsForCompany :many
SELECT c.id, c.deal_id, c.status, c.start_date, c.currency, c.mrr_minor, c.setup_minor, c.source
FROM customer_expansion.contracts c JOIN customer_expansion.customer_profiles p ON p.tenant_id = c.tenant_id AND p.id = c.customer_profile_id
WHERE c.tenant_id = @tenant_id AND p.crm_company_id = @crm_company_id ORDER BY c.start_date DESC;
