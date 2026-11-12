# Phase 4 status

Status: built and verified locally. **Phase 4 gate approved by the product owner on 2026-11-12.** The legal and network review points below still need doing before real use.

The rules from the deployment plan still hold until legal review:
- **Email:** proposal emails go to the local Mailpit sink, so nothing reaches a real inbox.
- **E-signature:** a fake provider sends nothing outside the system.
- **IT assessments:** results come from a fake prober that reaches no real domain.
- **AI:** text comes from the fake provider.

## What exists

### CRM

| Area | Built | Key properties |
|---|---|---|
| Companies and people | List, search, create, update, delete. People are created in the tenant's contact records and linked to CRM | A company with a domain links to the shared record; the name typed stays the tenant's own. A company with open deals cannot be deleted |
| Customer registry (D-02) | `EnsureCompany`, `EnsureContact`, `GetCompanySummary` and `SetLifecycle` in `crm/contract` | Works without the CRM module, so Lead Generation, Proposals and IT Assessment always have a customer to refer to. Records made before CRM is bought appear once it is activated |
| Prospect conversion | Lead Generation's "Convert to CRM" now works | Converting the same prospect twice returns the first company. Its contacts come along |
| Pipelines | A default pipeline (New lead to Won or Lost) is seeded per tenant on first use. Further pipelines can be added and stages edited | A stage any deal has used cannot be removed, and its kind cannot change, so historic deals keep their meaning. Stage lists must have open stages, one won and one lost |
| Deals | Create, edit, move, win, lose, reopen, delete, with a stage history | Every change needs the current `row_version`; a stale one returns `409 stale_version`, and two simultaneous moves give exactly one conflict. Won and lost are reached only through win and lose. Winning needs a value, makes the company a customer, and publishes `crm.deal.won` once however often it is retried |
| Board | Pipeline columns with deals, and totals per currency | Amounts in different currencies are never added together |
| Tasks and reminders | Tasks with due dates, assignees and reminders | Reminders become notifications from a one-minute schedule, each firing once |
| Notes, attachments, meetings, timeline | Notes and manual calls on companies and deals; files linked from File Storage; meetings with attendees; one timeline | Note text is shown as text, never markup. Attachments must be uploaded as attachments |
| Events from other modules | Proposal sent, viewed, signed and declined, and assessment completed, appear on the timeline | Each event is recorded once, even if replayed. Stage suggestions move a deal forward only and never close it: a signed proposal prompts a person to mark the deal won |
| Lifecycle | Company merges repoint CRM companies in every tenant. Contact erasure removes the person from CRM while deal totals stay | |

### Customer Expansion: contract record (D-03)

| Built | Key properties |
|---|---|
| A passive consumer records a contract when a deal is won, with its customer profile | Runs whether or not the tenant has Customer Expansion, so revenue records exist if it is bought later. Reopening the deal cancels the contract; winning again reactivates it. Customer Expansion's screens remain in Phase 6 |

### Proposals

| Area | Built | Key properties |
|---|---|---|
| Commercial calculation | One tested package for line totals, setup, MRR and TCV | The server always calculates; client totals are ignored. Half-up rounding at line level, in exact integer arithmetic. Limits keep every total far inside 64-bit integers. The design's worked example gives 3,290,000. Every proposal says "All prices exclude tax" (D-11) |
| Catalog and texts | Services with prices, SLA and terms texts, proposal templates | A price change never alters a proposal already written or sent. Price changes are audited |
| Proposals and versions | Numbered proposals (`P-2026-0001`), draft versions, editing, new versions | A new customer can be created from the proposal form through the registry |
| Approval | Submit, approve, reject | Approval is needed when total contract value is at least 1,000,000 minor units (a proposed default) or when AI drafted any text. The approver confirms the hash of exactly what they reviewed; any later edit sends the version back to draft |
| Issue | Freeze, share links, PDF and email as a background job | The sender confirms the hash too. Issuing again returns the issued version. Issuing replaces any earlier issued version, which then shows a "replaced" notice |
| Immutability | Database triggers on versions and line items | An issued version's content, prices, totals, hash and dates cannot change or be deleted, even by direct SQL through the application role. Tests prove it |
| Public view | `/p/{token}` in the client app, and public API routes | Shows only the frozen snapshot, with `noindex` and `no-referrer`. Tokens are 256-bit, derived from a server key and stored only as hashes. Revoked and expired links show nothing. Views are recorded at most once per link every ten minutes |
| E-signature | Signature requests through a fake provider; signed and declined events by signed webhook | A replayed webhook signs nothing twice, and publishes one `proposals.proposal.signed` |
| AI drafting | Suggests text for one section | Prose only, never prices. The suggestion is labelled "AI generated" and is not saved until the user uses it. Requesting it marks the version AI-assisted, which requires approval before issue |

