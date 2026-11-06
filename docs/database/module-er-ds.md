# Module ERDs

Status: **planning, pending approval.** Generated from the same specification as the [data dictionary](data-dictionary.md), which remains the fuller reference. Tenant foreign keys are omitted from the relationship lines to keep diagrams readable: assume every table with `tenant_id` belongs to `identity.tenants`. Attribute comments mark foreign keys and logical refs that point outside the diagram.

ERD numbering follows the brief. ERDs 1 to 4 are in [erd-overview](erd-overview.md), [identity-erd](identity-erd.md), [commercial-erd](commercial-erd.md) and [shared-identity-erd](shared-identity-erd.md).

## ERD 5. Lead Generation

```mermaid
erDiagram
    lead_generation__saved_searches |o--o{ lead_generation__search_jobs : "saved_search_id"
    lead_generation__search_jobs ||--o{ lead_generation__search_results : "search_job_id"
    lead_generation__prospects |o--o{ lead_generation__search_results : "promoted_prospect_id"
    lead_generation__prospects ||--o{ lead_generation__prospect_contacts : "prospect_id"
    lead_generation__prospects ||--o{ lead_generation__prospect_attributes : "prospect_id"
    lead_generation__prospects ||--o{ lead_generation__enrichment_runs : "prospect_id"
    lead_generation__prospects ||--o{ lead_generation__prospect_scores : "prospect_id"
    lead_generation__scoring_models ||--o{ lead_generation__prospect_scores : "model_id"
    lead_generation__prospect_scores ||--o{ lead_generation__prospect_score_factors : "score_id"
    lead_generation__prospect_lists ||--o{ lead_generation__prospect_list_members : "list_id"
    lead_generation__prospects ||--o{ lead_generation__prospect_list_members : "prospect_id"
    lead_generation__saved_searches {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        text name
        jsonb criteria
        uuid created_by "logical ref identity.users"
        timestamptz deleted_at
    }
    lead_generation__search_jobs {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid saved_search_id FK
        jsonb criteria_snapshot
        text status
        int progress_percent
        int result_count
        text_array provider_keys
        bigint credits_reserved
        bigint credits_used
        uuid job_id "logical ref jobs.jobs"
        uuid created_by "logical ref identity.users"
    }
    lead_generation__search_results {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        uuid search_job_id FK
        uuid company_id FK "FK shared.companies"
        int rank
        text source_key
        jsonb preview
        uuid promoted_prospect_id FK
    }
    lead_generation__prospects {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid company_id FK "FK shared.companies"
        text status
        text source_key
        text source_ref
        text data_quality
        int latest_score
        numeric latest_coverage
        uuid crm_company_id "logical ref crm.companies"
        uuid owner_user_id "logical ref identity.users"
        uuid created_by "logical ref identity.users"
        timestamptz deleted_at
    }
    lead_generation__prospect_contacts {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid prospect_id FK
        uuid contact_id FK "FK shared.contacts"
        text role_title
        boolean is_decision_maker
        numeric confidence
        text source_key
    }
    lead_generation__prospect_attributes {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        uuid prospect_id FK
        text attribute
        jsonb value
        text source_key
        text source_url
        text licence_class
        numeric confidence
        timestamptz observed_at
        timestamptz expires_at
    }
    lead_generation__enrichment_runs {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid prospect_id FK
        text kind
        text provider_key
        text status
        bigint credits_used
        text error_code
        uuid job_id "logical ref jobs.jobs"
        timestamptz finished_at
    }
    lead_generation__scoring_models {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        text name
        int version
        uuid icp_id "logical ref identity.tenant_icps"
        jsonb weights
        text status
        uuid created_by "logical ref identity.users"
    }
    lead_generation__prospect_scores {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        uuid prospect_id FK
        uuid model_id FK
        int score
        numeric coverage
        text explanation
        uuid ai_request_id "logical ref integration.ai_requests"
    }
    lead_generation__prospect_score_factors {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid score_id FK
        text factor_key
        text state
        numeric weight
        numeric earned
        jsonb evidence
    }
    lead_generation__prospect_lists {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        text name
        text description
        uuid created_by "logical ref identity.users"
        timestamptz deleted_at
    }
    lead_generation__prospect_list_members {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid list_id FK
        uuid prospect_id FK
        uuid added_by "logical ref identity.users"
    }
    lead_generation__import_jobs {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid source_file_id "logical ref files.objects"
        uuid report_file_id "logical ref files.objects"
        jsonb mapping
        boolean lawful_source_attested
        text status
        int rows_total
        int rows_imported
        int rows_duplicate
        int rows_rejected
        uuid created_by "logical ref identity.users"
    }
    lead_generation__export_jobs {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        jsonb criteria
        uuid file_id "logical ref files.objects"
        text status
        int rows_total
        uuid created_by "logical ref identity.users"
    }
```

## ERD 6. Competitor Intelligence

