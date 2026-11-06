# Observability, reliability and performance

Status: planning, pending approval. Targets are engineering goals to validate, not commitments to customers.

## Logs

- Structured JSON from all roles, with `timestamp`, `level`, `role`, `module`, `request_id`, `correlation_id`, `tenant_id`, `user_id` where known.
- Never logged: passwords, tokens, cookies, API keys, full email bodies, chat content, prompts, IP addresses unless needed for a security event, and hashed then.
- A central allowlist-based redactor is used, plus a test that fails on known secret patterns.
- Retention is short and proposed at 30 days, as logs may hold personal data.

## Correlation

Each request gets a `request_id`, returned in `X-Request-Id` and in error bodies. A `correlation_id` follows the work across jobs and events, and `causation_id` links each step, so a whole workflow such as lead to won deal can be traced.

## Metrics

| Area | Metrics |
|---|---|
| API | Rate, errors, duration by route template (never raw IDs) and module |
| Database | Pool saturation by role, slow queries, lock waits, replication if added, partition status, table bloat |
| Jobs | See [background-jobs](background-jobs.md) |
| Outbox | Oldest unpublished age, consumer lag, dead letters |
| Providers | Calls, errors, latency, circuit state, quota remaining |
| AI | Tokens, cost, validation failures, policy blocks |
| Business health | Purchase review age, sends, bounce and complaint rates per sender |
| Ingest | Events accepted and rejected, stream length, flush lag |
| Security | Login failures, denied cross-tenant attempts, signature failures, SSRF denials |

Avoid `tenant_id` as a metric label because it creates high cardinality. Tenant-level detail comes from logs and usage tables, except for a small "top tenants" derived view.

## Tracing

OpenTelemetry instrumentation, with sampling, for API, SQL and provider calls, and context carried on jobs and events. Backend choice is D-21. Span attributes exclude personal data.

## Health endpoints

| Endpoint | Meaning |
|---|---|
| `/healthz` | Process alive; no dependency checks |
| `/readyz` | Can serve: database reachable and migrations compatible. Redis is a degraded state, not failure, for `api` |
| Worker heartbeat | Stored and exposed per pool |

Health endpoints are not publicly routed through Traefik.

## Alerts

Page-worthy: API error burn rate, database unavailable or pool exhausted, outbox stalled, critical pool stalled, audit write failures, backup failure, sudden complaint spike, cross-tenant denial spike. Everything else creates a ticket.

## Targets and assumptions

| Target | Measured how | Assumptions |
|---|---|---|
| 99.9% availability objective, after stabilization | API success rate and synthetic checks, monthly (about 43 minutes of allowed failure) | Requires more than one application host and a database failover plan (D-23). Not achievable on a single-node deployment. Excludes module-specific third-party outages, reported separately |
| p95 below 500 ms for ordinary database-backed APIs | Server-side latency, by route | **Load profile for the first test:** 500 tenants, 2,500 concurrent users at peak, 100 requests per second, and a largest tenant holding 100,000 CRM companies and 500,000 activities. These are placeholders until commercial forecasts exist. Excludes AI, exports, streaming and provider calls, which run as jobs or have their own targets |
| Background jobs recover from temporary provider failure | Fault-injection tests | Failure shorter than total retry window |
| Backup restoration tested | Scheduled restore drill | See below |
| Visitor workload doesn't degrade CRM | CRM p95 stays within 10% during peak ingest test | 2,000 events per second, with bursts, against the separate ingest path |

Interactive chat has its own target: first token within a few seconds, depending on the provider.

## Performance design

- Keyset pagination, and tenant-leading indexes.
- Heavy work is asynchronous: search, enrichment, exports, PDFs, reports.
- `visitor_intelligence.events` and `sessions`, `audit.events`, `jobs.outbox_events` are partitioned by month. A job creates future partitions, and an alert fires if it fails, since a missing partition breaks inserts.
- Dashboards read rollups rather than raw events.
- Connection pools are separated by role and pool, and bounded so ingest or analytics can't take CRM's connections. Statement timeouts differ by role. A connection pooler may be added later, using transaction mode (compatible with `SET LOCAL`).
- Redis caches only rebuildable data, with tenant-prefixed keys. It is not relied on for correctness.

## Backups and recovery

Proposed starting objectives, to be confirmed against cost and D-23: **RPO 15 minutes, RTO 4 hours.**

- Continuous WAL archiving, plus daily base backups, encrypted, with an off-site copy.
- Object storage versioning and cross-location replication, and a copy of the encryption keys held separately from backups.
- Redis isn't backed up as a source of truth.
- A scheduled restore drill goes to an isolated environment, verifying row counts, RLS policies and sample tenant access, and the runbook is timed.
- Erasure after restore: a restored backup can bring back erased data, so a replay log of erasure requests is reapplied after any restore.

## Deployment

Docker images, Traefik with TLS, one image with role selected at start. Rolling updates use expand-then-contract migrations, and a health-gated rollout. The production topology, including whether a managed database is used, is D-23.
