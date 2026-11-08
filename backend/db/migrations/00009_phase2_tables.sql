-- +goose Up
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE shared.countries (
    code char(2) NOT NULL,
    name text NOT NULL,
    region text NOT NULL,
    PRIMARY KEY (code)
);

CREATE TABLE shared.industries (
    id uuid NOT NULL DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    code text NOT NULL,
    name text NOT NULL,
    parent_id uuid,
    PRIMARY KEY (id)
);

CREATE TABLE shared.companies (
    id uuid NOT NULL DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    primary_domain citext,
    display_name text NOT NULL,
    legal_name text,
    country_code char(2),
    industry_id uuid,
    employee_band text,
    status text NOT NULL,
    merged_into_id uuid,
    PRIMARY KEY (id),
    CONSTRAINT companies_ck1 CHECK (status IN ('active','merged','dissolved')),
    CONSTRAINT companies_ck2 CHECK ((status = 'merged') = (merged_into_id IS NOT NULL))
);

CREATE TABLE shared.company_identifiers (
    id uuid NOT NULL DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    company_id uuid NOT NULL,
    kind text NOT NULL,
    value_normalized text NOT NULL,
    provider_key text,
    verified_at timestamptz,
    PRIMARY KEY (id),
    CONSTRAINT company_identifiers_ck1 CHECK (kind IN ('domain','registry_number','provider_id','social_url'))
);

CREATE TABLE shared.company_source_facts (
    id uuid NOT NULL DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    company_id uuid NOT NULL,
    attribute text NOT NULL,
    value jsonb NOT NULL,
    source_key text NOT NULL,
    source_url text,
    licence_class text NOT NULL,
    confidence numeric(3,2) NOT NULL,
    observed_at timestamptz NOT NULL,
    expires_at timestamptz,
    PRIMARY KEY (id),
    CONSTRAINT company_source_facts_ck1 CHECK (licence_class IN ('public','licensed_shareable')),
    CONSTRAINT company_source_facts_ck2 CHECK (confidence BETWEEN 0 AND 1)
);

CREATE TABLE shared.company_merges (
    id uuid NOT NULL DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    source_company_id uuid NOT NULL,
    target_company_id uuid NOT NULL,
    reason text NOT NULL,
    decided_by text NOT NULL,
    PRIMARY KEY (id)
);

CREATE TABLE shared.contacts (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    company_id uuid,
    full_name text,
    first_name text,
    last_name text,
    job_title text,
    email citext,
    email_hash bytea,
    phone text,
    social_url text,
    country_code char(2),
    status text NOT NULL,
    erased_at timestamptz,
    created_by uuid,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT contacts_ck1 CHECK (status IN ('active','erased'))
);

CREATE TABLE shared.contact_sources (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    contact_id uuid NOT NULL,
    source_key text NOT NULL,
    source_ref text,
    licence_class text NOT NULL,
    acquired_at timestamptz NOT NULL,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT contact_sources_ck1 CHECK (licence_class IN ('public','licensed_tenant','licensed_shareable','tenant_provided','data_subject_provided'))
);

CREATE TABLE shared.contact_consents (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    contact_id uuid NOT NULL,
    purpose text NOT NULL,
    status text NOT NULL,
    basis text,
    jurisdiction text,
    evidence jsonb NOT NULL,
    captured_at timestamptz NOT NULL,
    withdrawn_at timestamptz,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT contact_consents_ck1 CHECK (status IN ('granted','withdrawn','not_required','unknown'))
);

CREATE TABLE shared.service_categories (
    id uuid NOT NULL DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    key text NOT NULL,
    name text NOT NULL,
    parent_id uuid,
    PRIMARY KEY (id)
);

CREATE TABLE shared.tenant_services (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    category_id uuid,
    name text NOT NULL,
    description text,
    status text NOT NULL,
    deleted_at timestamptz,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT tenant_services_ck1 CHECK (status IN ('active','retired'))
);

CREATE TABLE shared.fx_rates (
    id uuid NOT NULL DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    base_currency char(3) NOT NULL,
    quote_currency char(3) NOT NULL,
    rate numeric(18,8) NOT NULL,
    rate_date date NOT NULL,
    source_key text NOT NULL,
    PRIMARY KEY (id),
    CONSTRAINT fx_rates_ck1 CHECK (rate > 0)
);

