-- +goose Up
-- Phase 4: CRM, Proposals and IT Assessment, plus the Customer Expansion contract record (D-03).
CREATE TABLE it_assessment.templates (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    name text NOT NULL,
    tier text NOT NULL,
    check_keys text[] NOT NULL DEFAULT '{}',
    scoring_version text NOT NULL,
    deleted_at timestamptz,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT templates_ck1 CHECK (tier IN ('passive','exposure'))
);

CREATE TABLE it_assessment.landing_pages (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    template_id uuid NOT NULL,
    slug citext NOT NULL,
    title text NOT NULL,
    branding jsonb NOT NULL,
    consent_text_version text NOT NULL,
    status text NOT NULL,
    deleted_at timestamptz,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT landing_pages_ck1 CHECK (status IN ('draft','published','paused'))
);

CREATE TABLE it_assessment.domain_verifications (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    domain citext NOT NULL,
    method text NOT NULL,
    token_hash bytea NOT NULL,
    status text NOT NULL,
    verified_at timestamptz,
    expires_at timestamptz NOT NULL,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT domain_verifications_ck1 CHECK (method IN ('dns_txt','http_file')),
    CONSTRAINT domain_verifications_ck2 CHECK (status IN ('pending','verified','failed','expired'))
);

CREATE TABLE it_assessment.authorizations (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    domain citext NOT NULL,
    tier text NOT NULL,
    method text NOT NULL,
    authorizer_name text NOT NULL,
    authorizer_email citext NOT NULL,
    authorizer_role text,
    terms_version text NOT NULL,
    domain_verification_id uuid,
    ip_hash bytea,
    status text NOT NULL,
    granted_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT authorizations_ck1 CHECK (tier IN ('passive','exposure')),
    CONSTRAINT authorizations_ck2 CHECK (method IN ('attestation','verified_domain_control')),
    CONSTRAINT authorizations_ck3 CHECK (status IN ('active','expired','revoked')),
    CONSTRAINT authorizations_ck4 CHECK (tier <> 'exposure' OR domain_verification_id IS NOT NULL)
);

CREATE TABLE it_assessment.lead_captures (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    landing_page_id uuid NOT NULL,
    contact_id uuid,
    domain citext NOT NULL,
    consent jsonb NOT NULL,
    source_ip_hash bytea,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id)
);

CREATE TABLE it_assessment.assessments (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    authorization_id uuid NOT NULL,
    template_id uuid NOT NULL,
    lead_capture_id uuid,
    domain citext NOT NULL,
    company_id uuid,
    crm_company_id uuid,
    origin text NOT NULL,
    status text NOT NULL,
    score int,
    risk_level text,
    job_id uuid,
    started_at timestamptz,
    completed_at timestamptz,
    created_by uuid,
    deleted_at timestamptz,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT assessments_ck1 CHECK (origin IN ('internal','landing_page')),
    CONSTRAINT assessments_ck2 CHECK (status IN ('queued','running','completed','partial','failed','cancelled')),
    CONSTRAINT assessments_ck3 CHECK (risk_level IS NULL OR risk_level IN ('low','moderate','elevated','high')),
    CONSTRAINT assessments_ck4 CHECK (score IS NULL OR score BETWEEN 0 AND 100)
);

CREATE TABLE it_assessment.checks (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    assessment_id uuid NOT NULL,
    check_key text NOT NULL,
    status text NOT NULL,
    target text NOT NULL,
    raw_result jsonb,
    duration_ms int,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT checks_ck1 CHECK (status IN ('pending','running','passed','failed','not_detected','inconclusive','skipped','blocked_by_policy'))
);

CREATE TABLE it_assessment.findings (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    assessment_id uuid NOT NULL,
    check_id uuid NOT NULL,
    severity text NOT NULL,
    classification text NOT NULL,
    title text NOT NULL,
    description text NOT NULL,
    recommendation text NOT NULL,
    evidence jsonb,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT findings_ck1 CHECK (severity IN ('info','low','medium','high','critical')),
    CONSTRAINT findings_ck2 CHECK (classification IN ('informational','potential_issue','confirmed_misconfiguration'))
);

CREATE TABLE it_assessment.reports (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    assessment_id uuid NOT NULL,
    version int NOT NULL DEFAULT 1,
    file_id uuid,
    status text NOT NULL,
    deleted_at timestamptz,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT reports_ck1 CHECK (status IN ('queued','generating','ready','failed'))
);

CREATE TABLE it_assessment.consultation_bookings (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    assessment_id uuid NOT NULL,
    contact_id uuid,
    status text NOT NULL,
    starts_at timestamptz,
    ends_at timestamptz,
    provider_event_id text,
    crm_meeting_id uuid,
    idempotency_key text NOT NULL,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT consultation_bookings_ck1 CHECK (status IN ('requested','confirmed','cancelled','failed'))
);

CREATE TABLE it_assessment.egress_decisions (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    assessment_id uuid NOT NULL,
    check_key text NOT NULL,
    requested_host text NOT NULL,
    resolved_ip inet,
    decision text NOT NULL,
    reason text,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT egress_decisions_ck1 CHECK (decision IN ('allowed','denied'))
);

CREATE TABLE crm.companies (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    company_id uuid,
    name text NOT NULL,
    domain citext,
    lifecycle text NOT NULL,
    owner_user_id uuid,
    source_module text,
    source_ref uuid,
    created_by uuid,
    deleted_at timestamptz,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT companies_ck1 CHECK (lifecycle IN ('prospect','customer','former_customer','partner','other'))
);

CREATE TABLE crm.contacts (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    contact_id uuid NOT NULL,
    crm_company_id uuid,
    is_primary boolean NOT NULL,
    owner_user_id uuid,
    deleted_at timestamptz,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id)
);

CREATE TABLE crm.prospect_conversions (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    prospect_id uuid NOT NULL,
    crm_company_id uuid NOT NULL,
    deal_id uuid,
    converted_by uuid,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id)
);

CREATE TABLE crm.pipelines (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    name text NOT NULL,
    is_default boolean NOT NULL DEFAULT false,
    status text NOT NULL,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT pipelines_ck1 CHECK (status IN ('active','archived'))
);

CREATE TABLE crm.pipeline_stages (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    pipeline_id uuid NOT NULL,
    key text NOT NULL,
    name text NOT NULL,
    position int NOT NULL,
    kind text NOT NULL,
    default_probability int NOT NULL,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT pipeline_stages_ck1 CHECK (kind IN ('open','won','lost')),
    CONSTRAINT pipeline_stages_ck2 CHECK (default_probability BETWEEN 0 AND 100)
);

CREATE TABLE crm.deals (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    pipeline_id uuid NOT NULL,
    stage_id uuid NOT NULL,
    crm_company_id uuid NOT NULL,
    primary_contact_id uuid,
    title text NOT NULL,
    owner_user_id uuid,
    currency char(3) NOT NULL,
    contract_value_minor bigint,
    expected_mrr_minor bigint,
    probability int NOT NULL,
    expected_close_date date,
    status text NOT NULL,
    closed_at timestamptz,
    loss_reason text,
    source_module text,
    source_ref uuid,
    row_version int NOT NULL,
    created_by uuid,
    deleted_at timestamptz,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT deals_ck1 CHECK (probability BETWEEN 0 AND 100),
    CONSTRAINT deals_ck2 CHECK (status IN ('open','won','lost')),
    CONSTRAINT deals_ck3 CHECK (contract_value_minor IS NULL OR contract_value_minor >= 0),
    CONSTRAINT deals_ck4 CHECK (expected_mrr_minor IS NULL OR expected_mrr_minor >= 0),
    CONSTRAINT deals_ck5 CHECK ((status = 'open') = (closed_at IS NULL))
);

