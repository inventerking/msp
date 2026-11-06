# Testing server

Runs the whole system on one host with Docker: Traefik over HTTPS, the API, a worker, both apps, PostgreSQL, Redis and Mailpit.

**Rules until legal review is complete** (see [open-decisions](../../docs/implementation/open-decisions.md)): synthetic data only, no real recipients, no assessments against domains you don't control, and no tracking of real visitors. All email stays in Mailpit, which cannot deliver to real inboxes.

This setup is for testing. It runs on one host with no backups or failover, so it does not meet the production design.

## Set up

On the server, with Docker and Compose installed:

```bash
cd deploy/testing
./init-env.sh app.example.test admin.example.test   # generates .env with fresh secrets
docker compose up -d --build --wait
./smoke.sh                                          # checks the running stack from outside
```

Point both hostnames at the server, and open ports 80 and 443. To try it on your own machine, run `./init-env.sh` with no arguments, which uses `app.localhost` and `admin.localhost`.

**Certificates.** By default Traefik uses a self-signed certificate, so browsers show a warning. For public hostnames, set `ACME_EMAIL` in `.env` and add the second file:

```bash
docker compose -f compose.yaml -f compose.acme.yaml up -d --build --wait
```

**If ports 80 or 443 are taken,** set `HTTP_PORT` and `HTTPS_PORT` in `.env`, and set `PUBLIC_PORT_SUFFIX` to match, such as `:58443`. Set `MAILPIT_PORT` too if 58025 is taken.

## Create the first administrator

Reading the password this way keeps it out of your shell history:

```bash
read -rs ADMIN_PASSWORD; export ADMIN_PASSWORD
docker compose exec -e ADMIN_PASSWORD api /engine create-admin --email you@example.test --name "Your Name"
unset ADMIN_PASSWORD
```

## Read email

Mailpit's page is only published on the server's loopback address, so use an SSH tunnel, then open http://localhost:58025:

```bash
ssh -L 58025:127.0.0.1:58025 your-server
```

## Recover an administrator

Normally another Super Admin does this from the Platform admins page. If nobody can sign in, use the break-glass command, which needs server access and is recorded in the audit log:

```bash
read -rs ADMIN_PASSWORD; export ADMIN_PASSWORD
docker compose exec -e ADMIN_PASSWORD api /engine recover-admin --email you@example.test --reason "Lost authenticator"
unset ADMIN_PASSWORD
```

## Test a running server

From the repository root:

```bash
DEPLOYED_COMPOSE_DIR=$PWD/deploy/testing CLIENT_URL=https://app.localhost ADMIN_URL=https://admin.localhost MAILPIT_URL=http://127.0.0.1:58025 pnpm e2e
```

This creates test accounts, so run it only on a testing server.

## How it is locked down

| Control | Detail |
|---|---|
| Separate hosts | The customer host never serves admin routes, and the admin host serves only those |
| Network | The database, Redis and backend sit on a network with no internet route. Only Traefik publishes public ports |
| Containers | Read-only filesystems, no capabilities, no privilege escalation. Backend images have no shell |
| Credentials | Each process gets only what it uses, so the API never holds the migration password, and the worker holds no encryption keys |
| Startup checks | The backend refuses local `dev_` passwords on this server |
| Traefik | Uses a config file, so it needs no access to the Docker socket. Access logs omit paths, IP addresses and headers, because URLs can contain tokens |

The running backend also needs outbound internet once real providers arrive in later phases. That will need its own network rule.