CREATE TABLE integration.providers (
    id uuid NOT NULL DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    key text NOT NULL,
    kind text NOT NULL,
    name text NOT NULL,
    capabilities jsonb NOT NULL,
    tenant_connectable boolean NOT NULL,
    status text NOT NULL,
    PRIMARY KEY (id),
    CONSTRAINT providers_ck1 CHECK (kind IN ('ai','email','calendar','company_data','payment','esign','business_identification','fx')),
    CONSTRAINT providers_ck2 CHECK (status IN ('disabled','enabled','degraded'))
);

CREATE TABLE integration.platform_credentials (
    id uuid NOT NULL DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    provider_id uuid NOT NULL,
    label text NOT NULL,
    secret_ciphertext bytea NOT NULL,
    key_id text NOT NULL,
    rotated_at timestamptz,
    expires_at timestamptz,
    PRIMARY KEY (id)
);

CREATE TABLE integration.connections (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    provider_id uuid NOT NULL,
    display_name text NOT NULL,
    account_ref text,
    scopes text[] NOT NULL DEFAULT '{}',
    secret_ciphertext bytea,
    key_id text,
    token_expires_at timestamptz,
    status text NOT NULL,
    last_health_at timestamptz,
    last_error_code text,
    created_by uuid,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT connections_ck1 CHECK (status IN ('pending','connected','needs_reauth','error','disconnected'))
);

CREATE TABLE integration.webhook_endpoints (
    id uuid NOT NULL DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    tenant_id uuid,
    provider_id uuid NOT NULL,
    connection_id uuid,
    path_token text NOT NULL,
    secret_ciphertext bytea NOT NULL,
    key_id text NOT NULL,
    status text NOT NULL,
    PRIMARY KEY (id),
    CONSTRAINT webhook_endpoints_ck1 CHECK (status IN ('active','disabled'))
);

CREATE TABLE integration.webhook_deliveries (
    id uuid NOT NULL DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    tenant_id uuid,
    endpoint_id uuid NOT NULL,
    provider_event_id text,
    signature_verified boolean NOT NULL,
    payload_hash bytea NOT NULL,
    payload jsonb,
    status text NOT NULL,
    attempts int NOT NULL,
    last_error text,
    received_at timestamptz NOT NULL,
    PRIMARY KEY (id),
    CONSTRAINT webhook_deliveries_ck1 CHECK (status IN ('received','rejected','dispatched','failed'))
);

CREATE TABLE integration.provider_call_logs (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid,
    provider_id uuid NOT NULL,
    operation text NOT NULL,
    outcome text NOT NULL,
    status_code int,
    latency_ms int NOT NULL,
    correlation_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id),
    CONSTRAINT provider_call_logs_ck1 CHECK (outcome IN ('success','retryable_error','permanent_error','rate_limited','circuit_open'))
);

CREATE TABLE integration.ai_model_routes (
    id uuid NOT NULL DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    task_key text NOT NULL,
    provider_id uuid NOT NULL,
    model text NOT NULL,
    fallback_route_id uuid,
    max_input_tokens int NOT NULL,
    max_output_tokens int NOT NULL,
    status text NOT NULL,
    PRIMARY KEY (id),
    CONSTRAINT ai_model_routes_ck1 CHECK (status IN ('active','disabled'))
);

CREATE TABLE integration.prompt_templates (
    id uuid NOT NULL DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    key text NOT NULL,
    task_key text NOT NULL,
    action_class text NOT NULL,
    output_schema jsonb NOT NULL,
    status text NOT NULL,
    PRIMARY KEY (id),
    CONSTRAINT prompt_templates_ck1 CHECK (action_class IN ('informational','recommendation','human_approval_required','prohibited_without_authorization')),
    CONSTRAINT prompt_templates_ck2 CHECK (status IN ('active','retired'))
);

CREATE TABLE integration.prompt_versions (
    id uuid NOT NULL DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    template_id uuid NOT NULL,
    version int NOT NULL DEFAULT 1,
    system_text text NOT NULL,
    user_text text NOT NULL,
    status text NOT NULL,
    PRIMARY KEY (id),
    CONSTRAINT prompt_versions_ck1 CHECK (status IN ('draft','active','retired'))
);

