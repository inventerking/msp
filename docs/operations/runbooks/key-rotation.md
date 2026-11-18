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
| `ENCRYPTION_KEY` | Yes | Below |
| **`HMAC_KEY`** | **Only partly, by design** | See below |

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

## Encryption key (`ENCRYPTION_KEY`)

It encrypts the secrets stored in the database: two-step verification secrets, provider credentials and webhook secrets. Each encrypted value names the key that sealed it, and the system can read with the current key and any listed previous key, so rotation needs no downtime.

1. Generate a new key and pick a new ID, one higher than the last: `openssl rand -base64 32`.
2. In `.env`, keep the old key as a previous key and make the new one current:
   ```bash
   ENCRYPTION_KEYS_PREVIOUS=env-1:<the old ENCRYPTION_KEY>
   ENCRYPTION_KEY=<the new key>
   ENCRYPTION_KEY_ID=env-2
   ```
   If there were earlier previous keys still in use, keep them in the list, separated by commas.
3. Restart the processes that use it: `docker compose up -d api worker ingest`. New secrets are now written under `env-2`, and old ones are still read.
4. Re-encrypt everything stored:
   ```bash
   docker compose run --rm --no-deps api rotate-keys
   ```
   It reports how many values it changed and is safe to run again. A value changed while it runs is caught by its second pass, or by running it again.
5. Remove the old key from `ENCRYPTION_KEYS_PREVIOUS` and restart again. Keep a copy of the old key with the backups taken before step 4, because restoring one of those needs it: add it back to `ENCRYPTION_KEYS_PREVIOUS` and run `rotate-keys` after the restore.

Record the rotation, with the key IDs and never the values, in the operations log.

## HMAC key (`HMAC_KEY`)

It makes keyed hashes that are used to **find** things: sign-in attempts by email, suppression lists, visitor and session identifiers, IP address hashes, and CSRF tokens. Most of those hashes are kept precisely because the original value is not: a suppression entry for an erased person is only a hash of their email. A hash cannot be recomputed under a new key without the original value, so changing this key would make those entries unfindable, and an erased person who unsubscribed could be emailed again.

So this key is treated as long-lived, like a root secret, and protected rather than rotated on a schedule. If it may have leaked:

1. Treat it as a [data breach](data-breach.md): with the key, someone could test guesses of emails against the hashes.
2. Decide with engineering and counsel whether to change it. Changing it signs everyone out (CSRF tokens), resets visitor and session matching, and requires re-entering suppressions from any source that still holds the addresses. Suppressions of erased people cannot be carried over.

Supporting a planned HMAC rotation would mean storing every hash under both keys during a transition, for every lookup. That is not built, deliberately, for the reason above.
