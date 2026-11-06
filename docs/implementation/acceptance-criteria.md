# Acceptance criteria

Status: planning, pending approval. Module-specific criteria are section 22 of each [module document](../modules/module-overview.md). This file holds platform-wide and release-level criteria.

## Platform

### Commercial and access
- A tenant can request a module, a bundle or the complete suite.
- No entitlement activates without a platform admin, and all steps are audited.
- A bundle grants exactly the modules in its purchased version, and a later bundle change doesn't alter existing subscribers.
- Admins can activate, suspend, expire and revoke, and the backend blocks all module routes and jobs accordingly.
- Bundles and products can be changed without code changes.
- Access applies to all authorized workspace members, with no per-employee purchase.

### Identity and security
- Passwords use Argon2id. Email verification, reset, session revocation and optional MFA work.
- Platform admin and tenant sessions are not interchangeable, and platform admin MFA is mandatory.
- The permission matrix is enforced server-side, with generated tests.
- Tenant ID from a client is never used as authority.

### Tenant isolation
- All tenant tables have forced RLS and composite FK protection, verified by a catalog test.
- The two-tenant end-to-end scenario passes, including the canary scan.
- `shared` tables contain no tenant-origin data.

### Architecture
- CI fails on a cross-module import, a module cycle, wrong-schema SQL or a prohibited FK.
- Each module runs its tests with fakes for its cross-module contracts.
- Each module works when bought alone, per the [standalone table](../modules/module-overview.md).

### Reliability
- Events are delivered after a crash, and replay has no double effect.
- Jobs recover from temporary provider failure, and dead letters are visible and actionable.
- Provider outage in one capability doesn't take down unrelated features.

### AI
- All AI goes through the gateway, and usage is tracked per tenant.
- Outward-facing AI content can't be sent without required approval.
- AI has no direct database access.

### Frontend
- Client and admin are separate apps with shared packages.
- Each listed screen exists with loading, empty, error and permission states.
- Critical flows meet WCAG 2.2 AA in automated and manual checks.

## Release readiness (Phase 7)

- [ ] All module acceptance criteria met
- [ ] End-to-end two-tenant scenario passes
- [ ] No open critical or high security findings
- [ ] Independent penetration test complete, with findings resolved or formally accepted
- [ ] Legal review complete for terms, privacy, DPA, outreach rules, tracking, and assessment authorization
- [ ] Load tests meet targets, or targets are formally revised
- [ ] CRM p95 unaffected, within target, at peak visitor load
- [ ] Backup restore drill passed and timed
- [ ] Monitoring, alerts and on-call in place
- [ ] Incident and data-breach runbooks written
- [ ] Open decisions needed by launch closed
- [ ] Abuse process in place: reporting, review, and suspension
- [ ] Sub-processor list and provider data terms confirmed

## Definition of done per feature

Code reviewed; unit and integration tests; tenant and permission tests; OpenAPI and event schema updated; audit added; error and empty states; logs and metrics; no secrets or PII in logs; threat review if there's a new boundary; documentation updated.