CREATE TABLE crm.deal_stage_history (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    deal_id uuid NOT NULL,
    from_stage_id uuid,
    to_stage_id uuid NOT NULL,
    changed_by uuid,
    reason text,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id)
);

CREATE TABLE crm.sales_reps (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    user_id uuid NOT NULL,
    quota_minor bigint,
    quota_currency char(3),
    territory text,
    active boolean NOT NULL,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id)
);

CREATE TABLE crm.tasks (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    title text NOT NULL,
    description text,
    assignee_user_id uuid,
    crm_company_id uuid,
    deal_id uuid,
    due_at timestamptz,
    status text NOT NULL,
    completed_at timestamptz,
    created_by uuid,
    deleted_at timestamptz,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT tasks_ck1 CHECK (status IN ('open','done','cancelled'))
);

CREATE TABLE crm.reminders (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    task_id uuid,
    user_id uuid NOT NULL,
    remind_at timestamptz NOT NULL,
    fired_at timestamptz,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id)
);

CREATE TABLE crm.notes (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    subject_type text NOT NULL,
    subject_id uuid NOT NULL,
    body text NOT NULL,
    created_by uuid,
    deleted_at timestamptz,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT notes_ck1 CHECK (subject_type IN ('company','contact','deal'))
);

CREATE TABLE crm.attachments (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    subject_type text NOT NULL,
    subject_id uuid NOT NULL,
    file_id uuid NOT NULL,
    created_by uuid,
    deleted_at timestamptz,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT attachments_ck1 CHECK (subject_type IN ('company','contact','deal','note'))
);

CREATE TABLE crm.meetings (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    crm_company_id uuid,
    deal_id uuid,
    title text NOT NULL,
    starts_at timestamptz NOT NULL,
    ends_at timestamptz NOT NULL,
    status text NOT NULL,
    organizer_user_id uuid,
    provider_event_id text,
    source_module text,
    source_ref uuid,
    outcome text,
    idempotency_key text,
    deleted_at timestamptz,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT meetings_ck1 CHECK (status IN ('scheduled','completed','cancelled','no_show')),
    CONSTRAINT meetings_ck2 CHECK (ends_at > starts_at)
);

CREATE TABLE crm.meeting_attendees (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    meeting_id uuid NOT NULL,
    contact_id uuid,
    user_id uuid,
    response text,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT meeting_attendees_ck1 CHECK (contact_id IS NOT NULL OR user_id IS NOT NULL)
);

CREATE TABLE crm.activities (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    activity_type text NOT NULL,
    crm_company_id uuid,
    contact_id uuid,
    deal_id uuid,
    actor_user_id uuid,
    summary text NOT NULL,
    detail jsonb,
    occurred_at timestamptz NOT NULL DEFAULT now(),
    source_module text,
    source_event_id uuid,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT activities_ck1 CHECK (activity_type IN ('email_sent','email_replied','call','meeting','note','task','stage_change','assessment','proposal','chat','visit','system'))
);

CREATE TABLE crm.custom_field_definitions (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    entity text NOT NULL,
    key text NOT NULL,
    label text NOT NULL,
    field_type text NOT NULL,
    options jsonb,
    archived boolean NOT NULL,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT custom_field_definitions_ck1 CHECK (entity IN ('company','contact','deal')),
    CONSTRAINT custom_field_definitions_ck2 CHECK (field_type IN ('text','number','date','boolean','select','multi_select'))
);

CREATE TABLE crm.custom_field_values (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    definition_id uuid NOT NULL,
    subject_id uuid NOT NULL,
    value jsonb NOT NULL,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id)
);

CREATE TABLE crm.ai_recommendations (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deal_id uuid,
    crm_company_id uuid,
    kind text NOT NULL,
    content jsonb NOT NULL,
    status text NOT NULL,
    ai_request_id uuid,
    decided_by uuid,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT ai_recommendations_ck1 CHECK (status IN ('open','accepted','dismissed','expired'))
);

CREATE TABLE proposals.service_catalog_items (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    tenant_service_id uuid,
    name text NOT NULL,
    description text,
    pricing_model text NOT NULL,
    currency char(3) NOT NULL,
    unit_price_minor bigint NOT NULL,
    setup_fee_minor bigint NOT NULL,
    status text NOT NULL,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT service_catalog_items_ck1 CHECK (pricing_model IN ('per_user','per_device','fixed')),
    CONSTRAINT service_catalog_items_ck2 CHECK (unit_price_minor >= 0 AND setup_fee_minor >= 0),
    CONSTRAINT service_catalog_items_ck3 CHECK (status IN ('active','retired'))
);

CREATE TABLE proposals.sla_templates (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    name text NOT NULL,
    body text NOT NULL,
    version int NOT NULL DEFAULT 1,
    deleted_at timestamptz,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id)
);

CREATE TABLE proposals.terms_templates (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    name text NOT NULL,
    body text NOT NULL,
    version int NOT NULL DEFAULT 1,
    deleted_at timestamptz,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id)
);

CREATE TABLE proposals.proposal_templates (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    name text NOT NULL,
    sections jsonb NOT NULL,
    branding jsonb NOT NULL,
    default_sla_id uuid,
    default_terms_id uuid,
    deleted_at timestamptz,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id)
);

CREATE TABLE proposals.proposals (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    number text NOT NULL,
    title text NOT NULL,
    crm_company_id uuid NOT NULL,
    deal_id uuid,
    template_id uuid,
    current_version_id uuid,
    status text NOT NULL,
    owner_user_id uuid,
    created_by uuid,
    deleted_at timestamptz,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT proposals_ck1 CHECK (status IN ('draft','in_review','issued','viewed','signed','declined','expired','withdrawn'))
);

CREATE TABLE proposals.proposal_versions (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    proposal_id uuid NOT NULL,
    version int NOT NULL DEFAULT 1,
    status text NOT NULL,
    content jsonb NOT NULL,
    commercial_snapshot jsonb,
    snapshot_hash bytea,
    currency char(3) NOT NULL,
    setup_total_minor bigint NOT NULL,
    mrr_total_minor bigint NOT NULL,
    term_months int NOT NULL,
    tcv_minor bigint NOT NULL,
    valid_until date,
    pdf_file_id uuid,
    issued_at timestamptz,
    ai_request_id uuid,
    created_by uuid,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT proposal_versions_ck1 CHECK (status IN ('draft','pending_approval','approved','issued','superseded','expired')),
    CONSTRAINT proposal_versions_ck2 CHECK (term_months > 0),
    CONSTRAINT proposal_versions_ck3 CHECK (tcv_minor = setup_total_minor + mrr_total_minor * term_months),
    CONSTRAINT proposal_versions_ck4 CHECK (status NOT IN ('issued','superseded','expired') OR (commercial_snapshot IS NOT NULL AND snapshot_hash IS NOT NULL))
);

CREATE TABLE proposals.proposal_line_items (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    version_id uuid NOT NULL,
    catalog_item_id uuid,
    position int NOT NULL,
    name text NOT NULL,
    pricing_model text NOT NULL,
    quantity int NOT NULL,
    unit_price_minor bigint NOT NULL,
    setup_fee_minor bigint NOT NULL,
    discount_bps int NOT NULL,
    recurring_total_minor bigint NOT NULL,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT proposal_line_items_ck1 CHECK (pricing_model IN ('per_user','per_device','fixed')),
    CONSTRAINT proposal_line_items_ck2 CHECK (quantity > 0),
    CONSTRAINT proposal_line_items_ck3 CHECK (discount_bps BETWEEN 0 AND 10000)
);