```mermaid
erDiagram
    competitor_intelligence__competitors ||--o{ competitor_intelligence__research_configs : "competitor_id"
    competitor_intelligence__competitors ||--o{ competitor_intelligence__research_runs : "competitor_id"
    competitor_intelligence__research_configs |o--o{ competitor_intelligence__research_runs : "config_id"
    competitor_intelligence__competitors ||--o{ competitor_intelligence__sources : "competitor_id"
    competitor_intelligence__sources ||--o{ competitor_intelligence__source_snapshots : "source_id"
    competitor_intelligence__research_runs |o--o{ competitor_intelligence__source_snapshots : "run_id"
    competitor_intelligence__competitors ||--o{ competitor_intelligence__observations : "competitor_id"
    competitor_intelligence__source_snapshots |o--o{ competitor_intelligence__observations : "source_snapshot_id"
    competitor_intelligence__observations |o--o{ competitor_intelligence__observations : "superseded_by_id"
    competitor_intelligence__competitors ||--o{ competitor_intelligence__change_events : "competitor_id"
    competitor_intelligence__observations |o--o{ competitor_intelligence__change_events : "previous_observation_id"
    competitor_intelligence__observations ||--o{ competitor_intelligence__change_events : "new_observation_id"
    competitor_intelligence__market_opportunities ||--o{ competitor_intelligence__opportunity_evidence : "opportunity_id"
    competitor_intelligence__observations |o--o{ competitor_intelligence__opportunity_evidence : "observation_id"
    competitor_intelligence__comparison_sets ||--o{ competitor_intelligence__comparison_set_members : "comparison_set_id"
    competitor_intelligence__competitors ||--o{ competitor_intelligence__comparison_set_members : "competitor_id"
    competitor_intelligence__competitors {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid company_id FK "FK shared.companies"
        text status
        text tier
        text notes
        boolean monitoring_enabled
        uuid created_by "logical ref identity.users"
        timestamptz deleted_at
    }
    competitor_intelligence__research_configs {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid competitor_id FK
        text cadence
        jsonb scope
        boolean enabled
        timestamptz next_run_at
        timestamptz deleted_at
    }
    competitor_intelligence__research_runs {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid competitor_id FK
        uuid config_id FK
        text status
        int pages_fetched
        bigint credits_used
        uuid job_id "logical ref jobs.jobs"
        timestamptz finished_at
    }
    competitor_intelligence__sources {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid competitor_id FK
        text url
        text kind
        text access_status
        timestamptz last_fetched_at
        timestamptz deleted_at
    }
    competitor_intelligence__source_snapshots {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        uuid source_id FK
        uuid run_id FK
        bytea content_hash
        uuid file_id "logical ref files.objects"
        timestamptz fetched_at
        int http_status
    }
    competitor_intelligence__observations {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        uuid competitor_id FK
        text attribute_type
        jsonb value
        text evidence_class
        numeric confidence
        uuid source_snapshot_id FK
        uuid ai_request_id "logical ref integration.ai_requests"
        timestamptz observed_at
        uuid superseded_by_id FK
    }
    competitor_intelligence__change_events {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        uuid competitor_id FK
        text attribute_type
        uuid previous_observation_id FK
        uuid new_observation_id FK
        text summary
        timestamptz acknowledged_at
    }
    competitor_intelligence__market_opportunities {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        text geography
        uuid industry_id "logical ref shared.industries"
        text title
        text rationale
        int score
        text evidence_class
        text status
        uuid ai_request_id "logical ref integration.ai_requests"
    }
    competitor_intelligence__opportunity_evidence {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid opportunity_id FK
        uuid observation_id FK
        text note
    }
    competitor_intelligence__comparison_sets {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        text name
        text_array dimensions
        uuid created_by "logical ref identity.users"
        timestamptz deleted_at
    }
    competitor_intelligence__comparison_set_members {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid comparison_set_id FK
        uuid competitor_id FK
    }
    competitor_intelligence__reports {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        text title
        text kind
        text status
        jsonb content
        uuid file_id "logical ref files.objects"
        uuid ai_request_id "logical ref integration.ai_requests"
        uuid created_by "logical ref identity.users"
        timestamptz deleted_at
    }
```

## ERD 7. Outreach

```mermaid
erDiagram
    outreach__sending_domains ||--o{ outreach__sender_accounts : "sending_domain_id"
    outreach__template_versions |o--o{ outreach__templates : "current_version_id"
    outreach__templates ||--o{ outreach__template_versions : "template_id"
    outreach__audiences ||--o{ outreach__audience_members : "audience_id"
    outreach__sender_accounts ||--o{ outreach__campaigns : "sender_account_id"
    outreach__audiences |o--o{ outreach__campaigns : "audience_id"
    outreach__campaigns ||--o{ outreach__sequence_steps : "campaign_id"
    outreach__template_versions ||--o{ outreach__sequence_steps : "template_version_id"
    outreach__campaigns ||--o{ outreach__enrollments : "campaign_id"
    outreach__enrollments ||--o{ outreach__messages : "enrollment_id"
    outreach__sequence_steps ||--o{ outreach__messages : "step_id"
    outreach__approvals |o--o{ outreach__messages : "approval_id"
    outreach__messages |o--o{ outreach__delivery_events : "message_id"
    outreach__enrollments |o--o{ outreach__replies : "enrollment_id"
    outreach__messages |o--o{ outreach__replies : "message_id"
    outreach__sender_accounts ||--o{ outreach__sending_quota_counters : "sender_account_id"
    outreach__campaigns ||--o{ outreach__campaign_stats : "campaign_id"
    outreach__sending_domains {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        citext domain
        text spf_status
        text dkim_status
        text dmarc_status
        timestamptz last_checked_at
        timestamptz deleted_at
    }
    outreach__sender_accounts {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid sending_domain_id FK
        uuid connection_id "logical ref integration.connections"
        citext from_email
        text from_name
        text status
        int daily_limit
        text reputation_state
        timestamptz deleted_at
    }
    outreach__templates {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        text name
        uuid current_version_id FK
        uuid created_by "logical ref identity.users"
        timestamptz deleted_at
    }
    outreach__template_versions {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        uuid template_id FK
        int version
        text subject
        text body
        boolean ai_generated
        uuid ai_request_id "logical ref integration.ai_requests"
    }
    outreach__audiences {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        text name
        text kind
        jsonb definition
        uuid created_by "logical ref identity.users"
        timestamptz deleted_at
    }
    outreach__audience_members {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid audience_id FK
        uuid contact_id FK "FK shared.contacts"
    }
    outreach__campaigns {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        text name
        uuid sender_account_id FK
        uuid audience_id FK
        text status
        text approval_mode
        text timezone
        jsonb sending_window
        text purpose
        uuid created_by "logical ref identity.users"
        timestamptz deleted_at
    }
    outreach__sequence_steps {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid campaign_id FK
        int position
        int delay_minutes
        uuid template_version_id FK
        jsonb send_condition
    }
    outreach__approvals {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        text subject_type
        uuid subject_id
        text status
        bytea content_hash
        uuid requested_by "logical ref identity.users"
        uuid decided_by "logical ref identity.users"
        timestamptz decided_at
        text reason
    }
    outreach__enrollments {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid campaign_id FK
        uuid contact_id FK "FK shared.contacts"
        text status
        int current_step
        timestamptz next_send_at
        text stop_reason
    }
    outreach__messages {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid enrollment_id FK
        uuid step_id FK
        uuid approval_id FK
        text subject
        text body
        boolean ai_generated
        uuid ai_request_id "logical ref integration.ai_requests"
        text status
        timestamptz scheduled_at
        timestamptz sent_at
        text provider_message_id
        text idempotency_key
    }
    outreach__delivery_events {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        uuid message_id FK
        text provider_key
        text provider_event_id
        text event_type
        timestamptz occurred_at
        jsonb detail
    }
    outreach__replies {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid enrollment_id FK
        uuid message_id FK
        text provider_message_id
        text classification
        text snippet
        timestamptz received_at
        uuid ai_request_id "logical ref integration.ai_requests"
    }
    outreach__suppressions {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        bytea email_hash
        bytea domain_hash
        text reason
        text source
        uuid created_by "logical ref identity.users"
    }
    outreach__global_suppressions {
        uuid id PK
        timestamptz created_at
        timestamptz updated_at
        bytea email_hmac
        text reason
    }
    outreach__sending_quota_counters {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid sender_account_id FK
        date day
        int sent_count
        int bounce_count
        int complaint_count
    }
    outreach__campaign_stats {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid campaign_id FK
        date day
        int sent
        int delivered
        int bounced
        int replied
        int unsubscribed
    }
```

