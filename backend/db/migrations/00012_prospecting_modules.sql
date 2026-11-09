-- +goose Up
CREATE TABLE lead_generation.saved_searches (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    name text NOT NULL,
    criteria jsonb NOT NULL,
    created_by uuid,
    deleted_at timestamptz,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id)
);

CREATE TABLE lead_generation.search_jobs (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    saved_search_id uuid,
    criteria_snapshot jsonb NOT NULL,
    status text NOT NULL,
    progress_percent int NOT NULL,
    result_count int NOT NULL,
    provider_keys text[] NOT NULL DEFAULT '{}',
    credits_reserved bigint NOT NULL,
    credits_used bigint NOT NULL,
    job_id uuid,
    created_by uuid,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT search_jobs_ck1 CHECK (status IN ('queued','running','completed','failed','cancelled','partial')),
    CONSTRAINT search_jobs_ck2 CHECK (progress_percent BETWEEN 0 AND 100)
);

CREATE TABLE lead_generation.search_results (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    search_job_id uuid NOT NULL,
    company_id uuid NOT NULL,
    rank int NOT NULL,
    source_key text NOT NULL,
    preview jsonb NOT NULL,
    promoted_prospect_id uuid,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id)
);

CREATE TABLE lead_generation.prospects (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    company_id uuid NOT NULL,
    -- The tenant's view of the company. It may come from a tenant-licensed source, so it never
    -- flows back into the canonical record.
    display_name text NOT NULL,
    domain citext,
    status text NOT NULL,
    source_key text NOT NULL,
    source_ref text,
    data_quality text NOT NULL,
    latest_score int,
    latest_coverage numeric(3,2),
    crm_company_id uuid,
    owner_user_id uuid,
    created_by uuid,
    deleted_at timestamptz,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT prospects_ck1 CHECK (status IN ('new','enriching','qualified','disqualified','converted')),
    CONSTRAINT prospects_ck2 CHECK (data_quality IN ('unknown','low','medium','high')),
    CONSTRAINT prospects_ck3 CHECK (latest_score IS NULL OR latest_score BETWEEN 0 AND 100)
);

CREATE TABLE lead_generation.prospect_contacts (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    prospect_id uuid NOT NULL,
    contact_id uuid NOT NULL,
    role_title text,
    is_decision_maker boolean,
    confidence numeric(3,2),
    source_key text NOT NULL,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id)
);

CREATE TABLE lead_generation.prospect_attributes (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    prospect_id uuid NOT NULL,
    attribute text NOT NULL,
    value jsonb NOT NULL,
    source_key text NOT NULL,
    source_url text,
    licence_class text NOT NULL,
    confidence numeric(3,2),
    observed_at timestamptz NOT NULL,
    expires_at timestamptz,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT prospect_attributes_ck1 CHECK (licence_class IN ('public','licensed_tenant','licensed_shareable','tenant_provided'))
);

CREATE TABLE lead_generation.enrichment_runs (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    prospect_id uuid NOT NULL,
    kind text NOT NULL,
    provider_key text NOT NULL,
    status text NOT NULL,
    credits_used bigint NOT NULL,
    error_code text,
    job_id uuid,
    finished_at timestamptz,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT enrichment_runs_ck1 CHECK (kind IN ('company','contact')),
    CONSTRAINT enrichment_runs_ck2 CHECK (status IN ('queued','running','succeeded','no_data','failed','cancelled'))
);

CREATE TABLE lead_generation.scoring_models (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    name text NOT NULL,
    version int NOT NULL DEFAULT 1,
    icp_id uuid,
    weights jsonb NOT NULL,
    status text NOT NULL,
    created_by uuid,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT scoring_models_ck1 CHECK (status IN ('draft','active','retired'))
);

CREATE TABLE lead_generation.prospect_scores (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    prospect_id uuid NOT NULL,
    model_id uuid NOT NULL,
    score int NOT NULL,
    coverage numeric(3,2) NOT NULL,
    explanation text,
    ai_request_id uuid,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT prospect_scores_ck1 CHECK (score BETWEEN 0 AND 100),
    CONSTRAINT prospect_scores_ck2 CHECK (coverage BETWEEN 0 AND 1)
);

