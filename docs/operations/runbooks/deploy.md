# Deploy a new version (testing server)

Migrations follow expand-then-contract: a release only adds, or removes what the previous release already stopped using. So the previous image can run against the new schema, and rolling back the code does not need rolling back the database.

## Before

- CI is green on the commit: backend tests, lint, frontend tests, and the end-to-end suite.
- Read the migrations in the release. If one rewrites a large table, or is not expand-then-contract, plan a maintenance window.
- Tag the running images so you can go back: `docker tag msp-engine-backend:testing msp-engine-backend:previous` (and the same for `client` and `admin`).
- Take a backup: `./backup.sh`.

## Deploy

```bash
cd deploy/testing
git fetch && git checkout <release tag>
docker compose -f compose.yaml -f compose.monitoring.yaml up -d --build --wait
./smoke.sh
```

`migrate` runs before the API and worker start, and they refuse to start if the database is not on the version they expect. Watch the health dashboard for 15 minutes: error rate, latency and job backlog.

## Roll back

If the error rate or latency is worse and the cause is not quickly clear:

```bash
docker tag msp-engine-backend:previous msp-engine-backend:testing     # and client, admin
docker compose up -d --no-build api worker client admin
```

The migration stays; the previous code works with it. If a migration itself damaged data, follow [restore](restore.md) with a point in time just before the deploy.

## Production

Production deployment happens after handover (D-23). It needs at least two application hosts and a health-gated rolling update, which this single-host setup does not have.
