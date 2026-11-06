# Tenant isolation

Status: planning, pending approval. The mechanism is described in [multi-tenancy](../architecture/multi-tenancy.md). This document covers what must be private, where leaks could occur, and how isolation is proven.

## Private to a tenant

All of: CRM data, campaigns and message content, notes, lead scores, enrichment licensed to the tenant, prospect relationships, competitor designations and observations, assessments, proposals and pricing, visitor data, chat transcripts and knowledge, recommendations, contracts, metrics, forecasts, usage details, and the fact that a tenant has an interest in a company.

## Leak paths and controls

| Path | Example | Control |
|---|---|---|
| Missing filter | A query without `tenant_id` | RLS, SQL lint, tests |
| ID guessing | Fetch `/crm/deals/{otherTenantId}` | RLS gives `404` |
| Logical ref | Link a proposal to another tenant's deal ID | Service validates through the owner's contract, under RLS |
| Canonical data | Tenant note stored in `shared.companies` | Licence-class write rules, no free text path |
| Company search signal | "Already in your platform" hint | No such signal; results only from public or shareable sources |
| Dedupe signal | Timing or errors reveal another tenant's email | Contacts are tenant-scoped |
| Global suppression | Reveals that someone complained about another MSP | Only reports "not sendable" with a generic reason; D-09 |
| Cache | Redis key lacking a tenant prefix | Key helper requires tenant; query cache keyed by tenant |
| Frontend state | Workspace switch shows old data | Cache cleared on switch |
| Object storage | Guess a file path | Private buckets; tenant-prefixed keys; signed grants only after check |
| Shared link | Token for a proposal | High-entropy, scoped to one object; rate limited |
| Background job | A job runs with the wrong tenant | Tenant from the job row; `SET LOCAL`; RLS |
| Event | A consumer applies data to the wrong tenant | Consumer context from envelope; composite FKs |
| AI retrieval | Vector query across tenants | SQL filter on tenant and bot, plus RLS |
| AI provider | Prompt from A influences B | Stateless calls; no shared context or caches across tenants |
| Prompt caching | Provider prompt cache spans tenants | Only static system text may be shared; tenant data excluded from shared cache prefixes |
| Logs and metrics | Support sees another tenant's data | Redaction; role-limited log access |
| Analytics | Benchmarks reveal competitor numbers | No cross-tenant benchmarks |
| Platform admin | Over-broad view | Roles, metadata-first, time-limited grants |
| Connection pool | `SET` persists to the next request | Only `SET LOCAL`; lint forbids `SET` |
| Backups | Restore mixes data | Whole-database restores only, with a tested runbook; per-tenant restore via export |
| Webhooks | Forged payload changes tenant | Tenant from endpoint token; signature required |
| Error messages | "Email already belongs to..." | Generic text; `404` for foreign objects |

## Proof by testing

The isolation suite runs in CI for each change, and it is designed to fail automatically for new code.

**1. Schema tests.** Query the database catalog, and fail if a tenant-owned table:
- lacks `tenant_id`, RLS enabled, RLS forced, or a policy;
- lacks `UNIQUE (tenant_id, id)`;
- has an FK to another tenant-owned table that doesn't include `tenant_id`;
- is missing from the tenant purge registry.

Also: the runtime role has no `BYPASSRLS`, and doesn't own tables.

**2. Route tests.** Generated from the OpenAPI document. For each tenant route, with Tenant A's resource and Tenant B's session, the test expects `404` or `403`, and no A data in the response. A route not covered fails CI.

**3. RLS tests.** With no tenant set: zero rows. With tenant B set: A's rows invisible and inserts for A rejected.

**4. Cross-reference tests.** Attempts to create objects referencing another tenant's IDs, through every logical ref in the data dictionary.

**5. Non-API channels.** Jobs, events, SSE, WebSocket, exports, PDF, signed links, global search, webhooks, AI retrieval.

**6. Canonical leak test.** Tenant A adds private notes and tenant-licensed enrichment to a company. Tenant B searches and views the company, and the test asserts that no A data, or sign of A's interest, appears. A's notes appear nowhere in `shared` tables.

**7. Canary scan.** Tenant A's fixtures hold unique marker strings. After the full end-to-end run, every response captured for Tenant B, and all logs, are scanned for those markers.

## End-to-end check

The scenario in [testing-strategy](../implementation/testing-strategy.md) runs with Tenant B attempting access at each stage.

## Incident response for suspected leaks

Immediately: preserve logs, identify the affected tenants through audit and access records, contain by disabling the route or feature, and assess notification duties under contracts and law with counsel. The runbook is a Phase 7 deliverable.
