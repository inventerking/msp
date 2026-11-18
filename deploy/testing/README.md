# Testing server

Runs the whole system on one host with Docker: Traefik over HTTPS, the API, a worker, both apps, PostgreSQL, Redis and Mailpit.

**Rules until legal review is complete** (see [open-decisions](../../docs/implementation/open-decisions.md)): synthetic data only, no real recipients, no assessments against domains you don't control, and no tracking of real visitors. All email stays in Mailpit, which cannot deliver to real inboxes.

This setup is for testing. It runs on one host with no failover, so it does not meet the production design. It does have encrypted backups, a timed restore drill and monitoring, described below and in the [operations docs](../../docs/operations/README.md).

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

**Upgrading an existing server** to this version: add `BACKUP_CIPHER_PASS` (from `openssl rand -base64 36`), `GRAFANA_ADMIN_PASSWORD` and `GRAFANA_PORT=53000` to `.env`, then run the `up` command and `./backup.sh`.

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

## Backups

PostgreSQL archives its change log (WAL) continuously, encrypted, into the `pgbackups` volume, at least once a minute. Take a base backup daily from cron:

```bash
15 2 * * * cd /path/to/deploy/testing && ./backup.sh >> backup.log 2>&1
```

**Keep a copy of `BACKUP_CIPHER_PASS` away from this server.** Without it the backups cannot be read.

**Off-host copy.** Backups on the same disk do not survive losing the disk. To add a second, S3-compatible repository, create `backup-offsite.env` (it is read if present):

```bash
PGBACKREST_REPO2_TYPE=s3
PGBACKREST_REPO2_PATH=/msp-testing
PGBACKREST_REPO2_S3_BUCKET=your-bucket
PGBACKREST_REPO2_S3_ENDPOINT=s3.your-provider.example
PGBACKREST_REPO2_S3_REGION=your-region
PGBACKREST_REPO2_S3_KEY=...
PGBACKREST_REPO2_S3_KEY_SECRET=...
PGBACKREST_REPO2_CIPHER_TYPE=aes-256-cbc
PGBACKREST_REPO2_CIPHER_PASS=...
PGBACKREST_REPO2_RETENTION_FULL=14
```

Then `docker compose up -d postgres` and `./backup.sh`.

**Restore drill.** Run monthly. It restores into a throwaway container and never touches the running database:

```bash
./restore-drill.sh        # report in drills/, exit status 0 if it passed
```

To restore for real, follow the [restore runbook](../../docs/operations/runbooks/restore.md).

## Monitoring

```bash
docker compose -f compose.yaml -f compose.monitoring.yaml up -d --build --wait
ssh -L 53000:127.0.0.1:53000 your-server     # Grafana at http://localhost:53000, user admin
```

Prometheus scrapes the API and worker on an internal network; nothing is published. Alerts go to Mailpit until a real receiver is set in `monitoring/alertmanager.yml`. Each alert names its runbook in [docs/operations/runbooks](../../docs/operations/runbooks/).
