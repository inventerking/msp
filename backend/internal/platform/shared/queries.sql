-- name: FindCompanyByIdentifier :one
SELECT c.id, c.status, c.merged_into_id FROM shared.company_identifiers i
JOIN shared.companies c ON c.id = i.company_id
WHERE i.kind = @kind AND i.value_normalized = @value AND i.provider_key IS NOT DISTINCT FROM sqlc.narg(provider_key);

-- name: GetCompany :one
SELECT c.id, c.primary_domain, c.display_name, c.legal_name, c.country_code, c.industry_id, c.employee_band, c.status, c.merged_into_id
FROM shared.companies c WHERE c.id = @id;

-- name: InsertCompany :one
INSERT INTO shared.companies (primary_domain, display_name, status) VALUES (sqlc.narg(primary_domain), @display_name, 'active') RETURNING id;

-- name: InsertIdentifier :exec
INSERT INTO shared.company_identifiers (company_id, kind, value_normalized, provider_key) VALUES (@company_id, @kind, @value, sqlc.narg(provider_key))
ON CONFLICT (kind, value_normalized, provider_key) DO NOTHING;

-- name: InsertFact :exec
INSERT INTO shared.company_source_facts (company_id, attribute, value, source_key, source_url, licence_class, confidence, observed_at, expires_at)
VALUES (@company_id, @attribute, @value, @source_key, sqlc.narg(source_url), @licence_class, sqlc.arg(confidence)::float8, @observed_at, sqlc.narg(expires_at));

-- name: SearchCompanies :many
SELECT id, primary_domain, display_name, country_code, employee_band FROM shared.companies
WHERE status = 'active' AND (display_name ILIKE '%' || @query || '%' OR primary_domain::text ILIKE '%' || @query || '%')
ORDER BY display_name LIMIT @row_limit;

-- name: MergeSetStatus :execrows
UPDATE shared.companies SET status = 'merged', merged_into_id = @target_id, primary_domain = NULL WHERE id = @source_id AND status = 'active';

-- name: CompanyActive :one
SELECT count(*)::int FROM shared.companies WHERE id = @id AND status = 'active';

-- name: MergeMoveIdentifiers :exec
UPDATE shared.company_identifiers SET company_id = @target_id WHERE company_id = @source_id;

-- name: MergeMoveFacts :exec
UPDATE shared.company_source_facts SET company_id = @target_id WHERE company_id = @source_id;

-- name: InsertMerge :exec
INSERT INTO shared.company_merges (source_company_id, target_company_id, reason, decided_by) VALUES (@source_id, @target_id, @reason, @decided_by);

-- name: UpsertContact :one
INSERT INTO shared.contacts (tenant_id, company_id, full_name, first_name, last_name, job_title, email, email_hash, phone, social_url, country_code, status, created_by)
VALUES (@tenant_id, sqlc.narg(company_id), sqlc.narg(full_name), sqlc.narg(first_name), sqlc.narg(last_name), sqlc.narg(job_title), sqlc.narg(email), sqlc.narg(email_hash), sqlc.narg(phone), sqlc.narg(social_url), sqlc.narg(country_code), 'active', sqlc.narg(created_by))
ON CONFLICT (tenant_id, email) WHERE email IS NOT NULL
DO UPDATE SET full_name = COALESCE(shared.contacts.full_name, EXCLUDED.full_name), job_title = COALESCE(shared.contacts.job_title, EXCLUDED.job_title), company_id = COALESCE(shared.contacts.company_id, EXCLUDED.company_id)
RETURNING id;

-- name: InsertContactNoEmail :one
INSERT INTO shared.contacts (tenant_id, company_id, full_name, first_name, last_name, job_title, phone, social_url, country_code, status, created_by)
VALUES (@tenant_id, sqlc.narg(company_id), sqlc.narg(full_name), sqlc.narg(first_name), sqlc.narg(last_name), sqlc.narg(job_title), sqlc.narg(phone), sqlc.narg(social_url), sqlc.narg(country_code), 'active', sqlc.narg(created_by))
RETURNING id;

-- name: GetContact :one
SELECT id, company_id, full_name, first_name, last_name, job_title, email, phone, social_url, country_code, status, erased_at, created_at
FROM shared.contacts WHERE tenant_id = @tenant_id AND id = @id;

-- name: InsertContactSource :exec
INSERT INTO shared.contact_sources (tenant_id, contact_id, source_key, source_ref, licence_class, acquired_at) VALUES (@tenant_id, @contact_id, @source_key, sqlc.narg(source_ref), @licence_class, now());

-- name: ListContactSources :many
SELECT source_key, source_ref, licence_class, acquired_at FROM shared.contact_sources WHERE tenant_id = @tenant_id AND contact_id = @contact_id ORDER BY acquired_at;

-- name: InsertConsent :exec
INSERT INTO shared.contact_consents (tenant_id, contact_id, purpose, status, basis, jurisdiction, evidence, captured_at, withdrawn_at)
VALUES (@tenant_id, @contact_id, @purpose, @status, sqlc.narg(basis), sqlc.narg(jurisdiction), @evidence, now(), sqlc.narg(withdrawn_at));

-- name: ListConsents :many
SELECT purpose, status, basis, jurisdiction, captured_at, withdrawn_at FROM shared.contact_consents WHERE tenant_id = @tenant_id AND contact_id = @contact_id ORDER BY captured_at;

-- name: EraseContact :execrows
UPDATE shared.contacts SET full_name = NULL, first_name = NULL, last_name = NULL, job_title = NULL, email = NULL, email_hash = NULL, phone = NULL, social_url = NULL, company_id = NULL, status = 'erased', erased_at = now()
WHERE tenant_id = @tenant_id AND id = @id AND status = 'active';

-- name: DeleteContactSources :exec
DELETE FROM shared.contact_sources WHERE tenant_id = @tenant_id AND contact_id = @contact_id;

-- name: ScrubConsentEvidence :exec
UPDATE shared.contact_consents SET evidence = '{"erased":true}'::jsonb WHERE tenant_id = @tenant_id AND contact_id = @contact_id;

-- name: GetRate :one
SELECT rate::float8 AS rate FROM shared.fx_rates WHERE base_currency = @base AND quote_currency = @quote AND rate_date <= sqlc.arg(on_date)::timestamptz ORDER BY rate_date DESC LIMIT 1;

-- name: CountUnknownCountries :one
SELECT count(*)::int FROM unnest(@codes::text[]) AS c(code) WHERE NOT EXISTS (SELECT 1 FROM shared.countries s WHERE s.code = c.code);

-- name: CountUnknownIndustries :one
SELECT count(*)::int FROM unnest(@codes::text[]) AS c(code) WHERE NOT EXISTS (SELECT 1 FROM shared.industries s WHERE s.code = c.code);

-- name: ListCountries :many
SELECT code, name FROM shared.countries ORDER BY name;

-- name: ListIndustries :many
SELECT id, code, name FROM shared.industries ORDER BY name;

-- name: ContactsByIDs :many
SELECT id, company_id, full_name, job_title, email, phone, status, erased_at FROM shared.contacts
WHERE tenant_id = @tenant_id AND id = ANY(@ids::uuid[]);
