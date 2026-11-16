-- Local and test environments only. Real environments provision these roles separately.
-- Runs as the database superuser when the container is first created.
CREATE ROLE msp_owner    LOGIN PASSWORD 'dev_owner';
CREATE ROLE msp_app      LOGIN PASSWORD 'dev_app'      NOBYPASSRLS;
CREATE ROLE msp_platform LOGIN PASSWORD 'dev_platform' NOBYPASSRLS;
CREATE ROLE msp_system   LOGIN PASSWORD 'dev_system'   NOBYPASSRLS;
CREATE ROLE msp_ingest   LOGIN PASSWORD 'dev_ingest'   NOBYPASSRLS;
CREATE ROLE msp_readonly LOGIN PASSWORD 'dev_readonly' NOBYPASSRLS;
CREATE DATABASE msp_engine OWNER msp_owner;
-- The API switches to msp_ingest for public visitor tracking. Without INHERIT, msp_app does not
-- otherwise gain msp_ingest's privileges or policies.
GRANT msp_ingest TO msp_app WITH INHERIT FALSE, SET TRUE;

-- Extensions that need a superuser. Migrations then only check they exist.
\connect msp_engine
CREATE EXTENSION IF NOT EXISTS vector;
