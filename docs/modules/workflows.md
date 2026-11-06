# End-to-end workflows

Status: planning, pending approval. All workflows carry one `correlation_id`, from start to finish. "Tx" names the module that owns a local transaction: no transaction spans modules.

## Workflow A: Prospect to customer

**Modules:** Lead Generation, CRM, Outreach, IT Assessment, Proposals, Customer Expansion, Sales Intelligence.

```mermaid
sequenceDiagram
    actor U as MSP user
    participant LG as Lead Generation
    participant CRM
    participant OR as Outreach
    participant IT as IT Assessment
    participant PR as Proposals
    participant CE as Customer Expansion
    U->>LG: POST /searches
    LG-->>LG: job: provider search, resolve companies
    U->>LG: POST /results/promote, enrich
    LG-->>LG: score, publish prospect.scored
    U->>LG: POST /prospects/{id}/convert
    LG->>CRM: Registry.EnsureCompany and Contact, Deals.CreateDeal
    CRM-->>CRM: publish deal.created
    U->>OR: approve and start campaign
    OR-->>CRM: event message.sent → activity
    OR-->>CRM: event reply.received → task, stage Contacted
    U->>CRM: record meeting
    U->>IT: authorize and run assessment
    IT-->>CRM: event assessment.completed → stage forward
    U->>PR: create, approve and issue version
    PR-->>CRM: event version.issued → stage Proposal Sent
    PR-->>CRM: event proposal.signed → close prompt
    U->>CRM: POST /deals/{id}/win
    CRM-->>CE: event deal.won → contract activated
```

| Step | Call | Event | Tx owner | Failure and retry | Idempotency | Audit |
|---|---|---|---|---|---|---|
| Discover | `POST /searches` | `search.completed` | Lead Generation | Job backoff; partial result | Idempotency key | Criteria |
| Enrich | `POST /enrich` | `prospect.enriched` | Lead Generation | Retry; `no_data` isn't a failure; credits released | Prospect, kind, day | Yes |
| Score | Job | `prospect.scored` | Lead Generation | Retry | Input hash | Model publish |
| Qualify and convert | `POST /convert`, then CRM contract | `prospect.converted`, `crm.deal.created` | CRM, then Lead Generation | CRM failure leaves the prospect unchanged, and retry is safe | Unique prospect conversion | Yes |
| Outreach | Campaign APIs | `message.sent`, etc. | Outreach | See send gates | Enrollment and step | Approvals |
| Reply | Webhook or polling | `reply.received`, `sequence.stopped` | Outreach | Stop first, then classify | Provider message ID | Yes |
| Meeting | `POST /meetings` | `meeting.scheduled` | CRM | Calendar failure shows a local meeting with a warning | Idempotency key | Yes |
| Assessment | Authorization then run | `assessment.completed` | IT Assessment | Check-level; inconclusive allowed | Assessment ID | Full evidence |
| Proposal | Approve, issue | `version.issued`, `proposal.signed` | Proposals | Freeze first; PDF and send are retried | Version; provider event | Yes |
| Win | `POST /win` | `crm.deal.won` | CRM | Conflict gives 409 | Idempotent if already won | Yes |
| Contract | Consumer | `contract.activated` | Customer Expansion | Delivery retried; dead letter alert | Unique tenant and deal | Yes |

**Consistency.** Eventual between modules. CRM's forward-only stage rule protects against out-of-order events. If a late event arrives after a win, it adds a timeline entry but doesn't change the stage.

## Workflow B: Competitor research

**Modules:** Company Identity, Competitor Intelligence.

```mermaid
sequenceDiagram
    actor U as MSP user
    participant CI as Competitor Intelligence
    participant CO as Company Identity
    participant EG as Egress Guard
    participant AI as AI Gateway
    U->>CI: POST /competitors {domain}
    CI->>CO: ResolveOrCreate(domain)
    CI-->>CI: competitor.designated
    U->>CI: POST /research-runs
    CI->>EG: fetch public sources, with robots and SSRF checks
    CI-->>CI: snapshots stored, deduped by hash
    CI->>AI: extract observations (untrusted content)
    CI-->>CI: labelled observations, compare, change events
    U->>CI: POST /opportunities or /reports
    CI->>AI: analyze tenant's observations
    CI-->>U: report.ready, labelled
```

| Concern | Design |
|---|---|
| Data ownership | Canonical company in `shared`. All else tenant-private |
| Transactions | Per step, in Competitor Intelligence |
| Failure | Disallowed source recorded and skipped; run becomes `partial`; AI failure leaves snapshots to retry |
| Retry | Fetch backoff per domain; no retry on robots disallow |
| Idempotency | Run ID, content hash, unchanged content creates no observation |
| Audit | Designation, sources, reports |