CREATE TABLE proposals.approvals (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    version_id uuid NOT NULL,
    status text NOT NULL,
    content_hash bytea NOT NULL,
    requested_by uuid NOT NULL,
    decided_by uuid,
    decided_at timestamptz,
    reason text,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT approvals_ck1 CHECK (status IN ('pending','approved','rejected'))
);

CREATE TABLE proposals.share_links (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    version_id uuid NOT NULL,
    recipient_email citext NOT NULL,
    token_hash bytea NOT NULL,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id)
);

CREATE TABLE proposals.view_events (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    share_link_id uuid NOT NULL,
    viewed_at timestamptz NOT NULL,
    ip_hash bytea,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id)
);

CREATE TABLE proposals.signature_requests (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    version_id uuid NOT NULL,
    provider_key text NOT NULL,
    provider_envelope_id text,
    status text NOT NULL,
    signed_file_id uuid,
    idempotency_key text NOT NULL,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT signature_requests_ck1 CHECK (status IN ('created','sent','viewed','signed','declined','voided','failed'))
);

CREATE TABLE proposals.signature_events (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    signature_request_id uuid NOT NULL,
    provider_event_id text NOT NULL,
    event_type text NOT NULL,
    occurred_at timestamptz NOT NULL DEFAULT now(),
    detail jsonb,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id)
);

CREATE TABLE customer_expansion.customer_profiles (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    crm_company_id uuid NOT NULL,
    company_id uuid,
    health_status text NOT NULL,
    health_as_of timestamptz,
    deleted_at timestamptz,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT customer_profiles_ck1 CHECK (health_status IN ('unknown','healthy','watch','at_risk'))
);

CREATE TABLE customer_expansion.contracts (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    customer_profile_id uuid NOT NULL,
    proposal_version_id uuid,
    deal_id uuid,
    status text NOT NULL,
    start_date date NOT NULL,
    end_date date,
    term_months int,
    auto_renew boolean,
    notice_days int,
    currency char(3) NOT NULL,
    mrr_minor bigint NOT NULL,
    setup_minor bigint NOT NULL,
    source text NOT NULL,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT contracts_ck1 CHECK (status IN ('draft','active','renewing','ended','cancelled')),
    CONSTRAINT contracts_ck2 CHECK (source IN ('won_deal','signed_proposal','manual','csv_import')),
    CONSTRAINT contracts_ck3 CHECK (mrr_minor >= 0)
);

