-- Outreach queries. Every tenant query filters on tenant_id, under RLS as well.

-- name: InsertDomain :one
INSERT INTO outreach.sending_domains (tenant_id, domain, spf_status, dkim_status, dmarc_status) VALUES (@tenant_id, @domain, 'unknown', 'unknown', 'unknown') RETURNING id;

-- name: GetDomain :one
SELECT id, domain, spf_status, dkim_status, dmarc_status, last_checked_at, created_at FROM outreach.sending_domains WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: ListDomains :many
SELECT id, domain, spf_status, dkim_status, dmarc_status, last_checked_at, created_at FROM outreach.sending_domains WHERE tenant_id = @tenant_id AND deleted_at IS NULL ORDER BY domain;

-- name: SetDomainChecks :exec
UPDATE outreach.sending_domains SET spf_status = @spf_status, dkim_status = @dkim_status, dmarc_status = @dmarc_status, last_checked_at = now() WHERE tenant_id = @tenant_id AND id = @id;

-- name: InsertSender :one
INSERT INTO outreach.sender_accounts (tenant_id, sending_domain_id, connection_id, from_email, from_name, status, daily_limit, reputation_state)
VALUES (@tenant_id, @sending_domain_id, @connection_id, @from_email, @from_name, 'active', @daily_limit, 'healthy') RETURNING id;

-- name: GetSender :one
SELECT s.id, s.sending_domain_id, s.connection_id, s.from_email, s.from_name, s.status, s.daily_limit, s.reputation_state, s.created_at,
  d.spf_status, d.dkim_status, d.dmarc_status
FROM outreach.sender_accounts s JOIN outreach.sending_domains d ON d.tenant_id = s.tenant_id AND d.id = s.sending_domain_id
WHERE s.tenant_id = @tenant_id AND s.id = @id AND s.deleted_at IS NULL;

-- name: ListSenders :many
SELECT s.id, s.sending_domain_id, s.from_email, s.from_name, s.status, s.daily_limit, s.reputation_state, s.created_at
FROM outreach.sender_accounts s WHERE s.tenant_id = @tenant_id AND s.deleted_at IS NULL ORDER BY s.from_email;

-- name: UpdateSender :execrows
UPDATE outreach.sender_accounts SET from_name = coalesce(sqlc.narg(from_name), from_name), status = coalesce(sqlc.narg(status), status),
  daily_limit = coalesce(sqlc.narg(daily_limit), daily_limit), reputation_state = coalesce(sqlc.narg(reputation_state), reputation_state)
WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: DeleteSender :execrows
UPDATE outreach.sender_accounts SET deleted_at = now(), status = 'disconnected' WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: InsertTemplate :one
INSERT INTO outreach.templates (tenant_id, name, created_by) VALUES (@tenant_id, @name, sqlc.narg(created_by)) RETURNING id;

-- name: InsertTemplateVersion :one
INSERT INTO outreach.template_versions (tenant_id, template_id, version, subject, body, ai_generated, ai_request_id)
VALUES (@tenant_id, @template_id, (SELECT coalesce(max(version), 0) + 1 FROM outreach.template_versions v WHERE v.tenant_id = @tenant_id AND v.template_id = @template_id),
  @subject, @body, @ai_generated, sqlc.narg(ai_request_id)) RETURNING id, version;

-- name: SetCurrentTemplateVersion :exec
UPDATE outreach.templates SET current_version_id = @version_id WHERE tenant_id = @tenant_id AND id = @id;

-- name: ListTemplates :many
SELECT t.id, t.name, t.current_version_id, t.updated_at, v.version, v.subject, v.body, v.ai_generated
FROM outreach.templates t LEFT JOIN outreach.template_versions v ON v.tenant_id = t.tenant_id AND v.id = t.current_version_id
WHERE t.tenant_id = @tenant_id AND t.deleted_at IS NULL ORDER BY t.name;

-- name: GetTemplate :one
SELECT id, name, current_version_id FROM outreach.templates WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: GetTemplateVersion :one
SELECT id, template_id, version, subject, body, ai_generated FROM outreach.template_versions WHERE tenant_id = @tenant_id AND id = @id;

-- name: InsertAudience :one
INSERT INTO outreach.audiences (tenant_id, name, kind, created_by) VALUES (@tenant_id, @name, 'static', sqlc.narg(created_by)) RETURNING id;

