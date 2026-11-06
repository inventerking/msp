# Module 4: IT Assessment

Status: planning, pending approval. Common rules are in [module-overview](module-overview.md). The authorization design needs legal review (D-12).

## 1. Purpose
A lead-generation tool that produces useful, honest IT and security assessments.

## 2. Business responsibilities
Landing pages, authorization, scope control, non-invasive checks, findings, scoring, reports, lead capture and consultation booking.

## 3. Features
Branded landing pages; domain verification; authorization; scope; SSL/TLS, DNS, SPF, DKIM, DMARC checks; permitted public exposure checks; findings; risk classes; recommendations; score; PDF; lead capture; booking; history.

## 4. Data ownership
Authorizations and evidence, assessment results, reports, lead captures. A resulting CRM activity belongs to CRM.

## 5. Database tables
`templates`, `landing_pages`, `domain_verifications`, `authorizations`, `lead_captures`, `assessments`, `checks`, `findings`, `reports`, `consultation_bookings`, `egress_decisions`.

## 6. REST API (`/api/v1/it-assessments`)

| Method and path | Purpose | Permission |
|---|---|---|
| `GET, POST /templates`, `PATCH /templates/{id}` | Templates | `assessment.configure` |
| `GET, POST /landing-pages`, `PATCH /landing-pages/{id}`, `POST /landing-pages/{id}/publish` | Landing pages | `assessment.configure` |
| `POST /domain-verifications`, `POST /domain-verifications/{id}/check` | Verification | `assessment.run` |
| `POST /authorizations`, `GET /authorizations`, `POST /authorizations/{id}/revoke` | Authorization | `assessment.run` |
| `POST /assessments`, `GET /assessments`, `GET /assessments/{id}`, `POST .../cancel` | Runs | `assessment.run`, `assessment.read` |
| `GET /assessments/{id}/findings` | Findings | `assessment.read` |
| `POST /assessments/{id}/reports`, `GET /reports/{id}` | Reports | `assessment.read` |
| `GET /lead-captures` | Captured leads | `assessment.read` |
| Public: `GET /api/v1/public/assessments/{slug}` | Page data | None |
| Public: `POST /api/v1/public/assessments/{slug}/submissions` | Submit, attest | Bot and rate controls |
| Public: `GET /api/v1/public/assessments/results/{token}` | Result view | Signed token |
| Public: `POST /api/v1/public/assessments/results/{token}/booking` | Book | Signed token |

## 7. Request structures
**Public submission:** `{ domain, name, email, role, company?, attestation: { accepted: true, terms_version }, marketing_consent?, captcha_token }`.
**Domain verification:** `{ domain, method: "dns_txt" | "http_file" }`.
**Create assessment:** `{ authorization_id, template_id, crm_company_id? }`. Domain and tier come from the authorization, never the request.

## 8. Response structures
**Assessment:** `{ id, domain, status, score, risk_level, checks: { passed, failed, not_detected, inconclusive, skipped }, completed_at }`.
**Finding:** `{ id, severity, classification, title, description, recommendation, evidence }`. Evidence uses public values only.

## 9. Internal interfaces
None offered to other modules. Assessment results reach CRM through `it_assessment.assessment.completed`. Lifecycle contracts cover purge and erasure.

## 10. Required dependencies
Egress Guard, Contact Identity, Company Identity, Jobs, PDF, Files, Notification, Calendar (optional), AI Gateway (optional for wording), CRM registry and events (optional).

## 11. Published events
`it_assessment.lead.captured`, `.authorization.granted`, `.assessment.completed`, `.report.ready`, `.consultation.requested`, `.consultation.booked`.

## 12. Consumed events
`shared.contact.erased`, `platform.entitlement.revoked`, `identity.tenant.suspended` (unpublish pages, cancel runs).

## 13. Background jobs
`assessment.run` in the isolated `assessment` pool; `assessment.pdf`; domain verification rechecks; retention.

