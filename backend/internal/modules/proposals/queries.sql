-- Proposals queries. Every query filters on tenant_id, under RLS as well.

-- name: ListCatalog :many
SELECT id, name, description, pricing_model, currency, unit_price_minor, setup_fee_minor, status, updated_at FROM proposals.service_catalog_items
WHERE tenant_id = @tenant_id ORDER BY status, name;

-- name: GetCatalogItem :one
SELECT id, name, description, pricing_model, currency, unit_price_minor, setup_fee_minor, status, updated_at FROM proposals.service_catalog_items WHERE tenant_id = @tenant_id AND id = @id;

-- name: InsertCatalogItem :one
INSERT INTO proposals.service_catalog_items (tenant_id, name, description, pricing_model, currency, unit_price_minor, setup_fee_minor, status)
VALUES (@tenant_id, @name, sqlc.narg(description), @pricing_model, @currency, @unit_price_minor, @setup_fee_minor, 'active') RETURNING id;

-- name: UpdateCatalogItem :execrows
UPDATE proposals.service_catalog_items SET name = coalesce(sqlc.narg(name), name), description = coalesce(sqlc.narg(description), description),
  unit_price_minor = coalesce(sqlc.narg(unit_price_minor), unit_price_minor), setup_fee_minor = coalesce(sqlc.narg(setup_fee_minor), setup_fee_minor),
  status = coalesce(sqlc.narg(status), status)
WHERE tenant_id = @tenant_id AND id = @id;

-- name: ListTextTemplates :many
SELECT 'sla'::text AS kind, s.id, s.name, s.body, s.version, s.updated_at FROM proposals.sla_templates s WHERE s.tenant_id = @tenant_id AND s.deleted_at IS NULL
UNION ALL
SELECT 'terms'::text AS kind, t.id, t.name, t.body, t.version, t.updated_at FROM proposals.terms_templates t WHERE t.tenant_id = @tenant_id AND t.deleted_at IS NULL
ORDER BY 1, 3;

-- name: InsertSLA :one
INSERT INTO proposals.sla_templates (tenant_id, name, body) VALUES (@tenant_id, @name, @body) RETURNING id;

-- name: InsertTerms :one
INSERT INTO proposals.terms_templates (tenant_id, name, body) VALUES (@tenant_id, @name, @body) RETURNING id;

-- name: UpdateSLA :execrows
UPDATE proposals.sla_templates SET name = coalesce(sqlc.narg(name), name), body = coalesce(sqlc.narg(body), body), version = version + 1
WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: UpdateTerms :execrows
UPDATE proposals.terms_templates SET name = coalesce(sqlc.narg(name), name), body = coalesce(sqlc.narg(body), body), version = version + 1
WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: GetSLA :one
SELECT body FROM proposals.sla_templates WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: GetTerms :one
SELECT body FROM proposals.terms_templates WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: ListProposalTemplates :many
SELECT id, name, sections, default_sla_id, default_terms_id, updated_at FROM proposals.proposal_templates WHERE tenant_id = @tenant_id AND deleted_at IS NULL ORDER BY name;

-- name: GetProposalTemplate :one
SELECT id, name, sections, default_sla_id, default_terms_id FROM proposals.proposal_templates WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: InsertProposalTemplate :one
INSERT INTO proposals.proposal_templates (tenant_id, name, sections, branding, default_sla_id, default_terms_id)
VALUES (@tenant_id, @name, @sections, '{}', sqlc.narg(default_sla_id), sqlc.narg(default_terms_id)) RETURNING id;

-- name: UpdateProposalTemplate :execrows
UPDATE proposals.proposal_templates SET name = coalesce(sqlc.narg(name), name), sections = coalesce(sqlc.narg(sections), sections),
  default_sla_id = CASE WHEN @set_sla::bool THEN sqlc.narg(default_sla_id) ELSE default_sla_id END,
  default_terms_id = CASE WHEN @set_terms::bool THEN sqlc.narg(default_terms_id) ELSE default_terms_id END
WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: LockNumbering :one
SELECT pg_advisory_xact_lock(hashtextextended(@key::text, 0))::text AS locked;

-- name: CountProposalsThisYear :one
SELECT count(*)::int FROM proposals.proposals WHERE tenant_id = @tenant_id AND number LIKE @prefix::text || '%';

-- name: InsertProposal :one
INSERT INTO proposals.proposals (tenant_id, number, title, crm_company_id, deal_id, template_id, status, owner_user_id, created_by)
VALUES (@tenant_id, @number, @title, @crm_company_id, sqlc.narg(deal_id), sqlc.narg(template_id), 'draft', sqlc.narg(owner_user_id), sqlc.narg(created_by)) RETURNING id;

-- name: GetProposal :one
SELECT id, number, title, crm_company_id, deal_id, template_id, current_version_id, status, owner_user_id, created_at, updated_at
FROM proposals.proposals WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: ListProposals :many
SELECT p.id, p.number, p.title, p.crm_company_id, p.deal_id, p.status, p.owner_user_id, p.created_at, p.updated_at,
  v.version, v.currency, v.mrr_total_minor, v.tcv_minor
FROM proposals.proposals p
LEFT JOIN proposals.proposal_versions v ON v.tenant_id = p.tenant_id AND v.id = p.current_version_id
WHERE p.tenant_id = @tenant_id AND p.deleted_at IS NULL
  AND (sqlc.narg(status)::text IS NULL OR p.status = sqlc.narg(status)::text)
  AND (sqlc.narg(crm_company_id)::uuid IS NULL OR p.crm_company_id = sqlc.narg(crm_company_id)::uuid)
ORDER BY p.updated_at DESC LIMIT 500;

-- name: SetProposalStatus :exec
UPDATE proposals.proposals SET status = @status WHERE tenant_id = @tenant_id AND id = @id;

-- name: SetCurrentVersion :exec
UPDATE proposals.proposals SET current_version_id = @version_id, status = @status WHERE tenant_id = @tenant_id AND id = @id;

-- name: DeleteProposal :execrows
UPDATE proposals.proposals SET deleted_at = now() WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL AND status IN ('draft', 'in_review');

-- name: InsertVersion :one
INSERT INTO proposals.proposal_versions (tenant_id, proposal_id, version, status, content, currency, setup_total_minor, mrr_total_minor, term_months, tcv_minor, valid_until, ai_request_id, created_by)
VALUES (@tenant_id, @proposal_id, (SELECT coalesce(max(version), 0) + 1 FROM proposals.proposal_versions v WHERE v.tenant_id = @tenant_id AND v.proposal_id = @proposal_id),
  'draft', @content, @currency, @setup_total_minor, @mrr_total_minor, @term_months, @tcv_minor, sqlc.narg(valid_until), sqlc.narg(ai_request_id), sqlc.narg(created_by))
RETURNING id, version;

-- name: GetVersion :one
SELECT id, proposal_id, version, status, content, commercial_snapshot, snapshot_hash, currency, setup_total_minor, mrr_total_minor, term_months, tcv_minor,
  valid_until, pdf_file_id, issued_at, ai_request_id, created_by, created_at, updated_at
FROM proposals.proposal_versions WHERE tenant_id = @tenant_id AND proposal_id = @proposal_id AND version = @version;

-- name: GetVersionByID :one
SELECT id, proposal_id, version, status, content, commercial_snapshot, snapshot_hash, currency, setup_total_minor, mrr_total_minor, term_months, tcv_minor,
  valid_until, pdf_file_id, issued_at, ai_request_id, created_by, created_at, updated_at
FROM proposals.proposal_versions WHERE tenant_id = @tenant_id AND id = @id;

-- name: ListVersions :many
SELECT id, version, status, currency, mrr_total_minor, setup_total_minor, tcv_minor, term_months, valid_until, issued_at, created_at
FROM proposals.proposal_versions WHERE tenant_id = @tenant_id AND proposal_id = @proposal_id ORDER BY version DESC;