-- name: ListAudiences :many
SELECT a.id, a.name, a.kind, a.created_at,
  (SELECT count(*) FROM outreach.audience_members m WHERE m.tenant_id = a.tenant_id AND m.audience_id = a.id)::int AS members
FROM outreach.audiences a WHERE a.tenant_id = @tenant_id AND a.deleted_at IS NULL ORDER BY a.name;

-- name: GetAudience :one
SELECT id, name, kind FROM outreach.audiences WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: AddAudienceMember :exec
INSERT INTO outreach.audience_members (tenant_id, audience_id, contact_id) VALUES (@tenant_id, @audience_id, @contact_id) ON CONFLICT DO NOTHING;

-- name: AudienceMembers :many
SELECT contact_id FROM outreach.audience_members WHERE tenant_id = @tenant_id AND audience_id = @audience_id ORDER BY created_at LIMIT 20000;

-- name: InsertCampaign :one
INSERT INTO outreach.campaigns (tenant_id, name, sender_account_id, audience_id, status, approval_mode, timezone, sending_window, purpose, postal_address, created_by)
VALUES (@tenant_id, @name, @sender_account_id, sqlc.narg(audience_id), 'draft', 'campaign', @timezone, @sending_window, @purpose, @postal_address, sqlc.narg(created_by)) RETURNING id;

-- name: GetCampaign :one
SELECT id, name, sender_account_id, audience_id, status, approval_mode, timezone, sending_window, purpose, postal_address, created_at, updated_at
FROM outreach.campaigns WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: ListCampaigns :many
SELECT id, name, sender_account_id, audience_id, status, approval_mode, timezone, sending_window, purpose, postal_address, created_at, updated_at
FROM outreach.campaigns WHERE tenant_id = @tenant_id AND deleted_at IS NULL ORDER BY created_at DESC LIMIT 200;

-- name: UpdateCampaign :execrows
UPDATE outreach.campaigns SET name = coalesce(sqlc.narg(name), name), audience_id = coalesce(sqlc.narg(audience_id), audience_id), timezone = coalesce(sqlc.narg(timezone), timezone),
  sending_window = coalesce(sqlc.narg(sending_window), sending_window), purpose = coalesce(sqlc.narg(purpose), purpose), postal_address = coalesce(sqlc.narg(postal_address), postal_address),
  status = CASE WHEN status IN ('pending_approval', 'approved') THEN 'draft' ELSE status END
WHERE tenant_id = @tenant_id AND id = @id AND status IN ('draft', 'pending_approval', 'approved') AND deleted_at IS NULL;

-- name: SetCampaignStatus :execrows
UPDATE outreach.campaigns SET status = @status WHERE tenant_id = @tenant_id AND id = @id AND status = ANY(@from_statuses::text[]);

-- name: DeleteSteps :exec
DELETE FROM outreach.sequence_steps WHERE tenant_id = @tenant_id AND campaign_id = @campaign_id;

-- name: InsertStep :exec
INSERT INTO outreach.sequence_steps (tenant_id, campaign_id, position, delay_minutes, template_version_id) VALUES (@tenant_id, @campaign_id, @position, @delay_minutes, @template_version_id);

-- name: ListSteps :many
SELECT s.id, s.position, s.delay_minutes, s.template_version_id, v.subject, v.body, v.ai_generated
FROM outreach.sequence_steps s JOIN outreach.template_versions v ON v.tenant_id = s.tenant_id AND v.id = s.template_version_id
WHERE s.tenant_id = @tenant_id AND s.campaign_id = @campaign_id ORDER BY s.position;

-- name: InsertApproval :one
INSERT INTO outreach.approvals (tenant_id, subject_type, subject_id, status, content_hash, requested_by) VALUES (@tenant_id, 'campaign', @subject_id, 'pending', @content_hash, @requested_by) RETURNING id;

-- name: PendingApproval :one
SELECT id, content_hash, requested_by FROM outreach.approvals WHERE tenant_id = @tenant_id AND subject_type = 'campaign' AND subject_id = @subject_id AND status = 'pending' ORDER BY created_at DESC LIMIT 1;

