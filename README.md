# MSP Growth Engine

A multi-tenant SaaS platform for Managed Service Providers. **Phase 1 of the [roadmap](docs/implementation/implementation-roadmap.md) is built:** authentication, tenancy, platform administration, module entitlements, and both application shells. The ten business modules are placeholders until their phases.

- [What exists and what is left](docs/implementation/phase-1-status.md)
- [Full planning documentation](docs/README.md)

## Requirements

Go 1.27, Node 26 (`nvm use` picks it up from `.nvmrc`), pnpm 12, and Docker with Compose.

## Run it locally

```bash
make setup                 # creates .env with fresh keys, installs dependencies
make up                    # starts PostgreSQL, Redis and Mailpit
make migrate
make admin EMAIL=you@example.test NAME="Your Name"   # creates a Super Admin; asks for a password
```

Then, in separate terminals:

```bash
make api                   # API on http://127.0.0.1:8088
make web                   # client app and admin app
```

| Open | For |
|---|---|
| http://localhost:5180 | Client app |
| http://localhost:5181 | Platform admin app |
| http://localhost:58025 | Mailpit, where all email goes. Nothing reaches real inboxes |

**Try the main flow:** register in the client app and click the link from Mailpit. Create a workspace, and see that CRM is locked. Sign in to the admin app, where you must set up two-step verification. Activate CRM for your tenant, and watch the client page update without a reload.

These ports avoid the defaults on purpose: PostgreSQL is on 55432 and Redis on 56379.

## Run everything in Docker

Only Docker is needed: no Go or Node on the machine. It builds the same images as the testing server, but with local settings: development passwords, fake providers, no malware scanning and plain HTTP. Use it only on your own machine.

```sh
make docker-up                                              # builds and starts everything; creates deploy/docker-local/.env with fresh keys
make docker-admin EMAIL=you@example.test NAME="Your Name"   # creates a Super Admin; asks for a password
```

| Open | For |
|---|---|
| http://localhost:8180 | Client app (the local test page is at `/test-site/index.html`) |
| http://localhost:8181 | Platform admin app |
| http://localhost:8025 | Mailpit |

It has its own data and ports, so it can run beside `make up`. `make docker-logs` follows the API and worker logs, `make docker-down` stops it and keeps the data, and `make docker-reset` deletes the data. After changing code, run `make docker-up` again to rebuild. Change the ports with `CLIENT_PORT`, `ADMIN_PORT` and `MAILPIT_PORT` in `deploy/docker-local/.env`.

## Common commands

| Command | Does |
|---|---|
| `make test` | All backend and frontend tests. Backend tests start their own PostgreSQL in Docker |
| `make e2e` | Browser tests against the real stack. First install the browser: `pnpm --filter @msp/e2e exec playwright install chromium` |
| `make lint` | Go lint, ESLint, module boundaries, OpenAPI lint, type checks |
| `make gen` | Regenerate code after changing SQL queries or the OpenAPI contracts |
| `make reset` | Delete local data and start again, then run `make migrate` |

## Testing server

See [deploy/testing](deploy/testing/README.md).

## Layout

```text
api/openapi/     API contracts, the source of truth for both Go and TypeScript
backend/         Go modular monolith: one binary, several process roles
apps/client/     MSP customer application, with one folder per business module
apps/admin/      Separate platform administration application
apps/e2e/        Browser tests
packages/        Shared UI, API client, types, validation, design tokens and config
deploy/local/    Local Docker environment
docs/            Architecture, module contracts, database, security and roadmap
```

## Rules worth knowing

- **Tenant ID always comes from the server-side session,** never from a request.
- **Every API operation must declare its access rule** in the OpenAPI contract. The server will not start otherwise.
- **Modules only import each other's `contract` package,** and only touch their own schema. CI fails otherwise.
- **All email in non-production goes to a test sink.** See the [rules until legal review](docs/implementation/open-decisions.md).
