# PostgreSQL with pgvector and pgBackRest, for encrypted WAL archiving and base backups.
FROM pgvector/pgvector:0.8.7-pg18-trixie
RUN apt-get update \
 && apt-get install -y --no-install-recommends pgbackrest \
 && rm -rf /var/lib/apt/lists/* \
 && mkdir -p /backups /var/log/pgbackrest /tmp/pgbackrest \
 && chown postgres:postgres /backups /var/log/pgbackrest /tmp/pgbackrest
COPY deploy/testing/postgres/10-pgbackrest.sh /docker-entrypoint-initdb.d/10-pgbackrest.sh