-- name: DecideApproval :exec
UPDATE outreach.approvals SET status = @status, decided_by = @decided_by, decided_at = now(), reason = sqlc.narg(reason) WHERE tenant_id = @tenant_id AND id = @id AND status = 'pending';

-- name: ExpireApprovals :exec
UPDATE outreach.approvals SET status = 'expired' WHERE tenant_id = @tenant_id AND subject_type = 'campaign' AND subject_id = @subject_id AND status IN ('pending', 'approved');

-- name: ApprovedCampaign :one
SELECT id, content_hash FROM outreach.approvals WHERE tenant_id = @tenant_id AND subject_type = 'campaign' AND subject_id = @subject_id AND status = 'approved' ORDER BY decided_at DESC LIMIT 1;

-- name: ListPendingApprovals :many
SELECT a.id, a.subject_id, a.requested_by, a.created_at, c.name FROM outreach.approvals a
JOIN outreach.campaigns c ON c.tenant_id = a.tenant_id AND c.id = a.subject_id
WHERE a.tenant_id = @tenant_id AND a.status = 'pending' ORDER BY a.created_at;

-- name: Enroll :exec
INSERT INTO outreach.enrollments (tenant_id, campaign_id, contact_id, status, current_step, next_send_at)
VALUES (@tenant_id, @campaign_id, @contact_id, 'active', 1, @next_send_at) ON CONFLICT (tenant_id, campaign_id, contact_id) DO NOTHING;

-- name: DueEnrollments :many
-- Runs as the system role, which may only read active enrollments, to find work across tenants.
SELECT id, tenant_id, campaign_id, current_step FROM outreach.enrollments WHERE status = 'active' AND next_send_at <= now() ORDER BY next_send_at LIMIT 500;

-- name: GetEnrollment :one
SELECT id, campaign_id, contact_id, status, current_step, next_send_at, stop_reason FROM outreach.enrollments WHERE tenant_id = @tenant_id AND id = @id;

-- name: HoldEnrollment :exec
UPDATE outreach.enrollments SET next_send_at = @next_send_at WHERE tenant_id = @tenant_id AND id = @id AND status = 'active';

-- name: AdvanceEnrollment :exec
UPDATE outreach.enrollments SET current_step = current_step + 1, next_send_at = sqlc.narg(next_send_at), status = CASE WHEN sqlc.narg(next_send_at)::timestamptz IS NULL THEN 'completed' ELSE status END
WHERE tenant_id = @tenant_id AND id = @id AND status = 'active' AND current_step = @current_step;

-- name: StopEnrollment :exec
UPDATE outreach.enrollments SET status = 'stopped', stop_reason = @stop_reason, next_send_at = NULL WHERE tenant_id = @tenant_id AND id = @id AND status IN ('active', 'paused');

-- name: StopContactEnrollments :many
UPDATE outreach.enrollments SET status = 'stopped', stop_reason = @stop_reason, next_send_at = NULL WHERE tenant_id = @tenant_id AND contact_id = @contact_id AND status IN ('active', 'paused') RETURNING id;

-- name: StopCampaignEnrollments :exec
UPDATE outreach.enrollments SET status = 'stopped', stop_reason = 'campaign_cancelled', next_send_at = NULL WHERE tenant_id = @tenant_id AND campaign_id = @campaign_id AND status IN ('active', 'paused');

-- name: PauseCampaignEnrollments :exec
UPDATE outreach.enrollments SET status = 'paused' WHERE tenant_id = @tenant_id AND campaign_id = @campaign_id AND status = 'active';

-- name: ResumeCampaignEnrollments :exec
UPDATE outreach.enrollments SET status = 'active', next_send_at = greatest(coalesce(next_send_at, now()), now()) WHERE tenant_id = @tenant_id AND campaign_id = @campaign_id AND status = 'paused';

-- name: EnrollmentCounts :many
SELECT status, coalesce(stop_reason, '') AS stop_reason, count(*)::int AS n FROM outreach.enrollments WHERE tenant_id = @tenant_id AND campaign_id = @campaign_id GROUP BY 1, 2;

-- name: InsertMessage :one
INSERT INTO outreach.messages (id, tenant_id, enrollment_id, step_id, approval_id, subject, body, ai_generated, status, scheduled_at, idempotency_key)
VALUES (@id, @tenant_id, @enrollment_id, @step_id, sqlc.narg(approval_id), @subject, @body, false, 'sending', now(), @idempotency_key)
ON CONFLICT (tenant_id, idempotency_key) DO NOTHING RETURNING id;

