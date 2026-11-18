# Restore the database

Backups: pgBackRest archives WAL continuously (at least every 60 seconds) and takes a base backup daily, encrypted with `BACKUP_CIPHER_PASS`. Restoring needs that passphrase. **Keep a copy of it away from the server and its backups**, or the backups cannot be read.

## Monthly drill

```bash
cd deploy/testing
./restore-drill.sh
```

It restores the latest backup plus WAL into a throwaway container with no network, up to a restore point it creates at the start. The live database is not touched and the backup volume is mounted read-only. It checks row counts, the migration version, that row-level security is still forced, and that the application role sees no tenant's rows without a tenant set. It writes a report to `drills/` and exits non-zero on any failure. `RestoreDrillOverdue` fires if no drill has passed in 35 days.

**Result on 2026-11-18** (local run of the testing setup, synthetic data, 46 MB database): passed; restore and recovery took 19 seconds, and rows written after the base backup were recovered from WAL. A larger database takes longer, so repeat the drill on the testing server with realistic synthetic volume.

## Real restore

Decide first: **latest state** (the database is lost or corrupt) or **a point in time** (bad data was written, for example by a faulty release). Note the target time in UTC.

The timings below are for the incident timeline; the target is 4 hours in total.

1. **Stop writers** so nothing writes to the old database during the restore:
   ```bash
   cd deploy/testing
   docker compose stop api worker traefik
   ```
2. **Keep the old data** until the restore is confirmed: `docker compose stop postgres`, then copy the `pgdata` volume, or rename it (create the new volume under the old name).
3. **Restore into an empty data directory:**
   ```bash
   docker compose run --rm --no-deps -u postgres --entrypoint sh postgres -c '
     rm -rf "$PGBACKREST_PG1_PATH"/* &&
     pgbackrest restore --target-action=promote'                    # latest state
   # or, for a point in time:
   #   pgbackrest restore --type=time --target="2026-11-18 09:30:00+00" --target-action=promote
   ```
4. **Start Postgres and wait for recovery to finish:**
   ```bash
   docker compose up -d --wait postgres
   docker compose exec -u postgres postgres psql -d msp_engine -Atc 'SELECT pg_is_in_recovery()'   # f when finished
   ```
5. **Check:** migration version (`SELECT max(version_id) FROM goose_db_version`), row counts of key tables against expectations, and that the most recent data you expect is present.
6. **New backup chain:** after a point-in-time restore, the database is on a new timeline. Run `./backup.sh` at once so later restores start from it.
7. **Reapply erasures.** A restore can bring back data that was erased after the backup was taken. Every erasure is logged in object storage, outside the database, before it runs. Start the worker, then replay everything since the day of the backup you restored:
   ```bash
   docker compose up -d worker
   docker compose run --rm --no-deps api replay-erasures --since 2026-11-18
   ```
   It reports how many it found, queued, and did not find in the restored database (contacts created after the backup). Erasing is idempotent, so a generous start day is safe. Wait for the erasure jobs to finish (Admin → Jobs) before reopening.
8. **Reopen:** `docker compose up -d --wait`, then `./smoke.sh`.
9. Redis needs nothing: it holds only data that can be rebuilt.

## Files

Uploaded files are in SeaweedFS (`seaweed` volume) on the testing server and are **not** backed up by these scripts. Files whose database rows are lost become unreachable, which is safe; rows whose files are lost show as missing files. Production needs versioned object storage with replication (D-23).

## Known gaps

- **Erasure log location.** The log is in the same object store as uploaded files. On the testing server that is SeaweedFS on the same host, so losing the host loses both the database and the log. Production needs object storage on separate infrastructure, with versioning (D-23).
- **Off-host copy.** The testing setup keeps backups on the same host unless `backup-offsite.env` adds a second repository (see the testing README). Losing the host loses the backups.
