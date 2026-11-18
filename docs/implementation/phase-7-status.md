# Phase 7 status

Status: built for the testing server and verified locally. **Waiting for the Phase 7 gate.** The roadmap's gate is launch approval with legal sign-off, which happens after handover (D-19, D-23). What this gate can decide now is in [For the gate](#for-the-gate).

Choices made when the phase started (2026-11-18):

| Area | Choice |
|---|---|
| Payments | Provider-neutral: a payment interface, a test provider, and bank transfers confirmed by an admin. A real provider waits for D-04 |
| Charging (D-01) | At submission, with an automatic refund if a purchase is rejected |
| Infrastructure (D-23) | Ready for the testing server: encrypted backups, a timed restore drill, monitoring and alerts, runbooks. No production deployment |
| Legal | A review pack of facts and questions for counsel, and in-app display and acceptance of documents counsel supplies. No legal text written by the team |

The deployment plan's rules still apply: synthetic data only, no email to real recipients, no real visitors tracked, and every provider a fake.

## What exists

### Buying

| Area | Built | Key properties |
|---|---|---|
| Plans on sale | Published products with a current price, per currency | Plans carry seats per unit and included credits per period (AI and lead credits) |
| Purchase | Choose plan, quantity, payment method and optional promotion; idempotent per workspace and key | A quote is computed on the server; tax is shown as not calculated. Free and trial purchases go straight to review |
| Card payment | An invoice and a pending payment, then a checkout on the provider's page | Card details never reach us. Each payment moves out of pending once only, however many webhooks or returns arrive |
| Bank transfer | An invoice with the operator's bank instructions and a due date | An admin marks it paid, with the bank's reference, only when the money has arrived |
| Review | The admin queue shows waiting time against the target (24 hours by default) | Approving creates the subscription, activates modules and grants credits, and notifies the customer. Rejecting needs a reason, refunds a card at once, and lists a bank transfer as a manual refund. Both are audited and idempotent |
| Cancelling | An unpaid purchase can be cancelled; its invoice is voided and its checkout closed | Found during the phase: a cancelled purchase's checkout could still be paid |
| Invoices | Numbered in one sequence; the customer can see and pay open ones | One checkout per invoice per day |
| Seats | Plan seats × quantity limit members; invitations count | Enforced when inviting and when accepting |

### Renewals and payment problems (D-18)

| Area | Built | Key properties |
|---|---|---|
| Renewal | Every 15 minutes, due subscriptions get a new invoice and period | A saved card is charged after the transaction commits; a bank transfer gets a due date |
| Dunning policy | Admin-set: card retry days, suspend after, end after, bank transfer due days, review target | Validated (for example, ending cannot come before suspending). Audited |
| Past due | Card retries on schedule; suspension; then the subscription ends | The customer is notified at each step. Paying the open invoice recovers the subscription and its modules |
| Stop renewing | The customer can stop renewal; the subscription ends at the period's end | |
| Reconciliation | Daily comparison of our payments with the provider's | Differences are listed once each, with five kinds, and resolved with a note |

### Trust

| Area | Built | Key properties |
|---|---|---|
| Legal documents | Admin drafts, edits and publishes versions of five document kinds | Publishing retires the previous version. Whether counsel reviewed the text is recorded as the admin states it. Public at `/legal` |
| Acceptance | A dialog asks for acceptance of required versions before continuing | Recorded per user and version; a DPA is accepted by owners only, per workspace |
| Abuse reports | A public form (5 per hour per address), an admin queue, decisions recorded | Actioned or dismissed needs a decision. Suspension stays a separate, audited tenant action |

### Operations (testing server)

| Area | Built | Notes |
|---|---|---|
| Metrics | Prometheus endpoint on a separate, unpublished listener, for the API and the worker | HTTP by route template; pools; jobs, outbox and dead letters; WAL archiving; billing queues; abuse reports. Labels never carry tenant or user identifiers, which a test checks |
| Monitoring | `compose.monitoring.yaml`: Prometheus, Alertmanager, Grafana, node-exporter | 18 alert rules, each naming a runbook; one Grafana dashboard. Alerts go to Mailpit until a receiver is chosen |
| Backups | pgBackRest in the Postgres image: WAL archived at least every minute, daily full backups, 7 kept, AES-256 | Optional second, off-host S3 repository |
| Restore drill | `restore-drill.sh`: restores into a container with no network, checks, times and reports | Checks row counts, migration version, forced RLS, and that no tenant's rows show without a tenant set |
| Logs | Rotated by size for every container | Previously unbounded |
| Ingest process | `engine ingest` serves only the visitor tracker, with its own pool; Traefik routes the tracker path to it | Added after the first load test. Hits also got cheaper: module status and goals cached for 30 seconds, one transaction per hit in the usual case |
| Key rotation | Encrypted values name their key; previous keys stay readable; `engine rotate-keys` re-encrypts every stored secret | No downtime. The HMAC key is deliberately long-lived: its hashes cannot be recomputed without values the system no longer keeps |
| Erasure log | Each erasure is logged in object storage, by identifiers only, before it runs; `engine replay-erasures --since DAY` repeats them after a restore | |
| Runbooks | Incident, data breach, restore, deploy, payments, abuse, key rotation | [operations](../operations/README.md) |