## ERD 8. IT Assessment

```mermaid
erDiagram
    it_assessment__templates ||--o{ it_assessment__landing_pages : "template_id"
    it_assessment__domain_verifications |o--o{ it_assessment__authorizations : "domain_verification_id"
    it_assessment__landing_pages ||--o{ it_assessment__lead_captures : "landing_page_id"
    it_assessment__authorizations ||--o{ it_assessment__assessments : "authorization_id"
    it_assessment__templates ||--o{ it_assessment__assessments : "template_id"
    it_assessment__lead_captures |o--o{ it_assessment__assessments : "lead_capture_id"
    it_assessment__assessments ||--o{ it_assessment__checks : "assessment_id"
    it_assessment__assessments ||--o{ it_assessment__findings : "assessment_id"
    it_assessment__checks ||--o{ it_assessment__findings : "check_id"
    it_assessment__assessments ||--o{ it_assessment__reports : "assessment_id"
    it_assessment__assessments ||--o{ it_assessment__consultation_bookings : "assessment_id"
    it_assessment__assessments ||--o{ it_assessment__egress_decisions : "assessment_id"
    it_assessment__templates {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        text name
        text tier
        text_array check_keys
        text scoring_version
        timestamptz deleted_at
    }
    it_assessment__landing_pages {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid template_id FK
        citext slug
        text title
        jsonb branding
        text consent_text_version
        text status
        timestamptz deleted_at
    }
    it_assessment__domain_verifications {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        citext domain
        text method
        bytea token_hash
        text status
        timestamptz verified_at
        timestamptz expires_at
    }
    it_assessment__authorizations {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        citext domain
        text tier
        text method
        text authorizer_name
        citext authorizer_email
        text authorizer_role
        text terms_version
        uuid domain_verification_id FK
        bytea ip_hash
        text status
        timestamptz granted_at
        timestamptz expires_at
        timestamptz revoked_at
    }
    it_assessment__lead_captures {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        uuid landing_page_id FK
        uuid contact_id FK "FK shared.contacts"
        citext domain
        jsonb consent
        bytea source_ip_hash
    }
    it_assessment__assessments {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid authorization_id FK
        uuid template_id FK
        uuid lead_capture_id FK
        citext domain
        uuid company_id FK "FK shared.companies"
        uuid crm_company_id "logical ref crm.companies"
        text origin
        text status
        int score
        text risk_level
        uuid job_id "logical ref jobs.jobs"
        timestamptz started_at
        timestamptz completed_at
        uuid created_by "logical ref identity.users"
        timestamptz deleted_at
    }
    it_assessment__checks {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid assessment_id FK
        text check_key
        text status
        text target
        jsonb raw_result
        int duration_ms
    }
    it_assessment__findings {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid assessment_id FK
        uuid check_id FK
        text severity
        text classification
        text title
        text description
        text recommendation
        jsonb evidence
    }
    it_assessment__reports {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid assessment_id FK
        int version
        uuid file_id "logical ref files.objects"
        text status
        timestamptz deleted_at
    }
    it_assessment__consultation_bookings {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid assessment_id FK
        uuid contact_id FK "FK shared.contacts"
        text status
        timestamptz starts_at
        timestamptz ends_at
        text provider_event_id
        uuid crm_meeting_id "logical ref crm.meetings"
        text idempotency_key
    }
    it_assessment__egress_decisions {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        uuid assessment_id FK
        text check_key
        text requested_host
        inet resolved_ip
        text decision
        text reason
    }
```

## ERD 9. CRM

