#!/bin/sh
# Takes an encrypted base backup with pgBackRest. Run it daily from cron, for example:
#   15 2 * * * cd /srv/msp/deploy/testing && ./backup.sh >> backup.log 2>&1
# WAL is archived continuously between backups (archive_timeout is 60 seconds), which is what
# gives the 15-minute recovery point. Usage: ./backup.sh [full|diff|incr]
set -eu
cd "$(dirname "$0")"
TYPE="${1:-full}"
case "$TYPE" in full|diff|incr) ;; *) echo "usage: $0 [full|diff|incr]" >&2; exit 2 ;; esac
DC="docker compose --env-file ${ENV_FILE:-.env}"
PGBR="$DC exec -T -u postgres postgres pgbackrest"
$PGBR stanza-create
# check forces a WAL switch and waits until that segment is archived, so it proves archiving works.
$PGBR check
$PGBR backup --type="$TYPE"
$PGBR info
# Recorded for the BackupStale alert, through node-exporter's text file collector.
mkdir -p metrics-textfile
printf '# HELP msp_backup_last_success_timestamp_seconds When the last base backup succeeded.\n# TYPE msp_backup_last_success_timestamp_seconds gauge\nmsp_backup_last_success_timestamp_seconds{type="%s"} %s\n' "$TYPE" "$(date +%s)" > metrics-textfile/.backup.prom.tmp
mv metrics-textfile/.backup.prom.tmp metrics-textfile/backup.prom
