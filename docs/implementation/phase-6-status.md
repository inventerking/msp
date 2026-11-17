# Phase 6 status

Status: built and verified locally. **Waiting for the Phase 6 gate: a review of the [metric definitions](metric-definitions.md).** That page lists eleven choices to confirm or change.

The deployment plan's rules still apply: synthetic data only, no email to real recipients, and no real visitors tracked. Phase 6 adds no new outside provider. AI wording and commentary come from the fake AI provider, and no exchange-rate source exists yet.

## What exists

### Customer Expansion

| Area | Built | Key properties |
|---|---|---|
| Contracts (D-03) | Recorded from won deals for every workspace, whether or not it has the module. Added and changed by hand. Statuses draft, active, renewing, ended and cancelled | A contract follows one deal, so a repeated win records nothing. A signed proposal's terms win over the deal's estimate, and a disagreement is flagged on the contract for a person to review, never resolved silently. A reopened deal returns its contract to draft. A term with no end date stores the end it implies. Every change publishes how it moved MRR, and is audited with MRR before and after |
| Customers | List with health, MRR, next renewal and coverage summary. Added by CRM company, or by name and domain through the customer registry, which works without CRM (D-02). CSV import | Identity stays with CRM: a profile only overlays a CRM company. The import reads a scanned `csv_import` upload, takes up to 1,000 rows, and reports errors by line, never by cell content. Contracts in several currencies are not added together |
| Coverage | Each of the workspace's services is active, absent, declined or unknown for a customer | Unknown is the absence of a record, so it is never stored as a claim. A price can be recorded for active services |
| Analysis | Rule-based gaps, recommendations and health, then optional AI wording | See [metric-definitions](metric-definitions.md#customer-expansion-rules). Every suggestion has a rationale and at least one piece of evidence. AI receives only the rule text and evidence, no contact details, and its wording is kept only if every number and date is unchanged |
| Recommendations | Cross-sell, renewal and confirm-coverage suggestions. Accept, with or without a deal, or dismiss | Accepting is idempotent: one opportunity per suggestion, and CRM opens at most one deal for it, even when the request is repeated. Without CRM, the opportunity is tracked with no deal. If CRM cannot answer, the deal is retried |
| Opportunities | Followed to won or lost through the deal's events | Realised MRR is the won deal's MRR. Estimated and realised amounts are reported apart |
| Renewals | A scan every six hours raises alerts at 90, 60 and 30 days before a contract's end and its notice deadline | Each alert is raised once, publishes `renewal.due`, and notifies the contract's owner. Changing or ending a contract resolves its alerts |
| Health | Computed from business facts, or set by hand | A status set by hand is kept until cleared, and every change is audited |

### Sales Intelligence

| Area | Built | Key properties |
|---|---|---|
| Facts | Lead, outreach, deal and revenue facts, from events only | The module imports no other module, not even a contract, and not billing: the import linter enforces it. Facts hold IDs and amounts, never names or free text. Each fact is keyed by its event, so a replay adds nothing. Facts are recorded for every workspace (ADR-014), and are insert-only for the application role |
| Metrics | 35 metrics, defined once in code with tests | Recomputed a few seconds after new activity (events close together share one rollup), and at least hourly. A rollup replaces a workspace's metrics in one transaction, so readers see the old set or the new one |
| Screens | Dashboard, revenue, leads, campaigns with costs, funnel with wins and losses, forecasts, team, reports, and definitions | Every number shows its kind in words; estimates and forecasts are styled differently. Each screen shows when it was computed. Sections that cannot have data say why: no source module active, or no costs entered |
| Forecasts | Revenue and pipeline, 1 to 24 months ahead | Refused with under 3 months of history. Asking twice on the same day returns the same run. A failed run leaves the previous one showing |
| Costs | Entered by hand, audited | The only facts a workspace types in |
| Reports | Saved definitions, exported as CSV | Every cell is made safe against spreadsheet formulas. The download link lasts five minutes and three downloads. Exports are audited |
| Team | Results by team member | Needs `sales_intel.team`; owner breakdowns are not available anywhere else, including reports. Every view is audited |
| AI commentary | Optional, on aggregates only | It must cite the metrics it uses, and every number in it must appear in one of them, or nothing is kept |

### Platform, admin and client changes

| Change | Why |
|---|---|
| Workspace services (`/tenant/services`), in Settings | The table existed but nothing could fill it. Coverage, gap analysis and proposal catalogue items refer to these services. Any member can read the list; `settings.manage` changes it |
| Service categories in reference data | So services can be grouped, and security-related services can use assessment evidence |
| Every `crm.deal.*` event carries a full deal snapshot; new `crm.deal.updated` | Sales Intelligence rebuilds each deal from its latest event, whatever the order. See [event-contracts](../api/event-contracts.md#crm) |
| `outreach.message.bounced` and `outreach.unsubscribe.recorded` are published; sent and reply events name the recipient's company | They were in the catalogue but not published. The company is needed for campaign attribution |
| `visitor_intelligence.conversion.recorded` is published | It was in the catalogue but not published |
| `it_assessment.assessment.completed` includes `risk_level` | Customer Expansion uses it as evidence |
| CRM contract: `OpenDeal` and `CompanySummaries` | Accept-to-deal, and customer names without one call per customer. `OpenDeal` is idempotent per source record |
| Identity: `ReportingCurrency` | Modules learn a workspace's reporting currency without reading its tables |
| `kernel/csvx` | Formula-safe CSV writing and bounded reading, shared by Lead Generation and Sales Intelligence |

## Verification results

| Check | Result |
|---|---|
| Backend tests, with the race detector | All 25 packages pass. 324 test functions, up from 278 at Phase 5 |
| New backend tests | Customer Expansion and Sales Intelligence domain rules with worked numbers; API tests for won deals to contracts, replays, proposal mismatch, movements, coverage and analysis, AI wording, accept-to-one-deal, no-CRM workspaces, renewals, health overrides, MRR reconciling with contracts, late activation, unavailable sections, audited team views, forecasts, costs and CAC, safe CSV export, AI commentary checks, and isolation between workspaces |
| Schema catalog test | Now covers every module schema: forced RLS, a policy, a required `tenant_id` and a `(tenant_id, id)` key on every tenant table |
| Import rules | Sales Intelligence imports no other module and not billing; its SQL touches only its own schema |
| Frontend tests | 49 pass, up from 43 |
| Browser tests | All 6 flows pass, in two full runs in a row. The new flow covers a won deal becoming a customer, coverage and analysis, accepting a suggestion into a CRM deal, MRR on the revenue screen matching the contract, the forecast refusal, and a second workspace seeing none of it |
| Lint and types | Go lint, ESLint, module boundaries, OpenAPI lint and type checks are clean |
| Generated code | `make gen` reproduces all 31 generated files exactly |
| govulncheck | Nothing in called code. One known advisory in a required module's code that is not called, as at Phase 5 |
| pnpm audit | One moderate `minimatch` advisory, through ESLint only (development), as at Phase 5 |

## RLS performance comparison (risk R16)

`TestRLSPerformanceComparison`, run with `MSP_LOAD=1`, seeds 40 workspaces with 1.6 million fact rows and times the queries a metrics rollup runs, 25 times each after warm-up, with RLS bypassed (superuser) and enforced (application role):

| Query | Bypassed, median | Enforced, median | Ratio |
|---|---|---|---|
| One workspace's deal facts (25,000 rows) | 49.1 ms | 48.0 ms | 0.98 |
| One workspace's revenue facts (5,000 rows) | 4.85 ms | 4.90 ms | 1.01 |
| Won deals by month (aggregate) | 5.87 ms | 6.17 ms | 1.05 |
| Campaign sends in the last 90 days (index range) | 0.36 ms | 0.43 ms | 1.19 |

The overhead is within run-to-run noise for the large reads and under a tenth of a millisecond for the small one, because every index leads with `tenant_id`. Relying on the policy alone, without the tenant condition in the query, gave similar times (51.3, 3.9, 6.0 and 0.33 ms). A full rollup for one workspace with 40,000 facts took 0.3 seconds. This is one machine and one dataset shape: a load test against the stated production profile is still due in Phase 7.

## Defects found and fixed during the phase

| Defect | Found by |
|---|---|
| The catalog test that checks every tenant table for forced RLS and a `(tenant_id, id)` key covered only platform schemas, not the ten module schemas. It now covers all of them; it found two partitioned visitor tables, which are exempt with a stated reason | Code review |
| Costs spread over months lost part of a day when an entry's times were not midnight | Unit test |
| A deal opened from a suggestion without an estimate used the reporting currency instead of the customer's own, so the customer's MRR could no longer be shown as one amount | API test |
| PostgreSQL refuses `COPY` into tables under RLS, so the first rollup design could not store metrics | While building |
| Sales Intelligence imported the billing service to read module status. It now receives a read-only function, and the import linter forbids the dependency | Code review |
| The full test suite starved one test's rollup behind every other workspace's | Full test run |
| A customer can appear a moment before its contract, as they arrive by separate events | Browser test |
| An outdated browser test looked for the Sales Intelligence placeholder, which no longer exists | Browser test |

## Decisions made during the phase

| Decision | Choice | Reason |
|---|---|---|
| Reopened deal | Contract returns to draft, as the module spec says | Phase 4 cancelled it. Draft keeps the terms for review, and winning again reactivates the same contract |
| Suggestion estimates | Median of what the workspace's other customers pay for the service | Workspace services have no prices, and Proposals' catalogue is another module's data. An estimate is labelled and never counted as revenue |
| Unknown coverage | Its own suggestion kind, `confirm_coverage` | So it can never be accepted into a deal or carry an estimate |
| Acknowledging a renewal alert | Needs `expansion.write` | The spec table says `expansion.read`, but acknowledging changes state, and viewers have read |
| Renewal notifications | To the contract's owner: the deal owner, or whoever added the contract | Contracts had no owner before |
| Assessments for prospects | Not kept | A profile exists only for customers. An assessment done before a company becomes a customer is not used as evidence |
| Metrics storage | Computed from facts, then stored as monthly snapshots | Reads are cheap and consistent, and each screen can say when it was computed |
| Rollup timing | A few seconds after new activity, gathering events close together; hourly sweep | Fresh enough to feel live, without a rollup per event |
| Campaign names | Kept apart from facts, as labels | Facts hold no text; names are business labels the screens need |
| Costs | Spread by day; converted at the entry's start date | See the questions in [metric-definitions](metric-definitions.md) |
| Forecast key | One live run per workspace, kind, day and horizon | The spec's key, with the horizon added |

## Known gaps

**Waiting on decisions or later phases**
- **Exchange rates.** No source fills the rate table yet (D-05, D-11), so amounts in a currency other than the reporting currency are left out and flagged.
- **Backfill.** The spec's lifecycle `Backfill` operation, which would rebuild facts from other modules' exports, is not built. History comes from the passive consumers, which have run since migration 16; activity before Phase 6 is not in Sales Intelligence.
- **Real AI.** Wording and commentary use the fake provider (D-05).
- **IT Assessment leads.** `it_assessment.lead.captured` is not published, because landing pages, which capture those leads, were deferred in Phase 4.
- **PDF reports.** Exports are CSV only.
- **Load test.** The stated production load profile is a Phase 7 task.

**Smaller gaps**
- **Upsell suggestions.** The kind exists, but no rule produces one yet: it needs seat or device counts, which coverage does not record.
- **Customer names.** Edited in CRM only; Customer Expansion shows them.
- **Notifications** for new or accepted suggestions and won opportunities are published as events, but no notification template uses them yet.
- **Loss reasons** stay in CRM as free text, so win/loss cannot be broken down by reason.
