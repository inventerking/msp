#!/bin/sh
# Timed restore drill. It restores the latest backup plus archived WAL into a throwaway container
# with no network, then checks the copy and writes a report to drills/. It never touches the
# running database, and the backup volume is mounted read-only.
#
# Checks: the restored copy reaches a restore point created at the start of the drill; every
# counted table has at least the rows it had then; the migration version and the number of tables
# with forced row-level security match; and the application role sees no tenant rows without a
# tenant set.
set -eu
cd "$(dirname "$0")"
DC="docker compose --env-file ${ENV_FILE:-.env}"
PROJECT="${COMPOSE_PROJECT_NAME:-msp-testing}"
IMAGE=msp-engine-postgres:testing
LABEL="drill-$(date -u +%Y%m%dT%H%M%SZ)"
NAME="msp-restore-$LABEL"
VOL="msp-restore-$LABEL"
REPORT="drills/$LABEL.md"
RTO_SECONDS="${RTO_SECONDS:-14400}"
RPO_SECONDS="${RPO_SECONDS:-900}"
TABLES="identity.tenants identity.users identity.memberships platform.subscriptions platform.tenant_module_entitlements platform.credit_ledger platform.invoices platform.payments audit.events jobs.jobs"
mkdir -p drills

psql_live() { $DC exec -T -u postgres postgres psql -d msp_engine -At -v ON_ERROR_STOP=1 "$@"; }
psql_copy() { docker exec -i -u postgres "$NAME" psql -d msp_engine -At -v ON_ERROR_STOP=1 "$@"; }
cleanup() { docker rm -f "$NAME" >/dev/null 2>&1 || true; docker volume rm "$VOL" >/dev/null 2>&1 || true; }
trap cleanup EXIT INT TERM

BACKUPS=$(docker volume ls -q --filter "label=com.docker.compose.project=$PROJECT" --filter label=com.docker.compose.volume=pgbackups)
[ -n "$BACKUPS" ] || { echo "no pgbackups volume for project $PROJECT" >&2; exit 1; }
PGBR_ENV=$($DC exec -T postgres env | grep '^PGBACKREST_' | sed 's/^/-e /' | tr '\n' ' ')

echo "Recording live state and restore point $LABEL"
counts_sql="BEGIN ISOLATION LEVEL REPEATABLE READ;"
for t in $TABLES; do counts_sql="$counts_sql SELECT '$t', count(*) FROM $t;"; done
counts_sql="$counts_sql SELECT 'restore_point', pg_create_restore_point('$LABEL'); COMMIT;"
psql_live -F' ' -c "$counts_sql" | grep -v '^\(BEGIN\|COMMIT\)$' > "drills/.live-$LABEL"
LIVE_VERSION=$(psql_live -c "SELECT max(version_id) FROM goose_db_version")
LIVE_RLS=$(psql_live -c "SELECT count(*) FROM pg_class WHERE relrowsecurity AND relforcerowsecurity")
ARCHIVE_LAG=$(psql_live -c "SELECT COALESCE(EXTRACT(EPOCH FROM now() - last_archived_time)::int, -1) FROM pg_stat_archiver")
ARCHIVE_FAILED=$(psql_live -c "SELECT failed_count FROM pg_stat_archiver")
# Switches WAL and waits until the segment holding the restore point is archived.
$DC exec -T -u postgres postgres pgbackrest check >/dev/null

START=$(date +%s)
docker volume create "$VOL" >/dev/null
# shellcheck disable=SC2086
docker run --rm --network none -u postgres $PGBR_ENV -v "$BACKUPS:/backups:ro" -v "$VOL:/var/lib/postgresql" "$IMAGE" \
  sh -c 'mkdir -p "$PGBACKREST_PG1_PATH" && chmod 700 "$PGBACKREST_PG1_PATH" && pgbackrest restore --type=name --target="$0" --target-action=promote' "$LABEL"
# shellcheck disable=SC2086
docker run -d --name "$NAME" --network none $PGBR_ENV -v "$BACKUPS:/backups:ro" -v "$VOL:/var/lib/postgresql" "$IMAGE" \
  postgres -c archive_mode=off >/dev/null
i=0
until [ "$(psql_copy -c 'SELECT NOT pg_is_in_recovery()' </dev/null 2>/dev/null || true)" = "t" ]; do
  i=$((i + 1)); [ $i -lt 600 ] || { docker logs --tail 50 "$NAME" >&2; echo "restored copy never finished recovery" >&2; exit 1; }
  sleep 1
