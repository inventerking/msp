#!/bin/sh
# Creates database roles with generated passwords when the volume is first initialized.
set -eu
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" \
  -v owner="$DB_OWNER_PASSWORD" -v app="$DB_APP_PASSWORD" -v platform="$DB_PLATFORM_PASSWORD" -v sys="$DB_SYSTEM_PASSWORD" <<'SQL'
CREATE ROLE msp_owner    LOGIN PASSWORD :'owner';
CREATE ROLE msp_app      LOGIN PASSWORD :'app'      NOBYPASSRLS;
CREATE ROLE msp_platform LOGIN PASSWORD :'platform' NOBYPASSRLS;
CREATE ROLE msp_system   LOGIN PASSWORD :'sys'      NOBYPASSRLS;
CREATE ROLE msp_ingest   NOLOGIN NOBYPASSRLS;
CREATE ROLE msp_readonly NOLOGIN NOBYPASSRLS;
CREATE DATABASE msp_engine OWNER msp_owner;
-- The API switches to msp_ingest for public visitor tracking. Without INHERIT, msp_app does not
-- otherwise gain msp_ingest's privileges or policies.
GRANT msp_ingest TO msp_app WITH INHERIT FALSE, SET TRUE;
-- Extensions that need a superuser. Migrations then only check they exist.
\connect msp_engine
CREATE EXTENSION IF NOT EXISTS vector;
SQL
