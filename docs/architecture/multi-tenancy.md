# Multi-tenancy

Status: planning, pending approval. The security view of this design, including the tests that prove it, is in [tenant-isolation](../security/tenant-isolation.md).

## Model

- A **tenant** is one MSP workspace. All its members share its module entitlements.
- A **user** can belong to several tenants through memberships, with one role in each.
- A **platform admin** is a different identity in different tables, and is never a tenant member.

## How tenant context is established

The tenant ID always comes from the server-side session, never from a request.

1. The session row holds `active_tenant_id`.
2. Middleware checks the user has an active membership in that tenant and that the tenant isn't suspended.
3. The tenant ID goes into the request context, and a tenant ID in a body, path or query is never used as authority.
4. `POST /api/v1/me/active-tenant` changes the active tenant only after a membership check. The action is audited.

Public surfaces derive the tenant from a server-side lookup of a credential: the tracker's site key, the chat widget's bot key, a landing page slug, a hashed proposal share link, or a webhook endpoint token. Workers read tenant ID from the job row, which the server wrote.

## Enforcement layers

| Layer | Control | What it stops |
|---|---|---|
| 1. API | Tenant from session, membership checked | A forged tenant ID |
| 2. Service | Object checks, logical refs validated against the caller's tenant | Referencing another tenant's object through a logical ref |
| 3. Repository | Queries always filter on `tenant_id`, enforced by a static SQL check | A developer's missing filter |
| 4. Database constraint | Composite foreign keys `(tenant_id, x_id)` | A cross-tenant reference inside a schema |
| 5. RLS | Forced policies using `app.tenant_id` | Any query that bypasses layer 3 |
| 6. Cache and storage | Tenant-prefixed Redis keys and object paths; signed links tied to access checks | A leak through a side store |
| 7. AI retrieval | Queries filtered by tenant and bot, in code and RLS | A prompt or retrieval reaching other tenants' data |
| 8. Tests | Automated isolation suite over every route and table | Regressions |

## RLS design

**Pattern for tenant-owned tables:**

- Policy: row visible only when `tenant_id = current_setting('app.tenant_id')::uuid`. `missing_ok` is used so that an unset value returns zero rows rather than an error path that might be bypassed.
- `ENABLE` and `FORCE ROW LEVEL SECURITY` are both set, so the table owner is also subject to it.
- `WITH CHECK` applies the same condition, so an insert cannot write to another tenant.

**Database roles:**

| Role | Used by | RLS | Notes |
|---|---|---|---|
| `msp_owner` | Migrations only | Owner | Never used at runtime |
| `msp_app` | API and workers for tenant work | Enforced | No `BYPASSRLS`, no DDL |
| `msp_platform` | Platform-admin use cases | Scoped policies | Explicit cross-tenant read on named tables only; each use case audited |
| `msp_system` | Schedulers, dispatch and housekeeping | Limited | Reads job, schedule and outbox tables across tenants, and removes expired sessions and tokens |
| `msp_ingest` | Visitor batch writer | Enforced | Insert only on visitor tables, with per-batch tenant context |
| `msp_readonly` | Support diagnostics, backups verification | Enforced | Excludes credential and PII columns |

**Connection pooling.** `SET LOCAL` lasts only until the transaction ends, so it works with transaction-mode pooling and cannot leak to the next user of a connection. This is why every tenant unit of work runs in a transaction. A plain `SET` is banned by lint.

**Performance.** Every tenant table has `UNIQUE (tenant_id, id)`, and common indexes lead with `tenant_id`, so the RLS condition aligns with an index. Load tests in Phase 6 confirm the overhead. A comparison with RLS disabled is part of that test.

**Mixed tables.** Rows with `tenant_id` null, such as system roles or platform jobs, are readable by tenants only where a policy permits it. System roles are the only such case. Other mixed tables are not exposed to tenant users.

## Shared global data

| Global data | Tenant-visible? | Control |
|---|---|---|
| `shared.companies`, identifiers, source facts | Yes, through company search only | Only public or shareable data; no signal of other tenants' interest |
| Reference data | Yes | Read-only |
| Product catalog | Published items only | Service filter |
| Prompts and model routes | No | Platform only |

## Tenant lifecycle

| State | Behaviour |
|---|---|
| `pending` | Registered, email not verified or profile incomplete |
| `active` | Normal |
| `suspended` | Sign-in shows a status page; jobs paused; outbound sends and sequences stopped; public surfaces disabled; data retained |
| `offboarding` | Export available; no new activity; retention timer running (D-14) |
| `deleted` | Purge job removes tenant data, keeping only legally required records, such as invoices |

Tenant purge is a registered operation: every module implements a purge contract, and a completeness test checks that each tenant-owned table is covered by one. Suppression records need special treatment because the MSP's do-not-contact duty may outlive the tenant, a point for D-14 and legal review.

## Noisy neighbours

Fair sharing is per tenant: API rate limits, job concurrency limits per tenant and queue, provider quota shares, ingest limits per site, and storage and credit limits. Details are in [background-jobs](background-jobs.md).
