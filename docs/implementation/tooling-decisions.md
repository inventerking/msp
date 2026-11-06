# Tooling decisions (D-21)

Status: **approved by the product owner on 2026-11-06,** with Node 26 as the runtime. Items under "Decided later" are still open.

Versions and licences were looked up on 2026-11-06 from the Go module proxy, npm registry, Docker Hub and GitHub, except where marked *from memory*. Versions are pinned when each tool is added, so the exact numbers may move slightly.

## Needed in Phase 1

### Runtime and infrastructure

| Need | Choice | Version | Licence | Why |
|---|---|---|---|---|
| Go | Go toolchain | 1.27 (installed) | BSD-3-Clause (*from memory*) | Already on the machine |
| Node | Node.js LTS | 26 (active LTS), 24 installed | MIT (*from memory*) | Longest support for a new project. See question 1 |
| Database | PostgreSQL with pgvector image | `pgvector/pgvector:0.8.7-pg18-trixie` | PostgreSQL (*from memory*) | PostgreSQL 18 is the newest stable line, as 19 is still a release candidate. This image avoids changing images when Phase 5 needs `pgvector` |
| Cache and coordination | Redis | 8.8 | See flags below | Named in the brief |
| Reverse proxy | Traefik | 3.7 | MIT (*from memory*) | Named in the brief. Used on the testing server and optional locally |
| Local email | Mailpit | To be pinned | MIT (*from memory*) | Catches all email locally, which supports the no-real-recipients rule. Not checked in this review |

### Backend

| Need | Choice | Version | Licence | Why |
|---|---|---|---|---|
| HTTP router | chi | 5.5.0 | MIT | Small, works with standard `net/http`, and is supported by the OpenAPI generator |
| PostgreSQL driver | pgx | 5.12.0 | MIT | Standard choice, and handles `SET LOCAL` in explicit transactions |
| SQL access | sqlc | 1.31.1 | MIT | Hand-written SQL with generated types. It keeps RLS visible, and SQL files can be checked for wrong-schema access |
| Migrations | goose | 3.29.0 | MIT | Plain SQL files in one ordered sequence |
| API code generation | oapi-codegen | 2.8.2 | Apache-2.0 | Supports contract-first design with chi |
| Password hashing | `golang.org/x/crypto/argon2` | 0.58.0 | BSD-3-Clause (*from memory*) | Argon2id, as the brief requires |
| TOTP | pquerna/otp | 1.5.0 | Apache-2.0 (*from memory*) | Last released in 2024, but TOTP is a stable standard. Replaceable if it becomes a problem |
| WebAuthn | go-webauthn | 0.18.2 | BSD-3-Clause | Actively maintained |
| Redis client | go-redis | 9.23.0 | BSD-2-Clause (*from memory*) | Official client |
| Logging | `log/slog` | Standard library | BSD-3-Clause | Structured logging with no added dependency |
| Tracing and metrics | OpenTelemetry | 1.47.0 | Apache-2.0 (*from memory*) | Vendor-neutral. A viewer can be chosen later |
| Database tests | testcontainers-go | 0.45.0 | MIT (*from memory*) | Real PostgreSQL in tests, as the testing strategy requires |
| Lint and boundaries | golangci-lint with `depguard`, plus a small SQL schema check | 2.15.0 | GPL-3.0 (*from memory*), development only | Enforces the module import rules in CI |
| Common passwords | Bundled offline list | n/a | To be checked | Sends nothing outside the system. A breached-password API can be added later |

### Frontend

| Need | Choice | Version | Licence | Why |
|---|---|---|---|---|
| Monorepo | pnpm workspaces | 12.18.0 | MIT | Enough on its own, with no extra build orchestrator for now |
| UI framework | React, Vite, TypeScript | 19.3.1, 8.3.6, 7.0.2 | MIT, MIT, Apache-2.0 | Named in the brief |
| Styling and components | Tailwind CSS, shadcn/ui, Lucide | 4.3.5, 4.23.0, 1.70.0 | MIT, MIT, ISC | Named in the brief |
| Routing | React Router | 8.5.1 | MIT | Supports the lazy-loaded module routes |
| Server state | TanStack Query | 5.106.1 | MIT | Tenant-prefixed query keys, as the plan requires |
| Forms and validation | React Hook Form, Zod | 7.91.0, 4.6.7 | MIT | Schemas can live in `packages/validation` |
| API client | openapi-typescript, openapi-fetch | 7.13.0, 0.17.0 | MIT | Types generated from OpenAPI, with a very small runtime |
| Translation | i18next, react-i18next | 26.7.2, 17.1.13 | MIT | Text is externalized from the start |
| Sanitizing | DOMPurify | 3.4.17 | MPL-2.0 or Apache-2.0 | The plan's single sanitizer |
| Tests | Vitest, Testing Library, Playwright | 5.1.0, 16.3.3, 1.65.0 | MIT, MIT, Apache-2.0 | Component and end-to-end tests |
| Lint and boundaries | ESLint, dependency-cruiser | 10.14.0, 18.7.2 | MIT | Blocks imports between feature modules |
| OpenAPI lint | Spectral | 6.17.1 | Apache-2.0 | Enforces the API rules |

## Decided later, when first needed

| Need | Phase | Current thinking |
|---|---|---|
| Job queue | 2 | Compare River (0.51.1, MPL-2.0) with a small custom queue. River is active but not yet 1.0, and fair sharing between tenants would probably need custom work either way. Adopting it would change the `jobs.jobs` design |
| S3-compatible storage for local and testing use | 2 | SeaweedFS (Apache-2.0). **MinIO is ruled out:** its repository is archived and no longer maintained. RustFS (Apache-2.0) is the fallback |
| Malware scanner | 2 | ClamAV 1.5 |
| PDF renderer | 2 | Gotenberg, running with no network access |
| Event schema format | 2 | JSON Schema |

## Flags

1. **Redis licence.** From memory, not checked in this review: Redis 8 is offered under AGPLv3, RSALv2 or SSPLv1 rather than an older permissive licence. This matters for a commercial SaaS, so it should be on your boss's legal list. Valkey is a BSD-licensed, Redis-compatible alternative, and switching later should be low effort. The brief names Redis, so it stays unless you decide otherwise.
2. **TypeScript 7** is a major new release. Some tools may lag behind it. If one doesn't work during setup, the fallback is to pin the last 5.x or 6.x release rather than replace the tool, and I'll report it if that happens.
3. **pquerna/otp** has had no release since 2024. This is low risk, and it is noted so it isn't a surprise.
4. **golangci-lint** is GPL-licensed. It only runs during development and isn't shipped in the product.
