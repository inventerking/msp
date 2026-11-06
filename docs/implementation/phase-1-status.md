# Phase 1 status

Status: built and verified locally. **Phase 1 gate approved by the product owner on 2026-11-07.** Phase 2 is not authorized.

## What exists

| Area | Built |
|---|---|
| Repository | pnpm and Go monorepo, Makefile, CI workflow (not yet run, as there is no remote) |
| Local environment | PostgreSQL 18, Redis and Mailpit in Docker, on non-default ports |
| Testing server | Docker images and a Compose stack with Traefik over HTTPS. See [deploy/testing](../../deploy/testing/README.md) |
| Backend | One `engine` binary: `api`, `worker` (housekeeping only), `migrate`, `healthcheck`, `create-admin`, `recover-admin` |
| Database | All 18 schemas created. `identity`, `platform` and `audit` tables built, with forced RLS and least-privilege roles. Other schemas are empty |
| Authentication | Registration, email verification, Argon2id, sessions and revocation, password reset and change, TOTP MFA with recovery codes, recent-authentication checks, login throttling |
| Tenancy | Workspaces, invitations, memberships, seven seeded roles with the permission matrix, last-owner protection, workspace switching |
| Platform admin | Separate identity domain, mandatory MFA, five roles enforced per route, password change, and recovery for a lost password or authenticator |
| Commercial | Module catalog, products and bundles with immutable versions, plans, prices, subscription records, entitlement grants, admin activate, suspend, expire and revoke |
| Audit | Same-transaction, tamper-evident audit trail with a verification function |
| Client app | Shell, authentication screens, workspace, dashboard, team, settings, subscription, account, status pages, ten module manifests with placeholder screens |
| Admin app | Sign-in with MFA enrolment, dashboard, tenants, entitlement actions, catalog, subscriptions, audit log, admins |
| Contracts | OpenAPI for tenant, admin and module routes, generating Go and TypeScript |

## How access rules are enforced

Each operation in the OpenAPI contract must declare `x-access`, and any module or permission it needs. The API reads these at startup and refuses to start if a rule is missing, or if a served route and the contract disagree. Authorization therefore cannot drift from the contract.

## Verification results

| Check | Result |
|---|---|
| Backend tests, with race detector, against real PostgreSQL | 200 pass, none skipped |
| Frontend unit and component tests | 31 pass across five packages |
| Browser end-to-end (real API, database and email sink) | 2 flows pass, locally and against the deployed testing stack through Traefik over HTTPS, with no Content-Security-Policy violations |
| Testing stack smoke test from outside | All checks pass |
| Go lint, ESLint, dependency boundaries, OpenAPI lint | Clean |
| pnpm audit (production dependencies) | No known vulnerabilities |
| govulncheck | Nothing reachable from our code. GO-2026-5932 exists in `golang.org/x/crypto` 0.58.0, in code we do not call. Only a pre-release fix exists, so update when the next release ships |
| Clean rebuild from an empty database using the Makefile | Works |

**Mutation checks.** To confirm that tests detect real faults, these were broken one at a time, and each was caught: entitlement check, permission check, CSRF check, tenant suspension check, admin role check, invitation email check, role escalation check, session revocation on password reset, entitlement end date, forced RLS, and the tenant-status column grant.

The tests also include planted faults for the schema check and the module boundary rules.

The security review was done by the same author as the code. It does not replace the independent penetration test planned before launch.

## Defects found by testing and fixed

| Defect | How it was found |
|---|---|
| A user who enrolled in MFA could not sign in again during the same 30-second window | Browser test |
| When rate limited, the verify-email page wrongly said the link was invalid | Browser test |
| A tenant's database role could change its own status, including lifting a suspension | Security review |
| The audit chain could fork under concurrent writes, its hash depended on the session time zone, and nothing could verify it | Security review |
| Housekeeping functions were executable by every database role | Security review |
| Go standard library vulnerability GO-2026-6809 | govulncheck; fixed by moving to Go 1.27.3 |
| The API required the database owner password, though only migrations use it | Testing server work |
| Traefik's HTTP redirect pointed at its internal port, not the public one | Testing server smoke test |
| Re-entering a password for a sensitive action had no guessing limit | Password change work |

## Differences from the plan

| Plan | Built | Reason |
|---|---|---|
| TypeScript 7 | 6.0.3 | typescript-eslint requires a version below 6.1, and openapi-typescript failed on 7. Already flagged as the fallback |
| OpenAPI 3.1 | 3.0.3 | oapi-codegen does not fully support 3.1 |
| `db/migrations` at the repository root | `backend/db/migrations` | Go can embed files only from inside its module |
| Separate packages for catalog, subscription, entitlement and tenancy | `platform/commercial` and `platform/identity` | One owner per schema, which is the boundary that matters. Can be split later |
| `depguard` for import rules | A Go architecture test | It can also detect module cycles and wrong-schema SQL, and can prove itself with planted faults |
| shadcn/ui via its CLI | Components written by hand in shadcn's style, in `packages/ui` | shadcn copies source into a project anyway. This gave only what Phase 1 uses |
| `/admin/tenants/{id}/entitlements/...` for entitlement changes | The same, with `activate`, `suspend`, `expire` or `revoke` as a path value | Matches the plan's meaning |
| Data dictionary | Added `last_auth_at`, admin `idle_expires_at`, admin factor `last_used_at`, `audit.events.chain_seq` | Needed for recent authentication, idle timeouts, replay protection and a correct audit chain |

## Known gaps

Not yet built, and to be scheduled:

**Security and accounts**
- WebAuthn. TOTP is the only MFA factor so far, although the tables support both.
- Workspace policy requiring MFA for all members.
- Breached-password checking. There is only a small bundled list of common passwords.
- Field encryption reads one key from the environment. A managed secret store with envelope encryption is due before production.

**Roles**
- Platform Support sees the whole audit log, while the plan says "limited". Billing and Integration Admins have no audit access yet, while the plan gives them scoped access.
- Support access grants: the table exists, but no workflow uses it.
- Admin role assignment is set at creation only.

**Screens and APIs**
- Global search, notifications and onboarding screens. Search and notifications depend on later phases.
- Tenant purchase requests. Admins record subscriptions directly until Phase 7, as the roadmap intends.
- Tenant ICP editing. The table exists, and Lead Generation will need it in Phase 3.

**Operations**
- The testing stack has no backups or failover, and traffic between containers on its private network is not encrypted. Production needs both, and D-23 still decides how.
- The Mailpit image uses the `latest` tag. Pin it before relying on the stack.
- Metrics and tracing are not wired in. Structured logs and request IDs are.
- Accessibility has only been checked through label and role use in tests. There is no automated audit yet, and no manual review.
- CI is written but has not run, because the repository has no remote.
- The Mermaid diagrams in the docs remain unrendered.

## Before anyone uses the testing server

Keep the rules in [open-decisions](open-decisions.md): synthetic data only, and email going to a test sink. `deploy/testing/init-env.sh` generates fresh secrets, and the backend refuses the local development passwords.
