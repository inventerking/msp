# Architecture decisions

Status: planning, pending approval. "Confirmed" means fixed by the brief. "Approved" means recommended here and approved by the product owner on 2026-11-06. Related follow-up work is in [open-decisions](../implementation/open-decisions.md).

| ADR | Decision | Status |
|---|---|---|
| 001 | Go modular monolith | Confirmed |
| 002 | One codebase, several process roles | Approved |
| 003 | Hybrid frontend: one client build, separate admin app | Confirmed |
| 004 | One PostgreSQL database with 18 schemas | Confirmed |
| 005 | Module boundaries enforced by tooling and FK rules | Approved |
| 006 | Layered tenant isolation, including forced RLS | Approved |
| 007 | Opaque server-side sessions in cookies | Approved |
| 008 | Products and entitlements are separate | Confirmed |
| 009 | Contact identity is tenant-scoped | Approved (D-06) |
| 010 | Canonical company data is limited by licence class | Approved |
| 011 | Outbox on PostgreSQL, no message broker | Approved |
| 012 | PostgreSQL holds job state, Redis only coordinates | Approved |
| 013 | One AI Gateway with four action classes | Confirmed in principle |
| 014 | Entitlements gate access and costly actions, not passive projections | Approved (D-02, D-03) |
| 015 | REST with OpenAPI; SSE by default, WebSocket only for live chat | Approved |
| 016 | Separate ingest role for visitor events | Approved |
| 017 | Money as integer minor units | Approved |
| 018 | Immutable snapshots of commercial and approved content | Confirmed in principle |
| 019 | All tenant-influenced outbound requests go through an egress guard | Approved |

## ADR-001: Go modular monolith

**Context.** Ten modules share identity, data and workflows, and the team has not yet needed independent scaling per module.
**Decision.** One Go backend with strict internal module boundaries. No microservices.
**Consequences.** Simple deployment and local transactions. Boundaries depend on discipline, which ADR-005 enforces with tooling.
**Revisit when.** A documented need appears that process roles cannot meet. Extraction needs separate approval.

## ADR-002: One codebase, several process roles

**Decision.** One binary runs as `api`, `worker`, or `ingest`, plus a one-shot `migrate` command. Roles scale separately.
**Why.** Visitor tracking traffic and slow provider calls must not take capacity from CRM requests. This gives that isolation without new services: the code, release and database stay the same.
**Consequences.** All roles deploy from one image and the same version. This is not microservices.

## ADR-003: Hybrid frontend

**Decision.** One client app with ten feature modules in one build, plus a separate admin app. Runtime microfrontends are excluded.
**Consequences.** Module code is lazy-loaded by route, so bundles stay small and a user does not download modules their tenant lacks. Embeddable widgets are separately built artefacts because they run on third-party sites (D-15).

## ADR-004: One database, 18 schemas

**Decision.** As in the brief. Schema ownership is logical, one owner per schema.
**Consequences.** Cross-module joins are technically possible and forbidden by policy. High-volume data is partitioned, and its load limited by ADR-016. A read replica may be added later for analytics; it is the same database, but it still needs approval when proposed.

## ADR-005: Module boundaries are enforced

**Decision.**
1. Each module exposes only a `contract` package. Other modules may import only that.
2. An import linter fails the build on any other cross-module import, or on a module cycle.
3. Each repository only touches its own schema. A static check on SQL files enforces this.
4. A hard foreign key may cross a schema only to `identity.tenants`, `shared.companies` or `shared.contacts`. Everything else is a logical ref validated by the owning module's service.
5. One database transaction belongs to one module, plus its outbox and audit writes.

**Why the three FK exceptions.** They are the shared kernel the brief calls for. Other cross-module FKs would make modules impossible to reason about separately.
**Why rule 5.** Without it, a "module boundary" is only a folder name. Cross-module consistency uses events or explicit idempotent commands.
**Optional hardening.** `SET LOCAL ROLE` to a per-module database role would make the database refuse wrong-schema access. It is proposed for evaluation in Phase 2.

## ADR-006: Layered tenant isolation

**Decision.** Tenant context from the server-side session, repository scoping, composite foreign keys, and forced RLS with `SET LOCAL app.tenant_id`.
**Why RLS.** The brief asks for an evaluation of RLS. The finding is to adopt it: one missing `WHERE tenant_id` in 205 tables is the most likely serious defect this product could have. RLS turns that defect into an empty result rather than a data leak.
**Cost.** Slight query overhead, a transaction for each tenant-scoped database interaction, and careful role setup. These are accepted. See [multi-tenancy](multi-tenancy.md).

## ADR-007: Opaque server-side sessions

**Decision.** Random session tokens, stored only as hashes, delivered in `__Host-` cookies that are `HttpOnly`, `Secure`, `SameSite=Lax`. No JWTs for browser sessions.
**Why.** The brief requires session revocation. Revocation is immediate with server-side sessions and awkward with JWTs. Platform admin sessions use separate tables, cookies and hostname.