-- name: UpdateDraftVersion :execrows
UPDATE proposals.proposal_versions SET content = @content, currency = @currency, setup_total_minor = @setup_total_minor, mrr_total_minor = @mrr_total_minor,
  term_months = @term_months, tcv_minor = @tcv_minor, valid_until = sqlc.narg(valid_until), ai_request_id = sqlc.narg(ai_request_id)
WHERE tenant_id = @tenant_id AND id = @id AND status = 'draft';

-- name: SetVersionStatus :execrows
UPDATE proposals.proposal_versions SET status = @status WHERE tenant_id = @tenant_id AND id = @id AND status = @from_status;

-- name: IssueVersion :execrows
UPDATE proposals.proposal_versions SET status = 'issued', commercial_snapshot = @commercial_snapshot, snapshot_hash = @snapshot_hash, issued_at = now()
WHERE tenant_id = @tenant_id AND id = @id AND status IN ('draft', 'approved');

-- name: SupersedeOthers :exec
UPDATE proposals.proposal_versions SET status = 'superseded' WHERE tenant_id = @tenant_id AND proposal_id = @proposal_id AND id <> @keep AND status = 'issued';

-- name: SetVersionPDF :exec
UPDATE proposals.proposal_versions SET pdf_file_id = @pdf_file_id WHERE tenant_id = @tenant_id AND id = @id AND pdf_file_id IS NULL;

-- name: DeleteLines :exec
DELETE FROM proposals.proposal_line_items WHERE tenant_id = @tenant_id AND version_id = @version_id;

-- name: InsertLine :exec
INSERT INTO proposals.proposal_line_items (tenant_id, version_id, catalog_item_id, position, name, pricing_model, quantity, unit_price_minor, setup_fee_minor, discount_bps, recurring_total_minor)
VALUES (@tenant_id, @version_id, sqlc.narg(catalog_item_id), @position, @name, @pricing_model, @quantity, @unit_price_minor, @setup_fee_minor, @discount_bps, @recurring_total_minor);

-- name: ListLines :many
SELECT catalog_item_id, position, name, pricing_model, quantity, unit_price_minor, setup_fee_minor, discount_bps, recurring_total_minor
FROM proposals.proposal_line_items WHERE tenant_id = @tenant_id AND version_id = @version_id ORDER BY position;

-- name: InsertApproval :one
INSERT INTO proposals.approvals (tenant_id, version_id, status, content_hash, requested_by) VALUES (@tenant_id, @version_id, 'pending', @content_hash, @requested_by) RETURNING id;

-- name: PendingApproval :one
SELECT id, content_hash, requested_by FROM proposals.approvals WHERE tenant_id = @tenant_id AND version_id = @version_id AND status = 'pending' ORDER BY created_at DESC LIMIT 1;

-- name: DecideApproval :exec
UPDATE proposals.approvals SET status = @status, decided_by = @decided_by, decided_at = now(), reason = sqlc.narg(reason) WHERE tenant_id = @tenant_id AND id = @id AND status = 'pending';

-- name: ApprovedHash :one
SELECT content_hash FROM proposals.approvals WHERE tenant_id = @tenant_id AND version_id = @version_id AND status = 'approved' ORDER BY decided_at DESC LIMIT 1;

-- name: InsertShareLink :one
INSERT INTO proposals.share_links (id, tenant_id, version_id, recipient_email, token_hash, expires_at) VALUES (@id, @tenant_id, @version_id, @recipient_email, @token_hash, @expires_at) RETURNING id;

-- name: LinksForVersion :many
SELECT id, recipient_email FROM proposals.share_links WHERE tenant_id = @tenant_id AND version_id = @version_id AND revoked_at IS NULL ORDER BY created_at;

-- name: ListShareLinks :many
SELECT l.id, l.version_id, l.recipient_email, l.expires_at, l.revoked_at, l.created_at,
  (SELECT count(*) FROM proposals.view_events e WHERE e.tenant_id = l.tenant_id AND e.share_link_id = l.id)::int AS views