CREATE TABLE integration.ai_requests (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    module_key text NOT NULL,
    task_key text NOT NULL,
    prompt_version_id uuid,
    provider_id uuid,
    model text,
    input_tokens int,
    output_tokens int,
    cost_estimate_minor bigint,
    credits_charged bigint,
    outcome text NOT NULL,
    validation_passed boolean,
    input_hash bytea,
    latency_ms int,
    correlation_id uuid,
    idempotency_key text,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT ai_requests_ck1 CHECK (outcome IN ('success','provider_error','validation_failed','quota_exceeded','policy_blocked','timeout'))
);

CREATE TABLE notification.templates (
    id uuid NOT NULL DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    key text NOT NULL,
    channel text NOT NULL,
    locale text NOT NULL,
    subject text,
    body text NOT NULL,
    version int NOT NULL DEFAULT 1,
    PRIMARY KEY (id),
    CONSTRAINT templates_ck1 CHECK (channel IN ('in_app','email'))
);

CREATE TABLE notification.preferences (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    user_id uuid NOT NULL,
    notification_key text NOT NULL,
    channel text NOT NULL,
    enabled boolean NOT NULL,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id)
);

CREATE TABLE notification.notifications (
    id uuid NOT NULL DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    tenant_id uuid,
    recipient_type text NOT NULL,
    recipient_id uuid NOT NULL,
    notification_key text NOT NULL,
    title text NOT NULL,
    body text,
    link text,
    read_at timestamptz,
    idempotency_key text NOT NULL,
    PRIMARY KEY (id),
    CONSTRAINT notifications_ck1 CHECK (recipient_type IN ('user','platform_admin'))
);

CREATE TABLE notification.delivery_attempts (
    id uuid NOT NULL DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    notification_id uuid NOT NULL,
    channel text NOT NULL,
    provider_key text,
    status text NOT NULL,
    attempt int NOT NULL,
    error_code text,
    PRIMARY KEY (id),
    CONSTRAINT delivery_attempts_ck1 CHECK (status IN ('queued','sent','delivered','failed','bounced'))
);

CREATE TABLE jobs.jobs (
    id uuid NOT NULL DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    tenant_id uuid,
    queue text NOT NULL,
    kind text NOT NULL,
    payload jsonb NOT NULL,
    payload_version int NOT NULL,
    status text NOT NULL,
    priority int NOT NULL,
    run_at timestamptz NOT NULL,
    attempt int NOT NULL,
    max_attempts int NOT NULL,
    idempotency_key text,
    locked_by text,
    lock_expires_at timestamptz,
    progress jsonb,
    cancel_requested_at timestamptz,
    correlation_id uuid,
    last_error text,
    finished_at timestamptz,
    PRIMARY KEY (id),
    CONSTRAINT jobs_ck1 CHECK (status IN ('queued','running','retry_scheduled','succeeded','failed','cancelled','dead'))
);

CREATE TABLE jobs.job_attempts (
    id uuid NOT NULL DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    job_id uuid NOT NULL,
    attempt int NOT NULL,
    worker_id text NOT NULL,
    started_at timestamptz NOT NULL,
    finished_at timestamptz,
    outcome text,
    error text,
    PRIMARY KEY (id)
);

CREATE TABLE jobs.job_schedules (
    id uuid NOT NULL DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    tenant_id uuid,
    key text NOT NULL,
    cron text NOT NULL,
    timezone text NOT NULL,
    kind text NOT NULL,
    payload jsonb NOT NULL,
    next_run_at timestamptz NOT NULL,
    last_run_at timestamptz,
    enabled boolean NOT NULL,
    PRIMARY KEY (id)
);

CREATE TABLE jobs.dead_letters (
    id uuid NOT NULL DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    tenant_id uuid,
    origin text NOT NULL,
    origin_id uuid NOT NULL,
    kind text NOT NULL,
    payload jsonb NOT NULL,
    error text NOT NULL,
    resolution text,
    resolved_by uuid,
    resolved_at timestamptz,
    PRIMARY KEY (id),
    CONSTRAINT dead_letters_ck1 CHECK (origin IN ('job','event_delivery')),
    CONSTRAINT dead_letters_ck2 CHECK (resolution IS NULL OR resolution IN ('requeued','discarded'))
);

