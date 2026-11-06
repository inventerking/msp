# Open decisions

Status: **recommendations approved by the product owner on 2026-11-06.** Each "Recommendation" below is now the working decision, and the alternatives mentioned were not chosen. "Needed by" is the phase that relies on the decision.

Approval sets the direction, but these items still need work before they are closed:

| ID | Still needed | By |
|---|---|---|
| D-04 | Evaluate merchant-of-record options, then choose a provider | Before Phase 7 |
| D-05 | Select each provider, and check licence and data terms | Before the phase that needs it |
| D-09, D-10, D-12 | Legal opinion and jurisdiction rules, through the product owner's boss. If the review disagrees, the decision is reopened | Before handover, or before any real use |
| D-13 | Choose the production region. Deferred: no production deployment is planned yet, and EU is the working assumption | Before real customer data is stored |
| D-14 | Legal review of the proposed retention periods, through the product owner's boss | Before real customer data is stored |
| D-19 | The product owner's boss takes responsibility for legal review after handover (decided 2026-11-06). An earlier look at D-09, D-10 and D-12 is recommended, to reduce rework | Before real use |
| D-21 | Choose specific libraries and tools in the agreed review | Start of Phase 1 |
| D-23 | Environments decided 2026-11-06: local Docker first, then a testing server. Production hosting is decided after handover | After handover |

**Rules until legal review is complete:** the local and testing environments use synthetic data only. They send no email to real recipients, using a test sink instead. They run assessments only against domains the team controls, and don't track real visitors.

**This approval does not authorize implementation.** Phase 1 still needs its own explicit approval.

## Decisions to make first

These affect the commercial model or the structure of the system.

| ID | Decision | Recommendation | Why it matters | Needed by |
|---|---|---|---|---|
| **D-02** | How can customer-dependent modules work without CRM? The Growth bundle (Customer Expansion and Sales Intelligence) has no CRM, and Proposals can be bought alone | **Internal CRM registry** usable without entitlement; CRM screens and deals stay gated. Alternatives: (B) add CRM to Growth; (C) make CRM a required prerequisite, which is simplest but limits individual purchases | Shapes dependency design and what bundles mean | Phase 1 |
| **D-01** | When is payment taken relative to admin review? | **Capture at submission, with a published review target and automatic refund on rejection.** Alternatives: authorize only (card holds can expire); invoice after activation | Customers can pay and still wait; rejections need refunds | Phase 7, but it influences the Phase 1 model |
| **D-04** | Payment provider or merchant of record? | **Evaluate a merchant of record first**, as selling worldwide makes VAT and sales tax a major burden. Trade-off: higher fees, less control | Sets invoice, tax and activation design | Phase 7; early research recommended |
| **D-06** | Global or tenant-scoped contact identity? | **Tenant-scoped** (ADR-009). Trade-off: possible repeated enrichment costs | Privacy roles and leak risk | Phase 2 |
| **D-12** | Is attestation enough for passive assessment checks? | **Two tiers**: attestation for public reads, verified domain control for anything active. Needs counsel's opinion | Conversion against legal risk | Phase 4 |
| **D-10** | Outreach approval granularity and default jurisdiction rules | **Campaign-level approval for fixed templates, per-message for AI-personalized content; unknown jurisdiction blocked; tracking pixels off by default.** Rules from counsel | Scale against safety; product legal risk | Phase 5 |
| **D-23** | Production topology | **At least two application hosts, managed PostgreSQL with failover if available, managed object store, same-origin API routing.** A single Docker host can't meet 99.9% | Cost, availability, secret store choice | Phase 1 for dev; Phase 7 for production |

## Provider and technology decisions

| ID | Decision | Recommendation | Needed by |
|---|---|---|---|
| **D-05** | Which providers for AI, company data, email, calendar, e-signature, business identification? Tenant-owned AI keys? | Select per capability on capability, licence terms, data handling and price. Check whether licences permit `licensed_shareable` reuse. No tenant AI keys at launch | Before each provider-dependent phase; company data is the highest risk |
| **D-08** | Use `pgvector` for chatbot retrieval? | **Yes**: stays in the one approved database, and supports RLS. It needs an approved extension | Phase 5 |
| **D-17** | Use `pg_trgm` for fuzzy search? | **Yes.** No separate search engine | Phase 2 |
| **D-21** | Libraries and tools: Go router, SQL tooling, migration tool, job queue (build or adopt), OpenAPI tools, frontend state and forms, monorepo manager, malware scanner, PDF renderer, tracing backend, breached password source, event schema format | Proposed at the start of Phase 1, in one short review. Assess adopting a PostgreSQL-native Go queue before building one | Phase 1 |
| **D-16** | Which industry taxonomy? | Start with a small custom list mapped to whichever taxonomy the company-data provider uses | Phase 2 |
| **D-11** | Multi-currency reporting and tax on tenant proposals: FX source? Tax lines on proposals? | Convert at event-date rate from an approved source. Proposals say "excludes tax" at launch | Phase 4 |

## Policy decisions

| ID | Decision | Recommendation | Needed by |
|---|---|---|---|
| **D-03** | Is Customer Expansion's `contracts` the source of record for MRR, and is it populated even if the module isn't purchased? | **Yes to both** (ADR-014). It gives Sales Intelligence reliable MRR, whichever is bought | Phase 4 design, Phase 6 build |
| **D-07** | AI prompt and response logging | **Metadata only by default.** Debug capture is opt-in, short-lived and restricted | Phase 2 |
| **D-09** | Platform-wide suppression list for complaints across tenants? | **Yes, narrowly:** keyed hash for complaints and legal requests only, with a generic reason shown. Needs legal review, since it shares a signal across tenants | Phase 5 |
| **D-13** | Data residency: one region at launch? | **One region** and a documented plan. Decide which region based on target customers. Reconsider before EU enterprise sales | Phase 1 for the region; Phase 7 for the plan |
| **D-14** | Retention periods; data behaviour after an entitlement lapses; the offboarding window; handling suppression after tenant deletion | Use proposed values in the data dictionary. **Read-only for 30 days after expiry, data retained for 90 days, then purged after notice**, excluding statutory records | Phase 1 for policy structure; Phase 7 final |
| **D-15** | Public pages and embeds: is this outside the "two apps" rule? Custom domains for landing pages? | **Embeds as packages; public pages as a route group in the client build; custom domains later** | Phase 1 |
| **D-18** | Can subscriptions expire automatically? | **Yes, under an admin-configured dunning policy**, with admin notification. Admins still have manual control | Phase 7 |
| **D-19** | Legal review, role determination, terms and DPA | **Engage counsel before Phase 5 at the latest**, since Outreach, Assessment and Visitor Intelligence carry most legal risk | Before Phase 4 ideally |
| **D-20** | Custom tenant roles? | **Later.** Launch with seeded roles and a data model that can extend | After launch |
| **D-22** | Support access to tenant content; platform analytics scope | **Time-limited grant, visible to the tenant;** platform analytics use only operational data | Phase 1 |

## Not asked, assumed from the brief

| Assumption | Basis |
|---|---|
| Trials also require admin activation | "Administrator controlled" |
| Tenant roles are not per-module purchases | Section 2.2 |
| Launch language and locales aren't specified | Text is externalized, so no decision is needed yet |
| External customer API isn't in scope | Not requested |
| Native mobile apps aren't in scope | Not requested |