## ADR-008: Products and entitlements are separate

**Decision.** Modules are technical capabilities; products are what is sold. Subscriptions create grants; grants project into effective entitlements. Request-time checks read only effective entitlements.
**Consequences.** Bundles can change without code changes, and a product change never alters existing subscribers because product versions are immutable. See [commercial-and-activation](commercial-and-activation.md).

## ADR-009: Contact identity is tenant-scoped

**Context.** The brief says the company separation "must be considered" for contacts.
**Decision.** Unlike companies, `shared.contacts` has `tenant_id NOT NULL`. There is no global person record. The same person at two MSPs is two rows.
**Why.**
- MSPs are likely the controllers of their contact data and the platform a processor. A cross-tenant person record could make the platform a controller.
- A global record creates a cross-tenant inference channel ("this person is already known").
- Erasure and consent are simpler when they are per-tenant.
- Provider licences may not allow reuse between customers.

**Trade-off.** Enrichment may be purchased twice for the same person, costing more. Accepted, subject to D-06.

## ADR-010: Canonical companies limited by licence class

**Decision.** `shared.companies` and `shared.company_source_facts` hold only data marked `public` or `licensed_shareable`. Tenant-licensed values stay in that tenant's module schema. Nothing a tenant types ever reaches a global table.
**Why.** This is the main leak path in a shared-identity design. If a tenant-entered note reached a canonical record, another MSP could read it.
**Side-channel control.** A company search result for tenant B never reveals whether another tenant holds a relationship with that company.

## ADR-011: Outbox on PostgreSQL, no broker

**Decision.** Events go into `jobs.outbox_events` in the business transaction. A dispatcher delivers them to in-process consumers, tracking each in `jobs.event_deliveries`. Delivery is at-least-once, with idempotent consumers.
**Why not a broker.** All consumers are in one codebase. The brief discourages additional infrastructure, and nothing here needs one yet.
**Revisit when.** Sustained event throughput or external consumers justify it.

## ADR-012: PostgreSQL holds job state

**Decision.** `jobs.jobs` is the source of truth, claimed with `FOR UPDATE SKIP LOCKED`. Redis handles rate-limit tokens, tenant concurrency counters and wake-up signals. Losing Redis slows jobs but loses none. Build-or-adopt for the queue library is in D-21.

## ADR-013: One AI Gateway

**Decision.** All AI calls pass through one service that handles routing, quotas, prompt versions, structured output validation, and logging policy. Modules call it with explicit, minimal input, and AI has no database credentials or arbitrary tools. See [ai-gateway](ai-gateway.md).

## ADR-014: Entitlements gate access and costly actions, not passive projections

**Decision.** Without an entitlement, a tenant cannot use a module's UI, APIs, or provider or AI spending. But consumers that passively record facts still run for all tenants, namely Sales Intelligence facts and Customer Expansion contracts.
**Why.** Otherwise a tenant buying Sales Intelligence in month 12 sees an empty dashboard despite a year of activity, and backfilling across module boundaries is unreliable.
**Trade-off.** Stores derived data the tenant has not yet paid to see. Facts hold no personal data, and tenant purge removes them. Subject to D-03.

## ADR-015: REST and streaming

**Decision.** REST under `/api/v1/` documented by OpenAPI. Use SSE for progress and notifications because it is one-way and works over plain HTTP. Use WebSocket only for live human-handoff chat, which is two-way. Redis pub/sub fans out across API instances, with clients able to re-fetch current state, so a lost message causes no data loss.

## ADR-016: Separate ingest role

**Decision.** Tracker events go to a small `ingest` role that validates the site key, applies consent rules, discards IPs after use, and pushes to a Redis Stream. A worker batches them into partitioned tables using a limited connection pool.
**Why.** The brief requires that visitor workloads not degrade CRM.
**Trade-off.** Events could be lost if Redis fails before a flush. This is acceptable for analytics, but not for form conversions, which are written synchronously through the API.

## ADR-017: Money as integer minor units

**Decision.** `bigint` minor units plus ISO 4217 currency; basis points for discounts. Rounding rules live in one tested package. Zero-decimal and three-decimal currencies are handled through that package.

## ADR-018: Immutable snapshots

**Decision.** Issued proposal versions, purchase quotes, approved campaign content and published product versions never change. A database trigger blocks updates to issued proposal versions, and approvals store a content hash. If the content hash differs at send time, approval is invalid.

## ADR-019: Egress guard

**Decision.** Assessment checks, competitor research fetches and knowledge URL imports all use one guarded HTTP/DNS client. It resolves the name, rejects private, loopback, link-local, metadata, CGNAT and other reserved ranges (IPv4, IPv6 and mapped forms), pins the connection to the validated IP, and re-validates each redirect. These jobs run in a separate worker pool with container-level egress limits, so one code bug is not sufficient for internal access. A forward proxy is not proposed at first.
