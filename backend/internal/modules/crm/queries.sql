-- CRM queries. Every query filters on tenant_id, under RLS as well.

-- name: FindCompanyByCanonical :one
SELECT id FROM crm.companies WHERE tenant_id = @tenant_id AND company_id = @company_id AND deleted_at IS NULL;

-- name: FindCompanyBySource :one
SELECT id FROM crm.companies WHERE tenant_id = @tenant_id AND source_module = @source_module AND source_ref = @source_ref AND deleted_at IS NULL;

-- name: InsertCompany :one
INSERT INTO crm.companies (tenant_id, company_id, name, domain, lifecycle, owner_user_id, source_module, source_ref, created_by)
VALUES (@tenant_id, sqlc.narg(company_id), @name, sqlc.narg(domain), @lifecycle, sqlc.narg(owner_user_id), sqlc.narg(source_module), sqlc.narg(source_ref), sqlc.narg(created_by))
RETURNING id;

-- name: GetCompany :one
SELECT id, company_id, name, domain, lifecycle, owner_user_id, source_module, created_at, updated_at
FROM crm.companies WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: ListCompanies :many
SELECT c.id, c.company_id, c.name, c.domain, c.lifecycle, c.owner_user_id, c.source_module, c.created_at, c.updated_at,
  (SELECT count(*) FROM crm.deals d WHERE d.tenant_id = c.tenant_id AND d.crm_company_id = c.id AND d.status = 'open' AND d.deleted_at IS NULL)::int AS open_deals
FROM crm.companies c
WHERE c.tenant_id = @tenant_id AND c.deleted_at IS NULL
  AND (sqlc.narg(lifecycle)::text IS NULL OR c.lifecycle = sqlc.narg(lifecycle)::text)
  AND (sqlc.narg(q)::text IS NULL OR c.name ILIKE '%' || sqlc.narg(q)::text || '%' OR c.domain::text ILIKE '%' || sqlc.narg(q)::text || '%')
  AND (sqlc.narg(before)::uuid IS NULL OR c.id < sqlc.narg(before)::uuid)
ORDER BY c.id DESC LIMIT @lim;

-- name: UpdateCompany :execrows
UPDATE crm.companies SET name = coalesce(sqlc.narg(name), name), lifecycle = coalesce(sqlc.narg(lifecycle), lifecycle),
  owner_user_id = CASE WHEN @set_owner::bool THEN sqlc.narg(owner_user_id) ELSE owner_user_id END
WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: DeleteCompany :execrows
UPDATE crm.companies SET deleted_at = now() WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: CompanyOpenDeals :one
SELECT count(*)::int FROM crm.deals WHERE tenant_id = @tenant_id AND crm_company_id = @crm_company_id AND status = 'open' AND deleted_at IS NULL;

-- name: FindContact :one
SELECT id FROM crm.contacts WHERE tenant_id = @tenant_id AND contact_id = @contact_id AND deleted_at IS NULL;

-- name: InsertContact :one
INSERT INTO crm.contacts (tenant_id, contact_id, crm_company_id, is_primary, owner_user_id)
VALUES (@tenant_id, @contact_id, sqlc.narg(crm_company_id), @is_primary, sqlc.narg(owner_user_id)) RETURNING id;

-- name: GetContact :one
SELECT id, contact_id, crm_company_id, is_primary, owner_user_id, created_at, updated_at
FROM crm.contacts WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: ListContacts :many
SELECT id, contact_id, crm_company_id, is_primary, owner_user_id, created_at, updated_at FROM crm.contacts
WHERE tenant_id = @tenant_id AND deleted_at IS NULL AND (sqlc.narg(crm_company_id)::uuid IS NULL OR crm_company_id = sqlc.narg(crm_company_id)::uuid)
ORDER BY is_primary DESC, id DESC LIMIT 500;

-- name: UpdateContact :execrows
UPDATE crm.contacts SET crm_company_id = CASE WHEN @set_company::bool THEN sqlc.narg(crm_company_id) ELSE crm_company_id END,
  is_primary = coalesce(sqlc.narg(is_primary), is_primary)
WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: DeleteContact :execrows
UPDATE crm.contacts SET deleted_at = now() WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: EraseContactLinks :exec
UPDATE crm.contacts SET deleted_at = now() WHERE tenant_id = @tenant_id AND contact_id = @contact_id AND deleted_at IS NULL;

-- name: EraseAttendee :exec
DELETE FROM crm.meeting_attendees WHERE tenant_id = @tenant_id AND contact_id = ANY(@ids::uuid[]);

-- name: EraseActivityContact :exec
UPDATE crm.activities SET contact_id = NULL WHERE tenant_id = @tenant_id AND contact_id = ANY(@ids::uuid[]);

-- name: ClearDealContact :exec
UPDATE crm.deals SET primary_contact_id = NULL WHERE tenant_id = @tenant_id AND primary_contact_id = ANY(@ids::uuid[]);

-- name: FindContactCompany :one
SELECT id, crm_company_id FROM crm.contacts WHERE tenant_id = @tenant_id AND contact_id = @contact_id AND deleted_at IS NULL ORDER BY is_primary DESC, created_at LIMIT 1;

-- name: CrmContactIDsFor :many
SELECT id FROM crm.contacts WHERE tenant_id = @tenant_id AND contact_id = @contact_id;

-- name: InsertConversion :one
INSERT INTO crm.prospect_conversions (tenant_id, prospect_id, crm_company_id, converted_by)
VALUES (@tenant_id, @prospect_id, @crm_company_id, sqlc.narg(converted_by))
ON CONFLICT (tenant_id, prospect_id) DO NOTHING RETURNING crm_company_id;

-- name: GetConversion :one
SELECT crm_company_id FROM crm.prospect_conversions WHERE tenant_id = @tenant_id AND prospect_id = @prospect_id;

-- Pipelines.

-- name: ListPipelines :many
SELECT id, name, is_default, status, created_at FROM crm.pipelines WHERE tenant_id = @tenant_id ORDER BY is_default DESC, name;

-- name: GetPipeline :one
SELECT id, name, is_default, status FROM crm.pipelines WHERE tenant_id = @tenant_id AND id = @id;

-- name: DefaultPipeline :one
SELECT id FROM crm.pipelines WHERE tenant_id = @tenant_id AND is_default AND status = 'active' LIMIT 1;

-- name: InsertPipeline :one
INSERT INTO crm.pipelines (tenant_id, name, is_default, status) VALUES (@tenant_id, @name, @is_default, 'active') RETURNING id;

-- name: UpdatePipeline :execrows
UPDATE crm.pipelines SET name = coalesce(sqlc.narg(name), name), status = coalesce(sqlc.narg(status), status) WHERE tenant_id = @tenant_id AND id = @id;

-- name: ListStages :many
SELECT id, pipeline_id, key, name, position, kind, default_probability FROM crm.pipeline_stages
WHERE tenant_id = @tenant_id AND pipeline_id = @pipeline_id ORDER BY position;

-- name: GetStage :one
SELECT id, pipeline_id, key, name, position, kind, default_probability FROM crm.pipeline_stages WHERE tenant_id = @tenant_id AND id = @id;

-- name: InsertStage :one
INSERT INTO crm.pipeline_stages (tenant_id, pipeline_id, key, name, position, kind, default_probability)
VALUES (@tenant_id, @pipeline_id, @key, @name, @position, @kind, @default_probability) RETURNING id;

-- name: UpdateStage :exec
UPDATE crm.pipeline_stages SET name = @name, position = @position, kind = @kind, default_probability = @default_probability
WHERE tenant_id = @tenant_id AND id = @id;

-- name: DeleteStage :exec
DELETE FROM crm.pipeline_stages WHERE tenant_id = @tenant_id AND id = @id;

-- name: StageInUse :one
SELECT (EXISTS (SELECT 1 FROM crm.deals d WHERE d.tenant_id = @tenant_id AND d.stage_id = @stage_id)
    OR EXISTS (SELECT 1 FROM crm.deal_stage_history h WHERE h.tenant_id = @tenant_id AND (h.to_stage_id = @stage_id OR h.from_stage_id = @stage_id)))::bool AS in_use;