CREATE TABLE jobs.outbox_events (
    event_id uuid NOT NULL,
    event_type text NOT NULL,
    schema_version int NOT NULL,
    tenant_id uuid,
    source_module text NOT NULL,
    aggregate_type text NOT NULL,
    aggregate_id uuid NOT NULL,
    occurred_at timestamptz NOT NULL DEFAULT now(),
    correlation_id uuid NOT NULL,
    causation_id uuid,
    payload jsonb NOT NULL,
    published_at timestamptz,
    sequence bigint GENERATED ALWAYS AS IDENTITY,
    PRIMARY KEY (event_id, occurred_at)
) PARTITION BY RANGE (occurred_at);

CREATE TABLE jobs.event_deliveries (
    id uuid NOT NULL DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    tenant_id uuid,
    event_id uuid NOT NULL,
    consumer text NOT NULL,
    status text NOT NULL,
    attempts int NOT NULL,
    next_attempt_at timestamptz,
    last_error text,
    processed_at timestamptz,
    PRIMARY KEY (id),
    CONSTRAINT event_deliveries_ck1 CHECK (status IN ('pending','processing','processed','failed','dead'))
);

CREATE TABLE files.objects (
    id uuid NOT NULL DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    tenant_id uuid,
    bucket text NOT NULL,
    object_key text NOT NULL,
    sha256 bytea,
    size_bytes bigint,
    content_type text NOT NULL,
    original_name text,
    status text NOT NULL,
    classification text NOT NULL,
    owner_module text NOT NULL,
    retention_until timestamptz,
    uploaded_by uuid,
    deleted_at timestamptz,
    PRIMARY KEY (id),
    CONSTRAINT objects_ck1 CHECK (status IN ('pending_upload','scanning','clean','quarantined','deleted')),
    CONSTRAINT objects_ck2 CHECK (classification IN ('public_asset','tenant_private','sensitive'))
);

CREATE TABLE files.object_references (
    id uuid NOT NULL DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    object_id uuid NOT NULL,
    tenant_id uuid,
    owner_module text NOT NULL,
    owner_type text NOT NULL,
    owner_id uuid NOT NULL,
    PRIMARY KEY (id)
);

CREATE TABLE files.access_policies (
    id uuid NOT NULL DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    key text NOT NULL,
    requires_session boolean NOT NULL,
    allowed_permissions text[] NOT NULL DEFAULT '{}',
    max_link_ttl_seconds int NOT NULL,
    PRIMARY KEY (id)
);

CREATE TABLE files.access_grants (
    id uuid NOT NULL DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    tenant_id uuid,
    object_id uuid NOT NULL,
    policy_id uuid NOT NULL,
    token_hash bytea NOT NULL,
    expires_at timestamptz NOT NULL,
    max_downloads int,
    download_count int NOT NULL,
    PRIMARY KEY (id),
    CONSTRAINT access_grants_ck1 CHECK (download_count >= 0)
);