-- name: FindMessageByKey :one
SELECT id, status FROM outreach.messages WHERE tenant_id = @tenant_id AND idempotency_key = @idempotency_key;

-- name: MarkSent :exec
UPDATE outreach.messages SET status = 'sent', sent_at = now(), provider_message_id = @provider_message_id WHERE tenant_id = @tenant_id AND id = @id;

-- name: MarkMessageFailed :exec
UPDATE outreach.messages SET status = 'failed' WHERE tenant_id = @tenant_id AND id = @id;

-- name: GetMessage :one
SELECT m.id, m.enrollment_id, m.subject, m.body, m.status, m.sent_at, m.provider_message_id, m.created_at, e.contact_id, e.campaign_id
FROM outreach.messages m JOIN outreach.enrollments e ON e.tenant_id = m.tenant_id AND e.id = m.enrollment_id WHERE m.tenant_id = @tenant_id AND m.id = @id;

-- name: ListMessages :many
SELECT m.id, m.subject, m.status, m.sent_at, m.created_at, e.contact_id, e.campaign_id
FROM outreach.messages m JOIN outreach.enrollments e ON e.tenant_id = m.tenant_id AND e.id = m.enrollment_id
WHERE m.tenant_id = @tenant_id AND (sqlc.narg(campaign_id)::uuid IS NULL OR e.campaign_id = sqlc.narg(campaign_id)::uuid) ORDER BY m.created_at DESC LIMIT 500;

-- name: FindProviderMessage :one
-- Runs as the system role, to learn which tenant a provider message belongs to.
SELECT id, tenant_id, enrollment_id FROM outreach.messages WHERE provider_message_id = @provider_message_id;

-- name: InsertDeliveryEvent :execrows
INSERT INTO outreach.delivery_events (tenant_id, message_id, provider_key, provider_event_id, event_type, detail)
VALUES (@tenant_id, sqlc.narg(message_id), @provider_key, @provider_event_id, @event_type, sqlc.narg(detail)) ON CONFLICT DO NOTHING;

-- name: InsertReply :execrows
INSERT INTO outreach.replies (tenant_id, enrollment_id, message_id, provider_message_id, classification, snippet, received_at)
VALUES (@tenant_id, sqlc.narg(enrollment_id), sqlc.narg(message_id), @provider_message_id, 'unclassified', sqlc.narg(snippet), now()) ON CONFLICT DO NOTHING;

-- name: ListReplies :many
SELECT r.id, r.message_id, r.classification, r.snippet, r.received_at, e.contact_id, e.campaign_id
FROM outreach.replies r LEFT JOIN outreach.enrollments e ON e.tenant_id = r.tenant_id AND e.id = r.enrollment_id
WHERE r.tenant_id = @tenant_id ORDER BY r.received_at DESC LIMIT 500;

-- name: ClassifyReply :execrows
UPDATE outreach.replies SET classification = @classification WHERE tenant_id = @tenant_id AND id = @id;

-- name: Suppress :exec
INSERT INTO outreach.suppressions (tenant_id, email_hash, reason, source, created_by) VALUES (@tenant_id, @email_hash, @reason, @source, sqlc.narg(created_by))
ON CONFLICT (tenant_id, email_hash) DO NOTHING;

-- name: IsSuppressed :one
SELECT EXISTS (SELECT 1 FROM outreach.suppressions WHERE tenant_id = @tenant_id AND email_hash = @email_hash);

-- name: IsGloballySuppressed :one
SELECT outreach.is_globally_suppressed(@hash::bytea)::bool AS suppressed;

-- name: ListSuppressions :many
SELECT id, reason, source, created_at FROM outreach.suppressions WHERE tenant_id = @tenant_id ORDER BY created_at DESC LIMIT 1000;

-- name: DeleteSuppression :execrows
DELETE FROM outreach.suppressions WHERE tenant_id = @tenant_id AND id = @id;

-- name: JurisdictionRules :many
SELECT country_code, rule FROM outreach.jurisdiction_rules;

