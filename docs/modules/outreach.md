# Module 3: Outreach

Status: planning, pending approval. Common rules are in [module-overview](module-overview.md). Legal behaviour needs counsel's review, and nothing here claims compliance.

## 1. Purpose
Automate compliant outbound communication and prospect engagement. The product is designed to make spam difficult.

## 2. Business responsibilities
Sender setup, domain authentication status, templates, campaigns, approvals, scheduled sending, reply and bounce handling, unsubscribes, suppression, reputation protection, analytics.

## 3. Features
Email provider integration; sender accounts; domain status; templates; AI drafts; personalization; multi-step sequences; schedules and sending windows; segments; approval; delivery tracking; bounce and reply detection; unsubscribe; suppressions; analytics; sending limits; reputation protection; provider quotas.

## 4. Data ownership
Campaign and message state, tenant suppression list. Contact identity remains with Contact Identity; CRM timeline remains with CRM.

## 5. Database tables
`sending_domains`, `sender_accounts`, `templates`, `template_versions`, `audiences`, `audience_members`, `campaigns`, `sequence_steps`, `approvals`, `enrollments`, `messages`, `delivery_events`, `replies`, `suppressions`, `global_suppressions`, `sending_quota_counters`, `campaign_stats`.

## 6. REST API (`/api/v1/outreach`)

| Method and path | Purpose | Permission |
|---|---|---|
| `GET, POST /domains`, `POST /domains/{id}/check` | Domains | `outreach.configure` |
| `GET, POST /senders`, `PATCH, DELETE /senders/{id}` | Senders | `outreach.configure` |
| `GET, POST /templates`, `POST /templates/{id}/versions` | Templates | `outreach.write` |
| `POST /templates/ai-draft` | AI draft | `outreach.write` |
| `GET, POST /audiences`, `POST /audiences/{id}/members` | Audiences | `outreach.write` |
| `GET, POST /campaigns`, `GET, PATCH /campaigns/{id}` | Campaigns | `outreach.read`, `outreach.write` |
| `PUT /campaigns/{id}/steps` | Sequence | `outreach.write` |
| `POST /campaigns/{id}/preflight` | Check readiness | `outreach.write` |
| `POST /campaigns/{id}/submit-for-approval` | Submit | `outreach.write` |
| `POST /campaigns/{id}/start`, `/pause`, `/resume`, `/cancel` | Control | `outreach.send` |
| `GET /approvals`, `POST /approvals/{id}/approve`, `/reject` | Approval queue | `outreach.approve` |
| `GET /messages`, `GET /messages/{id}` | History | `outreach.read` |
| `GET /replies`, `PATCH /replies/{id}` | Replies | `outreach.read` |
| `GET, POST /suppressions`, `DELETE /suppressions/{id}` | Suppression list | `outreach.read`, `outreach.write`, `outreach.suppression_remove` |
| `GET /analytics/campaigns/{id}` | Analytics | `outreach.read` |
| Public: `GET, POST /api/v1/public/unsubscribe/{token}` | Unsubscribe and one-click | Signed token |
| Webhooks: `/api/v1/webhooks/email/{pathToken}` | Provider events | Signature |

## 7. Request structures
**Create campaign:** `{ name, sender_account_id, audience_id, timezone, sending_window: { days[], start, end }, approval_mode, purpose }`.
**Steps:** `[{ position, delay_minutes, template_version_id, send_condition? }]`.
**Approve:** `{ content_hash, note? }`. The hash must match what is currently awaiting approval.

## 8. Response structures
**Preflight:** `{ ready, blockers: [{ code, message }], warnings[], audience: { total, sendable, suppressed, jurisdiction_blocked, missing_email } }`.
**Campaign:** `{ id, name, status, approval_mode, sender, audience_summary, stats, next_send_at }`.

## 9. Internal interfaces

| Operation | Purpose | Called by |
|---|---|---|
| `IsSuppressed(email_hash)` | Check before marketing mail | IT Assessment |

Objections recorded elsewhere reach this module through `shared.contact.consent_changed`, not a direct call.

## 10. Required dependencies
Contact Identity, Email Provider, Integration, Jobs, Usage, AI Gateway, Notification. Lead Generation (optional, audience from lists), CRM (optional, through events).

## 11. Published events
`outreach.campaign.approved`, `.campaign.started`, `.campaign.completed`, `.message.sent`, `.message.bounced`, `.reply.received`, `.unsubscribe.recorded`, `.suppression.added`, `.sequence.stopped`.