### Documents for others

- [Legal review pack](../legal/review-pack.md): data map, sub-processors, retention (enforced and proposed), rights requests, security measures, and 29 open questions from every phase.
- [Penetration test scope](../security/penetration-test-scope.md).
- [Load testing](../operations/load-testing.md).

## Verification results

| Check | Result |
|---|---|
| Backend tests, with the race detector | All 27 packages pass. 344 test functions, up from 324 at Phase 6, including key rotation, the erasure log and the ingest process |
| New backend tests | 12 billing and trust API tests (card purchase to activation, rejection refund, bank transfer, cancel and void, renewal with dunning, suspension and recovery, expiry, seat limit, signed webhook and reconciliation, isolation between workspaces, policy validation, legal acceptance, abuse reports); metrics tests for label safety and failing sources; a worker metrics test against the real schema |
| Frontend tests | 53 pass, up from 49: the legal gate, and the admin review queue's rules |
| Browser tests | All 7 flows pass against the local stack, run outside office hours. The new flow puts a plan on sale in the admin app, buys it by card on the test checkout, approves it from the review queue, and sees the module turn on |
| Lint and types | Go lint, ESLint, module boundaries, OpenAPI lint and type checks are clean |
| Restore drill | Passed in 19 seconds on the testing setup run locally (46 MB database); rows written after the base backup were recovered from WAL |
| Monitoring | All targets up, 18 rules loaded, every metrics source readable, the dashboard provisioned, and a test alert delivered to Mailpit |
| Load test | CRM met its target; visitor ingest did not. See below |
| Generated code | `make gen` reproduces all generated files exactly |

## Load test

Full results in [load-testing](../operations/load-testing.md). With 500 tenants and the 100,000-company tenant, CRM alone met its target (p95 238 ms, no errors). In the first run, visitor tracking shared the API's connection pool, reached about 500 events per second, and pushed CRM p95 to about 11 seconds.

After the ingest process was built, CRM was unaffected by tracking on a smaller dataset (p95 20 ms with ingest running), and one ingest process sustained about 1,050 events per second against the 2,000 target. Its limit is one commit per hit. The full-profile run on this developer machine slowed everything, with CPU to spare, which points to the one shared disk; it should be repeated on the testing server.

## Defects found and fixed during the phase

| Defect | Found by |
|---|---|
| A cancelled or rejected purchase's checkout could still be paid. Checkouts are now closed after the change commits | API test |
| Credits on recovery were granted from inside the transaction, so a rollback could still grant them. They are now granted after commit | Code review |
| Billing queries joined identity tables, crossing the schema rule. Names now come through the identity service | Architecture test |
| The webhook test depended on how much work earlier tests left queued | Full test run |
| Container logs on the testing server had no size limit | Operations review |
| Since Phase 2, low-credit notifications were never delivered: the consumer panicked on `platform.credits.low`, and each event went to dead letters | Dead letters seen during the browser run |
| The outreach browser test passed only during office hours, because the campaign's default sending window held the email. The product was right; the test now sets an always-open window | Browser run at night |

## Decisions made during the phase

| Decision | Choice | Reason |
|---|---|---|
| Who pays for review | Card is charged at submission (D-01); the review target is a setting, 24 hours | The recommendation; the target is shown to the operator and alerted on |
| Bank transfer refunds | Recorded by hand, with a reference | No provider can refund them |
| Seats | Plan's seats × quantity; no seat limit when a plan does not set one | Plans without seats keep working |
| Metrics listener | Separate port, monitoring network only | Metrics must not be public, and need no authentication there |
| Health gauges | Read in the worker, not the API | So more API replicas do not multiply them |
| Backups | pgBackRest in the Postgres image, archive every 60 seconds | Meets the 15-minute recovery point with margin, encrypts, and supports an off-host copy |
| Grafana and Mailpit | Loopback only, reached by SSH tunnel | Same rule as before for admin tools |

## Known gaps

**Before the gate can be passed in full**
- **Ingest throughput.** About 1,050 events per second per ingest process against 2,000. See [For the gate](#for-the-gate).
- **Retention jobs** for the proposed periods that are not enforced yet, once counsel confirms them (D-14).
- **Tenant offboarding purge** (D-14).
- **Load test on the testing server**, with the load generator on a separate machine.

**Waiting on decisions or handover**
- A real payment provider (D-04), tax (L-24) and invoice content (L-25).
- Legal texts from counsel, and answers to the review pack.
- The independent penetration test.
- Production hosting (D-23), an on-call rota and a real alert receiver.
- Uploaded files are not backed up on the testing server.

**Smaller gaps**
- Promotions can be redeemed but not created in the admin app; they are created by data for now.
- Tenant users cannot close their own account; support does it.

## For the gate

1. **Ingest throughput.** Approve batched ingest writes (a hit is acknowledged before it is stored, so a crash loses at most the last fraction of a second of page views; consent records stay written one by one), or formally revise the target to about 1,000 events per second per ingest process and add processes as traffic grows.
2. **HMAC key.** Accept it as a long-lived key, protected rather than rotated, for the reason in [key-rotation](../operations/runbooks/key-rotation.md#hmac-key-hmac_key).
3. **Legal review pack.** Ready to hand to counsel through the product owner's boss.
