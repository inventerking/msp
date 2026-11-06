# Module 10: Sales Intelligence

Status: planning, pending approval. Common rules are in [module-overview](module-overview.md).

## 1. Purpose
Analyze sales performance and forecast business growth.

## 2. Business responsibilities
Project events into facts, calculate metrics, produce forecasts, provide reports. Read-oriented: it never owns or changes CRM or billing transactions.

## 3. Features
Lead and source performance; qualification metrics; outreach analytics; campaign ROI; meeting and proposal conversion; win/loss; rep performance; CAC; MRR; ARR; revenue and pipeline forecasts; trends; AI recommendations; historical reports.

## 4. Data ownership
Facts, metric snapshots, forecasts, report definitions, cost entries. Cost entries are the only tenant-authored facts, because the platform can't know marketing spend.

## 5. Database tables
`fact_lead_events`, `fact_outreach_events`, `fact_deal_events`, `fact_revenue_events`, `cost_entries`, `metric_snapshots`, `forecast_runs`, `forecast_points`, `report_definitions`, `report_exports`, `ai_insights`.

**No cross-schema reads.** This module reads no other module's tables. It gets data through events and contract calls. That protects module boundaries, though it means facts are eventually consistent, so reports show "as of" timestamps.

## 6. REST API (`/api/v1/sales-intelligence`)

| Method and path | Purpose | Permission |
|---|---|---|
| `GET /dashboard` | Overview | `sales_intel.read` |
| `GET /leads`, `/campaigns`, `/funnel`, `/win-loss`, `/team`, `/revenue` | Analytics | `sales_intel.read`, plus `sales_intel.team` for team |
| `GET /metrics/{key}` | One metric series | `sales_intel.read` |
| `GET, POST /forecasts`, `GET /forecasts/{id}` | Forecasts | `sales_intel.read`, `sales_intel.forecast` |
| `GET, POST /costs`, `PATCH, DELETE /costs/{id}` | Costs | `sales_intel.costs` |
| `GET, POST /reports`, `POST /reports/{id}/export`, `GET /exports/{id}` | Reports | `sales_intel.read`, `sales_intel.export` |
| `GET, POST /insights` | AI commentary | `sales_intel.read` |

## 7. Request structures
Common query: `from`, `to`, `grain`, `dimension`, `filters`.
**Forecast:** `{ kind: "revenue" | "pipeline", horizon_months, method_version? }`.
**Cost:** `{ period_start, period_end, category, campaign_ref?, amount_minor, currency }`.

## 8. Response structures
Every number carries a label: `{ value, value_kind: "actual" | "estimated" | "forecast", currency?, as_of }`.
**Forecast:** `{ id, kind, as_of, method_version, assumptions, points: [{ period, expected, low, high }], value_kind: "forecast" }`.
**Unavailable section:** `{ available: false, reason: "module_not_active" | "insufficient_history" | "no_cost_data" }`.

## 9. Internal interfaces
None required by others. `Backfill(tenant, source)` is a lifecycle operation calling each source module's export contract, used for repair. Rebuilds are limited to what those modules retain.

## 10. Required dependencies
Analytics Infrastructure, Events, Jobs, FX rates, AI Gateway (optional), Files, PDF. No synchronous dependency on business modules for reads.

## 11. Published events
`sales_intelligence.forecast.completed`.

## 12. Consumed events
All **passive** (ADR-014).

| Fact table | Events |
|---|---|
| `fact_lead_events` | `lead_generation.prospect.created`, `.qualified`, `.disqualified`, `.converted`; `chatbot.prospect.qualified`; `it_assessment.lead.captured`; `visitor_intelligence.conversion.recorded` |
| `fact_outreach_events` | `outreach.message.sent`, `.message.bounced`, `.reply.received`, `.unsubscribe.recorded` |
| `fact_deal_events` | `crm.deal.created`, `.stage_changed`, `.won`, `.lost`, `.reopened`; `crm.meeting.scheduled`, `.completed`; `proposals.version.issued`, `.proposal.signed`, `.proposal.declined` |
| `fact_revenue_events` | `customer_expansion.contract.activated`, `.changed`, `.ended` |

