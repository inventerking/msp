#!/bin/sh
# Creates the backup stanza when a new database volume is first initialized, so WAL archiving
# works from the start. backup.sh also runs this, which is safe to repeat.
set -eu
pgbackrest stanza-create
