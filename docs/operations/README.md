# Operations

Status: **for the testing server (D-23).** Production hosting is decided after handover, so these documents describe the single-host testing setup in [deploy/testing](../../deploy/testing/README.md). Where production must differ, the runbook says so.

## What runs where

| Piece | Testing server | Notes |
|---|---|---|
| API, worker, ingest, both apps | One Docker host | One backend image; the role is chosen at start. Ingest serves only the visitor tracker, with its own 10-connection pool, so tracking traffic cannot slow the app |
| PostgreSQL | Same host, `msp-engine-postgres` image | pgvector and pgBackRest |
| Backups | `pgbackups` volume, encrypted (AES-256) | Off-host copy is optional here and required in production |
| Monitoring | `compose.monitoring.yaml` | Prometheus, Alertmanager, Grafana, node-exporter |
| Alerts | Mailpit | Nobody outside receives them until a real receiver is chosen |

## Objectives

From [observability-and-reliability](../architecture/observability-and-reliability.md). These are proposed and are confirmed against cost when production hosting is chosen.

| Objective | Target | How it is checked |
|---|---|---|
| Recovery point (RPO) | 15 minutes | WAL archived at least every 60 seconds; `WALArchivingStale` alert at 15 minutes |
| Recovery time (RTO) | 4 hours | Timed by `restore-drill.sh`; [restore runbook](runbooks/restore.md) |
| API latency | p95 below 500 ms for ordinary routes | `SlowAPI` alert; load test in [load-testing](load-testing.md) |
| Purchase review | Within the target in the dunning policy (24 hours by default) | `PurchaseReviewOverdue` alert |

## Runbooks

| Situation | Runbook |
|---|---|
| Something is down or slow, or an alert fired | [incident](runbooks/incident.md) |
| Data may have been exposed | [data-breach](runbooks/data-breach.md) |
| Restoring the database, or the monthly drill | [restore](runbooks/restore.md) |
| Releasing a new version | [deploy](runbooks/deploy.md) |
| Payments, refunds, reconciliation | [payments](runbooks/payments.md) |
| An abuse report | [abuse](runbooks/abuse.md) |
| Rotating secrets and keys | [key-rotation](runbooks/key-rotation.md) |

## Monitoring

```bash
cd deploy/testing
docker compose -f compose.yaml -f compose.monitoring.yaml up -d --build --wait
ssh -L 53000:127.0.0.1:53000 your-server     # then open http://localhost:53000
```

Grafana's admin password is `GRAFANA_ADMIN_PASSWORD` in `.env`. The **MSP Growth Engine: health** dashboard shows the service, background work, backups, and billing and trust queues. Every alert in `monitoring/alerts.yml` names the runbook to follow.

The API and worker serve metrics on port 9100 on the monitoring network only; nothing publishes it. Metric labels use route templates and platform-wide counts, so they never carry tenant IDs, user IDs or anything a customer typed.

## Regular tasks

| When | Task | Command |
|---|---|---|
| Daily, from cron | Base backup | `./backup.sh` |
| Monthly, and after any change to backups or Postgres | Restore drill | `./restore-drill.sh` |
| Daily | Check the billing queues on the admin Billing page | — |
| Daily | Check abuse reports | — |
| Weekly | Check dead letters on the admin Jobs page | — |