```mermaid
erDiagram
    crm__companies |o--o{ crm__contacts : "crm_company_id"
    crm__companies ||--o{ crm__prospect_conversions : "crm_company_id"
    crm__deals |o--o{ crm__prospect_conversions : "deal_id"
    crm__pipelines ||--o{ crm__pipeline_stages : "pipeline_id"
    crm__pipelines ||--o{ crm__deals : "pipeline_id"
    crm__pipeline_stages ||--o{ crm__deals : "stage_id"
    crm__companies ||--o{ crm__deals : "crm_company_id"
    crm__contacts |o--o{ crm__deals : "primary_contact_id"
    crm__deals ||--o{ crm__deal_stage_history : "deal_id"
    crm__pipeline_stages |o--o{ crm__deal_stage_history : "from_stage_id"
    crm__pipeline_stages ||--o{ crm__deal_stage_history : "to_stage_id"
    crm__companies |o--o{ crm__tasks : "crm_company_id"
    crm__deals |o--o{ crm__tasks : "deal_id"
    crm__tasks |o--o{ crm__reminders : "task_id"
    crm__companies |o--o{ crm__meetings : "crm_company_id"
    crm__deals |o--o{ crm__meetings : "deal_id"
    crm__meetings ||--o{ crm__meeting_attendees : "meeting_id"
    crm__contacts |o--o{ crm__meeting_attendees : "contact_id"
    crm__companies |o--o{ crm__activities : "crm_company_id"
    crm__contacts |o--o{ crm__activities : "contact_id"
    crm__deals |o--o{ crm__activities : "deal_id"
    crm__custom_field_definitions ||--o{ crm__custom_field_values : "definition_id"
    crm__deals |o--o{ crm__ai_recommendations : "deal_id"
    crm__companies |o--o{ crm__ai_recommendations : "crm_company_id"
    crm__companies {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid company_id FK "FK shared.companies"
        text name
        citext domain
        text lifecycle
        uuid owner_user_id "logical ref identity.users"
        text source_module
        uuid source_ref
        uuid created_by "logical ref identity.users"
        timestamptz deleted_at
    }
    crm__contacts {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid contact_id FK "FK shared.contacts"
        uuid crm_company_id FK
        boolean is_primary
        uuid owner_user_id "logical ref identity.users"
        timestamptz deleted_at
    }
    crm__prospect_conversions {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        uuid prospect_id "logical ref lead_generation.prospects"
        uuid crm_company_id FK
        uuid deal_id FK
        uuid converted_by "logical ref identity.users"
    }
    crm__pipelines {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        text name
        boolean is_default
        text status
    }
    crm__pipeline_stages {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid pipeline_id FK
        text key
        text name
        int position
        text kind
        int default_probability
    }
    crm__deals {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid pipeline_id FK
        uuid stage_id FK
        uuid crm_company_id FK
        uuid primary_contact_id FK
        text title
        uuid owner_user_id "logical ref identity.users"
        char currency
        bigint contract_value_minor
        bigint expected_mrr_minor
        int probability
        date expected_close_date
        text status
        timestamptz closed_at
        text loss_reason
        text source_module
        uuid source_ref
        int row_version
        uuid created_by "logical ref identity.users"
        timestamptz deleted_at
    }
    crm__deal_stage_history {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        uuid deal_id FK
        uuid from_stage_id FK
        uuid to_stage_id FK
        uuid changed_by "logical ref identity.users"
        text reason
    }
    crm__sales_reps {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid user_id "logical ref identity.users"
        bigint quota_minor
        char quota_currency
        text territory
        boolean active
    }
    crm__tasks {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        text title
        text description
        uuid assignee_user_id "logical ref identity.users"
        uuid crm_company_id FK
        uuid deal_id FK
        timestamptz due_at
        text status
        timestamptz completed_at
        uuid created_by "logical ref identity.users"
        timestamptz deleted_at
    }
    crm__reminders {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid task_id FK
        uuid user_id "logical ref identity.users"
        timestamptz remind_at
        timestamptz fired_at
    }
    crm__notes {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        text subject_type
        uuid subject_id
        text body
        uuid created_by "logical ref identity.users"
        timestamptz deleted_at
    }
    crm__attachments {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        text subject_type
        uuid subject_id
        uuid file_id "logical ref files.objects"
        uuid created_by "logical ref identity.users"
        timestamptz deleted_at
    }
    crm__meetings {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid crm_company_id FK
        uuid deal_id FK
        text title
        timestamptz starts_at
        timestamptz ends_at
        text status
        uuid organizer_user_id "logical ref identity.users"
        text provider_event_id
        text source_module
        uuid source_ref
        text outcome
        text idempotency_key
        timestamptz deleted_at
    }
    crm__meeting_attendees {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid meeting_id FK
        uuid contact_id FK
        uuid user_id "logical ref identity.users"
        text response
    }
    crm__activities {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        text activity_type
        uuid crm_company_id FK
        uuid contact_id FK
        uuid deal_id FK
        uuid actor_user_id "logical ref identity.users"
        text summary
        jsonb detail
        timestamptz occurred_at
        text source_module
        uuid source_event_id
    }
    crm__custom_field_definitions {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        text entity
        text key
        text label
        text field_type
        jsonb options
        boolean archived
    }
    crm__custom_field_values {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid definition_id FK
        uuid subject_id
        jsonb value
    }
    crm__ai_recommendations {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid deal_id FK
        uuid crm_company_id FK
        text kind
        jsonb content
        text status
        uuid ai_request_id "logical ref integration.ai_requests"
        uuid decided_by "logical ref identity.users"
    }
```

## ERD 10. Proposals