-- Deals.

-- name: InsertDeal :one
INSERT INTO crm.deals (tenant_id, pipeline_id, stage_id, crm_company_id, primary_contact_id, title, owner_user_id, currency, contract_value_minor, expected_mrr_minor,
  probability, expected_close_date, status, source_module, source_ref, row_version, created_by)
VALUES (@tenant_id, @pipeline_id, @stage_id, @crm_company_id, sqlc.narg(primary_contact_id), @title, sqlc.narg(owner_user_id), @currency, sqlc.narg(contract_value_minor), sqlc.narg(expected_mrr_minor),
  @probability, sqlc.narg(expected_close_date), 'open', sqlc.narg(source_module), sqlc.narg(source_ref), 1, sqlc.narg(created_by))
RETURNING id;

-- name: GetDeal :one
SELECT d.id, d.pipeline_id, d.stage_id, d.crm_company_id, d.primary_contact_id, d.title, d.owner_user_id, d.currency, d.contract_value_minor, d.expected_mrr_minor,
  d.probability, d.expected_close_date, d.status, d.closed_at, d.loss_reason, d.source_module, d.row_version, d.created_at, d.updated_at,
  c.name AS company_name, s.name AS stage_name, s.kind AS stage_kind, s.position AS stage_position
FROM crm.deals d
JOIN crm.companies c ON c.tenant_id = d.tenant_id AND c.id = d.crm_company_id
JOIN crm.pipeline_stages s ON s.tenant_id = d.tenant_id AND s.id = d.stage_id
WHERE d.tenant_id = @tenant_id AND d.id = @id AND d.deleted_at IS NULL;

-- name: ListDeals :many
SELECT d.id, d.pipeline_id, d.stage_id, d.crm_company_id, d.title, d.owner_user_id, d.currency, d.contract_value_minor, d.expected_mrr_minor,
  d.probability, d.expected_close_date, d.status, d.row_version, d.updated_at, c.name AS company_name, s.name AS stage_name
FROM crm.deals d
JOIN crm.companies c ON c.tenant_id = d.tenant_id AND c.id = d.crm_company_id
JOIN crm.pipeline_stages s ON s.tenant_id = d.tenant_id AND s.id = d.stage_id
WHERE d.tenant_id = @tenant_id AND d.deleted_at IS NULL
  AND (sqlc.narg(status)::text IS NULL OR d.status = sqlc.narg(status)::text)
  AND (sqlc.narg(pipeline_id)::uuid IS NULL OR d.pipeline_id = sqlc.narg(pipeline_id)::uuid)
  AND (sqlc.narg(crm_company_id)::uuid IS NULL OR d.crm_company_id = sqlc.narg(crm_company_id)::uuid)
ORDER BY d.updated_at DESC, d.id DESC LIMIT 500;

-- name: UpdateDeal :one
UPDATE crm.deals SET title = coalesce(sqlc.narg(title), title), contract_value_minor = CASE WHEN @set_value::bool THEN sqlc.narg(contract_value_minor) ELSE contract_value_minor END,
  expected_mrr_minor = CASE WHEN @set_mrr::bool THEN sqlc.narg(expected_mrr_minor) ELSE expected_mrr_minor END,
  probability = coalesce(sqlc.narg(probability), probability),
  expected_close_date = CASE WHEN @set_close::bool THEN sqlc.narg(expected_close_date) ELSE expected_close_date END,
  owner_user_id = CASE WHEN @set_owner::bool THEN sqlc.narg(owner_user_id) ELSE owner_user_id END,
  primary_contact_id = CASE WHEN @set_contact::bool THEN sqlc.narg(primary_contact_id) ELSE primary_contact_id END,
  currency = coalesce(sqlc.narg(currency), currency),
  row_version = row_version + 1
WHERE tenant_id = @tenant_id AND id = @id AND row_version = @row_version AND deleted_at IS NULL RETURNING row_version;

