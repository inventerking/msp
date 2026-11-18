# Incident response

Use this for outages, errors, slowness and any alert. For possible exposure of data, also start [data-breach](data-breach.md) at once: it takes priority.

## 1. Declare and assign (first 5 minutes)

- Name one **incident lead**. They decide, and they keep the timeline. Everyone else reports to them.
- Start a timeline document: time (UTC), what was seen, what was done, by whom.
- Severity:

| Severity | Meaning | Examples |
|---|---|---|
| SEV1 | Service down for most tenants, data at risk, or a suspected breach | API down, database corrupt, cross-tenant access |
| SEV2 | A major feature down, or degraded for many | Worker stopped, payments failing, sign-in failing |
| SEV3 | Degraded for a few, with a workaround | One module slow, one provider failing |

## 2. Look

```bash
cd deploy/testing
docker compose ps                       # which containers are unhealthy or restarting
docker compose logs --since 30m api worker | grep -i '"level":"ERROR"' | tail -50
./smoke.sh                              # end-to-end check from outside
```

On Grafana's health dashboard, check in this order: request errors and latency, database pool use, oldest queued job and outbox age, then free disk.

| Alert | Usual cause | First action |
|---|---|---|
| TargetDown | Container stopped or failing its health check | `docker compose logs <service>`; `docker compose up -d <service>` |
| HighErrorRate | A bad release, the database, or a failing dependency | Find the route in "Slowest routes" and in logs; consider rolling back ([deploy](deploy.md)) |
| SlowAPI | Database load, a missing index, or pool exhaustion | Check pool use; `SELECT pid, now()-query_start, query FROM pg_stat_activity WHERE state <> 'idle' ORDER BY 2 DESC LIMIT 10;` |
| DatabasePoolSaturated | Slow queries holding connections, or a leak | As above; restarting the API frees connections but loses the evidence, so capture first |
| JobBacklog | Worker stopped, or a burst | `docker compose logs worker`; check the Jobs page |
| OutboxStuck | Worker stopped, or a poison event | Restart the worker; check dead letters |
| DeadLetters | A job or consumer failed permanently | Admin → Jobs: read the error, then requeue or discard with a reason |
| DiskFilling | WAL not archiving, logs, or backups | Check `WALArchivingFailing` first: unarchived WAL piles up in the data directory |
| MetricsSourceFailing | The database is unreachable to the worker | Check Postgres health |

## 3. Contain

Prefer actions that can be undone, and record each one in the timeline.

- **A bad release:** roll back to the previous image ([deploy](deploy.md)).
- **One tenant causing load or harm:** suspend it in Admin → Tenants, with the reason. This is audited and reversible.
- **A failing provider:** set it to disabled in Admin → Integrations. Work waits and retries instead of failing.
- **A feature leaking data:** treat as SEV1 and follow [data-breach](data-breach.md).

## 4. Recover and confirm

- `./smoke.sh` passes, alerts have cleared, and the backlog is draining.
- Requeue the dead letters the incident caused, once the cause is fixed.

## 5. Afterwards (within 5 working days for SEV1 and SEV2)

Write a blameless review: timeline, cause, what detected it, what slowed the response, and actions with owners. Add a test or an alert for the cause where you can.
