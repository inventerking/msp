# Load testing

Status: **first run 2026-11-18. The CRM target is met; the visitor-ingest targets are not.** The cause is known and the fix is a decision for the Phase 7 gate.

## Profile

From [observability-and-reliability](../architecture/observability-and-reliability.md), still placeholders until commercial forecasts exist: 500 tenants; a largest tenant with 100,000 CRM companies and 500,000 activities; 100 requests per second of ordinary database-backed API calls; 2,000 visitor events per second against the ingest path; CRM p95 below 500 ms, and within 10% of its normal value during peak ingest.

## How it is run

```bash
cd backend
go test -tags loadtest -run TestLoadProfile -timeout 90m -v ./internal/api/ 2>&1 | grep -v 'INFO request'
```

[load_test.go](../../backend/internal/api/load_test.go) starts PostgreSQL in a container and the full API in-process, with the production pool sizes (20 application connections). It creates the tenants through the real sign-up flow and seeds the largest tenant as the application role, so row-level security applies. Requests arrive at a fixed rate whatever the latency, so a slow server shows as latency. The mix is three-quarters reads (lists, search, detail, activity timelines, session context) and one in ten writes; a quarter of traffic goes to the largest tenant. Phase A runs CRM traffic alone for two minutes; phase B adds ingest. Knobs are environment variables listed in the file.

**Limits of this run.** One developer machine, with the load generator, API and database competing for the same CPU. Rate limiting used the in-memory limiter, not Redis. The numbers are a floor for one API process, not a statement about production capacity.

## Result

Profile: 500 tenants; largest tenant 100000 companies and 500000 activities; CRM 100 requests/s; ingest 2000 events/s to 50 sites; 2m0s per phase. Seeding took 3m13s.

**Phase A: CRM only**

| Operation | Requests | Errors | Rate limited | p50 | p95 | p99 | Max |
|---|---|---|---|---|---|---|---|
| all CRM | 12000 | 0 | 0 | 6.2 ms | 237.5 ms | 480.7 ms | 1481.7 ms |
| company activities | 1729 | 0 | 0 | 6.7 ms | 284.7 ms | 491.5 ms | 897.3 ms |
| get company | 2406 | 0 | 0 | 4.5 ms | 26.8 ms | 142.6 ms | 294.8 ms |
| list companies | 3598 | 0 | 0 | 5.7 ms | 149.0 ms | 283.1 ms | 825.2 ms |
| log activity | 1231 | 0 | 0 | 9.0 ms | 44.5 ms | 204.9 ms | 320.7 ms |
| search companies | 1857 | 0 | 0 | 6.9 ms | 486.9 ms | 764.7 ms | 1481.7 ms |
| session context | 1179 | 0 | 0 | 4.7 ms | 27.7 ms | 127.7 ms | 399.6 ms |

**Phase B: CRM with visitor ingest**

| Operation | Requests | Errors | Rate limited | p50 | p95 | p99 | Max |
|---|---|---|---|---|---|---|---|
| all CRM | 12001 | 0 | 0 | 7479.0 ms | 10941.7 ms | 11287.9 ms | 13377.7 ms |
| company activities | 1729 | 0 | 0 | 7593.2 ms | 10994.0 ms | 11510.8 ms | 12946.5 ms |
| get company | 2406 | 0 | 0 | 7376.6 ms | 10845.5 ms | 11211.8 ms | 11326.7 ms |
| ingest | 59835 | 0 | 0 | 4220.4 ms | 8295.0 ms | 8741.7 ms | 9016.1 ms |
| list companies | 3598 | 0 | 0 | 7495.5 ms | 10926.9 ms | 11272.8 ms | 11983.8 ms |
| log activity | 1232 | 0 | 0 | 7414.3 ms | 10881.3 ms | 11254.4 ms | 11338.8 ms |
| search companies | 1857 | 0 | 0 | 7518.9 ms | 11117.6 ms | 11926.6 ms | 13377.7 ms |
| session context | 1179 | 0 | 0 | 7286.5 ms | 10873.0 ms | 11257.7 ms | 11329.6 ms |

| Target | Result | Met? |
|---|---|---|
| CRM p95 below 500 ms | 237.5 ms alone, 10941.7 ms with ingest | **No** |
| CRM p95 within 10% during ingest | +4508% | **No** |
| Ingest sustains 2000 events/s | 499 events/s accepted at p95 8295.0 ms, 0 errors | **No** |
| No errors in CRM traffic | 0 errors | Yes |

## What it shows

1. **CRM on its own meets the target** with room to spare, at p95 238 ms and no errors, even with a quarter of traffic in the 100,000-company tenant. Company search in that tenant is the slowest route (p95 487 ms): it is the first to watch as tenants grow.
2. **Visitor ingest takes the API down with it.** Each tracked page view runs a transaction on the same 20-connection pool that CRM uses, plus follow-up checks. At 2,000 events per second the pool is saturated: ingest accepted only about 500 per second, and every CRM request queued behind it, to a p95 near 11 seconds. Nothing failed, but everything waited.
3. This is the risk the architecture named: connection pools are meant to be "separated by role and pool, and bounded so ingest or analytics can't take CRM's connections", with ingest on "the separate ingest path". The separate `ingest` process role was planned for Phase 5 and never built: `engine ingest` still exits with a message.

A smaller dry run (20 tenants, 200 events per second) already showed the effect: CRM p99 rose from 17 ms to 237 ms.

## Recommended fix (needs approval)

Build the `ingest` role: the same binary serving only `/public/collect`, with its own small database pool as `msp_ingest`, and with follow-up work (business identification, conversions) moved to jobs. On the testing server it runs as its own container; Traefik sends `/api/v1/public/collect` to it. Then repeat this test. If one ingest process cannot reach 2,000 events per second, the next steps are batching inserts, or revising the target, which the roadmap allows if it is done formally.