```mermaid
erDiagram
    proposals__sla_templates |o--o{ proposals__proposal_templates : "default_sla_id"
    proposals__terms_templates |o--o{ proposals__proposal_templates : "default_terms_id"
    proposals__proposal_templates |o--o{ proposals__proposals : "template_id"
    proposals__proposal_versions |o--o{ proposals__proposals : "current_version_id"
    proposals__proposals ||--o{ proposals__proposal_versions : "proposal_id"
    proposals__proposal_versions ||--o{ proposals__proposal_line_items : "version_id"
    proposals__service_catalog_items |o--o{ proposals__proposal_line_items : "catalog_item_id"
    proposals__proposal_versions ||--o{ proposals__approvals : "version_id"
    proposals__proposal_versions ||--o{ proposals__share_links : "version_id"
    proposals__share_links ||--o{ proposals__view_events : "share_link_id"
    proposals__proposal_versions ||--o{ proposals__signature_requests : "version_id"
    proposals__signature_requests ||--o{ proposals__signature_events : "signature_request_id"
    proposals__service_catalog_items {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid tenant_service_id "logical ref shared.tenant_services"
        text name
        text description
        text pricing_model
        char currency
        bigint unit_price_minor
        bigint setup_fee_minor
        text status
    }
    proposals__sla_templates {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        text name
        text body
        int version
        timestamptz deleted_at
    }
    proposals__terms_templates {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        text name
        text body
        int version
        timestamptz deleted_at
    }
    proposals__proposal_templates {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        text name
        jsonb sections
        jsonb branding
        uuid default_sla_id FK
        uuid default_terms_id FK
        timestamptz deleted_at
    }
    proposals__proposals {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        text number
        text title
        uuid crm_company_id "logical ref crm.companies"
        uuid deal_id "logical ref crm.deals"
        uuid template_id FK
        uuid current_version_id FK
        text status
        uuid owner_user_id "logical ref identity.users"
        uuid created_by "logical ref identity.users"
        timestamptz deleted_at
    }
    proposals__proposal_versions {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid proposal_id FK
        int version
        text status
        jsonb content
        jsonb commercial_snapshot
        bytea snapshot_hash
        char currency
        bigint setup_total_minor
        bigint mrr_total_minor
        int term_months
        bigint tcv_minor
        date valid_until
        uuid pdf_file_id "logical ref files.objects"
        timestamptz issued_at
        uuid ai_request_id "logical ref integration.ai_requests"
        uuid created_by "logical ref identity.users"
    }
    proposals__proposal_line_items {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid version_id FK
        uuid catalog_item_id FK
        int position
        text name
        text pricing_model
        int quantity
        bigint unit_price_minor
        bigint setup_fee_minor
        int discount_bps
        bigint recurring_total_minor
    }
    proposals__approvals {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        uuid version_id FK
        text status
        bytea content_hash
        uuid requested_by "logical ref identity.users"
        uuid decided_by "logical ref identity.users"
        timestamptz decided_at
        text reason
    }
    proposals__share_links {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid version_id FK
        citext recipient_email
        bytea token_hash
        timestamptz expires_at
        timestamptz revoked_at
    }
    proposals__view_events {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        uuid share_link_id FK
        timestamptz viewed_at
        bytea ip_hash
    }
    proposals__signature_requests {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid version_id FK
        text provider_key
        text provider_envelope_id
        text status
        uuid signed_file_id "logical ref files.objects"
        text idempotency_key
    }
    proposals__signature_events {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        uuid signature_request_id FK
        text provider_event_id
        text event_type
        timestamptz occurred_at
        jsonb detail
    }
```

## ERD 11. Visitor Intelligence

```mermaid
erDiagram
    visitor_intelligence__sites ||--o{ visitor_intelligence__visitors : "site_id"
    visitor_intelligence__sites ||--o{ visitor_intelligence__consent_records : "site_id"
    visitor_intelligence__sites ||--o{ visitor_intelligence__conversion_goals : "site_id"
    visitor_intelligence__sites ||--o{ visitor_intelligence__conversions : "site_id"
    visitor_intelligence__conversion_goals ||--o{ visitor_intelligence__conversions : "goal_id"
    visitor_intelligence__visitors |o--o{ visitor_intelligence__conversions : "visitor_id"
    visitor_intelligence__sites ||--o{ visitor_intelligence__business_identifications : "site_id"
    visitor_intelligence__sites ||--o{ visitor_intelligence__daily_rollups : "site_id"
    visitor_intelligence__sites {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        citext domain
        text site_key
        text_array allowed_origins
        text consent_mode
        int retention_days
        text ip_handling
        text status
        timestamptz last_event_at
        timestamptz deleted_at
    }
    visitor_intelligence__visitors {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid site_id FK
        bytea anonymous_id_hash
        timestamptz first_seen_at
        timestamptz last_seen_at
        uuid contact_id FK "FK shared.contacts"
    }
    visitor_intelligence__sessions {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        uuid site_id "logical ref visitor_intelligence.sites"
        uuid visitor_id "logical ref visitor_intelligence.visitors"
        timestamptz started_at PK
        timestamptz ended_at
        text landing_url
        text referrer_host
        jsonb utm
        text device_class
        text consent_state
    }
    visitor_intelligence__events {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        uuid site_id "logical ref visitor_intelligence.sites"
        uuid session_id "logical ref visitor_intelligence.sessions"
        text event_type
        text url
        jsonb properties
        timestamptz occurred_at PK
    }
    visitor_intelligence__consent_records {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        uuid site_id FK
        bytea anonymous_id_hash
        text state
        text mechanism
        text policy_version
    }
    visitor_intelligence__conversion_goals {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid site_id FK
        text name
        text kind
        jsonb rule
        timestamptz deleted_at
    }
    visitor_intelligence__conversions {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        uuid site_id FK
        uuid goal_id FK
        uuid session_id "logical ref visitor_intelligence.sessions"
        uuid visitor_id FK
        uuid contact_id FK "FK shared.contacts"
        jsonb attribution
        text dedupe_key
        timestamptz occurred_at
    }
    visitor_intelligence__business_identifications {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        uuid site_id FK
        uuid session_id "logical ref visitor_intelligence.sessions"
        uuid company_id FK "FK shared.companies"
        text provider_key
        numeric confidence
        timestamptz observed_at
    }
    visitor_intelligence__daily_rollups {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid site_id FK
        date day
        text dimension
        text dimension_value
        int sessions
        int visitors
        int page_views
        int conversions
    }
```

## ERD 12. Chatbot

