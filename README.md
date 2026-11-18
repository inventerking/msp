# MSP Growth Engine

A multi-tenant SaaS platform for Managed Service Providers. **All seven phases of the [roadmap](docs/implementation/implementation-roadmap.md) are built** and verified locally, with synthetic data and fake providers only. The final gate, launch approval with legal sign-off, comes after handover to the testing server and legal review.

- [What exists and what is left](docs/implementation/phase-7-status.md) (earlier phases: [1](docs/implementation/phase-1-status.md), [2](docs/implementation/phase-2-status.md), [3](docs/implementation/phase-3-status.md), [4](docs/implementation/phase-4-status.md), [5](docs/implementation/phase-5-status.md), [6](docs/implementation/phase-6-status.md))
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

## Database

PostgreSQL 18 with the `pgvector` extension. Everything is in one database, `msp_engine`, with one schema per area (`identity`, `platform`, `crm`, `outreach` and so on).

### Why there are four passwords

The app does not use one all-powerful database user. Each part connects as a user that can do only its own job, so a bug or a break-in in one part cannot do everything:

| Setting in `.env` | Database user | Used by | Can do |
|---|---|---|---|
| `DB_OWNER_PASSWORD` | `msp_owner` | `make migrate` only | Create and change tables. Never used while the app is running |
| `DB_APP_PASSWORD` | `msp_app` | The API, for customer requests | Read and write only the signed-in workspace's rows. The database enforces this with row-level security |
| `DB_PLATFORM_PASSWORD` | `msp_platform` | Admin and billing actions | Platform tables: catalog, billing, settings |
| `DB_SYSTEM_PASSWORD` | `msp_system` | The worker | Background jobs and housekeeping across workspaces |

The user names are fixed, so only the passwords go in `.env`, with `DB_HOST`, `DB_PORT` and `DB_NAME`. Two more users exist without their own setting: `msp_ingest`, which the app switches to for visitor tracking, and `msp_readonly`, kept for read-only reporting.

Locally, all four passwords may be the same. On a server they must differ: the testing server's `init-env.sh` generates a different one for each.

### Option 1: the local Docker database (default)

`make up` starts PostgreSQL on port 55432 and creates the users with the passwords already in `.env.example` (`dev_owner`, `dev_app`, `dev_platform`, `dev_system`). Nothing else to do:

```bash
make up
make migrate
```

### Option 2: your own PostgreSQL server

The server needs PostgreSQL 18 with `pgvector` installed (the `pgvector/pgvector:pg18` image, or your package manager's `postgresql-18-pgvector`).

1. As a superuser (for example `psql -U postgres`), create the users and the database. Use your own passwords:

   ```sql
   CREATE ROLE msp_owner    LOGIN PASSWORD 'owner-password';
   CREATE ROLE msp_app      LOGIN PASSWORD 'app-password'      NOBYPASSRLS;
   CREATE ROLE msp_platform LOGIN PASSWORD 'platform-password' NOBYPASSRLS;
   CREATE ROLE msp_system   LOGIN PASSWORD 'system-password'   NOBYPASSRLS;
   CREATE ROLE msp_ingest   NOLOGIN NOBYPASSRLS;
   CREATE ROLE msp_readonly NOLOGIN NOBYPASSRLS;
   CREATE DATABASE msp_engine OWNER msp_owner;
   -- The API switches to msp_ingest for visitor tracking.
   GRANT msp_ingest TO msp_app WITH INHERIT FALSE, SET TRUE;
   \connect msp_engine
   CREATE EXTENSION IF NOT EXISTS vector;   -- needs a superuser, so it is done here
   ```

2. Put the same values in `.env`:

   ```bash
   DB_HOST=127.0.0.1
   DB_PORT=5432
   DB_NAME=msp_engine
   DB_OWNER_PASSWORD=owner-password
   DB_APP_PASSWORD=app-password
   DB_PLATFORM_PASSWORD=platform-password
   DB_SYSTEM_PASSWORD=system-password
   ```

3. Create the tables with `make migrate`.

Do not give the app users `SUPERUSER` or `BYPASSRLS`: that would switch off the separation between workspaces.

### Migrations

| Task | How |
|---|---|
| Bring the database up to date | `make migrate`. Safe to run any time; it applies only what is missing |
| See the current version | `SELECT max(version_id) FROM goose_db_version;` |
| Change the schema | Add a new numbered file in `backend/db/migrations/`, such as `00018_something.sql`, with `-- +goose Up` and `-- +goose Down` sections. Never edit a migration that has already been applied |
| After changing SQL queries | `make gen` regenerates the Go code |

The API and worker refuse to start if the database is behind the code, with the message `run engine migrate first`.

### Looking at the data

```bash
docker compose -f deploy/local/compose.yaml exec postgres psql -U postgres -d msp_engine   # local Docker database
```

Connect as `postgres` (or `msp_owner`) to see everything. Connected as `msp_app`, tables look empty unless a workspace is set, which is row-level security working:

```sql
SET ROLE msp_app;
SELECT set_config('app.tenant_id', '<workspace id>', false);
SELECT name FROM crm.companies;   -- now shows that workspace's companies only
```

### Common tasks

| Task | How |
|---|---|
| Delete all local data and start again | `make reset`, then `make migrate` |
| Change a password | `ALTER ROLE msp_app PASSWORD 'new-password';` as a superuser, then update `.env` and restart the API and worker |
| Back up | Not set up for local development. The testing server has encrypted backups and a restore drill: see [deploy/testing](deploy/testing/README.md#backups) |

### Troubleshooting

| Message | Cause and fix |
|---|---|
| `password authentication failed for user "msp_app"` | The password in `.env` does not match the database. Fix `.env`, or change it with `ALTER ROLE` |
| `the database schema is at version N but this build needs M` | Run `make migrate` |
| `extension "vector" is not available` | `pgvector` is not installed on the server. Install it, then create the extension as a superuser, as in option 2 |
| `role "msp_owner" does not exist` | The users were not created. Run the SQL in option 2, or for the local Docker database, `make reset` |
| `connection refused` | The database is not running, or `DB_PORT` is wrong: 55432 for `make up`, usually 5432 for your own server |

## Deploy on a server (VPS)

The same Docker setup, on a server with real hostnames, with encrypted backups and monitoring. It joins the Traefik already running on the server, here on the `traefik_network` network. On a server without Traefik, use `ACME_EMAIL=you@example.com` instead of `TRAEFIK_NETWORK`, and the stack starts its own Traefik. On the server:

```bash
make server-init APP_HOST=app.example.com ADMIN_HOST=admin.example.com TRAEFIK_NETWORK=traefik_network
make server-up                                               # builds, migrates, starts, then checks itself
make server-admin EMAIL=you@example.com NAME="Your Name"
```

Updates are `make server-update`. Server size, DNS, firewall, backups and the other commands are in the [VPS guide](deploy/testing/README.md#deploy-on-a-vps).

This runs in testing mode until the legal review is done: email goes to Mailpit, not real inboxes, and all providers are fakes. Use synthetic data only.

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