## 13. Background jobs
`analytics.metric_rollup`, `analytics.forecast`, report exports.

## 14. Permissions
`sales_intel.read`, `sales_intel.team`, `sales_intel.forecast`, `sales_intel.costs`, `sales_intel.export`. Team performance is limited to manager roles, because it shows individual employee performance.

## 15. Validation rules

### Metric definitions
Defined once in code, each with tests.

| Metric | Definition | Kind |
|---|---|---|
| MRR | Sum of active contracts' MRR at period end, using `fact_revenue_events` | Actual, if from contracts; otherwise not shown as actual |
| ARR | MRR × 12 | Actual (derived) |
| Net new MRR | new + expansion + reactivation − contraction − churn | Actual |
| Win rate | Won ÷ (won + lost) for deals closed in the period | Actual |
| Meeting conversion | Deals with a meeting that are later won ÷ deals with a meeting | Actual |
| Proposal conversion | Signed ÷ issued | Actual |
| CAC | Costs ÷ new customers in the period; shown only when costs are entered | Estimated, because it depends on manual input |
| Campaign ROI | Attributed won MRR over a chosen horizon, minus cost, divided by cost | Estimated, because attribution is a model |
| Weighted pipeline | Sum of contract value × probability on open deals | Estimated |

### Forecast method
- **Revenue:** start with actual MRR, then roll forward using contract end dates, historical churn and expansion rates, plus weighted pipeline. Low and high come from historical variance. Needs a stated minimum history (proposed: 3 months); less returns `insufficient_history`.
- **Pipeline:** open deals by expected close month, using historical stage-to-win rates where the sample is sufficient, or else deal probability.
- **AI never produces the numbers.** It only writes commentary citing metric IDs, and any number in that commentary is checked against those IDs.

### Separation rule
`value_kind` is part of the metric's unique key, and no API or chart sums across kinds. UI styling also differs: forecast values appear visibly distinct.

### Currency
Original currency is kept, and reporting values are converted using the rate at the event date (D-11). If no rate is available, the amount is excluded from the total and flagged, not guessed.

## 16. Failure handling
Consumer lag shows through the "as of" indicator. Failed rollups retry, and old values stay with their timestamp. Forecast failure keeps the previous run marked as previous. Missing FX rates are flagged.

## 17. Idempotency
`(tenant_id, source_event_id)` unique on all fact tables. Rollups replace by key. Forecast key is tenant, kind and as-of date.

## 18. Audit
Cost entries, report exports, and forecast runs. Read access to team performance reports is logged.

## 19. Security considerations
Facts contain IDs and amounts, no contact PII. Team performance is sensitive, so it has a separate permission, and local employment law may matter (D-19). AI insights get aggregates only. Exports use short-lived grants. Formula injection is escaped in CSV. Metrics are never compared across tenants: no benchmarks without a separate decision, because that could leak tenant data.

## 20. Unit tests
Each metric formula on fixtures; MRR movement classification; forecast with short history; separation of value kinds; FX conversion and missing rate; zero denominators.

## 21. Integration tests
Replayed event doesn't double-count; out-of-order deal events still give the right current stage; module activated late shows history (ADR-014); lost CRM entitlement stops new CRM facts only if events stop, and shows history; heavy rollup doesn't affect CRM; tenant B cannot query tenant A's facts.

## 22. Acceptance criteria
- The dashboard shows leads, campaigns, funnel, wins, losses and revenue, for modules with data, and explains unavailable sections.
- All numbers are labelled actual, estimated or forecast, and no chart or total mixes them.
- MRR and ARR reconcile with `customer_expansion.contracts` on a test dataset.
- Forecasts display assumptions and ranges, and refuse to run when history is insufficient.
- The module has no write path into CRM or billing, verified by the import linter.
