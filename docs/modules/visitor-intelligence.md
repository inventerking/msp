# Module 7: Visitor Intelligence

Status: planning, pending approval. Common rules are in [module-overview](module-overview.md). Tracking and consent behaviour needs legal review.

## 1. Purpose
Help MSPs understand website traffic and conversions.

## 2. Business responsibilities
Tracking configuration, consent-aware collection, sessions and page views, attribution, conversions, lawful business-level identification, dashboards, retention.

## 3. Features
Tracking script; site configuration; anonymous IDs; sessions; page views; referrals; landing-page attribution; conversions; form conversions; business-level identification where lawful and technically supported; CRM integration; dashboard; retention controls.

**Promise limit.** The product says "business-level match, with confidence", and only when available. It doesn't claim to identify individuals. A visitor becomes a known contact only if they submit their details, such as in a form or chat.

## 4. Data ownership
Sites, visitors, sessions, events, consent records, conversions, business identifications, rollups.

## 5. Database tables
`sites`, `visitors`, `sessions`, `events`, `consent_records`, `conversion_goals`, `conversions`, `business_identifications`, `daily_rollups`.

## 6. REST API

**Authenticated: `/api/v1/visitor-intelligence`**

| Method and path | Purpose | Permission |
|---|---|---|
| `GET, POST /sites`, `GET, PATCH, DELETE /sites/{id}` | Sites | `visitor.read`, `visitor.configure` |
| `GET /sites/{id}/snippet`, `GET /sites/{id}/status` | Install | `visitor.read` |
| `GET, POST /sites/{id}/goals`, `PATCH /goals/{id}` | Goals | `visitor.configure` |
| `GET /sites/{id}/sessions`, `GET /sessions/{id}` | Sessions | `visitor.read` |
| `GET /sites/{id}/analytics/traffic`, `/conversions`, `/attribution` | Analytics | `visitor.read` |
| `GET /sites/{id}/businesses` | Business matches | `visitor.read` |
| `POST /businesses/{id}/create-crm-company` | Send to CRM | `visitor.write` |

**Public ingest: `ingest` hostname**

| Method and path | Purpose | Authentication |
|---|---|---|
| `POST /v1/collect` | Batched events | Site key and origin check |
| `POST /v1/consent` | Consent state | Site key and origin check |
| `GET /embed/tracker.js` | Script | None |

## 7. Request structures
**Create site:** `{ domain, allowed_origins[], consent_mode, retention_days, ip_handling }`.
**Collect:** `{ site_key, anon_id?, session_id?, consent_state, events: [{ type, url, referrer?, utm?, ts, name?, props? }] }`, up to a limited batch size.

## 8. Response structures
**Collect:** `202` with an empty body. Never returns data, including whether a site key was valid beyond a generic rejection.
**Traffic:** `{ range, totals, series[], top_pages[], top_referrers[], consent_rate, value_kind: "actual" }`.
**Business:** `{ company: { name, domain }, confidence, provider_key, sessions, last_seen }`.

## 9. Internal interfaces
`GetVisitorContext(visitor_ref)` for Chatbot, returning only pages and landing source of a consenting session.

## 10. Required dependencies
Company Data Provider (business identification, optional), Company Identity, Contact Identity, Redis Streams, Jobs. CRM (optional through events and registry).

## 11. Published events
`visitor_intelligence.conversion.recorded`, `.business.identified` (coarse, per day), `.consent.withdrawn`, `.site.first_event`.

## 12. Consumed events
`chatbot.contact.captured` (links visitor to contact when the person gave details in that session), `shared.contact.erased`, entitlement revocation (ingest returns inactive and cached state is cleared).

## 13. Background jobs
`visitor.flush`, `visitor.rollup`, `visitor.identify_business`, `visitor.partition_retention`, per-site retention sweep.

## 14. Permissions
`visitor.read`, `visitor.write`, `visitor.configure`.

## 15. Validation rules

### Consent modes

| Mode | Before consent | After consent |
|---|---|---|
| `opt_in` (recommended default where consent law may apply) | No cookie or stored ID; only aggregate, cookieless count, if counsel accepts | Anonymous ID, sessions, events |
| `opt_out` | Tracking on, with an opt-out | Withdrawn on opt-out |
| `cookieless_only` | Never stores an ID. Sessions use a daily-rotating salted hash, not persistent across days | Same |

The site's consent platform passes state using a documented API, or the MSP chooses. The tracker honours Global Privacy Control. The tenant configures this, and the tenant's own privacy policy must describe it.

### IP addresses
IPs can be personal data. The ingest role uses the IP for abuse limits, and, only if allowed by the consent state and configured, for a business lookup. It then discards the IP. Raw IPs are never written to the database or logs. Daily-salted hashes support abuse limits only.

### Other rules
Origin must match `allowed_origins`; URLs are stripped of query strings by default, apart from `utm_*`, because query strings can hold personal data. Batch and rate limits per site. Bot traffic is filtered. Sensitive form fields are never captured: the tracker only records that a form submit happened.

## 16. Failure handling
Redis down: the ingest role drops events and increments a counter, returning `202`, so the customer's website is never affected. Tracker failures are silent to the page. Buffer overflow is capped. Form conversions go through `POST /api/v1/public/...` synchronously, and business lookup failure yields no match.

## 17. Idempotency
Client event IDs dedupe within a batch window. Conversions use `dedupe_key`. Rollups are recomputed idempotently by site and day.

## 18. Audit
Site configuration, consent mode and retention changes, snippet regeneration, sends to CRM. Individual events aren't audited.

## 19. Security considerations
- Tracker script has a size budget and no dependencies, sets only first-party storage, and can be used with Subresource Integrity.
- Site keys are public by nature, so they only permit writing to that site. Rate limits and origin checks reduce pollution, and stored values are treated as untrusted when displayed.
- Stored URLs, referrers and custom properties are escaped. Dashboards don't render them as HTML.
- Separate pool and Redis path protect CRM.

## 20. Unit tests
Consent state machine; URL stripping; attribution rules; bot filter; salt rotation; retention date math.

## 21. Integration tests
No identifier is stored before consent in `opt_in` mode; withdrawal stops collection; raw IP appears nowhere after a test run (scan database and logs); wrong origin rejected; partition drop follows retention; heavy ingest test leaves CRM p95 within limit; one tenant's site key can't write to another's site.

## 22. Acceptance criteria
- A tenant can install the snippet, verify it, and see sessions, referrers, landing pages and conversions.
- Consent mode is honoured, shown by automated browser tests.
- The UI words business matches as such, with a confidence, and shows no individual identity without voluntary submission.
- Retention settings delete data on schedule.
- Peak tracking load doesn't degrade CRM beyond the target.
