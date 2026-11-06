# Module 9: Customer Expansion

Status: planning, pending approval. Common rules are in [module-overview](module-overview.md).

## 1. Purpose
Help MSPs grow revenue from existing customers.

## 2. Business responsibilities
Track service coverage and contracts, surface renewals, find service gaps, propose evidence-backed expansion, follow opportunities to realised MRR.

## 3. Features
Existing customer view; service coverage; contract information; renewal dates; service inventory; gap identification; cross-sell and upsell; health indicators; renewal alerts; expansion opportunities; AI recommendations; revenue tracking.

## 4. Data ownership
Coverage, contracts, renewal alerts, health, recommendations, opportunities. **Customer identity remains with CRM**: `customer_profiles` only overlays `crm.companies`. "Existing customer management" here means managing this overlay, and editing a name goes to CRM's registry.

**Contracts (D-03).** This module holds the source of record for recurring revenue, because a deal's `expected_mrr` is a sales estimate and proposals can be superseded. A contract records what is actually in force.

## 5. Database tables
`customer_profiles`, `contracts`, `service_inventory`, `renewal_alerts`, `health_indicators`, `service_gaps`, `recommendations`, `recommendation_evidence`, `revenue_opportunities`.

## 6. REST API (`/api/v1/customer-expansion`)

| Method and path | Purpose | Permission |
|---|---|---|
| `GET, POST /customers`, `GET, PATCH /customers/{id}` | Customers | `expansion.read`, `expansion.write` |
| `POST /customers/import` | CSV | `expansion.write` |
| `GET, POST /customers/{id}/contracts`, `PATCH /contracts/{id}` | Contracts | `expansion.write` |
| `GET, PUT /customers/{id}/services` | Coverage | `expansion.write` |
| `GET /renewals`, `POST /renewal-alerts/{id}/acknowledge` | Renewals | `expansion.read` |
| `POST /customers/{id}/analyze` | Run analysis | `expansion.analyze` |
| `GET /recommendations`, `GET /recommendations/{id}` | Recommendations | `expansion.read` |
| `POST /recommendations/{id}/accept`, `/dismiss` | Decide | `expansion.write` |
| `GET /opportunities`, `GET /opportunities/summary` | Tracking | `expansion.read` |

## 7. Request structures
**Create customer:** `{ crm_company_id }`, or `{ name, domain }`, which goes through the CRM registry.
**Contract:** `{ start_date, end_date?, term_months?, auto_renew?, notice_days?, currency, mrr_minor, setup_minor, proposal_version_id?, deal_id? }`.
**Coverage:** `[{ tenant_service_id, coverage, quantity?, mrr_minor?, currency? }]`.
**Accept:** `{ create_deal: true, deal_title? }`.

## 8. Response structures
**Recommendation:** `{ id, customer, kind, service, rationale, evidence: [{ kind, detail, reference }], estimated_mrr_minor, currency, evidence_class, status }`.
**Customer:** `{ id, crm_company, health_status, health_as_of, active_mrr_minor, renewal_next_date, coverage_summary: { active, absent, unknown } }`.

## 9. Internal interfaces
None for routine use. Contract facts leave through events, and a lifecycle export supports Sales Intelligence backfill.

## 10. Required dependencies
CRM registry; CRM `Deals` (optional, for accept-to-deal); Proposals summary (optional); `tenant_services`; AI Gateway; Jobs; Notification.

## 11. Published events
`customer_expansion.contract.activated`, `.contract.changed`, `.contract.ended`, `.renewal.due`, `.recommendation.created`, `.recommendation.accepted`, `.recommendation.dismissed`, `.opportunity.won`, `.opportunity.lost`.

## 12. Consumed events

| Event | Effect |
|---|---|
| `crm.deal.won` | **Passive** (ADR-014): create or update customer profile and a contract; set lifecycle to customer |
| `proposals.proposal.signed` | Enrich the contract with term and setup when a deal link exists |
| `crm.deal.lost` | Update linked opportunity |
| `crm.deal.reopened` | Passive: return the related contract to `draft` for review |
| `it_assessment.assessment.completed` | Keep the assessment reference as possible recommendation evidence |
| `crm.company.lifecycle_changed` | Sync status |
| `shared.company.merged` | Repoint |

When a contract is created from a won deal, the proposal's commercial snapshot takes priority over the deal's `expected_mrr`, and a mismatch is flagged for human review, not silently resolved.

## 13. Background jobs
`expansion.renewal_scan` (daily), `expansion.analyze`, health calculation.

## 14. Permissions
`expansion.read`, `expansion.write`, `expansion.analyze`.

## 15. Validation rules

**A recommendation must have:** a non-empty rationale and at least one evidence item. Rule-based evidence comes first, with AI used for wording. **Evidence examples:** "service not in coverage", "contract ends in 60 days", "assessment found missing DMARC".

**Unknown is not absent.** `coverage = unknown` produces `unknown_coverage`, worded as "confirm whether they have this". It never produces a claim that the customer lacks the service.

Other rules: contract dates consistent; `mrr_minor ≥ 0`; currency valid; services must exist in the tenant's list. Estimated MRR always has `evidence_class`, and doesn't count as revenue until realised.

## 16. Failure handling
AI failure: rule-based gaps still appear with plain rationale. CRM deal creation failure on accept: recommendation stays accepted, opportunity `open`, and a retry is scheduled. Registry down during event: delivery retries.

## 17. Idempotency
`(tenant_id, deal_id)` unique on contracts means repeated `crm.deal.won` is safe. Renewal alerts are unique by contract, date and window. One opportunity per recommendation. Accept uses `Idempotency-Key`.

## 18. Audit
Contract changes (with MRR before and after), coverage changes, accept and dismiss, manual health overrides.

## 19. Security considerations
AI receives only coverage, dates and the tenant's service list, with no contact PII. CRM notes aren't sent to analysis unless a future decision allows them. Health indicators must not be built from personal attributes of individuals. Imports use the file controls.

## 20. Unit tests
Renewal windows and notice periods; gap logic with unknown; contract state machine; MRR movements; rationale requirement.

## 21. Integration tests
Won deal to contract and profile; replay is safe; proposal-versus-deal mismatch flag; accept creates exactly one deal; Growth-only tenant (no CRM entitlement) works through the registry; analysis without AI still returns rule-based items.

## 22. Acceptance criteria
- A won deal gives a customer profile and contract, without manual double entry.
- Renewal alerts are created on schedule, including notice periods.
- All recommendations display rationale and evidence, and unknown coverage is never presented as missing.
- Accepting a recommendation creates a CRM deal when CRM is entitled, and realised MRR is tracked.
