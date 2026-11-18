# Rotating secrets and keys

Rotate a secret when it may have leaked, when someone who knew it leaves, and on a schedule once production has one. Record each rotation (what, when, who) in the operations log, never the value.

All secrets for the testing server are in `deploy/testing/.env`, which only the deploying user can read.

| Secret | Can rotate? | How |
|---|---|---|
| Database role passwords (`DB_*_PASSWORD`, `POSTGRES_PASSWORD`) | Yes | Below |
| Object storage keys (`S3_ACCESS_KEY`, `S3_SECRET_KEY`) | Yes | Change `.env` and `seaweedfs/s3.json`, then `docker compose up -d seaweedfs api worker` |
| Provider credentials | Yes | Admin → Integrations → provider → credentials. Stored encrypted |
| Webhook endpoint secrets | Yes | Create a new endpoint in Admin → Integrations, update the provider, then disable the old one |
| Grafana admin password | Yes | Change in Grafana, and in `.env` for new installs |
| Backup passphrase (`BACKUP_CIPHER_PASS`) | Only with a new repository | Below |
| Administrator passwords and two-step codes | Yes | Platform admins → Recover |
| **`ENCRYPTION_KEY`** | **No, not yet** | See gap |
| **`HMAC_KEY`** | **No, not yet** | See gap |

## Database passwords

```bash
cd deploy/testing
NEW=$(openssl rand -hex 24)
docker compose exec -u postgres postgres psql -c "ALTER ROLE msp_app PASSWORD '$NEW'"
# set DB_APP_PASSWORD=$NEW in .env, then
docker compose up -d api worker
```

The same for `msp_platform` (`DB_PLATFORM_PASSWORD`), `msp_system` (`DB_SYSTEM_PASSWORD`) and `msp_owner` (`DB_OWNER_PASSWORD`, used only by `migrate`). There is a short gap between the two steps where new connections fail, so do it at a quiet time.

## Backup passphrase

pgBackRest cannot re-encrypt an existing repository. To change the passphrase, start a new repository: set the new `BACKUP_CIPHER_PASS`, move the old `pgbackups` volume aside (keep it, with its old passphrase, until its backups age out), recreate Postgres, and run `./backup.sh`. Then run `./restore-drill.sh` to prove the new chain works.

## Gap: application keys

`ENCRYPTION_KEY` encrypts secrets stored in the database, such as provider credentials and two-step secrets. `HMAC_KEY` makes lookup hashes and signs CSRF tokens. Both are single keys with no version marker, so changing either one makes existing data unreadable or unfindable.

Supporting rotation needs keys with IDs, data marked with the key that wrote it, and a job that re-encrypts or re-hashes in the background. That is not built. It is recorded in [phase-7-status](../../implementation/phase-7-status.md) and should be done before production. Until then, if either key may have leaked, treat it as a [data breach](data-breach.md) and plan the re-encryption with engineering.