CREATE TABLE lead_generation.prospect_score_factors (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    score_id uuid NOT NULL,
    factor_key text NOT NULL,
    state text NOT NULL,
    weight numeric(5,2) NOT NULL,
    earned numeric(5,2),
    evidence jsonb NOT NULL,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT prospect_score_factors_ck1 CHECK (state IN ('matched','partial','not_matched','unknown')),
    CONSTRAINT prospect_score_factors_ck2 CHECK (state <> 'unknown' OR earned IS NULL)
);

CREATE TABLE lead_generation.prospect_lists (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    name text NOT NULL,
    description text,
    created_by uuid,
    deleted_at timestamptz,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id)
);

CREATE TABLE lead_generation.prospect_list_members (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    list_id uuid NOT NULL,
    prospect_id uuid NOT NULL,
    added_by uuid,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id)
);

CREATE TABLE lead_generation.import_jobs (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    source_file_id uuid NOT NULL,
    report_file_id uuid,
    mapping jsonb NOT NULL,
    lawful_source_attested boolean NOT NULL,
    status text NOT NULL,
    rows_total int,
    rows_imported int,
    rows_duplicate int,
    rows_rejected int,
    created_by uuid,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT import_jobs_ck1 CHECK (status IN ('uploaded','validating','importing','completed','failed','cancelled'))
);

CREATE TABLE lead_generation.export_jobs (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    criteria jsonb NOT NULL,
    file_id uuid,
    status text NOT NULL,
    rows_total int,
    values_withheld int,
    created_by uuid,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT export_jobs_ck1 CHECK (status IN ('queued','running','completed','failed','blocked_by_licence'))
);

CREATE TABLE competitor_intelligence.competitors (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    company_id uuid NOT NULL,
    -- The tenant's name for the competitor, which stays tenant data.
    display_name text NOT NULL,
    domain citext,
    status text NOT NULL,
    tier text,
    notes text,
    monitoring_enabled boolean NOT NULL,
    created_by uuid,
    deleted_at timestamptz,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT competitors_ck1 CHECK (status IN ('candidate','tracked','archived'))
);

CREATE TABLE competitor_intelligence.research_configs (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    competitor_id uuid NOT NULL,
    cadence text NOT NULL,
    scope jsonb NOT NULL,
    enabled boolean NOT NULL,
    next_run_at timestamptz,
    deleted_at timestamptz,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT research_configs_ck1 CHECK (cadence IN ('manual','weekly','monthly'))
);

CREATE TABLE competitor_intelligence.research_runs (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    competitor_id uuid NOT NULL,
    config_id uuid,
    status text NOT NULL,
    pages_fetched int NOT NULL,
    credits_used bigint NOT NULL,
    job_id uuid,
    finished_at timestamptz,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT research_runs_ck1 CHECK (status IN ('queued','running','completed','partial','failed','cancelled'))
);

CREATE TABLE competitor_intelligence.sources (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    competitor_id uuid NOT NULL,
    url text NOT NULL,
    kind text NOT NULL,
    access_status text NOT NULL,
    last_fetched_at timestamptz,
    deleted_at timestamptz,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT sources_ck1 CHECK (kind IN ('website','pricing_page','service_page','public_directory','provider')),
    CONSTRAINT sources_ck2 CHECK (access_status IN ('allowed','disallowed_by_robots','blocked','error','unchecked'))
);

CREATE TABLE competitor_intelligence.source_snapshots (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    source_id uuid NOT NULL,
    run_id uuid,
    content_hash bytea NOT NULL,
    file_id uuid,
    fetched_at timestamptz NOT NULL,
    http_status int,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id)
);

CREATE TABLE competitor_intelligence.observations (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    competitor_id uuid NOT NULL,
    attribute_type text NOT NULL,
    value jsonb NOT NULL,
    evidence_class text NOT NULL,
    confidence numeric(3,2),
    source_snapshot_id uuid,
    ai_request_id uuid,
    observed_at timestamptz NOT NULL,
    superseded_by_id uuid,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT observations_ck1 CHECK (attribute_type IN ('service','pricing','positioning','geography','industry','technology','other')),
    CONSTRAINT observations_ck2 CHECK (evidence_class IN ('verified_public','observed','inferred','ai_estimate')),
    CONSTRAINT observations_ck3 CHECK (evidence_class NOT IN ('verified_public','observed') OR source_snapshot_id IS NOT NULL)
);

