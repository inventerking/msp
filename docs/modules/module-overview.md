# Module overview

Status: planning, pending approval.

## The ten modules

| # | Module | Key | Schema | Owns | Does not own |
|---|---|---|---|---|---|
| 1 | Lead Generation | `lead_generation` | `lead_generation` | Discovery, enrichment, scoring, lists | Sales pipeline |
| 2 | Competitor Intelligence | `competitor_intelligence` | `competitor_intelligence` | Competitors, observations, opportunities | Canonical company record |
| 3 | Outreach | `outreach` | `outreach` | Campaigns, sequences, messages, suppressions | Contact identity, CRM activity |
| 4 | IT Assessment | `it_assessment` | `it_assessment` | Authorizations, checks, findings, reports | Deals, meetings |
| 5 | CRM | `crm` | `crm` | Customer relationships, pipeline, deals, activities | Canonical identity, contracts |
| 6 | Proposals | `proposals` | `proposals` | Service catalog, proposals, versions, signatures | Deals, customer identity |
| 7 | Visitor Intelligence | `visitor_intelligence` | `visitor_intelligence` | Sites, sessions, conversions | Contact identity |
| 8 | Chatbot | `chatbot` | `chatbot` | Bots, knowledge, conversations, bookings | CRM meetings, calendars |
| 9 | Customer Expansion | `customer_expansion` | `customer_expansion` | Coverage, contracts, renewals, recommendations | Customer identity, deals |
| 10 | Sales Intelligence | `sales_intelligence` | `sales_intelligence` | Facts, metrics, forecasts | CRM or billing transactions |

## Rules shared by all modules

To avoid repeating these in every document:

- **Authentication and tenancy.** All tenant routes require a valid session, active membership, an active entitlement for the module, and the listed permission. Tenant ID always comes from the session.
- **Standard list and error behaviour.** See [api-overview](../api/api-overview.md) and [error-conventions](../api/error-conventions.md).
- **Mutations.** Each runs in one module transaction with audit and outbox. Unsafe calls with external effects require `Idempotency-Key`.
- **Standard tests.** Each module has a tenant isolation test for every route and table, an entitlement test (blocked when suspended), and a permission matrix test.

## Standalone purchase behaviour

Modules can be bought alone, so each module must do something useful without the others. This table is the design answer to that requirement.

| Module | Works alone by | If this module is also present |
|---|---|---|
| Lead Generation | Search, score, list, export | CRM: "convert" becomes a pipeline action. Outreach: send to list |
| Competitor Intelligence | Fully independent | Lead Generation: shared canonical company |
| Outreach | Importing contacts into `shared.contacts`, or using tenant's own list | CRM: replies become activities. Lead Generation: audience from lists |
| IT Assessment | Landing pages, reports, captured leads visible in the module | CRM: creates activity and updates stage |
| CRM | Fully independent | Other modules enrich the timeline |
| Proposals | Uses CRM registry for customer name (D-02) | CRM: linked to a deal; signing updates the deal |
| Visitor Intelligence | Own analytics, business identification | CRM: conversions become activities |
| Chatbot | Own conversations, leads and direct calendar booking | CRM: meeting and contact sync |
| Customer Expansion | Uses CRM registry; customers entered or imported (D-02) | CRM: deals and customer data; recommendations create deals |
| Sales Intelligence | Reports whatever facts exist; unavailable sections say why | More modules, more sections |

### The CRM registry contract (D-02)

CRM owns customer identity, and yet modules that need a customer record can be bought without CRM. The recommended resolution is to make the `crm.companies` and `crm.contacts` data plane always available internally, through `crm.contract.Registry`. That lets Proposals or Customer Expansion create a record without a CRM entitlement. The CRM screens, deals and pipeline remain gated. If the tenant buys CRM later, those records are already there.

Alternatives are in [open-decisions](../implementation/open-decisions.md).

## Dependency summary

See [dependency-matrix](../implementation/dependency-matrix.md).

## Evidence labelling

Several modules present facts of differing certainty. A shared vocabulary keeps them consistent.

| Label | Meaning | UI treatment |
|---|---|---|
| `verified_public` | Directly seen in an authoritative public source, with link | Source link |
| `observed` | Seen in a source, not independently verified | Source link and date |
| `inferred` | Derived by rule from evidence | "Inferred" badge and reasoning |
| `ai_estimate` | AI-generated estimate | "AI estimate" badge; never appears as fact |

Sales Intelligence uses a parallel set for numbers: `actual`, `estimated`, `forecast`. IT Assessment uses `informational`, `potential_issue`, `confirmed_misconfiguration`.