ALTER TABLE it_assessment.templates ADD CONSTRAINT templates_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE it_assessment.landing_pages ADD CONSTRAINT landing_pages_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE it_assessment.landing_pages ADD CONSTRAINT landing_pages_template_id_fk FOREIGN KEY (tenant_id, template_id) REFERENCES it_assessment.templates (tenant_id, id) ON DELETE RESTRICT;
ALTER TABLE it_assessment.domain_verifications ADD CONSTRAINT domain_verifications_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE it_assessment.authorizations ADD CONSTRAINT authorizations_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE it_assessment.authorizations ADD CONSTRAINT authorizations_domain_verification_id_fk FOREIGN KEY (tenant_id, domain_verification_id) REFERENCES it_assessment.domain_verifications (tenant_id, id) ON DELETE RESTRICT;
ALTER TABLE it_assessment.lead_captures ADD CONSTRAINT lead_captures_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE it_assessment.lead_captures ADD CONSTRAINT lead_captures_landing_page_id_fk FOREIGN KEY (tenant_id, landing_page_id) REFERENCES it_assessment.landing_pages (tenant_id, id) ON DELETE RESTRICT;
ALTER TABLE it_assessment.lead_captures ADD CONSTRAINT lead_captures_contact_id_fk FOREIGN KEY (tenant_id, contact_id) REFERENCES shared.contacts (tenant_id, id) ON DELETE SET NULL (contact_id);
ALTER TABLE it_assessment.assessments ADD CONSTRAINT assessments_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE it_assessment.assessments ADD CONSTRAINT assessments_authorization_id_fk FOREIGN KEY (tenant_id, authorization_id) REFERENCES it_assessment.authorizations (tenant_id, id) ON DELETE RESTRICT;
ALTER TABLE it_assessment.assessments ADD CONSTRAINT assessments_template_id_fk FOREIGN KEY (tenant_id, template_id) REFERENCES it_assessment.templates (tenant_id, id) ON DELETE RESTRICT;
ALTER TABLE it_assessment.assessments ADD CONSTRAINT assessments_lead_capture_id_fk FOREIGN KEY (tenant_id, lead_capture_id) REFERENCES it_assessment.lead_captures (tenant_id, id) ON DELETE SET NULL (lead_capture_id);
ALTER TABLE it_assessment.assessments ADD CONSTRAINT assessments_company_id_fk FOREIGN KEY (company_id) REFERENCES shared.companies (id) ON DELETE RESTRICT;
ALTER TABLE it_assessment.checks ADD CONSTRAINT checks_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE it_assessment.checks ADD CONSTRAINT checks_assessment_id_fk FOREIGN KEY (tenant_id, assessment_id) REFERENCES it_assessment.assessments (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE it_assessment.findings ADD CONSTRAINT findings_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE it_assessment.findings ADD CONSTRAINT findings_assessment_id_fk FOREIGN KEY (tenant_id, assessment_id) REFERENCES it_assessment.assessments (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE it_assessment.findings ADD CONSTRAINT findings_check_id_fk FOREIGN KEY (tenant_id, check_id) REFERENCES it_assessment.checks (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE it_assessment.reports ADD CONSTRAINT reports_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE it_assessment.reports ADD CONSTRAINT reports_assessment_id_fk FOREIGN KEY (tenant_id, assessment_id) REFERENCES it_assessment.assessments (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE it_assessment.consultation_bookings ADD CONSTRAINT consultation_bookings_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE it_assessment.consultation_bookings ADD CONSTRAINT consultation_bookings_assessment_id_fk FOREIGN KEY (tenant_id, assessment_id) REFERENCES it_assessment.assessments (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE it_assessment.consultation_bookings ADD CONSTRAINT consultation_bookings_contact_id_fk FOREIGN KEY (tenant_id, contact_id) REFERENCES shared.contacts (tenant_id, id) ON DELETE SET NULL (contact_id);
ALTER TABLE it_assessment.egress_decisions ADD CONSTRAINT egress_decisions_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE it_assessment.egress_decisions ADD CONSTRAINT egress_decisions_assessment_id_fk FOREIGN KEY (tenant_id, assessment_id) REFERENCES it_assessment.assessments (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE crm.companies ADD CONSTRAINT companies_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE crm.companies ADD CONSTRAINT companies_company_id_fk FOREIGN KEY (company_id) REFERENCES shared.companies (id) ON DELETE RESTRICT;
ALTER TABLE crm.contacts ADD CONSTRAINT contacts_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE crm.contacts ADD CONSTRAINT contacts_contact_id_fk FOREIGN KEY (tenant_id, contact_id) REFERENCES shared.contacts (tenant_id, id) ON DELETE RESTRICT;
ALTER TABLE crm.contacts ADD CONSTRAINT contacts_crm_company_id_fk FOREIGN KEY (tenant_id, crm_company_id) REFERENCES crm.companies (tenant_id, id) ON DELETE SET NULL (crm_company_id);
ALTER TABLE crm.prospect_conversions ADD CONSTRAINT prospect_conversions_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE crm.prospect_conversions ADD CONSTRAINT prospect_conversions_crm_company_id_fk FOREIGN KEY (tenant_id, crm_company_id) REFERENCES crm.companies (tenant_id, id) ON DELETE RESTRICT;
ALTER TABLE crm.prospect_conversions ADD CONSTRAINT prospect_conversions_deal_id_fk FOREIGN KEY (tenant_id, deal_id) REFERENCES crm.deals (tenant_id, id) ON DELETE SET NULL (deal_id);
ALTER TABLE crm.pipelines ADD CONSTRAINT pipelines_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE crm.pipeline_stages ADD CONSTRAINT pipeline_stages_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE crm.pipeline_stages ADD CONSTRAINT pipeline_stages_pipeline_id_fk FOREIGN KEY (tenant_id, pipeline_id) REFERENCES crm.pipelines (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE crm.deals ADD CONSTRAINT deals_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE crm.deals ADD CONSTRAINT deals_pipeline_id_fk FOREIGN KEY (tenant_id, pipeline_id) REFERENCES crm.pipelines (tenant_id, id) ON DELETE RESTRICT;
ALTER TABLE crm.deals ADD CONSTRAINT deals_stage_id_fk FOREIGN KEY (tenant_id, stage_id) REFERENCES crm.pipeline_stages (tenant_id, id) ON DELETE RESTRICT;
ALTER TABLE crm.deals ADD CONSTRAINT deals_crm_company_id_fk FOREIGN KEY (tenant_id, crm_company_id) REFERENCES crm.companies (tenant_id, id) ON DELETE RESTRICT;
ALTER TABLE crm.deals ADD CONSTRAINT deals_primary_contact_id_fk FOREIGN KEY (tenant_id, primary_contact_id) REFERENCES crm.contacts (tenant_id, id) ON DELETE SET NULL (primary_contact_id);
ALTER TABLE crm.deal_stage_history ADD CONSTRAINT deal_stage_history_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE crm.deal_stage_history ADD CONSTRAINT deal_stage_history_deal_id_fk FOREIGN KEY (tenant_id, deal_id) REFERENCES crm.deals (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE crm.deal_stage_history ADD CONSTRAINT deal_stage_history_from_stage_id_fk FOREIGN KEY (tenant_id, from_stage_id) REFERENCES crm.pipeline_stages (tenant_id, id) ON DELETE RESTRICT;
ALTER TABLE crm.deal_stage_history ADD CONSTRAINT deal_stage_history_to_stage_id_fk FOREIGN KEY (tenant_id, to_stage_id) REFERENCES crm.pipeline_stages (tenant_id, id) ON DELETE RESTRICT;
ALTER TABLE crm.sales_reps ADD CONSTRAINT sales_reps_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE crm.tasks ADD CONSTRAINT tasks_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE crm.tasks ADD CONSTRAINT tasks_crm_company_id_fk FOREIGN KEY (tenant_id, crm_company_id) REFERENCES crm.companies (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE crm.tasks ADD CONSTRAINT tasks_deal_id_fk FOREIGN KEY (tenant_id, deal_id) REFERENCES crm.deals (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE crm.reminders ADD CONSTRAINT reminders_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE crm.reminders ADD CONSTRAINT reminders_task_id_fk FOREIGN KEY (tenant_id, task_id) REFERENCES crm.tasks (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE crm.notes ADD CONSTRAINT notes_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE crm.attachments ADD CONSTRAINT attachments_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE crm.meetings ADD CONSTRAINT meetings_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE crm.meetings ADD CONSTRAINT meetings_crm_company_id_fk FOREIGN KEY (tenant_id, crm_company_id) REFERENCES crm.companies (tenant_id, id) ON DELETE SET NULL (crm_company_id);
ALTER TABLE crm.meetings ADD CONSTRAINT meetings_deal_id_fk FOREIGN KEY (tenant_id, deal_id) REFERENCES crm.deals (tenant_id, id) ON DELETE SET NULL (deal_id);
ALTER TABLE crm.meeting_attendees ADD CONSTRAINT meeting_attendees_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE crm.meeting_attendees ADD CONSTRAINT meeting_attendees_meeting_id_fk FOREIGN KEY (tenant_id, meeting_id) REFERENCES crm.meetings (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE crm.meeting_attendees ADD CONSTRAINT meeting_attendees_contact_id_fk FOREIGN KEY (tenant_id, contact_id) REFERENCES crm.contacts (tenant_id, id) ON DELETE SET NULL (contact_id);
ALTER TABLE crm.activities ADD CONSTRAINT activities_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE crm.activities ADD CONSTRAINT activities_crm_company_id_fk FOREIGN KEY (tenant_id, crm_company_id) REFERENCES crm.companies (tenant_id, id) ON DELETE SET NULL (crm_company_id);
ALTER TABLE crm.activities ADD CONSTRAINT activities_contact_id_fk FOREIGN KEY (tenant_id, contact_id) REFERENCES crm.contacts (tenant_id, id) ON DELETE SET NULL (contact_id);
ALTER TABLE crm.activities ADD CONSTRAINT activities_deal_id_fk FOREIGN KEY (tenant_id, deal_id) REFERENCES crm.deals (tenant_id, id) ON DELETE SET NULL (deal_id);
ALTER TABLE crm.custom_field_definitions ADD CONSTRAINT custom_field_definitions_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE crm.custom_field_values ADD CONSTRAINT custom_field_values_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE crm.custom_field_values ADD CONSTRAINT custom_field_values_definition_id_fk FOREIGN KEY (tenant_id, definition_id) REFERENCES crm.custom_field_definitions (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE crm.ai_recommendations ADD CONSTRAINT ai_recommendations_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE crm.ai_recommendations ADD CONSTRAINT ai_recommendations_deal_id_fk FOREIGN KEY (tenant_id, deal_id) REFERENCES crm.deals (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE crm.ai_recommendations ADD CONSTRAINT ai_recommendations_crm_company_id_fk FOREIGN KEY (tenant_id, crm_company_id) REFERENCES crm.companies (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE proposals.service_catalog_items ADD CONSTRAINT service_catalog_items_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE proposals.sla_templates ADD CONSTRAINT sla_templates_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE proposals.terms_templates ADD CONSTRAINT terms_templates_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE proposals.proposal_templates ADD CONSTRAINT proposal_templates_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE proposals.proposal_templates ADD CONSTRAINT proposal_templates_default_sla_id_fk FOREIGN KEY (tenant_id, default_sla_id) REFERENCES proposals.sla_templates (tenant_id, id) ON DELETE SET NULL (default_sla_id);
ALTER TABLE proposals.proposal_templates ADD CONSTRAINT proposal_templates_default_terms_id_fk FOREIGN KEY (tenant_id, default_terms_id) REFERENCES proposals.terms_templates (tenant_id, id) ON DELETE SET NULL (default_terms_id);
ALTER TABLE proposals.proposals ADD CONSTRAINT proposals_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE proposals.proposals ADD CONSTRAINT proposals_template_id_fk FOREIGN KEY (tenant_id, template_id) REFERENCES proposals.proposal_templates (tenant_id, id) ON DELETE SET NULL (template_id);
ALTER TABLE proposals.proposals ADD CONSTRAINT proposals_current_version_id_fk FOREIGN KEY (tenant_id, current_version_id) REFERENCES proposals.proposal_versions (tenant_id, id) ON DELETE SET NULL (current_version_id);
ALTER TABLE proposals.proposal_versions ADD CONSTRAINT proposal_versions_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE proposals.proposal_versions ADD CONSTRAINT proposal_versions_proposal_id_fk FOREIGN KEY (tenant_id, proposal_id) REFERENCES proposals.proposals (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE proposals.proposal_line_items ADD CONSTRAINT proposal_line_items_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE proposals.proposal_line_items ADD CONSTRAINT proposal_line_items_version_id_fk FOREIGN KEY (tenant_id, version_id) REFERENCES proposals.proposal_versions (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE proposals.proposal_line_items ADD CONSTRAINT proposal_line_items_catalog_item_id_fk FOREIGN KEY (tenant_id, catalog_item_id) REFERENCES proposals.service_catalog_items (tenant_id, id) ON DELETE SET NULL (catalog_item_id);
ALTER TABLE proposals.approvals ADD CONSTRAINT approvals_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE proposals.approvals ADD CONSTRAINT approvals_version_id_fk FOREIGN KEY (tenant_id, version_id) REFERENCES proposals.proposal_versions (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE proposals.share_links ADD CONSTRAINT share_links_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE proposals.share_links ADD CONSTRAINT share_links_version_id_fk FOREIGN KEY (tenant_id, version_id) REFERENCES proposals.proposal_versions (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE proposals.view_events ADD CONSTRAINT view_events_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE proposals.view_events ADD CONSTRAINT view_events_share_link_id_fk FOREIGN KEY (tenant_id, share_link_id) REFERENCES proposals.share_links (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE proposals.signature_requests ADD CONSTRAINT signature_requests_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE proposals.signature_requests ADD CONSTRAINT signature_requests_version_id_fk FOREIGN KEY (tenant_id, version_id) REFERENCES proposals.proposal_versions (tenant_id, id) ON DELETE RESTRICT;
ALTER TABLE proposals.signature_events ADD CONSTRAINT signature_events_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE proposals.signature_events ADD CONSTRAINT signature_events_signature_request_id_fk FOREIGN KEY (tenant_id, signature_request_id) REFERENCES proposals.signature_requests (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE customer_expansion.customer_profiles ADD CONSTRAINT customer_profiles_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE customer_expansion.customer_profiles ADD CONSTRAINT customer_profiles_company_id_fk FOREIGN KEY (company_id) REFERENCES shared.companies (id) ON DELETE RESTRICT;
ALTER TABLE customer_expansion.contracts ADD CONSTRAINT contracts_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE customer_expansion.contracts ADD CONSTRAINT contracts_customer_profile_id_fk FOREIGN KEY (tenant_id, customer_profile_id) REFERENCES customer_expansion.customer_profiles (tenant_id, id) ON DELETE CASCADE;

CREATE TRIGGER touch_updated_at BEFORE UPDATE ON it_assessment.templates FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX landing_pages_uq1 ON it_assessment.landing_pages (slug) WHERE deleted_at IS NULL;
CREATE INDEX landing_pages_template_id_idx ON it_assessment.landing_pages (tenant_id, template_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON it_assessment.landing_pages FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE INDEX domain_verifications_ix1 ON it_assessment.domain_verifications (tenant_id, domain);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON it_assessment.domain_verifications FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE INDEX authorizations_domain_verification_id_idx ON it_assessment.authorizations (tenant_id, domain_verification_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON it_assessment.authorizations FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE INDEX lead_captures_landing_page_id_idx ON it_assessment.lead_captures (tenant_id, landing_page_id);
CREATE INDEX lead_captures_contact_id_idx ON it_assessment.lead_captures (tenant_id, contact_id);
CREATE INDEX assessments_authorization_id_idx ON it_assessment.assessments (tenant_id, authorization_id);
CREATE INDEX assessments_template_id_idx ON it_assessment.assessments (tenant_id, template_id);
CREATE INDEX assessments_lead_capture_id_idx ON it_assessment.assessments (tenant_id, lead_capture_id);
CREATE INDEX assessments_company_id_idx ON it_assessment.assessments (tenant_id, company_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON it_assessment.assessments FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX checks_uq1 ON it_assessment.checks (tenant_id, assessment_id, check_key, target);
CREATE INDEX checks_assessment_id_idx ON it_assessment.checks (tenant_id, assessment_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON it_assessment.checks FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE INDEX findings_assessment_id_idx ON it_assessment.findings (tenant_id, assessment_id);
CREATE INDEX findings_check_id_idx ON it_assessment.findings (tenant_id, check_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON it_assessment.findings FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX reports_uq1 ON it_assessment.reports (tenant_id, assessment_id, version);
CREATE INDEX reports_assessment_id_idx ON it_assessment.reports (tenant_id, assessment_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON it_assessment.reports FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX consultation_bookings_uq1 ON it_assessment.consultation_bookings (tenant_id, idempotency_key);
CREATE INDEX consultation_bookings_assessment_id_idx ON it_assessment.consultation_bookings (tenant_id, assessment_id);
CREATE INDEX consultation_bookings_contact_id_idx ON it_assessment.consultation_bookings (tenant_id, contact_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON it_assessment.consultation_bookings FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE INDEX egress_decisions_assessment_id_idx ON it_assessment.egress_decisions (tenant_id, assessment_id);
CREATE UNIQUE INDEX companies_uq1 ON crm.companies (tenant_id, company_id) WHERE company_id IS NOT NULL AND deleted_at IS NULL;
CREATE INDEX companies_ix1 ON crm.companies (tenant_id, lifecycle);
CREATE INDEX companies_ix2 ON crm.companies (tenant_id, lower(name));
CREATE INDEX companies_company_id_idx ON crm.companies (tenant_id, company_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON crm.companies FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX contacts_uq1 ON crm.contacts (tenant_id, contact_id) WHERE deleted_at IS NULL;
CREATE INDEX contacts_contact_id_idx ON crm.contacts (tenant_id, contact_id);
CREATE INDEX contacts_crm_company_id_idx ON crm.contacts (tenant_id, crm_company_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON crm.contacts FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX prospect_conversions_uq1 ON crm.prospect_conversions (tenant_id, prospect_id);
CREATE INDEX prospect_conversions_crm_company_id_idx ON crm.prospect_conversions (tenant_id, crm_company_id);
CREATE INDEX prospect_conversions_deal_id_idx ON crm.prospect_conversions (tenant_id, deal_id);
CREATE UNIQUE INDEX pipelines_uq1 ON crm.pipelines (tenant_id) WHERE is_default;
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON crm.pipelines FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX pipeline_stages_uq1 ON crm.pipeline_stages (tenant_id, pipeline_id, key);
CREATE INDEX pipeline_stages_pipeline_id_idx ON crm.pipeline_stages (tenant_id, pipeline_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON crm.pipeline_stages FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE INDEX deals_ix1 ON crm.deals (tenant_id, stage_id, updated_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX deals_ix2 ON crm.deals (tenant_id, owner_user_id, status);
CREATE INDEX deals_ix3 ON crm.deals (tenant_id, expected_close_date) WHERE status = 'open';
CREATE INDEX deals_pipeline_id_idx ON crm.deals (tenant_id, pipeline_id);
CREATE INDEX deals_stage_id_idx ON crm.deals (tenant_id, stage_id);
CREATE INDEX deals_crm_company_id_idx ON crm.deals (tenant_id, crm_company_id);
CREATE INDEX deals_primary_contact_id_idx ON crm.deals (tenant_id, primary_contact_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON crm.deals FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE INDEX deal_stage_history_deal_id_idx ON crm.deal_stage_history (tenant_id, deal_id);
CREATE INDEX deal_stage_history_from_stage_id_idx ON crm.deal_stage_history (tenant_id, from_stage_id);
CREATE INDEX deal_stage_history_to_stage_id_idx ON crm.deal_stage_history (tenant_id, to_stage_id);
CREATE UNIQUE INDEX sales_reps_uq1 ON crm.sales_reps (tenant_id, user_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON crm.sales_reps FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE INDEX tasks_ix1 ON crm.tasks (tenant_id, assignee_user_id, status, due_at);
CREATE INDEX tasks_crm_company_id_idx ON crm.tasks (tenant_id, crm_company_id);
CREATE INDEX tasks_deal_id_idx ON crm.tasks (tenant_id, deal_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON crm.tasks FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE INDEX reminders_ix1 ON crm.reminders (remind_at) WHERE fired_at IS NULL;
CREATE INDEX reminders_task_id_idx ON crm.reminders (tenant_id, task_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON crm.reminders FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE INDEX notes_ix1 ON crm.notes (tenant_id, subject_type, subject_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON crm.notes FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE INDEX attachments_ix1 ON crm.attachments (tenant_id, subject_type, subject_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON crm.attachments FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX meetings_uq1 ON crm.meetings (tenant_id, idempotency_key);
CREATE INDEX meetings_crm_company_id_idx ON crm.meetings (tenant_id, crm_company_id);
CREATE INDEX meetings_deal_id_idx ON crm.meetings (tenant_id, deal_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON crm.meetings FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE INDEX meeting_attendees_meeting_id_idx ON crm.meeting_attendees (tenant_id, meeting_id);
CREATE INDEX meeting_attendees_contact_id_idx ON crm.meeting_attendees (tenant_id, contact_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON crm.meeting_attendees FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX activities_uq1 ON crm.activities (tenant_id, source_event_id);
CREATE INDEX activities_ix1 ON crm.activities (tenant_id, crm_company_id, occurred_at DESC);
CREATE INDEX activities_ix2 ON crm.activities (tenant_id, deal_id, occurred_at DESC);
CREATE INDEX activities_crm_company_id_idx ON crm.activities (tenant_id, crm_company_id);
CREATE INDEX activities_contact_id_idx ON crm.activities (tenant_id, contact_id);
CREATE INDEX activities_deal_id_idx ON crm.activities (tenant_id, deal_id);
CREATE UNIQUE INDEX custom_field_definitions_uq1 ON crm.custom_field_definitions (tenant_id, entity, key);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON crm.custom_field_definitions FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX custom_field_values_uq1 ON crm.custom_field_values (tenant_id, definition_id, subject_id);
CREATE INDEX custom_field_values_definition_id_idx ON crm.custom_field_values (tenant_id, definition_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON crm.custom_field_values FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE INDEX ai_recommendations_deal_id_idx ON crm.ai_recommendations (tenant_id, deal_id);
CREATE INDEX ai_recommendations_crm_company_id_idx ON crm.ai_recommendations (tenant_id, crm_company_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON crm.ai_recommendations FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON proposals.service_catalog_items FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON proposals.sla_templates FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON proposals.terms_templates FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE INDEX proposal_templates_default_sla_id_idx ON proposals.proposal_templates (tenant_id, default_sla_id);
CREATE INDEX proposal_templates_default_terms_id_idx ON proposals.proposal_templates (tenant_id, default_terms_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON proposals.proposal_templates FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX proposals_uq1 ON proposals.proposals (tenant_id, number);
CREATE INDEX proposals_ix1 ON proposals.proposals (tenant_id, deal_id);
CREATE INDEX proposals_template_id_idx ON proposals.proposals (tenant_id, template_id);
CREATE INDEX proposals_current_version_id_idx ON proposals.proposals (tenant_id, current_version_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON proposals.proposals FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX proposal_versions_uq1 ON proposals.proposal_versions (tenant_id, proposal_id, version);
CREATE INDEX proposal_versions_proposal_id_idx ON proposals.proposal_versions (tenant_id, proposal_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON proposals.proposal_versions FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE INDEX proposal_line_items_version_id_idx ON proposals.proposal_line_items (tenant_id, version_id);
CREATE INDEX proposal_line_items_catalog_item_id_idx ON proposals.proposal_line_items (tenant_id, catalog_item_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON proposals.proposal_line_items FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE INDEX approvals_version_id_idx ON proposals.approvals (tenant_id, version_id);
CREATE UNIQUE INDEX share_links_uq1 ON proposals.share_links (token_hash);
CREATE INDEX share_links_version_id_idx ON proposals.share_links (tenant_id, version_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON proposals.share_links FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE INDEX view_events_share_link_id_idx ON proposals.view_events (tenant_id, share_link_id);
CREATE UNIQUE INDEX signature_requests_uq1 ON proposals.signature_requests (tenant_id, idempotency_key);
CREATE UNIQUE INDEX signature_requests_uq2 ON proposals.signature_requests (provider_key, provider_envelope_id);
CREATE INDEX signature_requests_version_id_idx ON proposals.signature_requests (tenant_id, version_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON proposals.signature_requests FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX signature_events_uq1 ON proposals.signature_events (tenant_id, provider_event_id);
CREATE INDEX signature_events_signature_request_id_idx ON proposals.signature_events (tenant_id, signature_request_id);
CREATE UNIQUE INDEX customer_profiles_uq1 ON customer_expansion.customer_profiles (tenant_id, crm_company_id) WHERE deleted_at IS NULL;
CREATE INDEX customer_profiles_company_id_idx ON customer_expansion.customer_profiles (tenant_id, company_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON customer_expansion.customer_profiles FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX contracts_uq1 ON customer_expansion.contracts (tenant_id, deal_id);
CREATE INDEX contracts_ix1 ON customer_expansion.contracts (tenant_id, end_date) WHERE status = 'active';
CREATE INDEX contracts_customer_profile_id_idx ON customer_expansion.contracts (tenant_id, customer_profile_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON customer_expansion.contracts FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();

ALTER TABLE it_assessment.templates ENABLE ROW LEVEL SECURITY;
ALTER TABLE it_assessment.templates FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON it_assessment.templates TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE it_assessment.landing_pages ENABLE ROW LEVEL SECURITY;
ALTER TABLE it_assessment.landing_pages FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON it_assessment.landing_pages TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE it_assessment.domain_verifications ENABLE ROW LEVEL SECURITY;
ALTER TABLE it_assessment.domain_verifications FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON it_assessment.domain_verifications TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE it_assessment.authorizations ENABLE ROW LEVEL SECURITY;
ALTER TABLE it_assessment.authorizations FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON it_assessment.authorizations TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE it_assessment.lead_captures ENABLE ROW LEVEL SECURITY;
ALTER TABLE it_assessment.lead_captures FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON it_assessment.lead_captures TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE it_assessment.assessments ENABLE ROW LEVEL SECURITY;
ALTER TABLE it_assessment.assessments FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON it_assessment.assessments TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE it_assessment.checks ENABLE ROW LEVEL SECURITY;
ALTER TABLE it_assessment.checks FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON it_assessment.checks TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE it_assessment.findings ENABLE ROW LEVEL SECURITY;
ALTER TABLE it_assessment.findings FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON it_assessment.findings TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE it_assessment.reports ENABLE ROW LEVEL SECURITY;
ALTER TABLE it_assessment.reports FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON it_assessment.reports TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE it_assessment.consultation_bookings ENABLE ROW LEVEL SECURITY;
ALTER TABLE it_assessment.consultation_bookings FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON it_assessment.consultation_bookings TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE it_assessment.egress_decisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE it_assessment.egress_decisions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON it_assessment.egress_decisions TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE crm.companies ENABLE ROW LEVEL SECURITY;
ALTER TABLE crm.companies FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON crm.companies TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE crm.contacts ENABLE ROW LEVEL SECURITY;
ALTER TABLE crm.contacts FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON crm.contacts TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE crm.prospect_conversions ENABLE ROW LEVEL SECURITY;
ALTER TABLE crm.prospect_conversions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON crm.prospect_conversions TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE crm.pipelines ENABLE ROW LEVEL SECURITY;
ALTER TABLE crm.pipelines FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON crm.pipelines TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE crm.pipeline_stages ENABLE ROW LEVEL SECURITY;
ALTER TABLE crm.pipeline_stages FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON crm.pipeline_stages TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE crm.deals ENABLE ROW LEVEL SECURITY;
ALTER TABLE crm.deals FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON crm.deals TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE crm.deal_stage_history ENABLE ROW LEVEL SECURITY;
ALTER TABLE crm.deal_stage_history FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON crm.deal_stage_history TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE crm.sales_reps ENABLE ROW LEVEL SECURITY;
ALTER TABLE crm.sales_reps FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON crm.sales_reps TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE crm.tasks ENABLE ROW LEVEL SECURITY;
ALTER TABLE crm.tasks FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON crm.tasks TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE crm.reminders ENABLE ROW LEVEL SECURITY;
ALTER TABLE crm.reminders FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON crm.reminders TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE crm.notes ENABLE ROW LEVEL SECURITY;
ALTER TABLE crm.notes FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON crm.notes TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE crm.attachments ENABLE ROW LEVEL SECURITY;
ALTER TABLE crm.attachments FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON crm.attachments TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE crm.meetings ENABLE ROW LEVEL SECURITY;
ALTER TABLE crm.meetings FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON crm.meetings TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE crm.meeting_attendees ENABLE ROW LEVEL SECURITY;
ALTER TABLE crm.meeting_attendees FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON crm.meeting_attendees TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE crm.activities ENABLE ROW LEVEL SECURITY;
ALTER TABLE crm.activities FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON crm.activities TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE crm.custom_field_definitions ENABLE ROW LEVEL SECURITY;
ALTER TABLE crm.custom_field_definitions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON crm.custom_field_definitions TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE crm.custom_field_values ENABLE ROW LEVEL SECURITY;
ALTER TABLE crm.custom_field_values FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON crm.custom_field_values TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE crm.ai_recommendations ENABLE ROW LEVEL SECURITY;
ALTER TABLE crm.ai_recommendations FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON crm.ai_recommendations TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE proposals.service_catalog_items ENABLE ROW LEVEL SECURITY;
ALTER TABLE proposals.service_catalog_items FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON proposals.service_catalog_items TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE proposals.sla_templates ENABLE ROW LEVEL SECURITY;
ALTER TABLE proposals.sla_templates FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON proposals.sla_templates TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE proposals.terms_templates ENABLE ROW LEVEL SECURITY;
ALTER TABLE proposals.terms_templates FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON proposals.terms_templates TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE proposals.proposal_templates ENABLE ROW LEVEL SECURITY;
ALTER TABLE proposals.proposal_templates FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON proposals.proposal_templates TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE proposals.proposals ENABLE ROW LEVEL SECURITY;
ALTER TABLE proposals.proposals FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON proposals.proposals TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE proposals.proposal_versions ENABLE ROW LEVEL SECURITY;
ALTER TABLE proposals.proposal_versions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON proposals.proposal_versions TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE proposals.proposal_line_items ENABLE ROW LEVEL SECURITY;
ALTER TABLE proposals.proposal_line_items FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON proposals.proposal_line_items TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE proposals.approvals ENABLE ROW LEVEL SECURITY;
ALTER TABLE proposals.approvals FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON proposals.approvals TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE proposals.share_links ENABLE ROW LEVEL SECURITY;
ALTER TABLE proposals.share_links FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON proposals.share_links TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE proposals.view_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE proposals.view_events FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON proposals.view_events TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE proposals.signature_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE proposals.signature_requests FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON proposals.signature_requests TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE proposals.signature_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE proposals.signature_events FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON proposals.signature_events TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE customer_expansion.customer_profiles ENABLE ROW LEVEL SECURITY;
ALTER TABLE customer_expansion.customer_profiles FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON customer_expansion.customer_profiles TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE customer_expansion.contracts ENABLE ROW LEVEL SECURITY;
ALTER TABLE customer_expansion.contracts FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON customer_expansion.contracts TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- Stage positions are swapped when stages are reordered, so uniqueness is checked at commit.
ALTER TABLE crm.pipeline_stages ADD CONSTRAINT pipeline_stages_position_uq UNIQUE (tenant_id, pipeline_id, position) DEFERRABLE INITIALLY DEFERRED;

GRANT USAGE ON SCHEMA crm, proposals, it_assessment, customer_expansion TO msp_app, msp_system;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA crm, proposals, it_assessment, customer_expansion TO msp_app;

-- A company merge is platform-wide, so the system role repoints CRM companies in every tenant.
GRANT SELECT, UPDATE ON crm.companies TO msp_system;
CREATE POLICY merge_repoint ON crm.companies TO msp_system USING (true) WITH CHECK (true);
-- The reminder scheduler finds due reminders across tenants, then fires each under its tenant.
GRANT SELECT ON crm.reminders TO msp_system;
CREATE POLICY scheduler_read ON crm.reminders FOR SELECT TO msp_system USING (true);
-- Public proposal links and landing pages find their tenant from a hashed token or a slug. The
-- system role may read only these columns' rows to resolve the tenant; all other work runs under it.
GRANT SELECT ON proposals.share_links, it_assessment.landing_pages TO msp_system;
CREATE POLICY public_lookup ON proposals.share_links FOR SELECT TO msp_system USING (true);
CREATE POLICY public_lookup ON it_assessment.landing_pages FOR SELECT TO msp_system USING (status = 'published' AND deleted_at IS NULL);
GRANT SELECT ON it_assessment.reports TO msp_system;
CREATE POLICY public_lookup ON it_assessment.reports FOR SELECT TO msp_system USING (true);

-- An issued proposal version is a record of what the customer was offered. The database refuses to
-- change it, so no code path, bug or manual edit can alter it. A correction is a new version.
-- +goose StatementBegin
CREATE FUNCTION proposals.protect_issued_version() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.status IN ('issued', 'superseded', 'expired') THEN
            RAISE EXCEPTION 'issued proposal versions cannot be deleted' USING ERRCODE = 'P0001';
        END IF;
        RETURN OLD;
    END IF;
    IF OLD.status IN ('issued', 'superseded', 'expired') THEN
        IF NEW.content IS DISTINCT FROM OLD.content OR NEW.commercial_snapshot IS DISTINCT FROM OLD.commercial_snapshot
           OR NEW.snapshot_hash IS DISTINCT FROM OLD.snapshot_hash OR NEW.currency IS DISTINCT FROM OLD.currency
           OR NEW.setup_total_minor IS DISTINCT FROM OLD.setup_total_minor OR NEW.mrr_total_minor IS DISTINCT FROM OLD.mrr_total_minor
           OR NEW.term_months IS DISTINCT FROM OLD.term_months OR NEW.tcv_minor IS DISTINCT FROM OLD.tcv_minor
           OR NEW.valid_until IS DISTINCT FROM OLD.valid_until OR NEW.issued_at IS DISTINCT FROM OLD.issued_at
           OR NEW.version IS DISTINCT FROM OLD.version OR NEW.proposal_id IS DISTINCT FROM OLD.proposal_id
           OR (OLD.pdf_file_id IS NOT NULL AND NEW.pdf_file_id IS DISTINCT FROM OLD.pdf_file_id)
           OR NOT (NEW.status = OLD.status OR (OLD.status = 'issued' AND NEW.status IN ('superseded', 'expired'))) THEN
            RAISE EXCEPTION 'issued proposal versions cannot be changed' USING ERRCODE = 'P0001';
        END IF;
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER protect_issued_version BEFORE UPDATE OR DELETE ON proposals.proposal_versions FOR EACH ROW EXECUTE FUNCTION proposals.protect_issued_version();

-- +goose StatementBegin
CREATE FUNCTION proposals.protect_issued_lines() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE v_status text;
BEGIN
    SELECT status INTO v_status FROM proposals.proposal_versions
    WHERE tenant_id = COALESCE(NEW.tenant_id, OLD.tenant_id) AND id = COALESCE(NEW.version_id, OLD.version_id);
    IF v_status IN ('issued', 'superseded', 'expired') THEN
        RAISE EXCEPTION 'line items of an issued proposal version cannot be changed' USING ERRCODE = 'P0001';
    END IF;
    RETURN COALESCE(NEW, OLD);
END $$;
-- +goose StatementEnd
CREATE TRIGGER protect_issued_lines BEFORE INSERT OR UPDATE OR DELETE ON proposals.proposal_line_items FOR EACH ROW EXECUTE FUNCTION proposals.protect_issued_lines();
REVOKE EXECUTE ON FUNCTION proposals.protect_issued_version(), proposals.protect_issued_lines() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION proposals.protect_issued_version(), proposals.protect_issued_lines() TO msp_app;

INSERT INTO notification.templates (key, channel, locale, subject, body) VALUES
    ('crm.task_reminder', 'in_app', 'en', NULL, 'Reminder: {{.title}}'),
    ('crm.task_reminder', 'email', 'en', 'Task reminder', 'Reminder: {{.title}}');

-- E-signature webhooks arrive on a platform endpoint and name only the provider's envelope, so the
-- system role finds which tenant's request it belongs to. All other work runs under that tenant.
GRANT SELECT ON proposals.signature_requests TO msp_system;
CREATE POLICY envelope_lookup ON proposals.signature_requests FOR SELECT TO msp_system USING (provider_envelope_id IS NOT NULL);

-- Fake e-signature: nothing leaves the system until a provider is chosen (D-05).
INSERT INTO integration.providers (key, kind, name, capabilities, tenant_connectable, status) VALUES
    ('fake_esign', 'esign', 'Fake e-signature (no external calls)', '{"envelopes": true}', false, 'enabled');

-- AI drafting for proposal text. It writes prose only: prices come from the calculation, and a
-- person must approve the text before a version can be issued.
INSERT INTO integration.prompt_templates (key, task_key, action_class, output_schema, status) VALUES
    ('proposals.draft_section', 'proposals.draft_section', 'recommendation',
     '{"type":"object","required":["heading","body"],"additionalProperties":false,"properties":{"heading":{"type":"string","maxLength":200},"body":{"type":"string","maxLength":6000}}}', 'active');
INSERT INTO integration.prompt_versions (template_id, version, system_text, user_text, status)
SELECT id, 1, 'Draft one section of a managed IT services proposal in plain, professional English. Never state prices, discounts or legal commitments: those come from elsewhere. Use only the facts given.',
    'Section: {{section}}\nCustomer: {{customer}}\nServices: {{services}}\nNotes from the salesperson: {{notes}}', 'active'
FROM integration.prompt_templates WHERE key = 'proposals.draft_section';
INSERT INTO integration.ai_model_routes (task_key, provider_id, model, max_input_tokens, max_output_tokens, status)
SELECT 'proposals.draft_section', p.id, 'fake-1', 8000, 1500, 'active' FROM integration.providers p WHERE p.key = 'fake_ai';

-- +goose Down
DELETE FROM integration.ai_model_routes WHERE task_key = 'proposals.draft_section';
DELETE FROM integration.prompt_versions WHERE template_id IN (SELECT id FROM integration.prompt_templates WHERE key = 'proposals.draft_section');
DELETE FROM integration.prompt_templates WHERE key = 'proposals.draft_section';
DELETE FROM integration.providers WHERE key = 'fake_esign';
DELETE FROM notification.templates WHERE key = 'crm.task_reminder';
DROP FUNCTION IF EXISTS proposals.protect_issued_lines() CASCADE;
DROP FUNCTION IF EXISTS proposals.protect_issued_version() CASCADE;
DROP TABLE IF EXISTS customer_expansion.contracts CASCADE;
DROP TABLE IF EXISTS customer_expansion.customer_profiles CASCADE;
DROP TABLE IF EXISTS proposals.signature_events CASCADE;
DROP TABLE IF EXISTS proposals.signature_requests CASCADE;
DROP TABLE IF EXISTS proposals.view_events CASCADE;
DROP TABLE IF EXISTS proposals.share_links CASCADE;
DROP TABLE IF EXISTS proposals.approvals CASCADE;
DROP TABLE IF EXISTS proposals.proposal_line_items CASCADE;
DROP TABLE IF EXISTS proposals.proposal_versions CASCADE;
DROP TABLE IF EXISTS proposals.proposals CASCADE;
DROP TABLE IF EXISTS proposals.proposal_templates CASCADE;
DROP TABLE IF EXISTS proposals.terms_templates CASCADE;
DROP TABLE IF EXISTS proposals.sla_templates CASCADE;
DROP TABLE IF EXISTS proposals.service_catalog_items CASCADE;
DROP TABLE IF EXISTS crm.ai_recommendations CASCADE;
DROP TABLE IF EXISTS crm.custom_field_values CASCADE;
DROP TABLE IF EXISTS crm.custom_field_definitions CASCADE;
DROP TABLE IF EXISTS crm.activities CASCADE;
DROP TABLE IF EXISTS crm.meeting_attendees CASCADE;
DROP TABLE IF EXISTS crm.meetings CASCADE;
DROP TABLE IF EXISTS crm.attachments CASCADE;
DROP TABLE IF EXISTS crm.notes CASCADE;
DROP TABLE IF EXISTS crm.reminders CASCADE;
DROP TABLE IF EXISTS crm.tasks CASCADE;
DROP TABLE IF EXISTS crm.sales_reps CASCADE;
DROP TABLE IF EXISTS crm.deal_stage_history CASCADE;
DROP TABLE IF EXISTS crm.deals CASCADE;
DROP TABLE IF EXISTS crm.pipeline_stages CASCADE;
DROP TABLE IF EXISTS crm.pipelines CASCADE;
DROP TABLE IF EXISTS crm.prospect_conversions CASCADE;
DROP TABLE IF EXISTS crm.contacts CASCADE;
DROP TABLE IF EXISTS crm.companies CASCADE;
DROP TABLE IF EXISTS it_assessment.egress_decisions CASCADE;
DROP TABLE IF EXISTS it_assessment.consultation_bookings CASCADE;
DROP TABLE IF EXISTS it_assessment.reports CASCADE;
DROP TABLE IF EXISTS it_assessment.findings CASCADE;
DROP TABLE IF EXISTS it_assessment.checks CASCADE;
DROP TABLE IF EXISTS it_assessment.assessments CASCADE;
DROP TABLE IF EXISTS it_assessment.lead_captures CASCADE;
DROP TABLE IF EXISTS it_assessment.authorizations CASCADE;
DROP TABLE IF EXISTS it_assessment.domain_verifications CASCADE;
DROP TABLE IF EXISTS it_assessment.landing_pages CASCADE;
DROP TABLE IF EXISTS it_assessment.templates CASCADE;