CREATE TABLE competitor_intelligence.change_events (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    competitor_id uuid NOT NULL,
    attribute_type text NOT NULL,
    previous_observation_id uuid,
    new_observation_id uuid NOT NULL,
    summary text,
    acknowledged_at timestamptz,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id)
);

CREATE TABLE competitor_intelligence.market_opportunities (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    geography text,
    industry_id uuid,
    title text NOT NULL,
    rationale text NOT NULL,
    score int,
    evidence_class text NOT NULL,
    status text NOT NULL,
    ai_request_id uuid,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT market_opportunities_ck1 CHECK (evidence_class IN ('verified_public','observed','inferred','ai_estimate')),
    CONSTRAINT market_opportunities_ck2 CHECK (status IN ('new','reviewing','pursuing','dismissed')),
    CONSTRAINT market_opportunities_ck3 CHECK (score IS NULL OR score BETWEEN 0 AND 100)
);

CREATE TABLE competitor_intelligence.opportunity_evidence (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    opportunity_id uuid NOT NULL,
    observation_id uuid,
    note text,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id)
);

CREATE TABLE competitor_intelligence.comparison_sets (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    name text NOT NULL,
    dimensions text[] NOT NULL DEFAULT '{}',
    created_by uuid,
    deleted_at timestamptz,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id)
);

CREATE TABLE competitor_intelligence.comparison_set_members (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    comparison_set_id uuid NOT NULL,
    competitor_id uuid NOT NULL,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id)
);

CREATE TABLE competitor_intelligence.reports (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    title text NOT NULL,
    kind text NOT NULL,
    status text NOT NULL,
    content jsonb,
    file_id uuid,
    ai_request_id uuid,
    created_by uuid,
    deleted_at timestamptz,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT reports_ck1 CHECK (kind IN ('competitor','market','comparison')),
    CONSTRAINT reports_ck2 CHECK (status IN ('queued','generating','ready','failed'))
);