```mermaid
erDiagram
    chatbot__bots ||--o{ chatbot__knowledge_sources : "bot_id"
    chatbot__bots ||--o{ chatbot__knowledge_chunks : "bot_id"
    chatbot__knowledge_sources ||--o{ chatbot__knowledge_chunks : "source_id"
    chatbot__bots ||--o{ chatbot__qualification_rules : "bot_id"
    chatbot__bots ||--o{ chatbot__booking_configs : "bot_id"
    chatbot__bots ||--o{ chatbot__conversations : "bot_id"
    chatbot__conversations ||--o{ chatbot__messages : "conversation_id"
    chatbot__conversations ||--o{ chatbot__qualifications : "conversation_id"
    chatbot__conversations ||--o{ chatbot__handoffs : "conversation_id"
    chatbot__conversations ||--o{ chatbot__bookings : "conversation_id"
    chatbot__booking_configs ||--o{ chatbot__bookings : "config_id"
    chatbot__bots ||--o{ chatbot__daily_stats : "bot_id"
    chatbot__bots {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        text name
        text public_key
        text_array allowed_origins
        jsonb persona
        text disclosure_text
        text status
        timestamptz deleted_at
    }
    chatbot__knowledge_sources {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid bot_id FK
        text kind
        text title
        uuid file_id "logical ref files.objects"
        text url
        bytea content_hash
        text status
        int version
        timestamptz deleted_at
    }
    chatbot__knowledge_chunks {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid bot_id FK
        uuid source_id FK
        int position
        text content
        vector embedding
        int token_count
    }
    chatbot__qualification_rules {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid bot_id FK
        int position
        text field_key
        text question
        int weight
        boolean required
        timestamptz deleted_at
    }
    chatbot__booking_configs {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid bot_id FK
        uuid connection_id "logical ref integration.connections"
        int duration_minutes
        int buffer_minutes
        int minimum_notice_minutes
        jsonb availability
        timestamptz deleted_at
    }
    chatbot__conversations {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid bot_id FK
        bytea session_token_hash
        uuid visitor_ref "logical ref visitor_intelligence.visitors"
        uuid contact_id FK "FK shared.contacts"
        text status
        timestamptz last_message_at
    }
    chatbot__messages {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        uuid conversation_id FK
        text role
        text content
        uuid_array retrieved_chunk_ids
        text_array safety_flags
        uuid ai_request_id "logical ref integration.ai_requests"
        uuid sender_user_id "logical ref identity.users"
    }
    chatbot__qualifications {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid conversation_id FK
        jsonb answers
        int score
        text status
        uuid crm_company_id "logical ref crm.companies"
    }
    chatbot__handoffs {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid conversation_id FK
        text reason
        text status
        uuid assigned_user_id "logical ref identity.users"
        timestamptz resolved_at
    }
    chatbot__bookings {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid conversation_id FK
        uuid config_id FK
        uuid contact_id FK "FK shared.contacts"
        timestamptz starts_at
        timestamptz ends_at
        text status
        text provider_event_id
        uuid crm_meeting_id "logical ref crm.meetings"
        text idempotency_key
    }
    chatbot__daily_stats {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid bot_id FK
        date day
        int conversations
        int qualified
        int handoffs
        int bookings
    }
```

## ERD 13. Customer Expansion

```mermaid
erDiagram
    customer_expansion__customer_profiles ||--o{ customer_expansion__contracts : "customer_profile_id"
    customer_expansion__customer_profiles ||--o{ customer_expansion__service_inventory : "customer_profile_id"
    customer_expansion__contracts |o--o{ customer_expansion__service_inventory : "contract_id"
    customer_expansion__contracts ||--o{ customer_expansion__renewal_alerts : "contract_id"
    customer_expansion__customer_profiles ||--o{ customer_expansion__health_indicators : "customer_profile_id"
    customer_expansion__customer_profiles ||--o{ customer_expansion__service_gaps : "customer_profile_id"
    customer_expansion__customer_profiles ||--o{ customer_expansion__recommendations : "customer_profile_id"
    customer_expansion__recommendations ||--o{ customer_expansion__recommendation_evidence : "recommendation_id"
    customer_expansion__recommendations ||--o{ customer_expansion__revenue_opportunities : "recommendation_id"
    customer_expansion__customer_profiles {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid crm_company_id "logical ref crm.companies"
        uuid company_id FK "FK shared.companies"
        text health_status
        timestamptz health_as_of
        timestamptz deleted_at
    }
    customer_expansion__contracts {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid customer_profile_id FK
        uuid proposal_version_id "logical ref proposals.proposal_versions"
        uuid deal_id "logical ref crm.deals"
        text status
        date start_date
        date end_date
        int term_months
        boolean auto_renew
        int notice_days
        char currency
        bigint mrr_minor
        bigint setup_minor
        text source
    }
    customer_expansion__service_inventory {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid customer_profile_id FK
        uuid tenant_service_id "logical ref shared.tenant_services"
        uuid contract_id FK
        text coverage
        int quantity
        bigint mrr_minor
        char currency
        text source
        timestamptz deleted_at
    }
    customer_expansion__renewal_alerts {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid contract_id FK
        int window_days
        date due_date
        text status
        uuid acknowledged_by "logical ref identity.users"
    }
    customer_expansion__health_indicators {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        uuid customer_profile_id FK
        text indicator_key
        jsonb value
        text polarity
        jsonb evidence
        timestamptz as_of
    }
    customer_expansion__service_gaps {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid customer_profile_id FK
        uuid tenant_service_id "logical ref shared.tenant_services"
        text gap_state
        uuid analysis_run_id
    }
    customer_expansion__recommendations {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid customer_profile_id FK
        uuid tenant_service_id "logical ref shared.tenant_services"
        text kind
        text rationale
        bigint estimated_mrr_minor
        char currency
        text evidence_class
        text status
        uuid ai_request_id "logical ref integration.ai_requests"
        uuid decided_by "logical ref identity.users"
    }
    customer_expansion__recommendation_evidence {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid recommendation_id FK
        text kind
        text reference_type
        uuid reference_id
        jsonb detail
    }
    customer_expansion__revenue_opportunities {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid recommendation_id FK
        uuid deal_id "logical ref crm.deals"
        bigint estimated_mrr_minor
        bigint realised_mrr_minor
        char currency
        text status
    }
```