## Workflow C: Website conversion

**Modules:** Visitor Intelligence, Chatbot, Contact Identity, CRM.

```mermaid
sequenceDiagram
    actor V as Visitor
    participant T as Tracker
    participant VI as Visitor Intelligence
    participant CB as Chatbot
    participant CAL as Calendar
    participant CRM
    T->>T: read consent state
    alt consent allows tracking
      T->>VI: POST /v1/collect
    else not allowed
      T->>T: no identifier stored
    end
    VI-->>VI: batch, goal match, conversion.recorded
    V->>CB: chat
    CB->>CB: answer from tenant knowledge, qualify
    V->>CB: contact details and consent
    CB-->>VI: contact.captured
    CB->>CRM: Registry.EnsureCompany and Contact
    CB-->>CRM: prospect.qualified
    V->>CB: choose slot
    CB->>CAL: re-check availability, create event
    CB->>CRM: Meetings.RecordMeeting
    CB-->>CRM: booking.confirmed
```

| Concern | Design |
|---|---|
| Consent | Checked by the tracker and the ingest role; a chat works without tracking consent, and chat data needs its own notice |
| Linking visitor to contact | Only when the person volunteers details in that session, and tracking consent exists |
| Transactions | Local per module; booking commits after the calendar confirms |
| Failure | Ingest drops without affecting the site; AI outage leads to contact form; calendar failure means no confirmation; CRM failure leads to retried sync |
| Idempotency | Dedupe keys for conversions; booking key; registry resolve |
| Audit | Bookings and human takeover; no audit for raw events |

## Workflow D: Customer expansion

**Modules:** CRM, Customer Expansion, Proposals, Sales Intelligence.

```mermaid
sequenceDiagram
    participant CRM
    participant CE as Customer Expansion
    participant AI as AI Gateway
    actor U as MSP user
    participant PR as Proposals
    CRM-->>CE: deal.won
    CE-->>CE: customer profile, contract, coverage
    CE-->>CE: renewal_scan (daily): alerts
    U->>CE: POST /customers/{id}/analyze
    CE-->>CE: rule-based gaps, unknown stays unknown
    CE->>AI: word the recommendation, rationale required
    U->>CE: POST /recommendations/{id}/accept
    CE->>CRM: Deals.CreateDeal
    U->>PR: issue proposal for the new deal
    PR-->>CRM: proposal.signed
    U->>CRM: win
    CRM-->>CE: deal.won → contract.changed (expansion)
    CE-->>CE: opportunity.won with realised MRR
```

| Concern | Design |
|---|---|
| Data ownership | Identity and deal in CRM; coverage and contracts in Customer Expansion |
| Failure | AI down: rule-based result. CRM down on accept: retry, opportunity remains open |
| Idempotency | One opportunity per recommendation; unique tenant and deal |
| Audit | Accept, dismiss, contract MRR change with before and after |

An expansion deal must be distinguished from a new-logo deal: `crm.deals.source_module` set to `customer_expansion` marks it, and the resulting contract event classifies as `expansion`.

## Workflow E: Revenue analytics

**Modules:** All producers, Sales Intelligence.

```mermaid
flowchart LR
    LG[Lead events] --> F1[fact_lead_events]
    OR[Outreach events] --> F2[fact_outreach_events]
    CRM[Deals and meetings] --> F3[fact_deal_events]
    PR[Proposal events] --> F3
    CE[Contract events] --> F4[fact_revenue_events]
    FX[FX rates] --> F4
    F1 --> R[Rollups]
    F2 --> R
    F3 --> R
    F4 --> R
    COST[Manual costs] --> R
    R --> M[metric_snapshots: actual, estimated]
    R --> FC[Forecast runs: forecast]
    M --> D[Dashboards and reports]
    FC --> D
    M --> AI[AI commentary, citing metrics]
```

| Concern | Design |
|---|---|
| Transactions | Consumer inserts one fact per event, in its own transaction |
| Ordering | Deal stage facts carry `occurred_at`; current state calculated at rollup |
| Failure | Consumer retries; lag shows as "as of"; dead-letter alert |
| Retry | Standard backoff |
| Idempotency | Unique source event per fact table |
| Audit | Cost entries, exports |
| Late data | Rollups recompute a rolling window (proposed: 7 days); closed historic months stay correctable by a rerun that records the rerun |
