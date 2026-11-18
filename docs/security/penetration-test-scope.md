# Penetration test scope

**For:** the independent testers the roadmap requires before launch (Phase 7). Prepared 2026-11-18. Nobody has been engaged yet; choosing a tester and agreeing dates is the product owner's decision.

## Goal

Find ways that one tenant could read or change another tenant's data, that an outsider could gain access, or that the platform could be used to attack others. Report findings by severity. Before launch, no critical or high findings may remain open (roadmap acceptance).

## Target

| Item | Detail |
|---|---|
| Environment | The testing server ([deploy/testing](../../deploy/testing/README.md)), on a build tagged for the test. Never production |
| Data | Synthetic only. Testers get their own workspaces and may create more |
| Hosts | The customer app host and the admin host, both over HTTPS. Exact names are given at engagement |
| Accounts given | Two workspaces, each with an Owner, an MSP Admin, a Sales Representative and a Viewer; a workspace with every module active; one platform admin per role (Super Admin, Platform Support, Billing Admin, Integration Admin, Platform Analyst), with two-step verification |
| Source code | Available on request (grey-box testing preferred) |
| Window | To agree. Monitoring alerts go to the team during it, and the team may contact testers if something breaks |

## In scope

**Tenant isolation, the highest priority.** Every API under `/api/v1`, especially where an ID from another workspace could be supplied: CRM records, files, proposals and share links, outreach, visitor sites, chatbots, reports and exports, invoices and purchases. The database enforces isolation with row-level security; testers should still try to defeat it through the API.

**Authentication and sessions.** Registration, email verification, sign-in, lockout, password reset, two-step verification, session expiry and revocation, CSRF protection, workspace switching, invitations and the seat limit. The admin host's separate sessions, roles and two-step verification.

**Authorization.** The tenant role matrix (Owner, MSP Admin, Sales Manager, Sales Representative, Marketing Manager, Analyst, Viewer, and their permissions) and the admin role matrix. Module entitlements: a workspace without a module must not reach its API.

**Public and embedded endpoints**, which take no session:

| Endpoint | Concern |
|---|---|
| `/public/collect` | Visitor tracking: origin checks, cross-site injection, volume |
| `/public/chat/{key}/…` | Chatbot: prompt injection, data leakage from the knowledge base, cost abuse |
| `/public/proposals/{token}`, `/public/files/{token}` | Token guessing, expiry, revocation |
| `/public/outreach/unsubscribe/{token}` | Token forgery, enumeration |
| `/public/abuse-reports` | Rate limit, injection into the admin review screen |
| `/legal/documents` | Stored content rendering |
| `/webhooks/{provider}/{token}` | Signature verification, replay |
| `/public/fake-checkout/…` | Exists only in test environments: confirm it is absent outside local, test and testing environments |

**Server-side request forgery.** Everything that fetches a URL: competitor research, website checks in assessments, webhook delivery, file imports. The Egress Guard should refuse private, loopback and metadata addresses, including through redirects and DNS rebinding.

**Billing.** Changing prices, plans or amounts in a purchase; replaying payment webhooks or checkout completion; idempotency keys; cancelling after paying; activating modules without approval; refund manipulation.

**Files.** Upload type and size checks, malware scanning, download authorization, content type handling.

**AI features.** Prompt injection through stored content (company pages, chat, notes) that reaches another user or another tenant; leaking system prompts; spending another tenant's credits.

**Frontend.** Cross-site scripting in both apps, especially where user or AI text is displayed; the content security policy; clickjacking.

## Out of scope

- Denial of service and load testing. The team runs [load tests](../operations/load-testing.md) separately.
- Social engineering, and physical access.
- The hosting provider's own infrastructure.
- Third-party providers: none are connected; all are fakes.
- Sending email to real recipients, or testing assessment checks against domains the testers do not control. These rules hold for the whole project until legal review.

## Known limitations to declare

These are known, so reporting them again adds nothing unless the tester finds a way to exploit them further:

- Rotating the application encryption and HMAC keys is not supported yet ([key-rotation](../operations/runbooks/key-rotation.md)).
- Embedded endpoints have per-address rate limits, but no per-site or per-bot limit.
- The assessment prober runs in the same worker process as other jobs; the separate network-restricted pool is not built. Live probing is off.
- Visitor ingest shares the API's database connection pool with the rest of the app ([load-testing](../operations/load-testing.md)).

## Reporting

For each finding: the steps to reproduce, the impact, a severity (CVSS or the tester's scale, stated), and the affected endpoints. Report critical findings, especially any cross-tenant access, by phone or message at once, not only in the final report. The team fixes and retests each critical and high finding before launch, and records them in the risk register.