-- name: MoveDeal :one
UPDATE crm.deals SET stage_id = @stage_id, probability = @probability, row_version = row_version + 1
WHERE tenant_id = @tenant_id AND id = @id AND row_version = @row_version AND status = 'open' AND deleted_at IS NULL RETURNING row_version;

-- name: CloseDeal :one
UPDATE crm.deals SET stage_id = @stage_id, status = @status, closed_at = now(), loss_reason = sqlc.narg(loss_reason), probability = @probability, row_version = row_version + 1
WHERE tenant_id = @tenant_id AND id = @id AND row_version = @row_version AND status = 'open' AND deleted_at IS NULL RETURNING row_version;

-- name: ReopenDeal :one
UPDATE crm.deals SET stage_id = @stage_id, status = 'open', closed_at = NULL, loss_reason = NULL, probability = @probability, row_version = row_version + 1
WHERE tenant_id = @tenant_id AND id = @id AND status <> 'open' AND deleted_at IS NULL RETURNING row_version;

-- name: DeleteDeal :execrows
UPDATE crm.deals SET deleted_at = now() WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: InsertStageHistory :exec
INSERT INTO crm.deal_stage_history (tenant_id, deal_id, from_stage_id, to_stage_id, changed_by, reason)
VALUES (@tenant_id, @deal_id, sqlc.narg(from_stage_id), @to_stage_id, sqlc.narg(changed_by), sqlc.narg(reason));

-- name: StageHistory :many
SELECT h.from_stage_id, h.to_stage_id, s.name AS to_stage_name, h.changed_by, h.reason, h.created_at
FROM crm.deal_stage_history h JOIN crm.pipeline_stages s ON s.tenant_id = h.tenant_id AND s.id = h.to_stage_id
WHERE h.tenant_id = @tenant_id AND h.deal_id = @deal_id ORDER BY h.created_at DESC, h.id DESC LIMIT 100;

-- name: LastOpenStage :one
SELECT h.to_stage_id FROM crm.deal_stage_history h JOIN crm.pipeline_stages s ON s.tenant_id = h.tenant_id AND s.id = h.to_stage_id
WHERE h.tenant_id = @tenant_id AND h.deal_id = @deal_id AND s.kind = 'open' ORDER BY h.created_at DESC, h.id DESC LIMIT 1;

-- name: BoardDeals :many
SELECT d.id, d.stage_id, d.crm_company_id, d.title, d.owner_user_id, d.currency, d.contract_value_minor, d.expected_mrr_minor, d.probability,
  d.expected_close_date, d.status, d.row_version, d.updated_at, c.name AS company_name
FROM crm.deals d JOIN crm.companies c ON c.tenant_id = d.tenant_id AND c.id = d.crm_company_id
WHERE d.tenant_id = @tenant_id AND d.pipeline_id = @pipeline_id AND d.deleted_at IS NULL
  AND (d.status = 'open' OR d.closed_at > now() - interval '30 days')
ORDER BY d.updated_at DESC LIMIT 1000;

-- Tasks and reminders.

-- name: InsertTask :one
INSERT INTO crm.tasks (tenant_id, title, description, assignee_user_id, crm_company_id, deal_id, due_at, status, created_by)
VALUES (@tenant_id, @title, sqlc.narg(description), sqlc.narg(assignee_user_id), sqlc.narg(crm_company_id), sqlc.narg(deal_id), sqlc.narg(due_at), 'open', sqlc.narg(created_by))
RETURNING id;

-- name: GetTask :one
SELECT id, title, description, assignee_user_id, crm_company_id, deal_id, due_at, status, completed_at, created_at, updated_at
FROM crm.tasks WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: ListTasks :many
SELECT id, title, description, assignee_user_id, crm_company_id, deal_id, due_at, status, completed_at, created_at, updated_at
FROM crm.tasks WHERE tenant_id = @tenant_id AND deleted_at IS NULL
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status)::text)
  AND (sqlc.narg(assignee)::uuid IS NULL OR assignee_user_id = sqlc.narg(assignee)::uuid)
  AND (sqlc.narg(deal_id)::uuid IS NULL OR deal_id = sqlc.narg(deal_id)::uuid)
  AND (sqlc.narg(crm_company_id)::uuid IS NULL OR crm_company_id = sqlc.narg(crm_company_id)::uuid)
