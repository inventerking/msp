# Module 5: CRM

Status: planning, pending approval. Common rules are in [module-overview](module-overview.md).

## 1. Purpose
Manage prospects, customers, relationships and sales opportunities.

## 2. Business responsibilities
Owner of tenant customer identity (over the canonical identity), the sales pipeline, the deal lifecycle, activity history, and meetings.

## 3. Features
Companies; contacts; prospect conversion; pipeline; deals; tasks; reminders; notes; attachments; communication history; meetings; activity history; sales reps; custom fields; pipeline configuration; deal ownership; AI recommendations.

## 4. Data ownership
All `crm` tables. Only CRM changes deal state; other modules request changes through its contract.

## 5. Database tables
`companies`, `contacts`, `prospect_conversions`, `pipelines`, `pipeline_stages`, `deals`, `deal_stage_history`, `sales_reps`, `tasks`, `reminders`, `notes`, `attachments`, `meetings`, `meeting_attendees`, `activities`, `custom_field_definitions`, `custom_field_values`, `ai_recommendations`.

**Seed pipeline.** New Lead → Qualified → Contacted → Meeting Scheduled → Assessment Completed → Proposal Sent → Negotiation → Won or Lost. It is seeded per tenant as configurable data, with `won` and `lost` stage kinds.

## 6. REST API (`/api/v1/crm`)

| Method and path | Purpose | Permission |
|---|---|---|
| `GET, POST /companies`, `GET, PATCH, DELETE /companies/{id}` | Companies | `crm.read`, `crm.write`, `crm.delete` |
| `GET, POST /contacts`, `GET, PATCH, DELETE /contacts/{id}` | Contacts | same |
| `GET, POST /pipelines`, `PATCH /pipelines/{id}`, `PUT /pipelines/{id}/stages` | Configuration | `crm.configure` |
| `GET /pipelines/{id}/board` | Kanban data | `crm.read` |
| `GET, POST /deals`, `GET, PATCH, DELETE /deals/{id}` | Deals | `crm.read`, `crm.write`, `crm.delete` |
| `POST /deals/{id}/move` | Change stage | `crm.write` |
| `POST /deals/{id}/win`, `POST /deals/{id}/lose` | Close | `crm.close_deal` |
| `POST /deals/{id}/reopen` | Reopen | `crm.configure` |
| `GET, POST /tasks`, `PATCH, DELETE /tasks/{id}`, `POST /tasks/{id}/complete` | Tasks | `crm.write` |
| `GET, POST /notes`, `PATCH, DELETE /notes/{id}` | Notes | `crm.write` |
| `POST, DELETE /attachments` | Attachments | `crm.write` |
| `GET, POST /meetings`, `PATCH /meetings/{id}` | Meetings | `crm.write` |
| `GET /activities`, `POST /activities` | Timeline | `crm.read`, `crm.write` |
| `GET, POST /custom-fields`, `PATCH /custom-fields/{id}` | Custom fields | `crm.configure` |
| `GET, POST /sales-reps`, `PATCH /sales-reps/{id}` | Reps | `crm.configure` |
| `GET /assistant/recommendations`, `POST /assistant/recommendations/{id}/accept`, `/dismiss`, `POST /assistant/analyze` | AI assistant | `crm.read`, `crm.write` |

## 7. Request structures
**Create deal:** `{ crm_company_id, primary_contact_id?, title, pipeline_id?, stage_id?, currency, contract_value_minor?, expected_mrr_minor?, probability?, expected_close_date?, owner_user_id? }`.
**Move:** `{ stage_id, row_version, reason? }`.
**Lose:** `{ loss_reason, row_version }`.

## 8. Response structures
**Deal:** `{ id, title, company, primary_contact, stage, pipeline, owner, currency, contract_value_minor, expected_mrr_minor, probability, expected_close_date, status, row_version, recent_activities[], custom_fields, created_at, updated_at }`.
**Board:** `{ stages: [{ id, name, position, totals, deals: [...] }], next_cursor }`.

## 9. Internal interfaces

| Contract | Operations | Called by |
|---|---|---|
| `Registry` (no entitlement required, D-02) | `EnsureCompany`, `EnsureContact`, `GetCompanySummary`, `SetLifecycle` | Lead Generation, Proposals, Customer Expansion, Chatbot, IT Assessment |
| `Deals` (entitlement required) | `CreateDeal`, `RequestStage`, `GetDealSummary` | Lead Generation, Customer Expansion, Proposals |
| `Meetings` | `RecordMeeting` | Chatbot, IT Assessment |
| `Activities` | `RecordActivity` | Consumers inside CRM, preferred through events |