ALTER TABLE lead_generation.saved_searches ADD CONSTRAINT saved_searches_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE lead_generation.search_jobs ADD CONSTRAINT search_jobs_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE lead_generation.search_jobs ADD CONSTRAINT search_jobs_saved_search_id_fk FOREIGN KEY (tenant_id, saved_search_id) REFERENCES lead_generation.saved_searches (tenant_id, id) ON DELETE SET NULL (saved_search_id);
ALTER TABLE lead_generation.search_results ADD CONSTRAINT search_results_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE lead_generation.search_results ADD CONSTRAINT search_results_search_job_id_fk FOREIGN KEY (tenant_id, search_job_id) REFERENCES lead_generation.search_jobs (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE lead_generation.search_results ADD CONSTRAINT search_results_company_id_fk FOREIGN KEY (company_id) REFERENCES shared.companies (id) ON DELETE RESTRICT;
ALTER TABLE lead_generation.search_results ADD CONSTRAINT search_results_promoted_prospect_id_fk FOREIGN KEY (tenant_id, promoted_prospect_id) REFERENCES lead_generation.prospects (tenant_id, id) ON DELETE SET NULL (promoted_prospect_id);
ALTER TABLE lead_generation.prospects ADD CONSTRAINT prospects_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE lead_generation.prospects ADD CONSTRAINT prospects_company_id_fk FOREIGN KEY (company_id) REFERENCES shared.companies (id) ON DELETE RESTRICT;
ALTER TABLE lead_generation.prospect_contacts ADD CONSTRAINT prospect_contacts_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE lead_generation.prospect_contacts ADD CONSTRAINT prospect_contacts_prospect_id_fk FOREIGN KEY (tenant_id, prospect_id) REFERENCES lead_generation.prospects (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE lead_generation.prospect_contacts ADD CONSTRAINT prospect_contacts_contact_id_fk FOREIGN KEY (tenant_id, contact_id) REFERENCES shared.contacts (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE lead_generation.prospect_attributes ADD CONSTRAINT prospect_attributes_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE lead_generation.prospect_attributes ADD CONSTRAINT prospect_attributes_prospect_id_fk FOREIGN KEY (tenant_id, prospect_id) REFERENCES lead_generation.prospects (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE lead_generation.enrichment_runs ADD CONSTRAINT enrichment_runs_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE lead_generation.enrichment_runs ADD CONSTRAINT enrichment_runs_prospect_id_fk FOREIGN KEY (tenant_id, prospect_id) REFERENCES lead_generation.prospects (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE lead_generation.scoring_models ADD CONSTRAINT scoring_models_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE lead_generation.prospect_scores ADD CONSTRAINT prospect_scores_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE lead_generation.prospect_scores ADD CONSTRAINT prospect_scores_prospect_id_fk FOREIGN KEY (tenant_id, prospect_id) REFERENCES lead_generation.prospects (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE lead_generation.prospect_scores ADD CONSTRAINT prospect_scores_model_id_fk FOREIGN KEY (tenant_id, model_id) REFERENCES lead_generation.scoring_models (tenant_id, id) ON DELETE RESTRICT;
ALTER TABLE lead_generation.prospect_score_factors ADD CONSTRAINT prospect_score_factors_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE lead_generation.prospect_score_factors ADD CONSTRAINT prospect_score_factors_score_id_fk FOREIGN KEY (tenant_id, score_id) REFERENCES lead_generation.prospect_scores (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE lead_generation.prospect_lists ADD CONSTRAINT prospect_lists_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE lead_generation.prospect_list_members ADD CONSTRAINT prospect_list_members_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE lead_generation.prospect_list_members ADD CONSTRAINT prospect_list_members_list_id_fk FOREIGN KEY (tenant_id, list_id) REFERENCES lead_generation.prospect_lists (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE lead_generation.prospect_list_members ADD CONSTRAINT prospect_list_members_prospect_id_fk FOREIGN KEY (tenant_id, prospect_id) REFERENCES lead_generation.prospects (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE lead_generation.import_jobs ADD CONSTRAINT import_jobs_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE lead_generation.export_jobs ADD CONSTRAINT export_jobs_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE competitor_intelligence.competitors ADD CONSTRAINT competitors_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE competitor_intelligence.competitors ADD CONSTRAINT competitors_company_id_fk FOREIGN KEY (company_id) REFERENCES shared.companies (id) ON DELETE RESTRICT;
ALTER TABLE competitor_intelligence.research_configs ADD CONSTRAINT research_configs_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE competitor_intelligence.research_configs ADD CONSTRAINT research_configs_competitor_id_fk FOREIGN KEY (tenant_id, competitor_id) REFERENCES competitor_intelligence.competitors (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE competitor_intelligence.research_runs ADD CONSTRAINT research_runs_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE competitor_intelligence.research_runs ADD CONSTRAINT research_runs_competitor_id_fk FOREIGN KEY (tenant_id, competitor_id) REFERENCES competitor_intelligence.competitors (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE competitor_intelligence.research_runs ADD CONSTRAINT research_runs_config_id_fk FOREIGN KEY (tenant_id, config_id) REFERENCES competitor_intelligence.research_configs (tenant_id, id) ON DELETE SET NULL (config_id);
ALTER TABLE competitor_intelligence.sources ADD CONSTRAINT sources_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE competitor_intelligence.sources ADD CONSTRAINT sources_competitor_id_fk FOREIGN KEY (tenant_id, competitor_id) REFERENCES competitor_intelligence.competitors (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE competitor_intelligence.source_snapshots ADD CONSTRAINT source_snapshots_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE competitor_intelligence.source_snapshots ADD CONSTRAINT source_snapshots_source_id_fk FOREIGN KEY (tenant_id, source_id) REFERENCES competitor_intelligence.sources (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE competitor_intelligence.source_snapshots ADD CONSTRAINT source_snapshots_run_id_fk FOREIGN KEY (tenant_id, run_id) REFERENCES competitor_intelligence.research_runs (tenant_id, id) ON DELETE SET NULL (run_id);
ALTER TABLE competitor_intelligence.observations ADD CONSTRAINT observations_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE competitor_intelligence.observations ADD CONSTRAINT observations_competitor_id_fk FOREIGN KEY (tenant_id, competitor_id) REFERENCES competitor_intelligence.competitors (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE competitor_intelligence.observations ADD CONSTRAINT observations_source_snapshot_id_fk FOREIGN KEY (tenant_id, source_snapshot_id) REFERENCES competitor_intelligence.source_snapshots (tenant_id, id) ON DELETE SET NULL (source_snapshot_id);
ALTER TABLE competitor_intelligence.observations ADD CONSTRAINT observations_superseded_by_id_fk FOREIGN KEY (tenant_id, superseded_by_id) REFERENCES competitor_intelligence.observations (tenant_id, id) ON DELETE SET NULL (superseded_by_id);
ALTER TABLE competitor_intelligence.change_events ADD CONSTRAINT change_events_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE competitor_intelligence.change_events ADD CONSTRAINT change_events_competitor_id_fk FOREIGN KEY (tenant_id, competitor_id) REFERENCES competitor_intelligence.competitors (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE competitor_intelligence.change_events ADD CONSTRAINT change_events_previous_observation_id_fk FOREIGN KEY (tenant_id, previous_observation_id) REFERENCES competitor_intelligence.observations (tenant_id, id) ON DELETE SET NULL (previous_observation_id);
ALTER TABLE competitor_intelligence.change_events ADD CONSTRAINT change_events_new_observation_id_fk FOREIGN KEY (tenant_id, new_observation_id) REFERENCES competitor_intelligence.observations (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE competitor_intelligence.market_opportunities ADD CONSTRAINT market_opportunities_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE competitor_intelligence.opportunity_evidence ADD CONSTRAINT opportunity_evidence_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE competitor_intelligence.opportunity_evidence ADD CONSTRAINT opportunity_evidence_opportunity_id_fk FOREIGN KEY (tenant_id, opportunity_id) REFERENCES competitor_intelligence.market_opportunities (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE competitor_intelligence.opportunity_evidence ADD CONSTRAINT opportunity_evidence_observation_id_fk FOREIGN KEY (tenant_id, observation_id) REFERENCES competitor_intelligence.observations (tenant_id, id) ON DELETE SET NULL (observation_id);
ALTER TABLE competitor_intelligence.comparison_sets ADD CONSTRAINT comparison_sets_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE competitor_intelligence.comparison_set_members ADD CONSTRAINT comparison_set_members_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE competitor_intelligence.comparison_set_members ADD CONSTRAINT comparison_set_members_comparison_set_id_fk FOREIGN KEY (tenant_id, comparison_set_id) REFERENCES competitor_intelligence.comparison_sets (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE competitor_intelligence.comparison_set_members ADD CONSTRAINT comparison_set_members_competitor_id_fk FOREIGN KEY (tenant_id, competitor_id) REFERENCES competitor_intelligence.competitors (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE competitor_intelligence.reports ADD CONSTRAINT reports_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;

CREATE TRIGGER touch_updated_at BEFORE UPDATE ON lead_generation.saved_searches FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE INDEX search_jobs_ix1 ON lead_generation.search_jobs (tenant_id, created_at DESC);
CREATE INDEX search_jobs_saved_search_id_idx ON lead_generation.search_jobs (tenant_id, saved_search_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON lead_generation.search_jobs FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX search_results_uq1 ON lead_generation.search_results (tenant_id, search_job_id, company_id);
CREATE INDEX search_results_search_job_id_idx ON lead_generation.search_results (tenant_id, search_job_id);
CREATE INDEX search_results_company_id_idx ON lead_generation.search_results (tenant_id, company_id);
CREATE INDEX search_results_promoted_prospect_id_idx ON lead_generation.search_results (tenant_id, promoted_prospect_id);
CREATE UNIQUE INDEX prospects_uq1 ON lead_generation.prospects (tenant_id, company_id) WHERE deleted_at IS NULL;
CREATE INDEX prospects_ix1 ON lead_generation.prospects (tenant_id, status, latest_score DESC);
CREATE INDEX prospects_company_id_idx ON lead_generation.prospects (tenant_id, company_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON lead_generation.prospects FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX prospect_contacts_uq1 ON lead_generation.prospect_contacts (tenant_id, prospect_id, contact_id);
CREATE INDEX prospect_contacts_prospect_id_idx ON lead_generation.prospect_contacts (tenant_id, prospect_id);
CREATE INDEX prospect_contacts_contact_id_idx ON lead_generation.prospect_contacts (tenant_id, contact_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON lead_generation.prospect_contacts FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE INDEX prospect_attributes_ix1 ON lead_generation.prospect_attributes (tenant_id, prospect_id, attribute, observed_at DESC);
CREATE INDEX prospect_attributes_prospect_id_idx ON lead_generation.prospect_attributes (tenant_id, prospect_id);
CREATE INDEX enrichment_runs_prospect_id_idx ON lead_generation.enrichment_runs (tenant_id, prospect_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON lead_generation.enrichment_runs FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX scoring_models_uq1 ON lead_generation.scoring_models (tenant_id, name, version);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON lead_generation.scoring_models FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE INDEX prospect_scores_ix1 ON lead_generation.prospect_scores (tenant_id, prospect_id, created_at DESC);
CREATE INDEX prospect_scores_prospect_id_idx ON lead_generation.prospect_scores (tenant_id, prospect_id);
CREATE INDEX prospect_scores_model_id_idx ON lead_generation.prospect_scores (tenant_id, model_id);
CREATE UNIQUE INDEX prospect_score_factors_uq1 ON lead_generation.prospect_score_factors (tenant_id, score_id, factor_key);
CREATE INDEX prospect_score_factors_score_id_idx ON lead_generation.prospect_score_factors (tenant_id, score_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON lead_generation.prospect_score_factors FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX prospect_lists_uq1 ON lead_generation.prospect_lists (tenant_id, name) WHERE deleted_at IS NULL;
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON lead_generation.prospect_lists FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX prospect_list_members_uq1 ON lead_generation.prospect_list_members (tenant_id, list_id, prospect_id);
CREATE INDEX prospect_list_members_list_id_idx ON lead_generation.prospect_list_members (tenant_id, list_id);
CREATE INDEX prospect_list_members_prospect_id_idx ON lead_generation.prospect_list_members (tenant_id, prospect_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON lead_generation.prospect_list_members FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON lead_generation.import_jobs FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON lead_generation.export_jobs FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX competitors_uq1 ON competitor_intelligence.competitors (tenant_id, company_id) WHERE deleted_at IS NULL;
CREATE INDEX competitors_company_id_idx ON competitor_intelligence.competitors (tenant_id, company_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON competitor_intelligence.competitors FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE INDEX research_configs_competitor_id_idx ON competitor_intelligence.research_configs (tenant_id, competitor_id);
CREATE UNIQUE INDEX research_configs_uq1 ON competitor_intelligence.research_configs (tenant_id, competitor_id) WHERE deleted_at IS NULL;
CREATE INDEX research_configs_due_idx ON competitor_intelligence.research_configs (next_run_at) WHERE enabled AND deleted_at IS NULL;
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON competitor_intelligence.research_configs FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE INDEX research_runs_competitor_id_idx ON competitor_intelligence.research_runs (tenant_id, competitor_id);
CREATE INDEX research_runs_config_id_idx ON competitor_intelligence.research_runs (tenant_id, config_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON competitor_intelligence.research_runs FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX sources_uq1 ON competitor_intelligence.sources (tenant_id, competitor_id, url) WHERE deleted_at IS NULL;
CREATE INDEX sources_competitor_id_idx ON competitor_intelligence.sources (tenant_id, competitor_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON competitor_intelligence.sources FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE INDEX source_snapshots_source_id_idx ON competitor_intelligence.source_snapshots (tenant_id, source_id);
CREATE INDEX source_snapshots_run_id_idx ON competitor_intelligence.source_snapshots (tenant_id, run_id);
CREATE INDEX observations_ix1 ON competitor_intelligence.observations (tenant_id, competitor_id, attribute_type, observed_at DESC);
CREATE INDEX observations_competitor_id_idx ON competitor_intelligence.observations (tenant_id, competitor_id);
CREATE INDEX observations_source_snapshot_id_idx ON competitor_intelligence.observations (tenant_id, source_snapshot_id);
CREATE INDEX observations_superseded_by_id_idx ON competitor_intelligence.observations (tenant_id, superseded_by_id);
CREATE INDEX change_events_competitor_id_idx ON competitor_intelligence.change_events (tenant_id, competitor_id);
CREATE INDEX change_events_previous_observation_id_idx ON competitor_intelligence.change_events (tenant_id, previous_observation_id);
CREATE INDEX change_events_new_observation_id_idx ON competitor_intelligence.change_events (tenant_id, new_observation_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON competitor_intelligence.market_opportunities FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE INDEX opportunity_evidence_opportunity_id_idx ON competitor_intelligence.opportunity_evidence (tenant_id, opportunity_id);
CREATE INDEX opportunity_evidence_observation_id_idx ON competitor_intelligence.opportunity_evidence (tenant_id, observation_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON competitor_intelligence.opportunity_evidence FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON competitor_intelligence.comparison_sets FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX comparison_set_members_uq1 ON competitor_intelligence.comparison_set_members (tenant_id, comparison_set_id, competitor_id);
CREATE INDEX comparison_set_members_comparison_set_id_idx ON competitor_intelligence.comparison_set_members (tenant_id, comparison_set_id);
CREATE INDEX comparison_set_members_competitor_id_idx ON competitor_intelligence.comparison_set_members (tenant_id, competitor_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON competitor_intelligence.comparison_set_members FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON competitor_intelligence.reports FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();

ALTER TABLE lead_generation.saved_searches ENABLE ROW LEVEL SECURITY;
ALTER TABLE lead_generation.saved_searches FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON lead_generation.saved_searches TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE lead_generation.search_jobs ENABLE ROW LEVEL SECURITY;
ALTER TABLE lead_generation.search_jobs FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON lead_generation.search_jobs TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE lead_generation.search_results ENABLE ROW LEVEL SECURITY;
ALTER TABLE lead_generation.search_results FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON lead_generation.search_results TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE lead_generation.prospects ENABLE ROW LEVEL SECURITY;
ALTER TABLE lead_generation.prospects FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON lead_generation.prospects TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE lead_generation.prospect_contacts ENABLE ROW LEVEL SECURITY;
ALTER TABLE lead_generation.prospect_contacts FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON lead_generation.prospect_contacts TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE lead_generation.prospect_attributes ENABLE ROW LEVEL SECURITY;
ALTER TABLE lead_generation.prospect_attributes FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON lead_generation.prospect_attributes TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE lead_generation.enrichment_runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE lead_generation.enrichment_runs FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON lead_generation.enrichment_runs TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE lead_generation.scoring_models ENABLE ROW LEVEL SECURITY;
ALTER TABLE lead_generation.scoring_models FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON lead_generation.scoring_models TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE lead_generation.prospect_scores ENABLE ROW LEVEL SECURITY;
ALTER TABLE lead_generation.prospect_scores FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON lead_generation.prospect_scores TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE lead_generation.prospect_score_factors ENABLE ROW LEVEL SECURITY;
ALTER TABLE lead_generation.prospect_score_factors FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON lead_generation.prospect_score_factors TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE lead_generation.prospect_lists ENABLE ROW LEVEL SECURITY;
ALTER TABLE lead_generation.prospect_lists FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON lead_generation.prospect_lists TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE lead_generation.prospect_list_members ENABLE ROW LEVEL SECURITY;
ALTER TABLE lead_generation.prospect_list_members FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON lead_generation.prospect_list_members TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE lead_generation.import_jobs ENABLE ROW LEVEL SECURITY;
ALTER TABLE lead_generation.import_jobs FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON lead_generation.import_jobs TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE lead_generation.export_jobs ENABLE ROW LEVEL SECURITY;
ALTER TABLE lead_generation.export_jobs FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON lead_generation.export_jobs TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE competitor_intelligence.competitors ENABLE ROW LEVEL SECURITY;
ALTER TABLE competitor_intelligence.competitors FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON competitor_intelligence.competitors TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE competitor_intelligence.research_configs ENABLE ROW LEVEL SECURITY;
ALTER TABLE competitor_intelligence.research_configs FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON competitor_intelligence.research_configs TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE competitor_intelligence.research_runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE competitor_intelligence.research_runs FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON competitor_intelligence.research_runs TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE competitor_intelligence.sources ENABLE ROW LEVEL SECURITY;
ALTER TABLE competitor_intelligence.sources FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON competitor_intelligence.sources TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE competitor_intelligence.source_snapshots ENABLE ROW LEVEL SECURITY;
ALTER TABLE competitor_intelligence.source_snapshots FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON competitor_intelligence.source_snapshots TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE competitor_intelligence.observations ENABLE ROW LEVEL SECURITY;
ALTER TABLE competitor_intelligence.observations FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON competitor_intelligence.observations TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE competitor_intelligence.change_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE competitor_intelligence.change_events FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON competitor_intelligence.change_events TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE competitor_intelligence.market_opportunities ENABLE ROW LEVEL SECURITY;
ALTER TABLE competitor_intelligence.market_opportunities FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON competitor_intelligence.market_opportunities TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE competitor_intelligence.opportunity_evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE competitor_intelligence.opportunity_evidence FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON competitor_intelligence.opportunity_evidence TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE competitor_intelligence.comparison_sets ENABLE ROW LEVEL SECURITY;
ALTER TABLE competitor_intelligence.comparison_sets FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON competitor_intelligence.comparison_sets TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE competitor_intelligence.comparison_set_members ENABLE ROW LEVEL SECURITY;
ALTER TABLE competitor_intelligence.comparison_set_members FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON competitor_intelligence.comparison_set_members TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE competitor_intelligence.reports ENABLE ROW LEVEL SECURITY;
ALTER TABLE competitor_intelligence.reports FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON competitor_intelligence.reports TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT USAGE ON SCHEMA lead_generation, competitor_intelligence TO msp_app, msp_system;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA lead_generation, competitor_intelligence TO msp_app;
-- The scheduler finds due research across tenants, then runs each under that tenant's context.
GRANT SELECT ON competitor_intelligence.research_configs TO msp_system;
-- A canonical company merge is platform-wide, so its consumer repoints rows in every tenant.
GRANT SELECT, UPDATE ON lead_generation.prospects, lead_generation.search_results, competitor_intelligence.competitors TO msp_system;
CREATE POLICY merge_repoint ON lead_generation.prospects TO msp_system USING (true) WITH CHECK (true);
CREATE POLICY merge_repoint ON lead_generation.search_results TO msp_system USING (true) WITH CHECK (true);
CREATE POLICY merge_repoint ON competitor_intelligence.competitors TO msp_system USING (true) WITH CHECK (true);
CREATE POLICY scheduler_read ON competitor_intelligence.research_configs FOR SELECT TO msp_system USING (true);

-- +goose Down
DROP TABLE IF EXISTS competitor_intelligence.reports CASCADE;
DROP TABLE IF EXISTS competitor_intelligence.comparison_set_members CASCADE;
DROP TABLE IF EXISTS competitor_intelligence.comparison_sets CASCADE;
DROP TABLE IF EXISTS competitor_intelligence.opportunity_evidence CASCADE;
DROP TABLE IF EXISTS competitor_intelligence.market_opportunities CASCADE;
DROP TABLE IF EXISTS competitor_intelligence.change_events CASCADE;
DROP TABLE IF EXISTS competitor_intelligence.observations CASCADE;
DROP TABLE IF EXISTS competitor_intelligence.source_snapshots CASCADE;
DROP TABLE IF EXISTS competitor_intelligence.sources CASCADE;
DROP TABLE IF EXISTS competitor_intelligence.research_runs CASCADE;
DROP TABLE IF EXISTS competitor_intelligence.research_configs CASCADE;
DROP TABLE IF EXISTS competitor_intelligence.competitors CASCADE;
DROP TABLE IF EXISTS lead_generation.export_jobs CASCADE;
DROP TABLE IF EXISTS lead_generation.import_jobs CASCADE;
DROP TABLE IF EXISTS lead_generation.prospect_list_members CASCADE;
DROP TABLE IF EXISTS lead_generation.prospect_lists CASCADE;
DROP TABLE IF EXISTS lead_generation.prospect_score_factors CASCADE;
DROP TABLE IF EXISTS lead_generation.prospect_scores CASCADE;
DROP TABLE IF EXISTS lead_generation.scoring_models CASCADE;
DROP TABLE IF EXISTS lead_generation.enrichment_runs CASCADE;
DROP TABLE IF EXISTS lead_generation.prospect_attributes CASCADE;
DROP TABLE IF EXISTS lead_generation.prospect_contacts CASCADE;
DROP TABLE IF EXISTS lead_generation.prospects CASCADE;
DROP TABLE IF EXISTS lead_generation.search_results CASCADE;
DROP TABLE IF EXISTS lead_generation.search_jobs CASCADE;
DROP TABLE IF EXISTS lead_generation.saved_searches CASCADE;
