# Phase 2 status

Status: built and verified locally. **Phase 2 gate approved by the product owner on 2026-11-09.**

## What exists

| Service | Built | Key properties |
|---|---|---|
| Background jobs | PostgreSQL-backed queue with pools, retries, dead letters, cancellation, schedules | A job is written in the same transaction as the change that needs it. A crashed worker's job is retried, and a worker whose lock lapsed cannot overwrite the retry. One tenant's batch cannot block another's, because a tenant at its limit is passed over |
| Events and outbox | Transactional outbox, per-consumer deliveries, per-aggregate ordering, replay | A rollback writes no event. A crash after commit still delivers. A consumer's effect and its delivery mark commit together, so a repeat changes nothing. A poison event goes to dead letters without blocking others |
| Company identity | Canonical companies, identifiers, merges, search | Only public or shareable-licensed facts can enter, and the database enforces it. No column records which tenant created a company. Merges are made by people, and modules hear of them through an event |
| Contact identity | Tenant-scoped contacts, sources, consent, erasure, export | The same person at two MSPs is two rows, and the email hash differs per tenant. Erasure runs through every registered module and retries until all succeed |
| Credits | Meters, grants, reserve, commit, release, sweep | Concurrent reservations cannot overspend. Settling a reservation twice changes nothing. A tenant request cannot change its own balance |
| AI Gateway | Tasks, routes, versioned prompts, output validation, action classes, circuit breaker | Only the fake provider exists (D-05). Unregistered tasks are refused. Untrusted content is fenced with a random marker. Output must match a strict schema. Prompt and response content is not stored |
| Integrations | Provider registry, encrypted credentials, call logs, circuit breaker, webhook inbox | Webhooks need a valid, fresh signature. The tenant comes from the endpoint record, never the payload. Repeats are stored once. Capability interfaces and fakes exist for company data, email and calendar |
| Egress guard | Guarded HTTP client for tenant-influenced URLs | Every resolved address must be public, connections are pinned to the checked address, and redirects are checked again. Covers IPv4-mapped IPv6, NAT64, 6to4, numeric hostnames and environment proxy settings |
| Files | Uploads through the API, private bucket, malware scanning, signed links | Type comes from the bytes, not the client. Storage keys never use the file name. A file is unusable until scanned clean, and a missing or failing scanner keeps it blocked |
| PDF | Escaping templates and a Gotenberg client | Gotenberg runs with no network route and refuses all URLs except its own temporary files and inline images |
| Notifications | In-app and email, preferences, event-driven | Email runs as a job, so an email outage loses nothing. A replayed event notifies nobody twice. Security notices cannot be turned off |
| Analytics plumbing | Value kinds, metric registry, currency conversion | Actual, estimated and forecast values refuse to be added together. A missing exchange rate gives no result rather than a guess |
| Admin screens | Integrations, AI routes and prompts, jobs and dead letters, tenant credits | Each is limited to the roles in the permission design |
| Client screens | Notification bell, credits and AI usage | |

## Verification results

| Check | Result |
|---|---|
| Backend tests, with race detector, against real PostgreSQL | 394 pass |
| Live tests against the real local services | SeaweedFS stores and refuses wrong credentials. ClamAV passes a clean file and flags the EICAR test file. Gotenberg made no network requests from a hostile document |
| Proof that the Gotenberg test can fail | An unhardened Gotenberg made 4 requests to the test server from the same document, so the hardening is both real and needed |
| Frontend tests | 31 pass |
| Browser end-to-end flows | 2 pass, twice in a row, against the full stack |
| Go lint, ESLint, module boundaries, OpenAPI lint, type checks | Clean |
| govulncheck and pnpm audit | No known vulnerabilities |

## Defects found and fixed during the phase

| Defect | How it was found |
|---|---|
| Routes with required query parameters returned a validation error to anonymous callers, before checking authentication | Generated route test |
| The admin job-stats page failed, because the platform role could not read the outbox | API test |
| An old database schema caused a 500 on whichever request first touched a new table. The API and worker now refuse to start, with a clear message, if the schema is behind the code | Browser test |
| Default Gotenberg settings let a document make network requests, which on the internal network could reach other services | Live test |
| The testing stack gave the migration job file-storage keys it does not use | Configuration review |
| The worker lacked the credentials to run real jobs on the testing stack | Configuration review |
| Vulnerability GO-2026-6865 in an indirect compression library | govulncheck; fixed by upgrading to 1.20.2 |
| The earlier `golang.org/x/crypto` finding no longer appears | govulncheck |

## Decisions made during the phase

| Decision | Choice | Reason |
|---|---|---|
| Job queue (D-21) | Small custom queue on `jobs.jobs` | River is pre-1.0, and fair sharing between tenants needed custom work either way |
| Fair sharing | In the claim query, not Redis | No extra moving part. A small overshoot under heavy concurrency is possible and harmless |
| Wake-ups | Polling every second | LISTEN/NOTIFY can be added if latency matters |
| Uploads | Through the API, up to 20 MB | Keeps the bucket private, with no public storage endpoint |
| File storage | SeaweedFS 4.52 | MinIO's repository is archived |
| AI provider | Fake only (D-05) | Approved recommendation: nothing leaves the system until a provider's data terms are checked |
| Industry list (D-16) | 16 broad industries | Mapped to a provider's taxonomy when one is chosen |
| Fuzzy search (D-17) | `pg_trgm` | No separate search engine |
| AI and outbox records | In the `integration` and `jobs` schemas | No schemas beyond the planned 18 |

## Known gaps

**Built later, when a phase needs them**
- Real providers for AI, company data, email and calendar. Only interfaces and fakes exist.
- Embeddings, for the Chatbot in Phase 5.
- Tenant OAuth connections, for Outreach and Chatbot in Phase 5.
- Tenant purge orchestration. Erasure exists, but whole-tenant deletion is still to come.

**Smaller gaps**
- The reference country list is not seeded. Profiles accept any two-letter code.
- No admin screen yet for company merges or event replay, which exist only in the API.
- Worker wake-up is by polling, so a job can wait up to a second before starting.
- Metrics and tracing are still not wired in.
- The testing server stack has the new services but has not yet been run end to end with them. It was last smoke-tested in Phase 1.
- ClamAV needs about 1.5 GB of memory and internet access to update its signatures.