### IT Assessment

| Area | Built | Key properties |
|---|---|---|
| Check allowlist | SPF, DMARC, DKIM on common selectors, MX and TLS (passive tier); plus browser security headers (exposure tier) | The list is in code. Templates choose from it and cannot add to it. Nothing else is possible: no port scanning, exploitation, credential attempts, content discovery or load |
| Authorizations (D-12) | Name, email, role, terms version, time and IP hash recorded, with a 30-day expiry and revocation | Passive needs attestation. Exposure also needs verified control of that exact domain |
| Domain verification | DNS TXT or HTTP file challenge; the token is shown once and stored hashed | With the fake prober, reserved `.example` and `.test` domains verify automatically, because nothing real is reached |
| Runs | Background jobs on their own queue, rate-limited per domain | Domain and tier come from the authorization, never the request, and the scope is recomputed at run time. A lookup that fails is inconclusive, never a failure. A revoked or expired authorization stops queued runs |
| Findings and score | Findings with severity and classification; a score over conclusive checks only, with coverage and risk level | Wording stays honest: "DKIM not detected" is informational, because selectors cannot be listed publicly. Every result and report says the product does not test for or confirm vulnerabilities |
| Reports | PDF reports | |
| CRM link | Each assessment links to a CRM company, created through the registry if needed. Completion appears on the CRM timeline and moves open deals forward | |
| Live prober | `ASSESSMENT_PROBE=live` resolves real DNS and connects only to public addresses | Every connection decision is stored as evidence. Off by default |

### Platform and client changes

| Change | Detail |
|---|---|
| Migration 14 | 43 tables across `crm`, `proposals`, `it_assessment` and two `customer_expansion` tables, with forced RLS, grants, the immutability triggers, and narrow system-role lookups for merges, reminders, share links and e-sign envelopes. Applied, rolled back and applied again locally |
| Worker queues | The worker now also polls the assessment queue. A test checks every module queue is polled |
| Module manifests | Modules can declare public pages, used for the proposal link page |
| Admin | No changes in this phase |
| Client screens | CRM board, companies, deals and tasks; proposal list, editor, approval and sending, catalog, public page; IT assessment authorizations, verification, runs and results |
| Go toolchain | Raised from 1.27.3 to 1.27.4, for GO-2026-6859 (an HTTP server crash) |

## Verification results

| Check | Result |
|---|---|
| Backend tests, with race detector, against real PostgreSQL, on Go 1.27.4 | 607 pass, in 19 packages |
| Frontend tests | 42 pass |
| Browser end-to-end flows | 4 pass, three runs in a row. The new flow adds a deal and moves it, writes, prices and sends a proposal, opens it from the emailed link without an account, then authorizes and runs an assessment |
| Generated route tests | Every CRM, Proposals and Assessment route rejects anonymous callers, requires its module, and enforces each declared permission for a Viewer |
| Isolation tests | Tenant B cannot read or change tenant A's companies, deals, notes, proposals, catalog, assessments or authorizations by ID, and its views contain none of A's data |
| Immutability | Direct `UPDATE` and `DELETE` on an issued version and its lines fail through the application role |
| Go lint, ESLint, module boundaries, OpenAPI lint, type checks | Clean |
| govulncheck and pnpm audit | No known vulnerabilities in called code, after the Go 1.27.4 upgrade |

## Defects found and fixed during the phase

