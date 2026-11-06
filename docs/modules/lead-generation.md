# Module 1: Lead Generation

Status: planning, pending approval. Common rules are in [module-overview](module-overview.md). Tables are detailed in the [data dictionary](../database/data-dictionary.md).

## 1. Purpose
Help MSPs discover and qualify potential B2B customers.

## 2. Business responsibilities
Run company searches, enrich companies and contacts, score fit in an explainable way, dedupe, and hold saved lists until a prospect moves to CRM.

## 3. Features
Search by geography, industry, size, employees and technology; decision-maker and contact discovery; enrichment; scores 0–100 with explanations; source attribution; dedupe; saved searches and lists; CSV import and export; search history and job tracking; enrichment status; data quality indicators.

## 4. Data ownership
Tenant prospect relationships, attributes, scores, lists. Not canonical companies (Company Identity), contacts (Contact Identity), or the sales pipeline (CRM).

## 5. Database tables
`saved_searches`, `search_jobs`, `search_results`, `prospects`, `prospect_contacts`, `prospect_attributes`, `enrichment_runs`, `scoring_models`, `prospect_scores`, `prospect_score_factors`, `prospect_lists`, `prospect_list_members`, `import_jobs`, `export_jobs`.

## 6. REST API (`/api/v1/lead-generation`)

| Method and path | Purpose | Permission |
|---|---|---|
| `POST /searches` | Start search job | `lead.search` |
| `GET /searches`, `GET /searches/{id}` | History and status | `lead.read` |
| `GET /searches/{id}/results` | Results | `lead.read` |
| `POST /searches/{id}/cancel` | Cancel | `lead.search` |
| `POST /searches/{id}/results/promote` | Save results as prospects | `lead.write` |
| `GET, POST /saved-searches`, `PATCH, DELETE /saved-searches/{id}` | Saved searches | `lead.write` |
| `GET, POST /prospects`, `GET, PATCH, DELETE /prospects/{id}` | Prospects | `lead.read`, `lead.write` |
| `POST /prospects/{id}/enrich` | Enrich | `lead.enrich` |
| `GET /prospects/{id}/score` | Score with factors | `lead.read` |
| `POST /prospects/{id}/rescore` | Rescore | `lead.write` |
| `POST /prospects/{id}/convert` | Convert to CRM | `lead.write` |
| `GET, POST /scoring-models`, `POST /scoring-models/{id}/publish` | Models | `lead.configure` |
| `GET, POST /lists`, `PATCH, DELETE /lists/{id}`, `POST, DELETE /lists/{id}/members` | Lists | `lead.write` |
| `POST /imports`, `GET /imports/{id}` | CSV import | `lead.import` |
| `POST /exports`, `GET /exports/{id}` | CSV export | `lead.export` |

## 7. Request structures
**Start search:** `{ criteria: { countries[], regions[], industries[], employee_min, employee_max, technologies[], keywords? }, saved_search_id?, max_results }`.
**Promote:** `{ company_ids[] }`, or `{ all: true }` with limit.
**Import:** `{ file_id, mapping, lawful_source_attested: true }`.

## 8. Response structures
**Prospect:** `{ id, company: { id, display_name, primary_domain, country, industry, employee_band }, status, data_quality, score: { value, coverage }, sources[], crm_company_id?, updated_at }`.
**Score:** `{ value, coverage, model_version, factors: [{ key, state, weight, earned?, evidence }], explanation? }`.

## 9. Internal interfaces

| Operation | Purpose | Called by |
|---|---|---|
| `ListMembersOfList(list)` | Contact IDs for audience | Outreach |
| `ExportForPurge`, `Purge`, `EraseContact` | Lifecycle | Tenant Management, Contact Identity |

## 10. Required dependencies
Company Identity, Contact Identity, Company Data Provider, Usage, Jobs, Files, AI Gateway (optional explanations). CRM registry contract for conversion.

## 11. Published events
`lead_generation.search.completed`, `.prospect.created`, `.prospect.enriched`, `.prospect.scored`, `.prospect.qualified`, `.prospect.disqualified`, `.prospect.converted`.

## 12. Consumed events
`shared.company.merged` (repoint), `shared.contact.erased`, `crm.company.created` (link if same company), `platform.entitlement.revoked` (cancel running jobs).

## 13. Background jobs
`lead.search`, `lead.enrich`, `lead.score`, `lead.import`, `lead.export`.

## 14. Permissions
`lead.read`, `lead.search`, `lead.write`, `lead.enrich`, `lead.import`, `lead.export`, `lead.configure`. Matrix in [authentication-and-authorization](../security/authentication-and-authorization.md).

## 15. Validation rules
- At least one narrowing criterion on every search. `max_results` bounded by plan.
- Country and industry codes come from reference data. Domains are normalized.
- Free-mail domains can't be company domains.
- CSV: size, row and type limits; formula-injection escaping on export (leading `=`, `+`, `-`, `@`).
- Scoring weights are non-negative and normalized on publish.

### Scoring method
Scores are deterministic and do not need AI, so they stay explainable and repeatable.

1. Each factor from the tenant's ICP (industry, geography, size, technology and so on) has a weight and returns `matched`, `partial`, `not_matched`, or `unknown`.
2. `known_score` is points earned divided by the weight of known factors.
3. `coverage` is the weight of known factors divided by total weight.
4. `score = 100 × (coverage × known_score + (1 − coverage) × 0.5)`.

**Effect.** Unknown data pulls toward a neutral 50 rather than down, which meets the "missing information is not negative evidence" rule. It also doesn't let one matched factor produce a 100, as thin data can't give an extreme score. The UI always shows coverage alongside score. **Example:** three of four equal factors known and two matched gives `known_score` 0.667, `coverage` 0.75, score 62.5.

## 16. Failure handling
Provider failure leaves a job `partial` with a clear reason, and unused credits are released. `no_data` is stored distinct from `failed`. CRM unavailable at conversion returns a retryable error, and the prospect does not change state.

## 17. Idempotency
Search start uses `Idempotency-Key`. Enrich key is prospect plus kind plus day. Convert relies on the unique key in `crm.prospect_conversions`. Scoring is keyed by input hash and model version.

## 18. Audit
Searches (criteria), enrichments, exports (row count), imports, conversions, scoring model publishes, deletions.

## 19. Security considerations
Enrichment licensed for a tenant stays in that tenant's `prospect_attributes`. Exports honour licence classes and can be blocked. Company search doesn't signal other tenants' interest. Contact data is personal data: sources are recorded for notices and objections (see [privacy-and-compliance](../security/privacy-and-compliance.md)). Import attestation is recorded.

## 20. Unit tests
Scoring: all-unknown gives 50 with zero coverage; missing isn't penalised; weight normalization. Dedupe normalization. CSV parsing and formula escaping. Licence routing.

## 21. Integration tests
Full search with fake provider; partial failure; credit reserve, commit and release; two tenants prospecting the same company stay isolated; merge repoints prospects; export is blocked by licence; conversion is idempotent.

## 22. Acceptance criteria
- A user can search by geography, industry and size, track progress, cancel, and see results with sources.
- Two runs on the same company dedupe to one prospect per tenant.
- Every score displays its factors, states and coverage, and an unknown factor moves the score toward 50 rather than counting as a failed match.
- CSV import reports duplicates and rejects per row. CSV export respects provider licence restrictions.
- Tenant B can't see that Tenant A has a prospect for a shared company, in any response.