`GetCompanySummary` returns name and domain only, and never notes. Stage requests from other modules can suggest a stage. CRM decides how to apply it, and moves it forward only, so that a late assessment event can't move a deal backward.

## 10. Required dependencies
Company Identity, Contact Identity, Files, Notification, Jobs, AI Gateway, Calendar (optional).

## 11. Published events
`crm.company.created`, `crm.company.lifecycle_changed`, `crm.contact.created`, `crm.deal.created`, `crm.deal.stage_changed`, `crm.deal.won`, `crm.deal.lost`, `crm.deal.reopened`, `crm.meeting.scheduled`, `crm.meeting.completed`, `crm.task.overdue`.

## 12. Consumed events

| Event | Effect (when CRM entitled) |
|---|---|
| `lead_generation.prospect.qualified` | Optional suggestion |
| `outreach.message.sent`, `.reply.received` | Activity; reply creates a task; forward stage |
| `it_assessment.assessment.completed` | Activity; forward stage |
| `it_assessment.lead.captured`, `.consultation.requested`, `.consultation.booked` | Activity; task or meeting prompt |
| `customer_expansion.renewal.due`, `.contract.ended` | Task; lifecycle review prompt |
| `proposals.version.issued`, `.proposal.viewed`, `.proposal.signed`, `.proposal.declined` | Activity; forward stage, as a suggestion for signing |
| `chatbot.prospect.qualified`, `.booking.confirmed` | Contact or meeting |
| `visitor_intelligence.conversion.recorded` | Activity where the contact is known |
| `shared.company.merged`, `shared.contact.erased` | Repoint, erase |

Whether a signed proposal marks a deal won automatically is a policy option. The default is that it shows a prompt to close, because winning triggers revenue records.

## 13. Background jobs
Reminder firing, overdue tasks, AI analysis, and CRM CSV import (later).

## 14. Permissions
`crm.read`, `crm.write`, `crm.delete`, `crm.close_deal`, `crm.configure`, `crm.export`. Module access is tenant-wide, as confirmed, so all members with `crm.read` see all CRM records in the tenant. Record-level privacy, such as "own deals only", isn't in the initial release.

## 15. Validation rules
- Stage must belong to the deal's pipeline; moving to a `won` or `lost` stage must go through win or lose.
- Currency is valid ISO 4217. Probability is 0 to 100.
- `win` requires `expected_mrr_minor` or `contract_value_minor` to be set.
- Custom field values must match their type.
- `owner_user_id` must be an active member, validated by Membership contract.
- Merged or erased contacts cannot be added to new records.

## 16. Failure handling
Optimistic-lock conflicts return `409` with current state, so the board can reconcile. Event-driven activity is retried, and is deduped on `source_event_id`. A consumer failing doesn't affect the source module.

## 17. Idempotency
`EnsureCompany` is keyed on tenant and canonical company ID, or on source module and reference when a company has no canonical record. Conversion is unique by prospect. `RecordMeeting` uses its `idempotency_key`. Win and lose are idempotent if already in that state.

## 18. Audit
All deal changes, including value, stage, owner; deletes; merges; pipeline and custom field config; exports; AI recommendation accept or dismiss.

## 19. Security considerations
- CRM data is the tenant's most sensitive. It is never given to public-facing AI tasks.
- Notes and emails are untrusted when given to the AI assistant, and the assistant is recommendation-only. It can't change data or send messages.
- XSS: note content renders through the sanitizer. Attachments come through File Storage rules.
- Logical refs to users validated at write time; deleted users stay as "former member".

## 20. Unit tests
Stage state machine; forward-only rule; won and lost transitions; probability defaults; custom field typing; last-write conflicts.

## 21. Integration tests
Conversion idempotency; replayed events produce one activity; concurrent stage moves give one conflict; registry-created records appear when CRM is later activated; custom pipeline changes don't break historic deals; erasure removes contact PII but keeps deal totals.

## 22. Acceptance criteria
- A user can configure a pipeline, move deals on a Kanban board, and record tasks, notes, meetings and attachments.
- Deals have all required fields, and winning a deal publishes one `crm.deal.won`, however many retries occur.
- The timeline shows activity from other entitled modules, with source labels.
- AI recommendations never change data until a user accepts.
- A tenant without CRM can't use CRM routes, while the registry contract still serves other modules.