ALTER TABLE shared.industries ADD CONSTRAINT industries_parent_id_fk FOREIGN KEY (parent_id) REFERENCES shared.industries (id) ON DELETE RESTRICT;
ALTER TABLE shared.companies ADD CONSTRAINT companies_industry_id_fk FOREIGN KEY (industry_id) REFERENCES shared.industries (id) ON DELETE RESTRICT;
ALTER TABLE shared.companies ADD CONSTRAINT companies_merged_into_id_fk FOREIGN KEY (merged_into_id) REFERENCES shared.companies (id) ON DELETE RESTRICT;
ALTER TABLE shared.company_identifiers ADD CONSTRAINT company_identifiers_company_id_fk FOREIGN KEY (company_id) REFERENCES shared.companies (id) ON DELETE CASCADE;
ALTER TABLE shared.company_source_facts ADD CONSTRAINT company_source_facts_company_id_fk FOREIGN KEY (company_id) REFERENCES shared.companies (id) ON DELETE CASCADE;
ALTER TABLE shared.company_merges ADD CONSTRAINT company_merges_source_company_id_fk FOREIGN KEY (source_company_id) REFERENCES shared.companies (id) ON DELETE RESTRICT;
ALTER TABLE shared.company_merges ADD CONSTRAINT company_merges_target_company_id_fk FOREIGN KEY (target_company_id) REFERENCES shared.companies (id) ON DELETE RESTRICT;
ALTER TABLE shared.contacts ADD CONSTRAINT contacts_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE shared.contacts ADD CONSTRAINT contacts_company_id_fk FOREIGN KEY (company_id) REFERENCES shared.companies (id) ON DELETE RESTRICT;
ALTER TABLE shared.contact_sources ADD CONSTRAINT contact_sources_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE shared.contact_sources ADD CONSTRAINT contact_sources_contact_id_fk FOREIGN KEY (tenant_id, contact_id) REFERENCES shared.contacts (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE shared.contact_consents ADD CONSTRAINT contact_consents_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE shared.contact_consents ADD CONSTRAINT contact_consents_contact_id_fk FOREIGN KEY (tenant_id, contact_id) REFERENCES shared.contacts (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE shared.service_categories ADD CONSTRAINT service_categories_parent_id_fk FOREIGN KEY (parent_id) REFERENCES shared.service_categories (id) ON DELETE RESTRICT;
ALTER TABLE shared.tenant_services ADD CONSTRAINT tenant_services_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE shared.tenant_services ADD CONSTRAINT tenant_services_category_id_fk FOREIGN KEY (category_id) REFERENCES shared.service_categories (id) ON DELETE RESTRICT;
ALTER TABLE integration.platform_credentials ADD CONSTRAINT platform_credentials_provider_id_fk FOREIGN KEY (provider_id) REFERENCES integration.providers (id) ON DELETE RESTRICT;
ALTER TABLE integration.connections ADD CONSTRAINT connections_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE integration.connections ADD CONSTRAINT connections_provider_id_fk FOREIGN KEY (provider_id) REFERENCES integration.providers (id) ON DELETE RESTRICT;
ALTER TABLE integration.webhook_endpoints ADD CONSTRAINT webhook_endpoints_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE CASCADE;
ALTER TABLE integration.webhook_endpoints ADD CONSTRAINT webhook_endpoints_provider_id_fk FOREIGN KEY (provider_id) REFERENCES integration.providers (id) ON DELETE RESTRICT;
ALTER TABLE integration.webhook_endpoints ADD CONSTRAINT webhook_endpoints_connection_id_fk FOREIGN KEY (connection_id) REFERENCES integration.connections (id) ON DELETE CASCADE;
ALTER TABLE integration.webhook_deliveries ADD CONSTRAINT webhook_deliveries_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE SET NULL;
ALTER TABLE integration.webhook_deliveries ADD CONSTRAINT webhook_deliveries_endpoint_id_fk FOREIGN KEY (endpoint_id) REFERENCES integration.webhook_endpoints (id) ON DELETE CASCADE;
ALTER TABLE integration.provider_call_logs ADD CONSTRAINT provider_call_logs_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE SET NULL;
ALTER TABLE integration.provider_call_logs ADD CONSTRAINT provider_call_logs_provider_id_fk FOREIGN KEY (provider_id) REFERENCES integration.providers (id) ON DELETE RESTRICT;
ALTER TABLE integration.ai_model_routes ADD CONSTRAINT ai_model_routes_provider_id_fk FOREIGN KEY (provider_id) REFERENCES integration.providers (id) ON DELETE RESTRICT;
ALTER TABLE integration.ai_model_routes ADD CONSTRAINT ai_model_routes_fallback_route_id_fk FOREIGN KEY (fallback_route_id) REFERENCES integration.ai_model_routes (id) ON DELETE SET NULL;
ALTER TABLE integration.prompt_versions ADD CONSTRAINT prompt_versions_template_id_fk FOREIGN KEY (template_id) REFERENCES integration.prompt_templates (id) ON DELETE RESTRICT;
ALTER TABLE integration.ai_requests ADD CONSTRAINT ai_requests_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE integration.ai_requests ADD CONSTRAINT ai_requests_prompt_version_id_fk FOREIGN KEY (prompt_version_id) REFERENCES integration.prompt_versions (id) ON DELETE RESTRICT;
ALTER TABLE integration.ai_requests ADD CONSTRAINT ai_requests_provider_id_fk FOREIGN KEY (provider_id) REFERENCES integration.providers (id) ON DELETE RESTRICT;
ALTER TABLE notification.preferences ADD CONSTRAINT preferences_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE notification.notifications ADD CONSTRAINT notifications_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE CASCADE;
ALTER TABLE notification.delivery_attempts ADD CONSTRAINT delivery_attempts_notification_id_fk FOREIGN KEY (notification_id) REFERENCES notification.notifications (id) ON DELETE CASCADE;
ALTER TABLE jobs.jobs ADD CONSTRAINT jobs_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE CASCADE;
ALTER TABLE jobs.job_attempts ADD CONSTRAINT job_attempts_job_id_fk FOREIGN KEY (job_id) REFERENCES jobs.jobs (id) ON DELETE CASCADE;
ALTER TABLE jobs.job_schedules ADD CONSTRAINT job_schedules_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE CASCADE;
ALTER TABLE jobs.dead_letters ADD CONSTRAINT dead_letters_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE SET NULL;
ALTER TABLE files.objects ADD CONSTRAINT objects_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE files.object_references ADD CONSTRAINT object_references_object_id_fk FOREIGN KEY (object_id) REFERENCES files.objects (id) ON DELETE CASCADE;
ALTER TABLE files.object_references ADD CONSTRAINT object_references_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE CASCADE;
ALTER TABLE files.access_grants ADD CONSTRAINT access_grants_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE CASCADE;
ALTER TABLE files.access_grants ADD CONSTRAINT access_grants_object_id_fk FOREIGN KEY (object_id) REFERENCES files.objects (id) ON DELETE CASCADE;
ALTER TABLE files.access_grants ADD CONSTRAINT access_grants_policy_id_fk FOREIGN KEY (policy_id) REFERENCES files.access_policies (id) ON DELETE RESTRICT;

CREATE UNIQUE INDEX industries_uq1 ON shared.industries (code);
CREATE INDEX industries_parent_id_idx ON shared.industries (parent_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON shared.industries FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX companies_uq1 ON shared.companies (primary_domain) WHERE status = 'active';
CREATE INDEX companies_ix1 ON shared.companies (lower(display_name));
CREATE INDEX companies_industry_id_idx ON shared.companies (industry_id);
CREATE INDEX companies_merged_into_id_idx ON shared.companies (merged_into_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON shared.companies FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX company_identifiers_uq1 ON shared.company_identifiers (kind, value_normalized, provider_key) NULLS NOT DISTINCT;
CREATE INDEX company_identifiers_company_id_idx ON shared.company_identifiers (company_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON shared.company_identifiers FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE INDEX company_source_facts_ix1 ON shared.company_source_facts (company_id, attribute, observed_at DESC);
CREATE INDEX company_source_facts_company_id_idx ON shared.company_source_facts (company_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON shared.company_source_facts FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE INDEX company_merges_source_company_id_idx ON shared.company_merges (source_company_id);
CREATE INDEX company_merges_target_company_id_idx ON shared.company_merges (target_company_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON shared.company_merges FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX contacts_uq1 ON shared.contacts (tenant_id, email) WHERE email IS NOT NULL;
CREATE INDEX contacts_ix1 ON shared.contacts (tenant_id, company_id);
CREATE INDEX contacts_ix2 ON shared.contacts (tenant_id, email_hash);
CREATE INDEX contacts_company_id_idx ON shared.contacts (tenant_id, company_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON shared.contacts FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE INDEX contact_sources_contact_id_idx ON shared.contact_sources (tenant_id, contact_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON shared.contact_sources FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE INDEX contact_consents_ix1 ON shared.contact_consents (tenant_id, contact_id, purpose);
CREATE INDEX contact_consents_contact_id_idx ON shared.contact_consents (tenant_id, contact_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON shared.contact_consents FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX service_categories_uq1 ON shared.service_categories (key);
CREATE INDEX service_categories_parent_id_idx ON shared.service_categories (parent_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON shared.service_categories FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX tenant_services_uq1 ON shared.tenant_services (tenant_id, name) WHERE deleted_at IS NULL;
CREATE INDEX tenant_services_category_id_idx ON shared.tenant_services (tenant_id, category_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON shared.tenant_services FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX fx_rates_uq1 ON shared.fx_rates (base_currency, quote_currency, rate_date, source_key);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON shared.fx_rates FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX providers_uq1 ON integration.providers (key);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON integration.providers FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX platform_credentials_uq1 ON integration.platform_credentials (provider_id, label);
CREATE INDEX platform_credentials_provider_id_idx ON integration.platform_credentials (provider_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON integration.platform_credentials FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE INDEX connections_provider_id_idx ON integration.connections (tenant_id, provider_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON integration.connections FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX webhook_endpoints_uq1 ON integration.webhook_endpoints (path_token);
CREATE INDEX webhook_endpoints_provider_id_idx ON integration.webhook_endpoints (provider_id);
CREATE INDEX webhook_endpoints_connection_id_idx ON integration.webhook_endpoints (connection_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON integration.webhook_endpoints FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX webhook_deliveries_uq1 ON integration.webhook_deliveries (endpoint_id, provider_event_id);
CREATE INDEX webhook_deliveries_endpoint_id_idx ON integration.webhook_deliveries (endpoint_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON integration.webhook_deliveries FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE INDEX provider_call_logs_ix1 ON integration.provider_call_logs (provider_id, created_at DESC);
CREATE INDEX provider_call_logs_provider_id_idx ON integration.provider_call_logs (provider_id);
CREATE UNIQUE INDEX ai_model_routes_uq1 ON integration.ai_model_routes (task_key, provider_id, model);
CREATE INDEX ai_model_routes_provider_id_idx ON integration.ai_model_routes (provider_id);
CREATE INDEX ai_model_routes_fallback_route_id_idx ON integration.ai_model_routes (fallback_route_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON integration.ai_model_routes FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX prompt_templates_uq1 ON integration.prompt_templates (key);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON integration.prompt_templates FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX prompt_versions_uq1 ON integration.prompt_versions (template_id, version);
CREATE INDEX prompt_versions_template_id_idx ON integration.prompt_versions (template_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON integration.prompt_versions FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX ai_requests_uq1 ON integration.ai_requests (tenant_id, idempotency_key);
CREATE INDEX ai_requests_ix1 ON integration.ai_requests (tenant_id, module_key, created_at DESC);
CREATE INDEX ai_requests_prompt_version_id_idx ON integration.ai_requests (tenant_id, prompt_version_id);
CREATE INDEX ai_requests_provider_id_idx ON integration.ai_requests (tenant_id, provider_id);
CREATE UNIQUE INDEX templates_uq1 ON notification.templates (key, channel, locale, version);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON notification.templates FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX preferences_uq1 ON notification.preferences (tenant_id, user_id, notification_key, channel);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON notification.preferences FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX notifications_uq1 ON notification.notifications (recipient_id, idempotency_key);
CREATE INDEX notifications_ix1 ON notification.notifications (recipient_id, read_at, created_at DESC);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON notification.notifications FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE INDEX delivery_attempts_notification_id_idx ON notification.delivery_attempts (notification_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON notification.delivery_attempts FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX jobs_uq1 ON jobs.jobs (tenant_id, kind, idempotency_key) NULLS NOT DISTINCT;
CREATE INDEX jobs_ix1 ON jobs.jobs (queue, priority, run_at) WHERE status IN ('queued','retry_scheduled');
CREATE INDEX jobs_ix2 ON jobs.jobs (tenant_id, status);
CREATE INDEX jobs_ix3 ON jobs.jobs (lock_expires_at) WHERE status = 'running';
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON jobs.jobs FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX job_attempts_uq1 ON jobs.job_attempts (job_id, attempt);
CREATE INDEX job_attempts_job_id_idx ON jobs.job_attempts (job_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON jobs.job_attempts FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX job_schedules_uq1 ON jobs.job_schedules (tenant_id, key) NULLS NOT DISTINCT;
CREATE INDEX job_schedules_ix1 ON jobs.job_schedules (next_run_at) WHERE enabled;
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON jobs.job_schedules FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON jobs.dead_letters FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE INDEX outbox_events_ix1 ON jobs.outbox_events (sequence) WHERE published_at IS NULL;
CREATE INDEX outbox_events_ix2 ON jobs.outbox_events (aggregate_type, aggregate_id, sequence);
CREATE UNIQUE INDEX event_deliveries_uq1 ON jobs.event_deliveries (event_id, consumer);
CREATE INDEX event_deliveries_ix1 ON jobs.event_deliveries (consumer, status, next_attempt_at);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON jobs.event_deliveries FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX objects_uq1 ON files.objects (bucket, object_key);
CREATE INDEX objects_ix1 ON files.objects (tenant_id, owner_module);
CREATE INDEX objects_ix2 ON files.objects (retention_until) WHERE deleted_at IS NULL;
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON files.objects FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX object_references_uq1 ON files.object_references (object_id, owner_module, owner_type, owner_id);
CREATE INDEX object_references_object_id_idx ON files.object_references (object_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON files.object_references FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX access_policies_uq1 ON files.access_policies (key);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON files.access_policies FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX access_grants_uq1 ON files.access_grants (token_hash);
CREATE INDEX access_grants_object_id_idx ON files.access_grants (object_id);
CREATE INDEX access_grants_policy_id_idx ON files.access_grants (policy_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON files.access_grants FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();

-- +goose Down
DROP TABLE IF EXISTS files.access_grants CASCADE;
DROP TABLE IF EXISTS files.access_policies CASCADE;
DROP TABLE IF EXISTS files.object_references CASCADE;
DROP TABLE IF EXISTS files.objects CASCADE;
DROP TABLE IF EXISTS jobs.event_deliveries CASCADE;
DROP TABLE IF EXISTS jobs.outbox_events CASCADE;
DROP TABLE IF EXISTS jobs.dead_letters CASCADE;
DROP TABLE IF EXISTS jobs.job_schedules CASCADE;
DROP TABLE IF EXISTS jobs.job_attempts CASCADE;
DROP TABLE IF EXISTS jobs.jobs CASCADE;
DROP TABLE IF EXISTS notification.delivery_attempts CASCADE;
DROP TABLE IF EXISTS notification.notifications CASCADE;
DROP TABLE IF EXISTS notification.preferences CASCADE;
DROP TABLE IF EXISTS notification.templates CASCADE;
DROP TABLE IF EXISTS integration.ai_requests CASCADE;
DROP TABLE IF EXISTS integration.prompt_versions CASCADE;
DROP TABLE IF EXISTS integration.prompt_templates CASCADE;
DROP TABLE IF EXISTS integration.ai_model_routes CASCADE;
DROP TABLE IF EXISTS integration.provider_call_logs CASCADE;
DROP TABLE IF EXISTS integration.webhook_deliveries CASCADE;
DROP TABLE IF EXISTS integration.webhook_endpoints CASCADE;
DROP TABLE IF EXISTS integration.connections CASCADE;
DROP TABLE IF EXISTS integration.platform_credentials CASCADE;
DROP TABLE IF EXISTS integration.providers CASCADE;
DROP TABLE IF EXISTS shared.fx_rates CASCADE;
DROP TABLE IF EXISTS shared.tenant_services CASCADE;
DROP TABLE IF EXISTS shared.service_categories CASCADE;
DROP TABLE IF EXISTS shared.contact_consents CASCADE;
DROP TABLE IF EXISTS shared.contact_sources CASCADE;
DROP TABLE IF EXISTS shared.contacts CASCADE;
DROP TABLE IF EXISTS shared.company_merges CASCADE;
DROP TABLE IF EXISTS shared.company_source_facts CASCADE;
DROP TABLE IF EXISTS shared.company_identifiers CASCADE;
DROP TABLE IF EXISTS shared.companies CASCADE;
DROP TABLE IF EXISTS shared.industries CASCADE;
DROP TABLE IF EXISTS shared.countries CASCADE;
DROP EXTENSION IF EXISTS pg_trgm;