## 12. Consumed events
`shared.contact.erased`, `shared.contact.consent_changed`, `crm.deal.won`, `crm.deal.lost` (end sequences, configurable), `platform.entitlement.suspended` and `.revoked` (halt), `identity.tenant.suspended` (halt), `integration.connection.needs_reauth` (pause sender).

## 13. Background jobs
`outreach.schedule_tick`, `outreach.send`, `outreach.process_reply`, `outreach.domain_check`, stats rollup.

## 14. Permissions
`outreach.read`, `outreach.write`, `outreach.send`, `outreach.approve`, `outreach.configure`, `outreach.suppression_remove`. **Separation of duties:** tenant configuration can require that the approver differs from the author. Recommended as the default for AI-drafted content.

## 15. Validation rules

### Send gates
A send is allowed only if all of these pass. They are re-checked immediately before each send, not only on campaign start.

1. Entitlement active and tenant not suspended.
2. Campaign `running`, enrollment `active`.
3. Approval valid, and hash of the rendered content matches. **Default (D-10):** a campaign approval covers messages rendered only from approved templates. Any AI-personalized message needs per-message approval, or a sample-based policy if D-10 chooses one.
4. Contact not in tenant or global suppressions.
5. Jurisdiction policy passes. Unknown jurisdiction blocks by default for regions listed as consent-required.
6. Sender active, domain SPF and DKIM pass, DMARC present, reputation not `paused`.
7. Within sending window, sender daily limit, provider quota and tenant limit.
8. Required content is present: unsubscribe link, one-click header, sender's postal address, truthful subject (template check and confirmation).

### Jurisdiction policy
A table maps recipient country to rule (`opt_out_allowed`, `consent_required`, `unknown_block`), maintained by platform admins after legal advice. Examples for discussion only: CAN-SPAM is opt-out based; CASL generally needs consent; some EU states require prior consent even for B2B email. Rules aren't encoded here, and must come from counsel.

### Reputation protection
Automatic pausing when bounce or complaint rate over a rolling window exceeds thresholds set by platform admins. New senders have ramped limits. Platform admins can pause a tenant's sending. No features are offered to evade filters, such as content spinning or sender rotation meant to mask reputation.

## 16. Failure handling
- Provider error: retried by class, with no double send.
- Unknown outcome from a timeout: reconcile using provider message lookup where available. Where it is not, the message is marked for review rather than resent, because a duplicate is worse than a delay.
- Reply processing order: stop the sequence first, then classify. AI failure can't leave a sequence running.
- Unsubscribe is processed synchronously and works even if the rest of the module or the tenant's entitlement is inactive.

## 17. Idempotency
A message's key is enrollment plus step. Provider event IDs dedupe delivery events. Reply key is the provider message ID. Unsubscribe is naturally idempotent.

## 18. Audit
Sender and domain changes, template versions, approvals, start, pause, cancel, suppression add and remove, limit changes by admin.

## 19. Security considerations
- Tracking pixels and link rewriting are off by default, because of privacy implications (D-10).
- Reply text and AI context are untrusted input. Reply content never controls any action other than a stop.
- Header injection blocked. Templates escape values, and variables come from an allowlist.
- Suppressions keep a keyed hash, so they survive contact erasure. Removing one needs a specific permission and a reason.
- Abuse monitoring by platform admins. Terms of use should prohibit purchased-list spam (D-19).

## 20. Unit tests
Each send gate, individually. Sending window across time zones and daylight saving. Template rendering escapes. Sequence state machine. Stop reasons. Hash binding of approval.

## 21. Integration tests
Reply stops future steps even when a send job is already queued. Unsubscribe race: a suppression recorded just before a send blocks it. Bounces increment and pause. Webhook forgery rejected and replay ignored. Edit after approval invalidates it. Revocation halts sending at once. Suppressed contact excluded after re-import.

## 22. Acceptance criteria
- No external email can be sent without a valid approval. A test tries every path around it.
- A reply, unsubscribe, hard bounce or complaint stops the sequence before the next step can send.
- Every outreach email has a working unsubscribe, including RFC 8058 one-click, that works without login.
- A campaign can't start with a failing domain or a missing postal address, and preflight explains why.
- Sending stops when limits or reputation thresholds trip.
- Suppressed addresses are never sent to, even if imported again.
