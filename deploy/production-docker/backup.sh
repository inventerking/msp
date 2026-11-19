#!/bin/sh
# Takes an encrypted base backup with pgBackRest. Run it daily from cron, for example:
#   15 2 * * * cd /srv/msp/deploy/production-docker && ./backup.sh >> backup.log 2>&1
# Between backups the change log (WAL) is archived at least once a minute.
# Usage: ./backup.sh [full|diff|incr]
set -eu
cd "$(dirname "$0")"
TYPE="${1:-full}"
case "$TYPE" in full|diff|incr) ;; *) echo "usage: $0 [full|diff|incr]" >&2; exit 2 ;; esac
PGBR="docker compose exec -T -u postgres postgres pgbackrest"
$PGBR stanza-create
# check forces a WAL switch and waits until that segment is archived, so it proves archiving works.
$PGBR check
$PGBR backup --type="$TYPE"
$PGBR info