FROM proposals.share_links l JOIN proposals.proposal_versions v ON v.tenant_id = l.tenant_id AND v.id = l.version_id
WHERE l.tenant_id = @tenant_id AND v.proposal_id = @proposal_id ORDER BY l.created_at DESC;

-- name: RevokeShareLink :execrows
UPDATE proposals.share_links SET revoked_at = now() WHERE tenant_id = @tenant_id AND id = @id AND revoked_at IS NULL;

-- name: FindShareLink :one
-- Runs as the system role, to learn which tenant a public token belongs to.
SELECT id, tenant_id, version_id, expires_at, revoked_at FROM proposals.share_links WHERE token_hash = @token_hash;

-- name: RecentView :one
SELECT EXISTS (SELECT 1 FROM proposals.view_events WHERE tenant_id = @tenant_id AND share_link_id = @share_link_id AND viewed_at > now() - interval '10 minutes');

-- name: InsertView :exec
INSERT INTO proposals.view_events (tenant_id, share_link_id, viewed_at, ip_hash) VALUES (@tenant_id, @share_link_id, now(), sqlc.narg(ip_hash));

-- name: ListViews :many
SELECT e.viewed_at, l.recipient_email FROM proposals.view_events e JOIN proposals.share_links l ON l.tenant_id = e.tenant_id AND l.id = e.share_link_id
JOIN proposals.proposal_versions v ON v.tenant_id = l.tenant_id AND v.id = l.version_id
WHERE e.tenant_id = @tenant_id AND v.proposal_id = @proposal_id ORDER BY e.viewed_at DESC LIMIT 200;

-- name: InsertSignatureRequest :one
INSERT INTO proposals.signature_requests (tenant_id, version_id, provider_key, status, idempotency_key)
VALUES (@tenant_id, @version_id, @provider_key, 'created', @idempotency_key)
ON CONFLICT (tenant_id, idempotency_key) DO NOTHING RETURNING id;

-- name: FindSignatureRequestByKey :one
SELECT id FROM proposals.signature_requests WHERE tenant_id = @tenant_id AND idempotency_key = @idempotency_key;

-- name: GetSignatureRequest :one
SELECT id, version_id, provider_key, provider_envelope_id, status, created_at, updated_at FROM proposals.signature_requests WHERE tenant_id = @tenant_id AND id = @id;

-- name: SetEnvelope :exec
UPDATE proposals.signature_requests SET provider_envelope_id = @provider_envelope_id, status = 'sent' WHERE tenant_id = @tenant_id AND id = @id;

-- name: SetSignatureStatus :exec
UPDATE proposals.signature_requests SET status = @status WHERE tenant_id = @tenant_id AND id = @id;

-- name: FindEnvelope :one
-- Runs as the system role, to learn which tenant a provider's envelope belongs to.
SELECT id, tenant_id FROM proposals.signature_requests WHERE provider_key = @provider_key AND provider_envelope_id = @provider_envelope_id;

-- name: InsertSignatureEvent :execrows
INSERT INTO proposals.signature_events (tenant_id, signature_request_id, provider_event_id, event_type, detail)
VALUES (@tenant_id, @signature_request_id, @provider_event_id, @event_type, sqlc.narg(detail))
ON CONFLICT (tenant_id, provider_event_id) DO NOTHING;

-- name: ListSignatureEvents :many
SELECT e.event_type, e.occurred_at, r.status FROM proposals.signature_events e
JOIN proposals.signature_requests r ON r.tenant_id = e.tenant_id AND r.id = e.signature_request_id
JOIN proposals.proposal_versions v ON v.tenant_id = r.tenant_id AND v.id = r.version_id
WHERE e.tenant_id = @tenant_id AND v.proposal_id = @proposal_id ORDER BY e.occurred_at DESC LIMIT 200;

-- name: MarkAIAssisted :exec
UPDATE proposals.proposal_versions SET ai_request_id = @ai_request_id WHERE tenant_id = @tenant_id AND id = @id AND status = 'draft';
