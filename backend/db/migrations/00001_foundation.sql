-- +goose Up
CREATE EXTENSION IF NOT EXISTS citext;

CREATE SCHEMA platform;
CREATE SCHEMA identity;
CREATE SCHEMA shared;
CREATE SCHEMA lead_generation;
CREATE SCHEMA competitor_intelligence;
CREATE SCHEMA outreach;
CREATE SCHEMA it_assessment;
CREATE SCHEMA crm;
CREATE SCHEMA proposals;
CREATE SCHEMA visitor_intelligence;
CREATE SCHEMA chatbot;
CREATE SCHEMA customer_expansion;
CREATE SCHEMA sales_intelligence;
CREATE SCHEMA integration;
CREATE SCHEMA audit;
CREATE SCHEMA notification;
CREATE SCHEMA jobs;
CREATE SCHEMA files;

-- +goose StatementBegin
CREATE FUNCTION shared.touch_updated_at() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose Down
DROP SCHEMA files, jobs, notification, audit, integration, sales_intelligence, customer_expansion,
    chatbot, visitor_intelligence, proposals, crm, it_assessment, outreach, competitor_intelligence,
    lead_generation, shared, identity, platform CASCADE;