ORDER BY due_at NULLS LAST, id DESC LIMIT 500;

-- name: UpdateTask :execrows
UPDATE crm.tasks SET title = coalesce(sqlc.narg(title), title), description = coalesce(sqlc.narg(description), description),
  assignee_user_id = CASE WHEN @set_assignee::bool THEN sqlc.narg(assignee_user_id) ELSE assignee_user_id END,
  due_at = CASE WHEN @set_due::bool THEN sqlc.narg(due_at) ELSE due_at END,
  status = coalesce(sqlc.narg(status), status), completed_at = CASE WHEN sqlc.narg(status)::text = 'done' THEN now() WHEN sqlc.narg(status)::text IS NOT NULL THEN NULL ELSE completed_at END
WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: DeleteTask :execrows
UPDATE crm.tasks SET deleted_at = now() WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: InsertReminder :exec
INSERT INTO crm.reminders (tenant_id, task_id, user_id, remind_at) VALUES (@tenant_id, @task_id, @user_id, @remind_at);

-- name: DueReminders :many
-- Runs as the system role, which may only read reminders, to find work across tenants.
SELECT id, tenant_id FROM crm.reminders WHERE fired_at IS NULL AND remind_at <= now() ORDER BY remind_at LIMIT 500;

-- name: ClaimReminder :one
UPDATE crm.reminders r SET fired_at = now() FROM crm.tasks t
WHERE r.tenant_id = @tenant_id AND r.id = @id AND r.fired_at IS NULL AND t.tenant_id = r.tenant_id AND t.id = r.task_id
RETURNING r.user_id, t.id AS task_id, t.title, t.status, t.deleted_at;

-- Notes, attachments, meetings and activities.

-- name: InsertNote :one
INSERT INTO crm.notes (tenant_id, subject_type, subject_id, body, created_by) VALUES (@tenant_id, @subject_type, @subject_id, @body, sqlc.narg(created_by)) RETURNING id, created_at;

-- name: ListNotes :many
SELECT id, subject_type, subject_id, body, created_by, created_at, updated_at FROM crm.notes
WHERE tenant_id = @tenant_id AND subject_type = @subject_type AND subject_id = @subject_id AND deleted_at IS NULL ORDER BY created_at DESC LIMIT 200;

-- name: UpdateNote :execrows
UPDATE crm.notes SET body = @body WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: DeleteNote :execrows
UPDATE crm.notes SET deleted_at = now() WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: InsertAttachment :one
INSERT INTO crm.attachments (tenant_id, subject_type, subject_id, file_id, created_by) VALUES (@tenant_id, @subject_type, @subject_id, @file_id, sqlc.narg(created_by)) RETURNING id;

-- name: ListAttachments :many
SELECT id, subject_type, subject_id, file_id, created_by, created_at FROM crm.attachments
WHERE tenant_id = @tenant_id AND subject_type = @subject_type AND subject_id = @subject_id AND deleted_at IS NULL ORDER BY created_at DESC;

-- name: DeleteAttachment :execrows
UPDATE crm.attachments SET deleted_at = now() WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: InsertMeeting :one
INSERT INTO crm.meetings (tenant_id, crm_company_id, deal_id, title, starts_at, ends_at, status, organizer_user_id, source_module, source_ref, idempotency_key)
VALUES (@tenant_id, sqlc.narg(crm_company_id), sqlc.narg(deal_id), @title, @starts_at, @ends_at, 'scheduled', sqlc.narg(organizer_user_id), sqlc.narg(source_module), sqlc.narg(source_ref), sqlc.narg(idempotency_key))
ON CONFLICT DO NOTHING RETURNING id;

-- name: FindMeetingByKey :one
SELECT id FROM crm.meetings WHERE tenant_id = @tenant_id AND idempotency_key = @idempotency_key;