## ERD 14. Sales Intelligence

```mermaid
erDiagram
    sales_intelligence__forecast_runs ||--o{ sales_intelligence__forecast_points : "run_id"
    sales_intelligence__report_definitions |o--o{ sales_intelligence__report_exports : "definition_id"
    sales_intelligence__fact_lead_events {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        uuid source_event_id
        text event_type
        uuid prospect_ref
        text source_key
        int score
        timestamptz occurred_at
    }
    sales_intelligence__fact_outreach_events {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        uuid source_event_id
        text event_type
        uuid campaign_ref
        timestamptz occurred_at
    }
    sales_intelligence__fact_deal_events {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        uuid source_event_id
        text event_type
        uuid deal_ref
        uuid proposal_ref
        uuid owner_user_ref
        text stage_key
        text source_module
        bigint amount_minor
        char currency
        timestamptz occurred_at
    }
    sales_intelligence__fact_revenue_events {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        uuid source_event_id
        text movement
        uuid contract_ref
        bigint mrr_delta_minor
        char currency
        bigint reporting_mrr_delta_minor
        numeric fx_rate
        timestamptz occurred_at
    }
    sales_intelligence__cost_entries {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        date period_start
        date period_end
        text category
        uuid campaign_ref
        bigint amount_minor
        char currency
        uuid created_by "logical ref identity.users"
        timestamptz deleted_at
    }
    sales_intelligence__metric_snapshots {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        text metric_key
        text period_grain
        date period_start
        text dimension
        text dimension_value
        numeric value
        text value_kind
        timestamptz computed_at
    }
    sales_intelligence__forecast_runs {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        text kind
        text method_version
        jsonb assumptions
        text status
        date as_of
        uuid job_id "logical ref jobs.jobs"
    }
    sales_intelligence__forecast_points {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid run_id FK
        date period_start
        bigint expected_minor
        bigint low_minor
        bigint high_minor
        char currency
    }
    sales_intelligence__report_definitions {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        text name
        jsonb definition
        uuid created_by "logical ref identity.users"
        timestamptz deleted_at
    }
    sales_intelligence__report_exports {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid definition_id FK
        uuid file_id "logical ref files.objects"
        text status
        uuid created_by "logical ref identity.users"
    }
    sales_intelligence__ai_insights {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        text scope
        jsonb content
        uuid_array cited_metric_ids
        uuid ai_request_id "logical ref integration.ai_requests"
    }
```

## ERD 15. Shared integrations, jobs, files, audit and notifications

### `integration`

```mermaid
erDiagram
    integration__providers ||--o{ integration__platform_credentials : "provider_id"
    integration__providers ||--o{ integration__connections : "provider_id"
    integration__providers ||--o{ integration__webhook_endpoints : "provider_id"
    integration__connections |o--o{ integration__webhook_endpoints : "connection_id"
    integration__webhook_endpoints ||--o{ integration__webhook_deliveries : "endpoint_id"
    integration__providers ||--o{ integration__provider_call_logs : "provider_id"
    integration__providers ||--o{ integration__ai_model_routes : "provider_id"
    integration__ai_model_routes |o--o{ integration__ai_model_routes : "fallback_route_id"
    integration__prompt_templates ||--o{ integration__prompt_versions : "template_id"
    integration__prompt_versions |o--o{ integration__ai_requests : "prompt_version_id"
    integration__providers |o--o{ integration__ai_requests : "provider_id"
    integration__providers {
        uuid id PK
        timestamptz created_at
        timestamptz updated_at
        text key
        text kind
        text name
        jsonb capabilities
        boolean tenant_connectable
        text status
    }
    integration__platform_credentials {
        uuid id PK
        timestamptz created_at
        timestamptz updated_at
        uuid provider_id FK
        text label
        bytea secret_ciphertext
        text key_id
        timestamptz rotated_at
        timestamptz expires_at
    }
    integration__connections {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid provider_id FK
        text display_name
        text account_ref
        text_array scopes
        bytea secret_ciphertext
        text key_id
        timestamptz token_expires_at
        text status
        timestamptz last_health_at
        text last_error_code
        uuid created_by "logical ref identity.users"
    }
    integration__webhook_endpoints {
        uuid id PK
        timestamptz created_at
        timestamptz updated_at
        uuid tenant_id FK "FK identity.tenants"
        uuid provider_id FK
        uuid connection_id FK
        text path_token
        bytea secret_ciphertext
        text key_id
        text status
    }
    integration__webhook_deliveries {
        uuid id PK
        timestamptz created_at
        timestamptz updated_at
        uuid tenant_id FK "FK identity.tenants"
        uuid endpoint_id FK
        text provider_event_id
        boolean signature_verified
        bytea payload_hash
        jsonb payload
        text status
        int attempts
        text last_error
        timestamptz received_at
    }
    integration__provider_call_logs {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        uuid provider_id FK
        text operation
        text outcome
        int status_code
        int latency_ms
        uuid correlation_id
        timestamptz created_at
    }
    integration__ai_model_routes {
        uuid id PK
        timestamptz created_at
        timestamptz updated_at
        text task_key
        uuid provider_id FK
        text model
        uuid fallback_route_id FK
        int max_input_tokens
        int max_output_tokens
        text status
    }
    integration__prompt_templates {
        uuid id PK
        timestamptz created_at
        timestamptz updated_at
        text key
        text task_key
        text action_class
        jsonb output_schema
        text status
    }
    integration__prompt_versions {
        uuid id PK
        timestamptz created_at
        timestamptz updated_at
        uuid template_id FK
        int version
        text system_text
        text user_text
        text status
    }
    integration__ai_requests {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        text module_key
        text task_key
        uuid prompt_version_id FK
        uuid provider_id FK
        text model
        int input_tokens
        int output_tokens
        bigint cost_estimate_minor
        bigint credits_charged
        text outcome
        boolean validation_passed
        bytea input_hash
        int latency_ms
        uuid correlation_id
        text idempotency_key
    }
```