-- name: SentToday :one
SELECT coalesce((SELECT sent_count FROM outreach.sending_quota_counters WHERE tenant_id = @tenant_id AND sender_account_id = @sender_account_id AND day = current_date), 0)::int AS sent;

-- name: CountSend :exec
INSERT INTO outreach.sending_quota_counters (tenant_id, sender_account_id, day, sent_count, bounce_count, complaint_count)
VALUES (@tenant_id, @sender_account_id, current_date, 1, 0, 0)
ON CONFLICT (tenant_id, sender_account_id, day) DO UPDATE SET sent_count = outreach.sending_quota_counters.sent_count + 1;

-- name: CountStat :exec
INSERT INTO outreach.campaign_stats (tenant_id, campaign_id, day, sent, delivered, bounced, replied, unsubscribed)
VALUES (@tenant_id, @campaign_id, current_date, @sent, @delivered, @bounced, @replied, @unsubscribed)
ON CONFLICT (tenant_id, campaign_id, day) DO UPDATE SET sent = outreach.campaign_stats.sent + EXCLUDED.sent, delivered = outreach.campaign_stats.delivered + EXCLUDED.delivered,
  bounced = outreach.campaign_stats.bounced + EXCLUDED.bounced, replied = outreach.campaign_stats.replied + EXCLUDED.replied, unsubscribed = outreach.campaign_stats.unsubscribed + EXCLUDED.unsubscribed;

-- name: CampaignTotals :one
SELECT coalesce(sum(sent), 0)::int AS sent, coalesce(sum(delivered), 0)::int AS delivered, coalesce(sum(bounced), 0)::int AS bounced,
  coalesce(sum(replied), 0)::int AS replied, coalesce(sum(unsubscribed), 0)::int AS unsubscribed
FROM outreach.campaign_stats WHERE tenant_id = @tenant_id AND campaign_id = @campaign_id;

-- name: InsertGlobalSuppression :exec
-- Runs as the system role when a provider reports a complaint (D-09).
INSERT INTO outreach.global_suppressions (email_hmac, reason) VALUES (@email_hmac, @reason) ON CONFLICT (email_hmac) DO NOTHING;

-- name: ClaimDue :one
-- Takes a due enrollment off the schedule, so the next tick does not queue it twice. The send job
-- always sets the next time, or stops the enrollment.
UPDATE outreach.enrollments SET next_send_at = NULL WHERE tenant_id = @tenant_id AND id = @id AND status = 'active' AND next_send_at <= now() RETURNING current_step;

-- name: LockEnrollment :one
SELECT id, campaign_id, contact_id, status, current_step FROM outreach.enrollments WHERE tenant_id = @tenant_id AND id = @id FOR UPDATE;

-- name: ContactEnrollments :many
SELECT id FROM outreach.enrollments WHERE tenant_id = @tenant_id AND contact_id = @contact_id;

-- name: RedactContactMessages :exec
UPDATE outreach.messages m SET subject = '[erased]', body = '[erased]' FROM outreach.enrollments e
WHERE e.tenant_id = m.tenant_id AND e.id = m.enrollment_id AND m.tenant_id = @tenant_id AND e.contact_id = @contact_id;

-- name: RedactContactReplies :exec
UPDATE outreach.replies r SET snippet = NULL FROM outreach.enrollments e
WHERE e.tenant_id = r.tenant_id AND e.id = r.enrollment_id AND r.tenant_id = @tenant_id AND e.contact_id = @contact_id;

-- name: AdminJurisdictionRules :many
-- Runs as the platform role.
SELECT country_code, rule, note, updated_by, updated_at FROM outreach.jurisdiction_rules ORDER BY country_code;

-- name: UpsertJurisdictionRule :exec
INSERT INTO outreach.jurisdiction_rules (country_code, rule, note, updated_by) VALUES (@country_code, @rule, sqlc.narg(note), @updated_by)
ON CONFLICT (country_code) DO UPDATE SET rule = EXCLUDED.rule, note = EXCLUDED.note, updated_by = EXCLUDED.updated_by;

-- name: DeleteJurisdictionRule :execrows
DELETE FROM outreach.jurisdiction_rules WHERE country_code = @country_code;

-- name: CountGlobalSuppressions :one
SELECT count(*)::int FROM outreach.global_suppressions;