-- name: GetMeeting :one
SELECT id, crm_company_id, deal_id, title, starts_at, ends_at, status, organizer_user_id, source_module, outcome, created_at, updated_at
FROM crm.meetings WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: ListMeetings :many
SELECT id, crm_company_id, deal_id, title, starts_at, ends_at, status, organizer_user_id, source_module, outcome, created_at, updated_at
FROM crm.meetings WHERE tenant_id = @tenant_id AND deleted_at IS NULL
  AND (sqlc.narg(crm_company_id)::uuid IS NULL OR crm_company_id = sqlc.narg(crm_company_id)::uuid)
  AND (sqlc.narg(deal_id)::uuid IS NULL OR deal_id = sqlc.narg(deal_id)::uuid)
  AND (sqlc.narg(from_time)::timestamptz IS NULL OR starts_at >= sqlc.narg(from_time)::timestamptz)
ORDER BY starts_at LIMIT 500;

-- name: UpdateMeeting :execrows
UPDATE crm.meetings SET title = coalesce(sqlc.narg(title), title), starts_at = coalesce(sqlc.narg(starts_at), starts_at), ends_at = coalesce(sqlc.narg(ends_at), ends_at),
  status = coalesce(sqlc.narg(status), status), outcome = coalesce(sqlc.narg(outcome), outcome)
WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: InsertAttendee :exec
INSERT INTO crm.meeting_attendees (tenant_id, meeting_id, contact_id, user_id) VALUES (@tenant_id, @meeting_id, sqlc.narg(contact_id), sqlc.narg(user_id));

-- name: ListAttendees :many
SELECT meeting_id, contact_id, user_id, response FROM crm.meeting_attendees WHERE tenant_id = @tenant_id AND meeting_id = ANY(@meeting_ids::uuid[]);

-- name: InsertActivity :one
INSERT INTO crm.activities (tenant_id, activity_type, crm_company_id, contact_id, deal_id, actor_user_id, summary, detail, occurred_at, source_module, source_event_id)
VALUES (@tenant_id, @activity_type, sqlc.narg(crm_company_id), sqlc.narg(contact_id), sqlc.narg(deal_id), sqlc.narg(actor_user_id), @summary, sqlc.narg(detail), coalesce(sqlc.narg(occurred_at)::timestamptz, now()), sqlc.narg(source_module), sqlc.narg(source_event_id))
ON CONFLICT DO NOTHING RETURNING id;

-- name: ListActivities :many
SELECT id, activity_type, crm_company_id, contact_id, deal_id, actor_user_id, summary, detail, occurred_at, source_module
FROM crm.activities WHERE tenant_id = @tenant_id
  AND (sqlc.narg(crm_company_id)::uuid IS NULL OR crm_company_id = sqlc.narg(crm_company_id)::uuid)
  AND (sqlc.narg(deal_id)::uuid IS NULL OR deal_id = sqlc.narg(deal_id)::uuid)
  AND (sqlc.narg(contact_id)::uuid IS NULL OR contact_id = sqlc.narg(contact_id)::uuid)
ORDER BY occurred_at DESC, id DESC LIMIT 200;

-- name: CompanyForDeal :one
SELECT crm_company_id FROM crm.deals WHERE tenant_id = @tenant_id AND id = @id;

-- Merge repointing runs as the system role, across tenants, because a merge is platform-wide.

-- name: RepointCompanies :execrows
UPDATE crm.companies c SET company_id = @target
WHERE c.company_id = @source AND NOT EXISTS (
  SELECT 1 FROM crm.companies o WHERE o.tenant_id = c.tenant_id AND o.company_id = @target AND o.deleted_at IS NULL);

-- name: LockTenantPipelines :one
SELECT pg_advisory_xact_lock(hashtextextended(@key::text, 0))::text AS locked;

-- name: OpenDealsForCompany :many
SELECT id FROM crm.deals WHERE tenant_id = @tenant_id AND crm_company_id = @crm_company_id AND status = 'open' AND deleted_at IS NULL;