## 14. Permissions
`assessment.read`, `assessment.run`, `assessment.configure`.

## 15. Validation rules

### Authorization tiers (D-12)

| Tier | Checks | Requirement |
|---|---|---|
| **Passive** | Public DNS records, SPF, DMARC, DKIM on common selectors, and a TLS handshake to the domain's own public web and mail hosts | Attestation of authority, recorded with name, email, role, terms version, time |
| **Exposure** | Additional permitted non-invasive public checks: HTTP security headers, and a small approved list of public service presence probes | Attestation **and** verified control of the domain by DNS TXT or HTTP file |

**Why two tiers.** Passive checks read only what public infrastructure already publishes and generate almost no load on the target. Active probing against a domain, by contrast, should need proof of control. Whether attestation alone is enough for Tier 1 in all jurisdictions needs counsel's view.

### Scope rules
- Allowlist of check keys per tier, in code. Templates select from that list and cannot add to it.
- Only the authorized domain and hosts derived from its own public DNS. Third-party hosts discovered in DNS (for example, a hosted mail provider) get passive reads only.
- Prohibited, with no configuration to allow: exploitation, credential attempts, brute force, content discovery or fuzzing, denial of service, wide port scanning, authenticated scanning.
- Rate limits per domain and per tenant. An authorization expires, proposed at 30 days.
- A submission's email domain is compared with the target, and a mismatch is flagged in results.

### Network rules
Uses the Egress Guard (ADR-019), in the isolated worker pool. All allow and deny decisions are stored in `egress_decisions`. Public IP validation occurs for all resolved addresses and on each redirect.

## 16. Failure handling
Each check is independent. A DNS timeout gives `inconclusive`, not a failure. A partial assessment reports what wasn't completed. The score excludes inconclusive and skipped checks, and shows coverage, in the same spirit as lead scoring.

## 17. Idempotency
A public submission dedupes by page, domain, email and a short time window. Check uniqueness is by assessment, check and target. Booking uses an idempotency key.

## 18. Audit
Authorization (full evidence), revocation, verification, assessment start and end, report access by token, landing page publish, denials from the Egress Guard.

## 19. Security considerations
- **Main risks.** The scanner used as a proxy against internal infrastructure; the platform used to scan third parties without permission; public forms abused to generate email or scanning load.
- **Mitigations.** Egress Guard, allowlisted checks, the two tiers, CAPTCHA and rate limits by IP, tenant and target. Abuse contact details and a mechanism for a domain owner to opt out of scanning, via a platform-level blocklist.
- **Honest results.** A finding's `classification` separates `informational`, `potential_issue`, and `confirmed_misconfiguration`. This product doesn't confirm vulnerabilities from public signals. "DKIM not detected" says only that common selectors weren't found, since DKIM selectors can't be enumerated publicly. Reports state their limits.
- **Report access.** Results links use long random tokens, stored hashed, with expiry.

## 20. Unit tests
Check parsers for SPF (including lookup limits), DMARC, DKIM, TLS. Score ignores unknowns. Classification mapping. Tier gating. Domain normalization and punycode.

## 21. Integration tests
Against a controlled DNS and TLS lab: a domain resolving to `10.0.0.1`, `169.254.169.254`, `::1`, or an IPv4-mapped IPv6 address is denied. A rebinding DNS response is denied. Redirect to internal is denied. A revoked authorization stops a running assessment. Unverified domain can't run exposure tier. Network limits verified from inside the worker container.

## 22. Acceptance criteria
- An assessment can't start without a valid authorization, enforced by a foreign key and a service check.
- Exposure tier can't run without verified domain control.
- Internal or reserved targets are denied, with a recorded decision.
- Reports never call a potential issue a confirmed vulnerability, and they disclose inconclusive checks.
- A public visitor can get a branded report and book a consultation, and the tenant receives the captured lead.