done
RESTORE_SECONDS=$(( $(date +%s) - START ))

FAIL=0
fail() { echo "- FAILED: $1" >> "$REPORT.tmp"; FAIL=1; }
: > "$REPORT.tmp"
while read -r table live; do
  [ "$table" = restore_point ] && continue
  got=$(psql_copy -c "SELECT count(*) FROM $table" </dev/null)
  echo "| $table | $live | $got |" >> "$REPORT.rows"
  [ "$got" -ge "$live" ] || fail "$table has $got rows, fewer than the $live recorded"
done < "drills/.live-$LABEL"
COPY_VERSION=$(psql_copy -c "SELECT max(version_id) FROM goose_db_version" </dev/null)
COPY_RLS=$(psql_copy -c "SELECT count(*) FROM pg_class WHERE relrowsecurity AND relforcerowsecurity" </dev/null)
[ "$COPY_VERSION" = "$LIVE_VERSION" ] || fail "migration version $COPY_VERSION, live is $LIVE_VERSION"
[ "$COPY_RLS" = "$LIVE_RLS" ] || fail "$COPY_RLS tables force row-level security, live has $LIVE_RLS"
# As the application role, with no tenant set, no tenant's rows may be visible. Shared rows with no
# tenant, such as the built-in roles, are meant to be visible to everyone.
LEAKS=$(psql_copy <<'SQL'
SET ROLE msp_app;
SELECT count(*) FROM (
  SELECT DISTINCT p.schemaname, p.tablename,
    EXISTS (SELECT 1 FROM information_schema.columns c WHERE c.table_schema = p.schemaname AND c.table_name = p.tablename AND c.column_name = 'tenant_id') AS has_tenant
  FROM pg_policies p
  WHERE p.qual LIKE '%app.tenant_id%' AND has_table_privilege('msp_app', format('%I.%I', p.schemaname, p.tablename), 'SELECT')
) t, LATERAL (SELECT (xpath('/row/c/text()', query_to_xml(
    format('SELECT count(*) AS c FROM %I.%I', t.schemaname, t.tablename) || CASE WHEN t.has_tenant THEN ' WHERE tenant_id IS NOT NULL' ELSE '' END,
    false, true, '')))[1]::text::int AS n) r
WHERE r.n > 0;
SQL
)
LEAKS=$(echo "$LEAKS" | tail -1)
[ "$LEAKS" = "0" ] || fail "$LEAKS tenant tables returned rows with no tenant set"
[ "$RESTORE_SECONDS" -le "$RTO_SECONDS" ] || fail "restore took ${RESTORE_SECONDS}s, over the ${RTO_SECONDS}s target"
[ "$ARCHIVE_LAG" -ge 0 ] && [ "$ARCHIVE_LAG" -le "$RPO_SECONDS" ] || fail "last WAL archived ${ARCHIVE_LAG}s before the drill, over the ${RPO_SECONDS}s target"

{
  echo "# Restore drill $LABEL"
  echo
  echo "- Result: $([ $FAIL -eq 0 ] && echo PASSED || echo FAILED)"
  echo "- Restore and recovery time: ${RESTORE_SECONDS}s (target ${RTO_SECONDS}s)"
  echo "- Last WAL archived before the drill: ${ARCHIVE_LAG}s earlier (target ${RPO_SECONDS}s); archive failures so far: $ARCHIVE_FAILED"
  echo "- Migration version: $COPY_VERSION; tables forcing row-level security: $COPY_RLS"
  echo "- Tenant tables visible without a tenant: $LEAKS"
  echo
  echo "| Table | Rows at restore point | Rows restored |"
  echo "|---|---|---|"
  cat "$REPORT.rows"
  echo
  cat "$REPORT.tmp"
  echo
  echo "Timing covers restore and recovery only. A real recovery also needs the steps in docs/operations/runbooks/restore.md."
} > "$REPORT"
rm -f "$REPORT.tmp" "$REPORT.rows" "drills/.live-$LABEL"
cat "$REPORT"
if [ $FAIL -eq 0 ]; then
  mkdir -p metrics-textfile
  printf '# TYPE msp_restore_drill_last_success_timestamp_seconds gauge\nmsp_restore_drill_last_success_timestamp_seconds %s\n# TYPE msp_restore_drill_duration_seconds gauge\nmsp_restore_drill_duration_seconds %s\n' "$(date +%s)" "$RESTORE_SECONDS" > metrics-textfile/.drill.prom.tmp
  mv metrics-textfile/.drill.prom.tmp metrics-textfile/drill.prom
fi
exit $FAIL
