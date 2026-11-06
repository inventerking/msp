# Module 2: Competitor Intelligence

Status: planning, pending approval. Common rules are in [module-overview](module-overview.md).

## 1. Purpose
Help MSPs understand competitors and find underserved markets.

## 2. Business responsibilities
Identify competitors, collect lawful public information, keep observation history, detect changes, analyze opportunities, and label every statement by certainty.

## 3. Features
Discovery; website and public service catalog analysis; public pricing observation; positioning; geographic and industry focus; public technology information; market opportunities; comparison; history; change monitoring; AI reports; attribution; confidence levels.

## 4. Data ownership
Tenant competitor relationships and everything observed for that tenant. A company being a competitor is private to the tenant.

## 5. Database tables
`competitors`, `research_configs`, `research_runs`, `sources`, `source_snapshots`, `observations`, `change_events`, `market_opportunities`, `opportunity_evidence`, `comparison_sets`, `comparison_set_members`, `reports`.

## 6. REST API (`/api/v1/competitor-intelligence`)

| Method and path | Purpose | Permission |
|---|---|---|
| `POST /discover` | Suggest candidates | `competitor.research` |
| `GET, POST /competitors`, `GET, PATCH, DELETE /competitors/{id}` | Directory | `competitor.read`, `competitor.write` |
| `GET, POST /competitors/{id}/sources`, `DELETE /sources/{id}` | Sources | `competitor.write` |
| `GET, PUT /competitors/{id}/research-config` | Schedule | `competitor.configure` |
| `POST /competitors/{id}/research-runs`, `GET /research-runs/{id}`, `POST .../cancel` | Run research | `competitor.research` |
| `GET /competitors/{id}/observations` | History | `competitor.read` |
| `GET /competitors/{id}/changes`, `POST /changes/{id}/acknowledge` | Changes | `competitor.read` |
| `GET, POST /opportunities`, `PATCH /opportunities/{id}` | Opportunities | `competitor.read`, `competitor.write` |
| `GET, POST /comparisons`, `GET /comparisons/{id}` | Comparison | `competitor.read` |
| `GET, POST /reports`, `GET /reports/{id}` | Reports | `competitor.read`, `competitor.research` |

## 7. Request structures
**Add competitor:** `{ domain or company_id, tier?, notes?, monitoring_enabled }`.
**Add source:** `{ url, kind }`.
**Create report:** `{ kind, competitor_ids[], geography?, industry? }`.

## 8. Response structures
**Observation:** `{ id, attribute_type, value, evidence_class, confidence, source: { url, fetched_at }?, observed_at, superseded_by_id? }`.
**Opportunity:** `{ id, title, rationale, geography, industry, score, evidence_class, evidence[], status }`.

## 9. Internal interfaces
`IsCompetitor(company_id)`, so Lead Generation can warn before saving a competitor as a prospect. Optional and tenant-scoped. Lifecycle contracts for purge.

## 10. Required dependencies
Company Identity, Egress Guard, Jobs, Files, Usage, AI Gateway, Company Data Provider (optional), PDF.

## 11. Published events
`competitor_intelligence.competitor.designated`, `.research.completed`, `.change.detected`, `.opportunity.identified`, `.report.ready`.

## 12. Consumed events
`shared.company.merged`, `platform.entitlement.revoked`, `platform.entitlement.suspended` (stop monitoring).

## 13. Background jobs
`competitor.research`, `competitor.analyze`, `competitor.report`, and a schedule per enabled config.

## 14. Permissions
`competitor.read`, `competitor.write`, `competitor.research`, `competitor.configure`.

## 15. Validation rules
- Source URLs must be `https` or `http`, on a public domain, and pass the Egress Guard. Fetching follows `robots.txt`, with a per-domain rate limit.
- `verified_public` and `observed` require a source snapshot (database check).
- `ai_estimate` cannot be promoted to `verified_public` by any automated process.

## 16. Failure handling
A blocked or disallowed source is marked and not retried aggressively. Partial runs are shown as partial. An AI extraction failure leaves the snapshot, so analysis can retry.

## 17. Idempotency
Run ID on research. Snapshots are deduplicated by content hash. Unchanged content doesn't create new observations.

## 18. Audit
Competitor changes, source additions, config changes, reports generated. Fetches go through the job record, with no per-page audit.

## 19. Security considerations
- SSRF through user URLs is the main risk, which the Egress Guard mitigates.
- Fetched pages are untrusted AI input, in the sense of prompt injection.
- Stored HTML is never rendered in the app; only extracted text is displayed, escaped.
- Respect site terms and provider licences. Whether particular sources may be used needs legal review.
- Statements about competitors can cause reputational or legal harm when shared, hence the strict labelling, and report footers explain it.

## 20. Unit tests
Label rules; change detection; robots parsing; URL validation including IPv6 forms, decimal IPs and redirects.

## 21. Integration tests
Run against a controlled test site: redirect to internal IP is blocked; DNS rebinding is blocked; robots disallow is honoured; injected page text doesn't alter a report's structure; the same company as a competitor for A and a prospect for B stays private.

## 22. Acceptance criteria
- Each displayed statement has an evidence label, and each non-AI statement has a source and date.
- The UI never shows an AI estimate without its label, including in PDF reports.
- Observations are append-only, and a user can view change history over time.
- Internal-address URLs are rejected, both on input and after redirect.