### `jobs`

```mermaid
erDiagram
    jobs__jobs ||--o{ jobs__job_attempts : "job_id"
    jobs__jobs {
        uuid id PK
        timestamptz created_at
        timestamptz updated_at
        uuid tenant_id FK "FK identity.tenants"
        text queue
        text kind
        jsonb payload
        int payload_version
        text status
        int priority
        timestamptz run_at
        int attempt
        int max_attempts
        text idempotency_key
        text locked_by
        timestamptz lock_expires_at
        jsonb progress
        timestamptz cancel_requested_at
        uuid correlation_id
        text last_error
        timestamptz finished_at
    }
    jobs__job_attempts {
        uuid id PK
        timestamptz created_at
        timestamptz updated_at
        uuid job_id FK
        int attempt
        text worker_id
        timestamptz started_at
        timestamptz finished_at
        text outcome
        text error
    }
    jobs__job_schedules {
        uuid id PK
        timestamptz created_at
        timestamptz updated_at
        uuid tenant_id FK "FK identity.tenants"
        text key
        text cron
        text timezone
        text kind
        jsonb payload
        timestamptz next_run_at
        timestamptz last_run_at
        boolean enabled
    }
    jobs__dead_letters {
        uuid id PK
        timestamptz created_at
        timestamptz updated_at
        uuid tenant_id FK "FK identity.tenants"
        text origin
        uuid origin_id
        text kind
        jsonb payload
        text error
        text resolution
        uuid resolved_by "logical ref identity.platform_admins"
        timestamptz resolved_at
    }
    jobs__outbox_events {
        uuid event_id PK
        text event_type
        int schema_version
        uuid tenant_id "logical ref identity.tenants"
        text source_module
        text aggregate_type
        uuid aggregate_id
        timestamptz occurred_at PK
        uuid correlation_id
        uuid causation_id
        jsonb payload
        timestamptz published_at
        bigint sequence
    }
    jobs__event_deliveries {
        uuid id PK
        timestamptz created_at
        timestamptz updated_at
        uuid tenant_id "logical ref identity.tenants"
        uuid event_id "logical ref jobs.outbox_events"
        text consumer
        text status
        int attempts
        timestamptz next_attempt_at
        text last_error
        timestamptz processed_at
    }
```

### `files`

```mermaid
erDiagram
    files__objects ||--o{ files__object_references : "object_id"
    files__objects ||--o{ files__access_grants : "object_id"
    files__access_policies ||--o{ files__access_grants : "policy_id"
    files__objects {
        uuid id PK
        timestamptz created_at
        timestamptz updated_at
        uuid tenant_id FK "FK identity.tenants"
        text bucket
        text object_key
        bytea sha256
        bigint size_bytes
        text content_type
        text original_name
        text status
        text classification
        text owner_module
        timestamptz retention_until
        uuid uploaded_by "logical ref identity.users"
        timestamptz deleted_at
    }
    files__object_references {
        uuid id PK
        timestamptz created_at
        timestamptz updated_at
        uuid object_id FK
        uuid tenant_id FK "FK identity.tenants"
        text owner_module
        text owner_type
        uuid owner_id
    }
    files__access_policies {
        uuid id PK
        timestamptz created_at
        timestamptz updated_at
        text key
        boolean requires_session
        text_array allowed_permissions
        int max_link_ttl_seconds
    }
    files__access_grants {
        uuid id PK
        timestamptz created_at
        timestamptz updated_at
        uuid tenant_id FK "FK identity.tenants"
        uuid object_id FK
        uuid policy_id FK
        bytea token_hash
        timestamptz expires_at
        int max_downloads
        int download_count
    }
```

### `audit`

```mermaid
erDiagram
    audit__events {
        uuid id PK
        uuid tenant_id "logical ref identity.tenants"
        text actor_type
        uuid actor_id
        text action
        text resource_type
        uuid resource_id
        text outcome
        bytea ip_hash
        text user_agent
        uuid correlation_id
        jsonb before
        jsonb after
        bigint chain_seq
        bytea prev_hash
        bytea row_hash
        timestamptz occurred_at PK
    }
    audit__security_events {
        uuid id PK
        uuid tenant_id "logical ref identity.tenants"
        text event_type
        text severity
        text actor_ref
        bytea ip_hash
        jsonb detail
        uuid correlation_id
        timestamptz occurred_at PK
    }
    audit__support_access_grants {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        uuid admin_id "logical ref identity.platform_admins"
        text reason
        text scope
        text ticket_ref
        timestamptz expires_at
        timestamptz revoked_at
    }
```

### `notification`

```mermaid
erDiagram
    notification__notifications ||--o{ notification__delivery_attempts : "notification_id"
    notification__templates {
        uuid id PK
        timestamptz created_at
        timestamptz updated_at
        text key
        text channel
        text locale
        text subject
        text body
        int version
    }
    notification__preferences {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid user_id "logical ref identity.users"
        text notification_key
        text channel
        boolean enabled
    }
    notification__notifications {
        uuid id PK
        timestamptz created_at
        timestamptz updated_at
        uuid tenant_id FK "FK identity.tenants"
        text recipient_type
        uuid recipient_id
        text notification_key
        text title
        text body
        text link
        timestamptz read_at
        text idempotency_key
    }
    notification__delivery_attempts {
        uuid id PK
        timestamptz created_at
        timestamptz updated_at
        uuid notification_id FK
        text channel
        text provider_key
        text status
        int attempt
        text error_code
    }
```
