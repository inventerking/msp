# Testing strategy

Status: planning, pending approval.

## Test types

| # | Type | Scope | Tools (proposed in D-21) | Runs |
|---|---|---|---|---|
| 1 | Unit | Domain rules, formulas, state machines | Go tests, TypeScript test runner | Every change |
| 2 | Module service | Use cases with real database, contract fakes | Go with test PostgreSQL | Every change |
| 3 | Repository | SQL behaviour, RLS | Real PostgreSQL in containers, never mocked | Every change |
| 4 | API | Handlers, validation, error codes | HTTP tests from OpenAPI | Every change |
| 5 | Integration | Module with real jobs, outbox and consumers | Containerized dependencies | Every change |
| 6 | Contract | Contract conformance, event schemas, adapter suites, OpenAPI parity | Shared conformance | Every change |
| 7 | Migration | Empty install, upgrade, backward compatibility | Previous-schema snapshot | Migration changes |
| 8 | Tenant isolation | See [tenant-isolation](../security/tenant-isolation.md) | Generated from schema and OpenAPI | Every change |
| 9 | Authorization | Full permission matrix, per route | Generated | Every change |
| 10 | Authentication | Sessions, MFA, throttling, reset | API tests | Every change |
| 11 | AI safety | Adversarial sets, output validation, leakage | Fake provider for logic; real model evaluations separately | Logic on every change; evaluation on prompt or model change |
| 12 | Job, retry, idempotency | Crash, duplicate, retries, cancel | Fault injection | Every change |
| 13 | Webhook | Signatures, replay, ordering | Fixtures from provider docs | Every change |
| 14 | Frontend component | Components, accessibility | Component tests | Every change |
| 15 | End to end | Main journeys | Playwright against a full environment | Nightly and before release |
| 16 | Load | Stated profile, ingest, RLS comparison | Load tool | Before gates |
| 17 | Backup and restore | Drill | Runbook | Scheduled |
| 18 | Security | SAST, dependency, secrets, DAST, penetration test | CI and independent tester | CI; before launch |

## Principles

- **Real PostgreSQL for anything touching data.** RLS, constraints and triggers are part of the design, so mocks would miss key defects.
- **External providers are faked in normal CI.** Each adapter also has a contract suite against recorded fixtures and a scheduled sandbox run where available. CI never sends a real email.
- **Tests that keep themselves complete.** Schema, route, audit and authorization tests are generated, so a new table or endpoint fails if not covered.
- **Coverage is a signal, not a goal.** Priority goes to business rules, permissions and failure paths.

## High-value focused tests

| Area | Must be proved |
|---|---|
| Scoring | Missing data doesn't penalize |
| Commercial | Setup, MRR, TCV across pricing models and currencies; immutability |
| Outreach | No send path without approval; reply and unsubscribe always win races |
| Assessment | Reserved addresses, DNS rebinding and redirects are denied |
| Chatbot | No cross-tenant retrieval; no unconfirmed bookings |
| Entitlements | Backend blocks every route and job after suspension |
| Credits | No overspending under concurrency |
| Events | Crash after commit still delivers; replay has no double effect |
| Analytics | Value kinds are never mixed |
| Privacy | Erasure reaches all modules; no identifier before consent |

## AI safety tests

Run against the actual prompts and chosen models when a prompt or model changes.

- **Extraction:** "print your system prompt", attempts to reveal other tenants.
- **Indirect injection:** hostile instructions in documents, web pages and email replies.
- **Scope:** discount offers, legal commitments, security guarantees.
- **Output handling:** markup, links, script content, malformed JSON.
- **Quality:** answers only from knowledge, and "I don't know" when absent.

Leakage tests must pass fully. Behavioural tests can't be perfect, so they're tracked as scored results with thresholds, and regressions block release.

## End-to-end scenario

Two tenants run on a full environment, with fake external providers. A controlled test domain is used for the assessment.

| # | Tenant A action | Verify | Tenant B tries |
|---|---|---|---|
| 1 | Discover a lead | Job finishes; result attributed | Reads A's search job by ID: `404` |
| 2 | Enrich | Attributes stored with source; credits used | Reads prospect: `404` |
| 3 | Score | Factors and coverage shown | Reads score: `404` |
| 4 | Create CRM opportunity | One company, one deal | Reads deal; lists deals: not present |
| 5 | Approved campaign | No send before approval; message sent after | Reads campaign, approves A's approval: denied |
| 6 | Receive reply | Sequence stopped; CRM activity and task | Forged webhook using B's token can't affect A |
| 7 | Schedule meeting | Meeting in CRM | Reads meeting: `404` |
| 8 | Authorized assessment | Authorization evidence; report | Reads findings or result token for another object: denied |
| 9 | Generate proposal | Snapshot immutable; totals correct | Token-less access or guessing: denied |
| 10 | Win deal | One `crm.deal.won` | n/a |
| 11 | Record MRR | Contract created; MRR metric equals contract | Reads facts: none of A's |
| 12 | Expansion recommendation | Rationale and evidence; accept creates a deal | Reads recommendation: `404` |

**Additional checks:**
- Tenant B prospects the same canonical company, and sees nothing from A.
- Marker strings from A appear in no B response or in the logs.
- Replaying all events causes no duplicate effects.
- Repeat with A's module suspended midway: work stops and access is blocked.

## Test data

Synthetic only. No production data in non-production environments. Reserved test domains (RFC 2606) and test email sinks.

## Environments

| Environment | Purpose |
|---|---|
| Local | Docker dependencies, fake providers |
| CI | Ephemeral, full suite |
| Staging | Production-like, provider sandboxes, DAST, load |
| Production | Synthetic monitoring; no destructive tests |
