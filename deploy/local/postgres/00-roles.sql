-- Local and test environments only. Real environments provision these roles separately.
-- Runs as the database superuser when the container is first created.
CREATE ROLE msp_owner    LOGIN PASSWORD 'dev_owner';
CREATE ROLE msp_app      LOGIN PASSWORD 'dev_app'      NOBYPASSRLS;
CREATE ROLE msp_platform LOGIN PASSWORD 'dev_platform' NOBYPASSRLS;
CREATE ROLE msp_system   LOGIN PASSWORD 'dev_system'   NOBYPASSRLS;
CREATE ROLE msp_ingest   LOGIN PASSWORD 'dev_ingest'   NOBYPASSRLS;
CREATE ROLE msp_readonly LOGIN PASSWORD 'dev_readonly' NOBYPASSRLS;
CREATE DATABASE msp_engine OWNER msp_owner;
