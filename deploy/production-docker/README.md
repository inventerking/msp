# Server deployment

Runs the whole app on one server behind the Traefik that is already running there, on the Docker network `traefik_network`.

**Until the legal review is done:** synthetic data only. The app runs with `APP_ENV=testing`, so every email goes to Mailpit and never reaches a real inbox, and all external providers are fakes.

## First time

Point `msp.hiksoda.com` and `mspadmin.hiksoda.com` at the server, then on the server:

```bash
git clone <your repository URL> /srv/msp
cd /srv/msp/deploy/production-docker
./init-env.sh                # fills .env with generated passwords and keys
docker compose up -d         # builds, migrates and starts everything (the first build takes a while)
```

Copy `BACKUP_CIPHER_PASS`, `ENCRYPTION_KEY` and `HMAC_KEY` from `.env` to a password manager. Without them, backups and stored secrets cannot be read.

Create the first administrator. It asks for a password:

```bash
read -rs ADMIN_PASSWORD; export ADMIN_PASSWORD
docker compose exec -e ADMIN_PASSWORD api /engine create-admin --email you@example.com --name "Your Name"
unset ADMIN_PASSWORD
```

Open https://msp.hiksoda.com and https://mspadmin.hiksoda.com.

Other hostnames: `./init-env.sh app.example.com admin.example.com`, or edit `APP_HOST` and `ADMIN_HOST` in `.env`.

## Files

| File | Holds |
|---|---|
| `compose.yaml` | The app: API, ingest, worker, both web apps, PostgreSQL, Redis, file storage, PDF rendering, virus scanning and Mailpit. It publishes no public port |
| `compose.override.yaml` | The Traefik settings: network, hostnames, entrypoint `websecure`, certificate resolver `letsencrypt`. Change them here if your Traefik uses other names |
| `nginx-client.conf`, `nginx-admin.conf` | Each web app passes its own API routes to the backend. The client host never serves admin routes |
| `.env` | Secrets and hostnames. Not in git |

## Day to day

| Task | Command |
|---|---|
| Update | `git pull && ./init-env.sh && docker compose up -d --build` |
| Status | `docker compose ps` |
| Logs | `docker compose logs -f --tail=100 api ingest worker` |
| Stop, keeping data | `docker compose down` |
| Read email | `ssh -L 58025:127.0.0.1:58025 your-server`, then http://localhost:58025 |
| Back up now | `./backup.sh` |

Never run `docker compose down -v` on the server: it deletes the database and the backups.

## Backups

The database's change log is archived and encrypted at least once a minute. Add a daily base backup with `crontab -e`:

```bash
15 2 * * * cd /srv/msp/deploy/production-docker && ./backup.sh >> backup.log 2>&1
```

Backups stay in a Docker volume on the same disk, so they do not survive losing the server. For an off-site copy, see [deploy/testing](../testing/README.md#backups). To restore, follow the [restore runbook](../../docs/operations/runbooks/restore.md).

## Server size

At least 2 vCPUs, 4 GB of memory and 40 GB of disk. 8 GB of memory is comfortable. The running stack uses about 2 GB, ClamAV about 1 GB of that, and building the images needs more.