| Defect | How it was found |
|---|---|
| Meeting attendees and timeline entries pointed at the shared contact record, which the database's foreign keys rejected | API test |
| A task linked to a deal did not carry the deal's company, so it was missing from the company timeline | API test |
| Board totals check depended on JSON key order (test defect) | API test |
| Share links got their own generated ID, so their token could not be derived again for a retried email | Code review while writing the delivery job |
| The live prober's decision callback was shared between runs, which would race under concurrency | Code review |
| The fake prober read a DKIM record's profile from the selector instead of the domain | API test |
| The fake AI provider had no reply for the proposal drafting task | API test |
| Two earlier browser tests assumed CRM was still a placeholder | Browser test |
| `make gen` did not list the Phase 4 contracts | Generation check |
| GO-2026-6859 in the Go 1.27.3 standard library | govulncheck; fixed by Go 1.27.4 |

## Decisions made during the phase

| Decision | Choice | Reason |
|---|---|---|
| Conversion without CRM | Allowed, through the registry | D-02 says the registry needs no entitlement. Converted companies are waiting when CRM is bought |
| Approval threshold | TCV of 1,000,000 minor units, or any AI-drafted text | Proposed default until tenants can set it |
| Self-approval | Allowed, and audited | Small MSPs may have one person. Separation of duties can be a tenant setting later |
| Proposal expiry | Share links expire the day after `valid_until`, and the public page shows when an offer has expired | A scheduled job to mark versions expired needs a cross-tenant read; deferred |
| Public landing pages for assessments | Not built | They would let anyone who attests trigger checks on a domain, which conflicts with the rule of no assessments on domains the team does not control. They wait for legal review of D-12 |
| Assessment rate limit | One run per domain per tenant every ten minutes | Proposed |
| Probe and e-sign | Fakes by default; live prober behind `ASSESSMENT_PROBE=live` | Nothing reaches a real domain or provider until approved |

## For legal review

1. **Authorization wording.** Terms version `2026-11-draft`. The attestation reads: "The person named has authority over this domain and has allowed these checks." Is attestation enough for passive checks (D-12)?
2. **Report and result notice.** "This assessment read only public DNS records and, where authorized, the public website. It does not test for or confirm vulnerabilities, and a passed check does not mean a system is secure."
3. **Finding texts.** In `itassessment/domain/domain.go`, including the honest DKIM wording.
4. **Proposals.** Approval rules, the "All prices exclude tax" note, whether e-signatures through a future provider are valid in the target jurisdictions, and the fact that SLA and terms texts belong to the tenant.
5. **Public landing pages**, deferred as above.

## For the network review

- **Allowlist.** The check allowlist and the tier rule are in `itassessment/domain/domain.go`, with tests that templates cannot widen it.
- **Live prober** (`itassessment/app/probe.go`):
  - It resolves the domain and refuses unless every resolved address is public.
  - It connects to the first address on port 443 only.
  - It records each decision in `it_assessment.egress_decisions`.
- **Headers check.** This runs through the Egress Guard, which re-checks addresses on each redirect.
- **Not yet done.** The roadmap's isolated worker pool and network lab tests are not done. The assessment queue has its own concurrency, but runs in the same worker process. A separate deployment with network policy is needed before live probing is turned on.

## Known gaps

**Waiting on decisions or later phases**
- Real e-signature, calendar and email providers (D-05).
- Customer Expansion screens (Phase 6).
- Outreach and Chatbot events on the CRM timeline (Phase 5).
- Consultation booking from assessments, which needs a calendar provider.

**Smaller gaps**
- **No screens yet for some CRM features:** pipeline stage editing, custom fields, sales reps and AI recommendations. Pipeline stage editing has an API; the rest have tables only.
- **Assessment templates** can be created through the API but have no screen.
- **Money display:** the PDF shows two decimals for every currency. The browser formats each currency correctly.
- **One intermittent failure:** the new browser flow timed out once at four minutes and did not reproduce in the next four runs. It may be email delivery timing.
- **Carried over from Phase 3:** the general `/files/{id}/download` route lets any member download any of their workspace's files by ID.
- **Testing server:** not run end to end since Phase 1. It will need `ASSESSMENT_PROBE` and `RESEARCH_FETCHER` left at `fake`, and the worker image rebuilt on Go 1.27.4.
