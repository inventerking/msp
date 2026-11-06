# Constraints and indexes

Status: **planning, pending approval.** Each entry is design intent for later migrations.

## Rules that apply to every table

1. Every tenant-owned table has `UNIQUE (tenant_id, id)` so that child tables can use composite foreign keys. A child row therefore cannot reference another tenant's parent, whatever the application does.
2. Every tenant-owned table has RLS enabled and forced (see [multi-tenancy](../architecture/multi-tenancy.md)).
3. Every foreign key column has a supporting index, listed below as *FK support*.
4. `SET NULL` on a composite foreign key names only the referencing column, `ON DELETE SET NULL (col)`, because `tenant_id` must stay set. This needs PostgreSQL 15 or later.
5. Status columns use `CHECK` constraints rather than PostgreSQL enums, because removing an enum value is awkward in a long-lived schema.
6. Partitioned tables include the partition key in their primary key, and nothing has a foreign key into them.
7. Partial unique indexes on soft-deleted tables include `WHERE deleted_at IS NULL`, so a deleted row does not block re-creation.

## Boundary rule

A foreign key may cross a schema boundary only to `identity.tenants`, `shared.companies` or `shared.contacts`. All other cross-schema references are logical refs validated by the owning module's service.

## `platform`

### `platform.settings`

- **Primary key:** `(key)`

### `platform.modules`

- **Primary key:** `(id)`
- **Unique:** `(key)`
- **Check:** `status IN ('available','hidden','retired')`

### `platform.module_dependencies`

- **Primary key:** `(module_id, depends_on_module_id)`
- **FK:** `module_id → platform.modules(id) ON DELETE CASCADE`
- **FK:** `depends_on_module_id → platform.modules(id) ON DELETE RESTRICT`
- **Check:** `kind IN ('requires','enhances')`
- **Check:** `module_id <> depends_on_module_id`
- **FK support:** `(module_id)`
- **FK support:** `(depends_on_module_id)`

### `platform.products`

- **Primary key:** `(id)`
- **Unique:** `(key)`
- **Check:** `kind IN ('module','bundle','suite','addon')`
- **Check:** `status IN ('draft','active','retired')`

### `platform.product_versions`

- **Primary key:** `(id)`
- **FK:** `product_id → platform.products(id) ON DELETE RESTRICT`
- **Unique:** `(product_id, version)`
- **Check:** `status IN ('draft','published','retired')`
- **FK support:** `(product_id)`

### `platform.product_version_modules`

- **Primary key:** `(product_version_id, module_id)`
- **FK:** `product_version_id → platform.product_versions(id) ON DELETE CASCADE`
- **FK:** `module_id → platform.modules(id) ON DELETE RESTRICT`
- **FK support:** `(product_version_id)`
- **FK support:** `(module_id)`

### `platform.plans`

- **Primary key:** `(id)`
- **FK:** `product_version_id → platform.product_versions(id) ON DELETE RESTRICT`
- **Unique:** `(key)`
- **Check:** `billing_period IN ('monthly','annual','custom')`
- **Check:** `trial_days >= 0`
- **Check:** `status IN ('draft','active','retired')`
- **FK support:** `(product_version_id)`

### `platform.prices`

- **Primary key:** `(id)`
- **FK:** `plan_id → platform.plans(id) ON DELETE RESTRICT`
- **Unique:** `(plan_id, currency, valid_from)`
- **Check:** `amount_minor >= 0`
- **FK support:** `(plan_id)`

### `platform.promotions`

- **Primary key:** `(id)`
- **Unique:** `(code)`
- **Check:** `kind IN ('percent','fixed','trial_extension')`
- **Check:** `status IN ('draft','active','retired')`

### `platform.promotion_redemptions`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `promotion_id → platform.promotions(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, purchase_request_id) → platform.purchase_requests(tenant_id, id) ON DELETE RESTRICT`
- **Unique:** `(tenant_id, promotion_id, purchase_request_id)`
- **FK support:** `(tenant_id, promotion_id)`
- **FK support:** `(tenant_id, purchase_request_id)`

### `platform.purchase_requests`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `plan_id → platform.plans(id) ON DELETE RESTRICT`
- **FK:** `promotion_id → platform.promotions(id) ON DELETE RESTRICT`
- **Unique:** `(tenant_id, idempotency_key)`
- **Check:** `status IN ('draft','submitted','payment_pending','payment_verified','under_review','approved','rejected','cancelled')`
- **Check:** `quantity > 0`
- **Index:** `(status, created_at)`
- **FK support:** `(tenant_id, plan_id)`
- **FK support:** `(tenant_id, promotion_id)`

### `platform.subscriptions`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `plan_id → platform.plans(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, purchase_request_id) → platform.purchase_requests(tenant_id, id) ON DELETE RESTRICT`
- **Check:** `status IN ('pending_activation','trialing','active','past_due','suspended','cancelled','expired')`
- **Index:** `(status, current_period_end)`
- **FK support:** `(tenant_id, plan_id)`
- **FK support:** `(tenant_id, purchase_request_id)`

### `platform.entitlement_grants`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `module_id → platform.modules(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, subscription_id) → platform.subscriptions(tenant_id, id) ON DELETE RESTRICT`
- **Unique:** `(tenant_id, module_id, subscription_id) NULLS DISTINCT`
- **Check:** `source IN ('subscription','trial','admin_override')`
- **Check:** `status IN ('pending','active','suspended','expired','revoked')`
- **Check:** `source <> 'subscription' OR subscription_id IS NOT NULL`
- **FK support:** `(tenant_id, module_id)`
- **FK support:** `(tenant_id, subscription_id)`

### `platform.tenant_module_entitlements`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `module_id → platform.modules(id) ON DELETE RESTRICT`
- **Unique:** `(tenant_id, module_id)`
- **Check:** `status IN ('inactive','active','suspended','expired','revoked')`
- **FK support:** `(tenant_id, module_id)`

### `platform.entitlement_status_history`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `module_id → platform.modules(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, grant_id) → platform.entitlement_grants(tenant_id, id) ON DELETE RESTRICT`
- **Check:** `actor_type IN ('platform_admin','system')`
- **FK support:** `(tenant_id, module_id)`
- **FK support:** `(tenant_id, grant_id)`

### `platform.invoices`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, subscription_id) → platform.subscriptions(tenant_id, id) ON DELETE RESTRICT`
- **Unique:** `(number)`
- **Check:** `status IN ('draft','issued','paid','void','uncollectible')`
- **Check:** `total_minor = subtotal_minor + tax_minor`
- **FK support:** `(tenant_id, subscription_id)`

### `platform.invoice_lines`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, invoice_id) → platform.invoices(tenant_id, id) ON DELETE CASCADE`
- **FK support:** `(tenant_id, invoice_id)`

### `platform.payments`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, invoice_id) → platform.invoices(tenant_id, id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, purchase_request_id) → platform.purchase_requests(tenant_id, id) ON DELETE RESTRICT`
- **Unique:** `(provider, provider_payment_ref)`
- **Check:** `method IN ('card','bank_transfer','manual','other')`
- **Check:** `status IN ('pending','succeeded','failed','refunded','disputed')`
- **FK support:** `(tenant_id, invoice_id)`
- **FK support:** `(tenant_id, purchase_request_id)`

### `platform.usage_meters`

- **Primary key:** `(id)`
- **Unique:** `(key)`
- **Check:** `status IN ('active','retired')`

### `platform.credit_accounts`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `meter_id → platform.usage_meters(id) ON DELETE RESTRICT`
- **Unique:** `(tenant_id, meter_id)`
- **Check:** `balance >= 0 AND reserved >= 0 AND reserved <= balance`
- **FK support:** `(tenant_id, meter_id)`

### `platform.credit_ledger`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, account_id) → platform.credit_accounts(tenant_id, id) ON DELETE RESTRICT`
- **Unique:** `(tenant_id, idempotency_key)`
- **Check:** `entry_type IN ('grant','reservation','commit','release','expiry','adjustment','refund')`
- **FK support:** `(tenant_id, account_id)`

### `platform.tenant_usage_limits`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `meter_id → platform.usage_meters(id) ON DELETE RESTRICT`
- **Unique:** `(tenant_id, meter_id, period)`
- **Check:** `period IN ('day','month','billing_period')`
- **FK support:** `(tenant_id, meter_id)`

### `platform.usage_rollups`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `meter_id → platform.usage_meters(id) ON DELETE RESTRICT`
- **Unique:** `(tenant_id, meter_id, module_key, period_start)`
- **FK support:** `(tenant_id, meter_id)`

## `identity`

### `identity.tenants`

- **Primary key:** `(id)`
- **Unique:** `(slug)`
- **Check:** `status IN ('pending','active','suspended','offboarding','deleted')`
- **Index:** `(status)`

### `identity.tenant_profiles`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **Unique:** `(tenant_id)`
- **Check:** `onboarding_status IN ('not_started','in_progress','complete')`

### `identity.tenant_icps`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **Unique:** `(tenant_id) WHERE is_default AND deleted_at IS NULL`
- **Check:** `min_employees IS NULL OR max_employees IS NULL OR min_employees <= max_employees`

### `identity.users`

- **Primary key:** `(id)`
- **Unique:** `(email) WHERE deleted_at IS NULL`
- **Check:** `status IN ('pending_verification','active','locked','disabled')`

### `identity.user_credentials`

- **Primary key:** `(user_id)`
- **FK:** `user_id → identity.users(id) ON DELETE CASCADE`
- **FK support:** `(user_id)`

### `identity.memberships`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `user_id → identity.users(id) ON DELETE RESTRICT`
- **FK:** `role_id → identity.roles(id) ON DELETE RESTRICT`
- **Unique:** `(tenant_id, user_id)`
- **Check:** `status IN ('invited','active','suspended','removed')`
- **Index:** `(user_id, status)`
- **FK support:** `(tenant_id, user_id)`
- **FK support:** `(tenant_id, role_id)`

### `identity.roles`

- **Primary key:** `(id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE CASCADE`
- **Unique:** `(tenant_id, key) NULLS NOT DISTINCT`
- **Check:** `is_system = (tenant_id IS NULL)`

### `identity.permissions`

- **Primary key:** `(id)`
- **Unique:** `(key)`

### `identity.role_permissions`

- **Primary key:** `(role_id, permission_id)`
- **FK:** `role_id → identity.roles(id) ON DELETE CASCADE`
- **FK:** `permission_id → identity.permissions(id) ON DELETE RESTRICT`
- **FK support:** `(role_id)`
- **FK support:** `(permission_id)`

### `identity.invitations`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `role_id → identity.roles(id) ON DELETE RESTRICT`
- **Unique:** `(token_hash)`
- **Unique:** `(tenant_id, email) WHERE accepted_at IS NULL AND revoked_at IS NULL`
- **FK support:** `(tenant_id, role_id)`

### `identity.sessions`

- **Primary key:** `(id)`
- **FK:** `user_id → identity.users(id) ON DELETE CASCADE`
- **FK:** `active_tenant_id → identity.tenants(id) ON DELETE SET NULL`
- **Unique:** `(token_hash)`
- **Index:** `(user_id) WHERE revoked_at IS NULL`
- **Index:** `(expires_at)`
- **FK support:** `(user_id)`
- **FK support:** `(active_tenant_id)`

### `identity.verification_tokens`

- **Primary key:** `(id)`
- **FK:** `user_id → identity.users(id) ON DELETE CASCADE`
- **Unique:** `(token_hash)`
- **Check:** `purpose IN ('email_verify','password_reset','email_change')`
- **FK support:** `(user_id)`

### `identity.mfa_factors`

- **Primary key:** `(id)`
- **FK:** `user_id → identity.users(id) ON DELETE CASCADE`
- **Check:** `kind IN ('totp','webauthn')`
- **FK support:** `(user_id)`

### `identity.mfa_recovery_codes`

- **Primary key:** `(id)`
- **FK:** `user_id → identity.users(id) ON DELETE CASCADE`
- **Unique:** `(user_id, code_hash)`
- **FK support:** `(user_id)`

### `identity.login_attempts`

- **Primary key:** `(id)`
- **Check:** `domain IN ('tenant','platform')`
- **Index:** `(email_hash, created_at DESC)`
- **Index:** `(ip_hash, created_at DESC)`

### `identity.platform_admins`

- **Primary key:** `(id)`
- **Unique:** `(email)`
- **Check:** `status IN ('invited','active','disabled')`

### `identity.platform_admin_role_assignments`

- **Primary key:** `(admin_id, role)`
- **FK:** `admin_id → identity.platform_admins(id) ON DELETE CASCADE`
- **FK:** `granted_by → identity.platform_admins(id) ON DELETE SET NULL`
- **Check:** `role IN ('super_admin','platform_support','billing_admin','integration_admin','platform_analyst')`
- **FK support:** `(admin_id)`
- **FK support:** `(granted_by)`

### `identity.platform_admin_mfa_factors`

- **Primary key:** `(id)`
- **FK:** `admin_id → identity.platform_admins(id) ON DELETE CASCADE`
- **Check:** `kind IN ('totp','webauthn')`
- **FK support:** `(admin_id)`

### `identity.platform_admin_sessions`

- **Primary key:** `(id)`
- **FK:** `admin_id → identity.platform_admins(id) ON DELETE CASCADE`
- **Unique:** `(token_hash)`
- **FK support:** `(admin_id)`

## `shared`

### `shared.countries`

- **Primary key:** `(code)`

### `shared.industries`

- **Primary key:** `(id)`
- **FK:** `parent_id → shared.industries(id) ON DELETE RESTRICT`
- **Unique:** `(code)`
- **FK support:** `(parent_id)`

### `shared.companies`

- **Primary key:** `(id)`
- **FK:** `industry_id → shared.industries(id) ON DELETE RESTRICT`
- **FK:** `merged_into_id → shared.companies(id) ON DELETE RESTRICT`
- **Unique:** `(primary_domain) WHERE status = 'active'`
- **Check:** `status IN ('active','merged','dissolved')`
- **Check:** `(status = 'merged') = (merged_into_id IS NOT NULL)`
- **Index:** `(lower(display_name))` (pg_trgm candidate, see D-17)
- **FK support:** `(industry_id)`
- **FK support:** `(merged_into_id)`

### `shared.company_identifiers`

- **Primary key:** `(id)`
- **FK:** `company_id → shared.companies(id) ON DELETE CASCADE`
- **Unique:** `(kind, value_normalized, provider_key) NULLS NOT DISTINCT`
- **Check:** `kind IN ('domain','registry_number','provider_id','social_url')`
- **FK support:** `(company_id)`

### `shared.company_source_facts`

- **Primary key:** `(id)`
- **FK:** `company_id → shared.companies(id) ON DELETE CASCADE`
- **Check:** `licence_class IN ('public','licensed_shareable')`
- **Check:** `confidence BETWEEN 0 AND 1`
- **Index:** `(company_id, attribute, observed_at DESC)`
- **FK support:** `(company_id)`

### `shared.company_merges`

- **Primary key:** `(id)`
- **FK:** `source_company_id → shared.companies(id) ON DELETE RESTRICT`
- **FK:** `target_company_id → shared.companies(id) ON DELETE RESTRICT`
- **FK support:** `(source_company_id)`
- **FK support:** `(target_company_id)`

### `shared.contacts`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `company_id → shared.companies(id) ON DELETE RESTRICT`
- **Unique:** `(tenant_id, email) WHERE email IS NOT NULL`
- **Check:** `status IN ('active','erased')`
- **Index:** `(tenant_id, company_id)`
- **Index:** `(tenant_id, email_hash)`
- **FK support:** `(tenant_id, company_id)`

### `shared.contact_sources`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, contact_id) → shared.contacts(tenant_id, id) ON DELETE CASCADE`
- **Check:** `licence_class IN ('public','licensed_tenant','licensed_shareable','tenant_provided','data_subject_provided')`
- **FK support:** `(tenant_id, contact_id)`

### `shared.contact_consents`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, contact_id) → shared.contacts(tenant_id, id) ON DELETE CASCADE`
- **Check:** `status IN ('granted','withdrawn','not_required','unknown')`
- **Index:** `(tenant_id, contact_id, purpose)`
- **FK support:** `(tenant_id, contact_id)`

### `shared.service_categories`

- **Primary key:** `(id)`
- **FK:** `parent_id → shared.service_categories(id) ON DELETE RESTRICT`
- **Unique:** `(key)`
- **FK support:** `(parent_id)`

### `shared.tenant_services`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `category_id → shared.service_categories(id) ON DELETE RESTRICT`
- **Unique:** `(tenant_id, name) WHERE deleted_at IS NULL`
- **Check:** `status IN ('active','retired')`
- **FK support:** `(tenant_id, category_id)`

### `shared.fx_rates`

- **Primary key:** `(id)`
- **Unique:** `(base_currency, quote_currency, rate_date, source_key)`
- **Check:** `rate > 0`

## `lead_generation`

### `lead_generation.saved_searches`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`

### `lead_generation.search_jobs`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, saved_search_id) → lead_generation.saved_searches(tenant_id, id) ON DELETE SET NULL (saved_search_id)`
- **Check:** `status IN ('queued','running','completed','failed','cancelled','partial')`
- **Check:** `progress_percent BETWEEN 0 AND 100`
- **Index:** `(tenant_id, created_at DESC)`
- **FK support:** `(tenant_id, saved_search_id)`

### `lead_generation.search_results`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, search_job_id) → lead_generation.search_jobs(tenant_id, id) ON DELETE CASCADE`
- **FK:** `company_id → shared.companies(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, promoted_prospect_id) → lead_generation.prospects(tenant_id, id) ON DELETE SET NULL (promoted_prospect_id)`
- **Unique:** `(tenant_id, search_job_id, company_id)`
- **FK support:** `(tenant_id, search_job_id)`
- **FK support:** `(tenant_id, company_id)`
- **FK support:** `(tenant_id, promoted_prospect_id)`

### `lead_generation.prospects`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `company_id → shared.companies(id) ON DELETE RESTRICT`
- **Unique:** `(tenant_id, company_id) WHERE deleted_at IS NULL`
- **Check:** `status IN ('new','enriching','qualified','disqualified','converted')`
- **Check:** `data_quality IN ('unknown','low','medium','high')`
- **Check:** `latest_score IS NULL OR latest_score BETWEEN 0 AND 100`
- **Index:** `(tenant_id, status, latest_score DESC)`
- **FK support:** `(tenant_id, company_id)`

### `lead_generation.prospect_contacts`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, prospect_id) → lead_generation.prospects(tenant_id, id) ON DELETE CASCADE`
- **FK:** `(tenant_id, contact_id) → shared.contacts(tenant_id, id) ON DELETE CASCADE`
- **Unique:** `(tenant_id, prospect_id, contact_id)`
- **FK support:** `(tenant_id, prospect_id)`
- **FK support:** `(tenant_id, contact_id)`

### `lead_generation.prospect_attributes`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, prospect_id) → lead_generation.prospects(tenant_id, id) ON DELETE CASCADE`
- **Check:** `licence_class IN ('public','licensed_tenant','licensed_shareable','tenant_provided')`
- **Index:** `(tenant_id, prospect_id, attribute, observed_at DESC)`
- **FK support:** `(tenant_id, prospect_id)`

### `lead_generation.enrichment_runs`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, prospect_id) → lead_generation.prospects(tenant_id, id) ON DELETE CASCADE`
- **Check:** `kind IN ('company','contact')`
- **Check:** `status IN ('queued','running','succeeded','no_data','failed','cancelled')`
- **FK support:** `(tenant_id, prospect_id)`

### `lead_generation.scoring_models`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **Unique:** `(tenant_id, name, version)`
- **Check:** `status IN ('draft','active','retired')`

### `lead_generation.prospect_scores`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, prospect_id) → lead_generation.prospects(tenant_id, id) ON DELETE CASCADE`
- **FK:** `(tenant_id, model_id) → lead_generation.scoring_models(tenant_id, id) ON DELETE RESTRICT`
- **Check:** `score BETWEEN 0 AND 100`
- **Check:** `coverage BETWEEN 0 AND 1`
- **Index:** `(tenant_id, prospect_id, created_at DESC)`
- **FK support:** `(tenant_id, prospect_id)`
- **FK support:** `(tenant_id, model_id)`

### `lead_generation.prospect_score_factors`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, score_id) → lead_generation.prospect_scores(tenant_id, id) ON DELETE CASCADE`
- **Unique:** `(tenant_id, score_id, factor_key)`
- **Check:** `state IN ('matched','partial','not_matched','unknown')`
- **Check:** `state <> 'unknown' OR earned IS NULL`
- **FK support:** `(tenant_id, score_id)`

### `lead_generation.prospect_lists`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **Unique:** `(tenant_id, name) WHERE deleted_at IS NULL`

### `lead_generation.prospect_list_members`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, list_id) → lead_generation.prospect_lists(tenant_id, id) ON DELETE CASCADE`
- **FK:** `(tenant_id, prospect_id) → lead_generation.prospects(tenant_id, id) ON DELETE CASCADE`
- **Unique:** `(tenant_id, list_id, prospect_id)`
- **FK support:** `(tenant_id, list_id)`
- **FK support:** `(tenant_id, prospect_id)`

### `lead_generation.import_jobs`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **Check:** `status IN ('uploaded','validating','importing','completed','failed','cancelled')`

### `lead_generation.export_jobs`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **Check:** `status IN ('queued','running','completed','failed','blocked_by_licence')`

## `competitor_intelligence`

### `competitor_intelligence.competitors`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `company_id → shared.companies(id) ON DELETE RESTRICT`
- **Unique:** `(tenant_id, company_id) WHERE deleted_at IS NULL`
- **Check:** `status IN ('candidate','tracked','archived')`
- **FK support:** `(tenant_id, company_id)`

### `competitor_intelligence.research_configs`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, competitor_id) → competitor_intelligence.competitors(tenant_id, id) ON DELETE CASCADE`
- **Check:** `cadence IN ('manual','weekly','monthly')`
- **FK support:** `(tenant_id, competitor_id)`

### `competitor_intelligence.research_runs`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, competitor_id) → competitor_intelligence.competitors(tenant_id, id) ON DELETE CASCADE`
- **FK:** `(tenant_id, config_id) → competitor_intelligence.research_configs(tenant_id, id) ON DELETE SET NULL (config_id)`
- **Check:** `status IN ('queued','running','completed','partial','failed','cancelled')`
- **FK support:** `(tenant_id, competitor_id)`
- **FK support:** `(tenant_id, config_id)`

### `competitor_intelligence.sources`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, competitor_id) → competitor_intelligence.competitors(tenant_id, id) ON DELETE CASCADE`
- **Unique:** `(tenant_id, competitor_id, url) WHERE deleted_at IS NULL`
- **Check:** `kind IN ('website','pricing_page','service_page','public_directory','provider')`
- **Check:** `access_status IN ('allowed','disallowed_by_robots','blocked','error','unchecked')`
- **FK support:** `(tenant_id, competitor_id)`

### `competitor_intelligence.source_snapshots`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, source_id) → competitor_intelligence.sources(tenant_id, id) ON DELETE CASCADE`
- **FK:** `(tenant_id, run_id) → competitor_intelligence.research_runs(tenant_id, id) ON DELETE SET NULL (run_id)`
- **FK support:** `(tenant_id, source_id)`
- **FK support:** `(tenant_id, run_id)`

### `competitor_intelligence.observations`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, competitor_id) → competitor_intelligence.competitors(tenant_id, id) ON DELETE CASCADE`
- **FK:** `(tenant_id, source_snapshot_id) → competitor_intelligence.source_snapshots(tenant_id, id) ON DELETE SET NULL (source_snapshot_id)`
- **FK:** `(tenant_id, superseded_by_id) → competitor_intelligence.observations(tenant_id, id) ON DELETE SET NULL (superseded_by_id)`
- **Check:** `attribute_type IN ('service','pricing','positioning','geography','industry','technology','other')`
- **Check:** `evidence_class IN ('verified_public','observed','inferred','ai_estimate')`
- **Check:** `evidence_class NOT IN ('verified_public','observed') OR source_snapshot_id IS NOT NULL`
- **Index:** `(tenant_id, competitor_id, attribute_type, observed_at DESC)`
- **FK support:** `(tenant_id, competitor_id)`
- **FK support:** `(tenant_id, source_snapshot_id)`
- **FK support:** `(tenant_id, superseded_by_id)`

### `competitor_intelligence.change_events`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, competitor_id) → competitor_intelligence.competitors(tenant_id, id) ON DELETE CASCADE`
- **FK:** `(tenant_id, previous_observation_id) → competitor_intelligence.observations(tenant_id, id) ON DELETE SET NULL (previous_observation_id)`
- **FK:** `(tenant_id, new_observation_id) → competitor_intelligence.observations(tenant_id, id) ON DELETE CASCADE`
- **FK support:** `(tenant_id, competitor_id)`
- **FK support:** `(tenant_id, previous_observation_id)`
- **FK support:** `(tenant_id, new_observation_id)`

### `competitor_intelligence.market_opportunities`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **Check:** `evidence_class IN ('verified_public','observed','inferred','ai_estimate')`
- **Check:** `status IN ('new','reviewing','pursuing','dismissed')`
- **Check:** `score IS NULL OR score BETWEEN 0 AND 100`

### `competitor_intelligence.opportunity_evidence`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, opportunity_id) → competitor_intelligence.market_opportunities(tenant_id, id) ON DELETE CASCADE`
- **FK:** `(tenant_id, observation_id) → competitor_intelligence.observations(tenant_id, id) ON DELETE SET NULL (observation_id)`
- **FK support:** `(tenant_id, opportunity_id)`
- **FK support:** `(tenant_id, observation_id)`

### `competitor_intelligence.comparison_sets`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`

### `competitor_intelligence.comparison_set_members`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, comparison_set_id) → competitor_intelligence.comparison_sets(tenant_id, id) ON DELETE CASCADE`
- **FK:** `(tenant_id, competitor_id) → competitor_intelligence.competitors(tenant_id, id) ON DELETE CASCADE`
- **Unique:** `(tenant_id, comparison_set_id, competitor_id)`
- **FK support:** `(tenant_id, comparison_set_id)`
- **FK support:** `(tenant_id, competitor_id)`

### `competitor_intelligence.reports`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **Check:** `kind IN ('competitor','market','comparison')`
- **Check:** `status IN ('queued','generating','ready','failed')`

## `outreach`

### `outreach.sending_domains`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **Unique:** `(tenant_id, domain) WHERE deleted_at IS NULL`
- **Check:** `spf_status IN ('unknown','pass','fail')`
- **Check:** `dkim_status IN ('unknown','pass','fail','not_detected')`
- **Check:** `dmarc_status IN ('unknown','pass','fail','none_policy')`

### `outreach.sender_accounts`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, sending_domain_id) → outreach.sending_domains(tenant_id, id) ON DELETE RESTRICT`
- **Unique:** `(tenant_id, from_email) WHERE deleted_at IS NULL`
- **Check:** `status IN ('pending','active','paused','disconnected')`
- **Check:** `daily_limit > 0`
- **Check:** `reputation_state IN ('healthy','warning','paused')`
- **FK support:** `(tenant_id, sending_domain_id)`

### `outreach.templates`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, current_version_id) → outreach.template_versions(tenant_id, id) ON DELETE SET NULL (current_version_id)`
- **FK support:** `(tenant_id, current_version_id)`

### `outreach.template_versions`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, template_id) → outreach.templates(tenant_id, id) ON DELETE CASCADE`
- **Unique:** `(tenant_id, template_id, version)`
- **FK support:** `(tenant_id, template_id)`

### `outreach.audiences`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **Check:** `kind IN ('static','dynamic')`

### `outreach.audience_members`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, audience_id) → outreach.audiences(tenant_id, id) ON DELETE CASCADE`
- **FK:** `(tenant_id, contact_id) → shared.contacts(tenant_id, id) ON DELETE CASCADE`
- **Unique:** `(tenant_id, audience_id, contact_id)`
- **FK support:** `(tenant_id, audience_id)`
- **FK support:** `(tenant_id, contact_id)`

### `outreach.campaigns`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, sender_account_id) → outreach.sender_accounts(tenant_id, id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, audience_id) → outreach.audiences(tenant_id, id) ON DELETE RESTRICT`
- **Check:** `status IN ('draft','pending_approval','approved','running','paused','completed','cancelled')`
- **Check:** `approval_mode IN ('campaign','per_message')`
- **FK support:** `(tenant_id, sender_account_id)`
- **FK support:** `(tenant_id, audience_id)`

### `outreach.sequence_steps`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, campaign_id) → outreach.campaigns(tenant_id, id) ON DELETE CASCADE`
- **FK:** `(tenant_id, template_version_id) → outreach.template_versions(tenant_id, id) ON DELETE RESTRICT`
- **Unique:** `(tenant_id, campaign_id, position)`
- **Check:** `delay_minutes >= 0`
- **FK support:** `(tenant_id, campaign_id)`
- **FK support:** `(tenant_id, template_version_id)`

### `outreach.approvals`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **Check:** `subject_type IN ('campaign','message')`
- **Check:** `status IN ('pending','approved','rejected','expired')`
- **Index:** `(tenant_id, status, created_at)`

### `outreach.enrollments`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, campaign_id) → outreach.campaigns(tenant_id, id) ON DELETE CASCADE`
- **FK:** `(tenant_id, contact_id) → shared.contacts(tenant_id, id) ON DELETE CASCADE`
- **Unique:** `(tenant_id, campaign_id, contact_id)`
- **Check:** `status IN ('active','paused','completed','stopped')`
- **Check:** `stop_reason IS NULL OR stop_reason IN ('reply','unsubscribe','hard_bounce','complaint','suppressed','manual','jurisdiction_block','campaign_cancelled')`
- **Index:** `(next_send_at) WHERE status = 'active'`
- **FK support:** `(tenant_id, campaign_id)`
- **FK support:** `(tenant_id, contact_id)`

### `outreach.messages`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, enrollment_id) → outreach.enrollments(tenant_id, id) ON DELETE CASCADE`
- **FK:** `(tenant_id, step_id) → outreach.sequence_steps(tenant_id, id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, approval_id) → outreach.approvals(tenant_id, id) ON DELETE RESTRICT`
- **Unique:** `(tenant_id, idempotency_key)`
- **Check:** `status IN ('draft','pending_approval','approved','queued','sending','sent','failed','cancelled')`
- **Index:** `(tenant_id, status, scheduled_at)`
- **FK support:** `(tenant_id, enrollment_id)`
- **FK support:** `(tenant_id, step_id)`
- **FK support:** `(tenant_id, approval_id)`

### `outreach.delivery_events`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, message_id) → outreach.messages(tenant_id, id) ON DELETE SET NULL (message_id)`
- **Unique:** `(tenant_id, provider_key, provider_event_id)`
- **Check:** `event_type IN ('accepted','delivered','deferred','soft_bounce','hard_bounce','complaint','open','click')`
- **FK support:** `(tenant_id, message_id)`

### `outreach.replies`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, enrollment_id) → outreach.enrollments(tenant_id, id) ON DELETE SET NULL (enrollment_id)`
- **FK:** `(tenant_id, message_id) → outreach.messages(tenant_id, id) ON DELETE SET NULL (message_id)`
- **Unique:** `(tenant_id, provider_message_id)`
- **Check:** `classification IN ('unclassified','interested','not_interested','out_of_office','unsubscribe_request','other')`
- **FK support:** `(tenant_id, enrollment_id)`
- **FK support:** `(tenant_id, message_id)`

### `outreach.suppressions`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **Unique:** `(tenant_id, email_hash)`
- **Check:** `reason IN ('unsubscribe','hard_bounce','complaint','manual','legal_request')`

### `outreach.global_suppressions`

- **Primary key:** `(id)`
- **Unique:** `(email_hmac)`
- **Check:** `reason IN ('complaint','legal_request','abuse')`

### `outreach.sending_quota_counters`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, sender_account_id) → outreach.sender_accounts(tenant_id, id) ON DELETE CASCADE`
- **Unique:** `(tenant_id, sender_account_id, day)`
- **FK support:** `(tenant_id, sender_account_id)`

### `outreach.campaign_stats`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, campaign_id) → outreach.campaigns(tenant_id, id) ON DELETE CASCADE`
- **Unique:** `(tenant_id, campaign_id, day)`
- **FK support:** `(tenant_id, campaign_id)`

## `it_assessment`

### `it_assessment.templates`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **Check:** `tier IN ('passive','exposure')`

### `it_assessment.landing_pages`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, template_id) → it_assessment.templates(tenant_id, id) ON DELETE RESTRICT`
- **Unique:** `(slug) WHERE deleted_at IS NULL`
- **Check:** `status IN ('draft','published','paused')`
- **FK support:** `(tenant_id, template_id)`

### `it_assessment.domain_verifications`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **Check:** `method IN ('dns_txt','http_file')`
- **Check:** `status IN ('pending','verified','failed','expired')`
- **Index:** `(tenant_id, domain)`

### `it_assessment.authorizations`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, domain_verification_id) → it_assessment.domain_verifications(tenant_id, id) ON DELETE RESTRICT`
- **Check:** `tier IN ('passive','exposure')`
- **Check:** `method IN ('attestation','verified_domain_control')`
- **Check:** `status IN ('active','expired','revoked')`
- **Check:** `tier <> 'exposure' OR domain_verification_id IS NOT NULL`
- **FK support:** `(tenant_id, domain_verification_id)`

### `it_assessment.lead_captures`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, landing_page_id) → it_assessment.landing_pages(tenant_id, id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, contact_id) → shared.contacts(tenant_id, id) ON DELETE SET NULL (contact_id)`
- **FK support:** `(tenant_id, landing_page_id)`
- **FK support:** `(tenant_id, contact_id)`

### `it_assessment.assessments`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, authorization_id) → it_assessment.authorizations(tenant_id, id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, template_id) → it_assessment.templates(tenant_id, id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, lead_capture_id) → it_assessment.lead_captures(tenant_id, id) ON DELETE SET NULL (lead_capture_id)`
- **FK:** `company_id → shared.companies(id) ON DELETE RESTRICT`
- **Check:** `origin IN ('internal','landing_page')`
- **Check:** `status IN ('queued','running','completed','partial','failed','cancelled')`
- **Check:** `risk_level IS NULL OR risk_level IN ('low','moderate','elevated','high')`
- **Check:** `score IS NULL OR score BETWEEN 0 AND 100`
- **FK support:** `(tenant_id, authorization_id)`
- **FK support:** `(tenant_id, template_id)`
- **FK support:** `(tenant_id, lead_capture_id)`
- **FK support:** `(tenant_id, company_id)`

### `it_assessment.checks`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, assessment_id) → it_assessment.assessments(tenant_id, id) ON DELETE CASCADE`
- **Unique:** `(tenant_id, assessment_id, check_key, target)`
- **Check:** `status IN ('pending','running','passed','failed','not_detected','inconclusive','skipped','blocked_by_policy')`
- **FK support:** `(tenant_id, assessment_id)`

### `it_assessment.findings`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, assessment_id) → it_assessment.assessments(tenant_id, id) ON DELETE CASCADE`
- **FK:** `(tenant_id, check_id) → it_assessment.checks(tenant_id, id) ON DELETE CASCADE`
- **Check:** `severity IN ('info','low','medium','high','critical')`
- **Check:** `classification IN ('informational','potential_issue','confirmed_misconfiguration')`
- **FK support:** `(tenant_id, assessment_id)`
- **FK support:** `(tenant_id, check_id)`

### `it_assessment.reports`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, assessment_id) → it_assessment.assessments(tenant_id, id) ON DELETE CASCADE`
- **Unique:** `(tenant_id, assessment_id, version)`
- **Check:** `status IN ('queued','generating','ready','failed')`
- **FK support:** `(tenant_id, assessment_id)`

### `it_assessment.consultation_bookings`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, assessment_id) → it_assessment.assessments(tenant_id, id) ON DELETE CASCADE`
- **FK:** `(tenant_id, contact_id) → shared.contacts(tenant_id, id) ON DELETE SET NULL (contact_id)`
- **Unique:** `(tenant_id, idempotency_key)`
- **Check:** `status IN ('requested','confirmed','cancelled','failed')`
- **FK support:** `(tenant_id, assessment_id)`
- **FK support:** `(tenant_id, contact_id)`

### `it_assessment.egress_decisions`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, assessment_id) → it_assessment.assessments(tenant_id, id) ON DELETE CASCADE`
- **Check:** `decision IN ('allowed','denied')`
- **FK support:** `(tenant_id, assessment_id)`

## `crm`

### `crm.companies`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `company_id → shared.companies(id) ON DELETE RESTRICT`
- **Unique:** `(tenant_id, company_id) WHERE company_id IS NOT NULL AND deleted_at IS NULL`
- **Check:** `lifecycle IN ('prospect','customer','former_customer','partner','other')`
- **Index:** `(tenant_id, lifecycle)`
- **Index:** `(tenant_id, lower(name))`
- **FK support:** `(tenant_id, company_id)`

### `crm.contacts`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, contact_id) → shared.contacts(tenant_id, id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, crm_company_id) → crm.companies(tenant_id, id) ON DELETE SET NULL (crm_company_id)`
- **Unique:** `(tenant_id, contact_id) WHERE deleted_at IS NULL`
- **FK support:** `(tenant_id, contact_id)`
- **FK support:** `(tenant_id, crm_company_id)`

### `crm.prospect_conversions`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, crm_company_id) → crm.companies(tenant_id, id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, deal_id) → crm.deals(tenant_id, id) ON DELETE SET NULL (deal_id)`
- **Unique:** `(tenant_id, prospect_id)`
- **FK support:** `(tenant_id, crm_company_id)`
- **FK support:** `(tenant_id, deal_id)`

### `crm.pipelines`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **Unique:** `(tenant_id) WHERE is_default`
- **Check:** `status IN ('active','archived')`

### `crm.pipeline_stages`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, pipeline_id) → crm.pipelines(tenant_id, id) ON DELETE CASCADE`
- **Unique:** `(tenant_id, pipeline_id, key)`
- **Unique:** `(tenant_id, pipeline_id, position) DEFERRABLE`
- **Check:** `kind IN ('open','won','lost')`
- **Check:** `default_probability BETWEEN 0 AND 100`
- **FK support:** `(tenant_id, pipeline_id)`

### `crm.deals`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, pipeline_id) → crm.pipelines(tenant_id, id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, stage_id) → crm.pipeline_stages(tenant_id, id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, crm_company_id) → crm.companies(tenant_id, id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, primary_contact_id) → crm.contacts(tenant_id, id) ON DELETE SET NULL (primary_contact_id)`
- **Check:** `probability BETWEEN 0 AND 100`
- **Check:** `status IN ('open','won','lost')`
- **Check:** `contract_value_minor IS NULL OR contract_value_minor >= 0`
- **Check:** `expected_mrr_minor IS NULL OR expected_mrr_minor >= 0`
- **Check:** `(status = 'open') = (closed_at IS NULL)`
- **Index:** `(tenant_id, stage_id, updated_at DESC) WHERE deleted_at IS NULL`
- **Index:** `(tenant_id, owner_user_id, status)`
- **Index:** `(tenant_id, expected_close_date) WHERE status = 'open'`
- **FK support:** `(tenant_id, pipeline_id)`
- **FK support:** `(tenant_id, stage_id)`
- **FK support:** `(tenant_id, crm_company_id)`
- **FK support:** `(tenant_id, primary_contact_id)`

### `crm.deal_stage_history`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, deal_id) → crm.deals(tenant_id, id) ON DELETE CASCADE`
- **FK:** `(tenant_id, from_stage_id) → crm.pipeline_stages(tenant_id, id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, to_stage_id) → crm.pipeline_stages(tenant_id, id) ON DELETE RESTRICT`
- **FK support:** `(tenant_id, deal_id)`
- **FK support:** `(tenant_id, from_stage_id)`
- **FK support:** `(tenant_id, to_stage_id)`

### `crm.sales_reps`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **Unique:** `(tenant_id, user_id)`

### `crm.tasks`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, crm_company_id) → crm.companies(tenant_id, id) ON DELETE CASCADE`
- **FK:** `(tenant_id, deal_id) → crm.deals(tenant_id, id) ON DELETE CASCADE`
- **Check:** `status IN ('open','done','cancelled')`
- **Index:** `(tenant_id, assignee_user_id, status, due_at)`
- **FK support:** `(tenant_id, crm_company_id)`
- **FK support:** `(tenant_id, deal_id)`

### `crm.reminders`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, task_id) → crm.tasks(tenant_id, id) ON DELETE CASCADE`
- **Index:** `(remind_at) WHERE fired_at IS NULL`
- **FK support:** `(tenant_id, task_id)`

### `crm.notes`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **Check:** `subject_type IN ('company','contact','deal')`
- **Index:** `(tenant_id, subject_type, subject_id)`

### `crm.attachments`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **Check:** `subject_type IN ('company','contact','deal','note')`
- **Index:** `(tenant_id, subject_type, subject_id)`

### `crm.meetings`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, crm_company_id) → crm.companies(tenant_id, id) ON DELETE SET NULL (crm_company_id)`
- **FK:** `(tenant_id, deal_id) → crm.deals(tenant_id, id) ON DELETE SET NULL (deal_id)`
- **Unique:** `(tenant_id, idempotency_key)`
- **Check:** `status IN ('scheduled','completed','cancelled','no_show')`
- **Check:** `ends_at > starts_at`
- **FK support:** `(tenant_id, crm_company_id)`
- **FK support:** `(tenant_id, deal_id)`

### `crm.meeting_attendees`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, meeting_id) → crm.meetings(tenant_id, id) ON DELETE CASCADE`
- **FK:** `(tenant_id, contact_id) → crm.contacts(tenant_id, id) ON DELETE SET NULL (contact_id)`
- **Check:** `contact_id IS NOT NULL OR user_id IS NOT NULL`
- **FK support:** `(tenant_id, meeting_id)`
- **FK support:** `(tenant_id, contact_id)`

### `crm.activities`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, crm_company_id) → crm.companies(tenant_id, id) ON DELETE SET NULL (crm_company_id)`
- **FK:** `(tenant_id, contact_id) → crm.contacts(tenant_id, id) ON DELETE SET NULL (contact_id)`
- **FK:** `(tenant_id, deal_id) → crm.deals(tenant_id, id) ON DELETE SET NULL (deal_id)`
- **Unique:** `(tenant_id, source_event_id)`
- **Check:** `activity_type IN ('email_sent','email_replied','call','meeting','note','task','stage_change','assessment','proposal','chat','visit','system')`
- **Index:** `(tenant_id, crm_company_id, occurred_at DESC)`
- **Index:** `(tenant_id, deal_id, occurred_at DESC)`
- **FK support:** `(tenant_id, crm_company_id)`
- **FK support:** `(tenant_id, contact_id)`
- **FK support:** `(tenant_id, deal_id)`

### `crm.custom_field_definitions`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **Unique:** `(tenant_id, entity, key)`
- **Check:** `entity IN ('company','contact','deal')`
- **Check:** `field_type IN ('text','number','date','boolean','select','multi_select')`

### `crm.custom_field_values`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, definition_id) → crm.custom_field_definitions(tenant_id, id) ON DELETE CASCADE`
- **Unique:** `(tenant_id, definition_id, subject_id)`
- **FK support:** `(tenant_id, definition_id)`

### `crm.ai_recommendations`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, deal_id) → crm.deals(tenant_id, id) ON DELETE CASCADE`
- **FK:** `(tenant_id, crm_company_id) → crm.companies(tenant_id, id) ON DELETE CASCADE`
- **Check:** `status IN ('open','accepted','dismissed','expired')`
- **FK support:** `(tenant_id, deal_id)`
- **FK support:** `(tenant_id, crm_company_id)`

## `proposals`

### `proposals.service_catalog_items`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **Check:** `pricing_model IN ('per_user','per_device','fixed')`
- **Check:** `unit_price_minor >= 0 AND setup_fee_minor >= 0`
- **Check:** `status IN ('active','retired')`

### `proposals.sla_templates`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`

### `proposals.terms_templates`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`

### `proposals.proposal_templates`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, default_sla_id) → proposals.sla_templates(tenant_id, id) ON DELETE SET NULL (default_sla_id)`
- **FK:** `(tenant_id, default_terms_id) → proposals.terms_templates(tenant_id, id) ON DELETE SET NULL (default_terms_id)`
- **FK support:** `(tenant_id, default_sla_id)`
- **FK support:** `(tenant_id, default_terms_id)`

### `proposals.proposals`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, template_id) → proposals.proposal_templates(tenant_id, id) ON DELETE SET NULL (template_id)`
- **FK:** `(tenant_id, current_version_id) → proposals.proposal_versions(tenant_id, id) ON DELETE SET NULL (current_version_id)`
- **Unique:** `(tenant_id, number)`
- **Check:** `status IN ('draft','in_review','issued','viewed','signed','declined','expired','withdrawn')`
- **Index:** `(tenant_id, deal_id)`
- **FK support:** `(tenant_id, template_id)`
- **FK support:** `(tenant_id, current_version_id)`

### `proposals.proposal_versions`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, proposal_id) → proposals.proposals(tenant_id, id) ON DELETE CASCADE`
- **Unique:** `(tenant_id, proposal_id, version)`
- **Check:** `status IN ('draft','pending_approval','approved','issued','superseded','expired')`
- **Check:** `term_months > 0`
- **Check:** `tcv_minor = setup_total_minor + mrr_total_minor * term_months`
- **Check:** `status NOT IN ('issued','superseded','expired') OR (commercial_snapshot IS NOT NULL AND snapshot_hash IS NOT NULL)`
- **FK support:** `(tenant_id, proposal_id)`

### `proposals.proposal_line_items`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, version_id) → proposals.proposal_versions(tenant_id, id) ON DELETE CASCADE`
- **FK:** `(tenant_id, catalog_item_id) → proposals.service_catalog_items(tenant_id, id) ON DELETE SET NULL (catalog_item_id)`
- **Check:** `pricing_model IN ('per_user','per_device','fixed')`
- **Check:** `quantity > 0`
- **Check:** `discount_bps BETWEEN 0 AND 10000`
- **FK support:** `(tenant_id, version_id)`
- **FK support:** `(tenant_id, catalog_item_id)`

### `proposals.approvals`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, version_id) → proposals.proposal_versions(tenant_id, id) ON DELETE CASCADE`
- **Check:** `status IN ('pending','approved','rejected')`
- **FK support:** `(tenant_id, version_id)`

### `proposals.share_links`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, version_id) → proposals.proposal_versions(tenant_id, id) ON DELETE CASCADE`
- **Unique:** `(token_hash)`
- **FK support:** `(tenant_id, version_id)`

### `proposals.view_events`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, share_link_id) → proposals.share_links(tenant_id, id) ON DELETE CASCADE`
- **FK support:** `(tenant_id, share_link_id)`

### `proposals.signature_requests`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, version_id) → proposals.proposal_versions(tenant_id, id) ON DELETE RESTRICT`
- **Unique:** `(tenant_id, idempotency_key)`
- **Unique:** `(provider_key, provider_envelope_id)`
- **Check:** `status IN ('created','sent','viewed','signed','declined','voided','failed')`
- **FK support:** `(tenant_id, version_id)`

### `proposals.signature_events`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, signature_request_id) → proposals.signature_requests(tenant_id, id) ON DELETE CASCADE`
- **Unique:** `(tenant_id, provider_event_id)`
- **FK support:** `(tenant_id, signature_request_id)`

## `visitor_intelligence`

### `visitor_intelligence.sites`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **Unique:** `(site_key)`
- **Unique:** `(tenant_id, domain) WHERE deleted_at IS NULL`
- **Check:** `consent_mode IN ('opt_in','opt_out','cookieless_only')`
- **Check:** `ip_handling IN ('discard','truncate_then_discard')`
- **Check:** `retention_days BETWEEN 1 AND 730`
- **Check:** `status IN ('pending','active','paused')`

### `visitor_intelligence.visitors`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, site_id) → visitor_intelligence.sites(tenant_id, id) ON DELETE CASCADE`
- **FK:** `(tenant_id, contact_id) → shared.contacts(tenant_id, id) ON DELETE SET NULL (contact_id)`
- **Unique:** `(tenant_id, site_id, anonymous_id_hash)`
- **Index:** `(tenant_id, last_seen_at)`
- **FK support:** `(tenant_id, site_id)`
- **FK support:** `(tenant_id, contact_id)`

### `visitor_intelligence.sessions`

- **Primary key:** `(id, started_at)`
- **Tenant key:** via partitioned primary key and the tenant-leading indexes below
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **Check:** `consent_state IN ('granted','denied','not_required','unknown')`
- **Index:** `(tenant_id, site_id, started_at DESC)`
- **Partitioning:** `RANGE (started_at) monthly`

### `visitor_intelligence.events`

- **Primary key:** `(id, occurred_at)`
- **Tenant key:** via partitioned primary key and the tenant-leading indexes below
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **Check:** `event_type IN ('page_view','conversion','custom')`
- **Index:** `(tenant_id, site_id, occurred_at DESC)`
- **Partitioning:** `RANGE (occurred_at) monthly`

### `visitor_intelligence.consent_records`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, site_id) → visitor_intelligence.sites(tenant_id, id) ON DELETE CASCADE`
- **Check:** `state IN ('granted','denied','withdrawn')`
- **FK support:** `(tenant_id, site_id)`

### `visitor_intelligence.conversion_goals`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, site_id) → visitor_intelligence.sites(tenant_id, id) ON DELETE CASCADE`
- **Check:** `kind IN ('url_match','form_submit','custom_event')`
- **FK support:** `(tenant_id, site_id)`

### `visitor_intelligence.conversions`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, site_id) → visitor_intelligence.sites(tenant_id, id) ON DELETE CASCADE`
- **FK:** `(tenant_id, goal_id) → visitor_intelligence.conversion_goals(tenant_id, id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, visitor_id) → visitor_intelligence.visitors(tenant_id, id) ON DELETE SET NULL (visitor_id)`
- **FK:** `(tenant_id, contact_id) → shared.contacts(tenant_id, id) ON DELETE SET NULL (contact_id)`
- **Unique:** `(tenant_id, dedupe_key)`
- **FK support:** `(tenant_id, site_id)`
- **FK support:** `(tenant_id, goal_id)`
- **FK support:** `(tenant_id, visitor_id)`
- **FK support:** `(tenant_id, contact_id)`

### `visitor_intelligence.business_identifications`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, site_id) → visitor_intelligence.sites(tenant_id, id) ON DELETE CASCADE`
- **FK:** `company_id → shared.companies(id) ON DELETE RESTRICT`
- **Check:** `confidence BETWEEN 0 AND 1`
- **Index:** `(tenant_id, site_id, observed_at DESC)`
- **FK support:** `(tenant_id, site_id)`
- **FK support:** `(tenant_id, company_id)`

### `visitor_intelligence.daily_rollups`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, site_id) → visitor_intelligence.sites(tenant_id, id) ON DELETE CASCADE`
- **Unique:** `(tenant_id, site_id, day, dimension, dimension_value)`
- **FK support:** `(tenant_id, site_id)`

## `chatbot`

### `chatbot.bots`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **Unique:** `(public_key)`
- **Check:** `status IN ('draft','active','paused')`

### `chatbot.knowledge_sources`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, bot_id) → chatbot.bots(tenant_id, id) ON DELETE CASCADE`
- **Check:** `kind IN ('document','service_catalog','url','faq')`
- **Check:** `status IN ('pending','processing','ready','failed','disabled')`
- **FK support:** `(tenant_id, bot_id)`

### `chatbot.knowledge_chunks`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, bot_id) → chatbot.bots(tenant_id, id) ON DELETE CASCADE`
- **FK:** `(tenant_id, source_id) → chatbot.knowledge_sources(tenant_id, id) ON DELETE CASCADE`
- **Unique:** `(tenant_id, source_id, position)`
- **Index:** `(tenant_id, bot_id)`
- **Index:** `USING hnsw (embedding)` (pgvector, see D-08)
- **FK support:** `(tenant_id, bot_id)`
- **FK support:** `(tenant_id, source_id)`

### `chatbot.qualification_rules`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, bot_id) → chatbot.bots(tenant_id, id) ON DELETE CASCADE`
- **FK support:** `(tenant_id, bot_id)`

### `chatbot.booking_configs`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, bot_id) → chatbot.bots(tenant_id, id) ON DELETE CASCADE`
- **Unique:** `(tenant_id, bot_id) WHERE deleted_at IS NULL`
- **Check:** `duration_minutes > 0`
- **FK support:** `(tenant_id, bot_id)`

### `chatbot.conversations`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, bot_id) → chatbot.bots(tenant_id, id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, contact_id) → shared.contacts(tenant_id, id) ON DELETE SET NULL (contact_id)`
- **Unique:** `(session_token_hash)`
- **Check:** `status IN ('active','awaiting_human','human_active','closed')`
- **Index:** `(tenant_id, bot_id, last_message_at DESC)`
- **FK support:** `(tenant_id, bot_id)`
- **FK support:** `(tenant_id, contact_id)`

### `chatbot.messages`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, conversation_id) → chatbot.conversations(tenant_id, id) ON DELETE CASCADE`
- **Check:** `role IN ('visitor','assistant','human_agent','system')`
- **Index:** `(tenant_id, conversation_id, created_at)`
- **FK support:** `(tenant_id, conversation_id)`

### `chatbot.qualifications`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, conversation_id) → chatbot.conversations(tenant_id, id) ON DELETE CASCADE`
- **Unique:** `(tenant_id, conversation_id)`
- **Check:** `status IN ('in_progress','qualified','not_qualified','incomplete')`
- **FK support:** `(tenant_id, conversation_id)`

### `chatbot.handoffs`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, conversation_id) → chatbot.conversations(tenant_id, id) ON DELETE CASCADE`
- **Check:** `status IN ('requested','accepted','resolved','missed')`
- **FK support:** `(tenant_id, conversation_id)`

### `chatbot.bookings`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, conversation_id) → chatbot.conversations(tenant_id, id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, config_id) → chatbot.booking_configs(tenant_id, id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, contact_id) → shared.contacts(tenant_id, id) ON DELETE SET NULL (contact_id)`
- **Unique:** `(tenant_id, idempotency_key)`
- **Check:** `status IN ('holding','confirmed','cancelled','failed')`
- **Check:** `ends_at > starts_at`
- **FK support:** `(tenant_id, conversation_id)`
- **FK support:** `(tenant_id, config_id)`
- **FK support:** `(tenant_id, contact_id)`

### `chatbot.daily_stats`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, bot_id) → chatbot.bots(tenant_id, id) ON DELETE CASCADE`
- **Unique:** `(tenant_id, bot_id, day)`
- **FK support:** `(tenant_id, bot_id)`

## `customer_expansion`

### `customer_expansion.customer_profiles`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `company_id → shared.companies(id) ON DELETE RESTRICT`
- **Unique:** `(tenant_id, crm_company_id) WHERE deleted_at IS NULL`
- **Check:** `health_status IN ('unknown','healthy','watch','at_risk')`
- **FK support:** `(tenant_id, company_id)`

### `customer_expansion.contracts`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, customer_profile_id) → customer_expansion.customer_profiles(tenant_id, id) ON DELETE CASCADE`
- **Unique:** `(tenant_id, deal_id)`
- **Check:** `status IN ('draft','active','renewing','ended','cancelled')`
- **Check:** `source IN ('won_deal','signed_proposal','manual','csv_import')`
- **Check:** `mrr_minor >= 0`
- **Index:** `(tenant_id, end_date) WHERE status = 'active'`
- **FK support:** `(tenant_id, customer_profile_id)`

### `customer_expansion.service_inventory`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, customer_profile_id) → customer_expansion.customer_profiles(tenant_id, id) ON DELETE CASCADE`
- **FK:** `(tenant_id, contract_id) → customer_expansion.contracts(tenant_id, id) ON DELETE SET NULL (contract_id)`
- **Unique:** `(tenant_id, customer_profile_id, tenant_service_id) WHERE deleted_at IS NULL`
- **Check:** `coverage IN ('active','absent','declined','unknown')`
- **FK support:** `(tenant_id, customer_profile_id)`
- **FK support:** `(tenant_id, contract_id)`

### `customer_expansion.renewal_alerts`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, contract_id) → customer_expansion.contracts(tenant_id, id) ON DELETE CASCADE`
- **Unique:** `(tenant_id, contract_id, due_date, window_days)`
- **Check:** `status IN ('open','acknowledged','resolved')`
- **FK support:** `(tenant_id, contract_id)`

### `customer_expansion.health_indicators`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, customer_profile_id) → customer_expansion.customer_profiles(tenant_id, id) ON DELETE CASCADE`
- **Check:** `polarity IN ('positive','neutral','negative','unknown')`
- **FK support:** `(tenant_id, customer_profile_id)`

### `customer_expansion.service_gaps`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, customer_profile_id) → customer_expansion.customer_profiles(tenant_id, id) ON DELETE CASCADE`
- **Unique:** `(tenant_id, customer_profile_id, tenant_service_id, analysis_run_id)`
- **Check:** `gap_state IN ('confirmed_absent','unknown_coverage')`
- **FK support:** `(tenant_id, customer_profile_id)`

### `customer_expansion.recommendations`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, customer_profile_id) → customer_expansion.customer_profiles(tenant_id, id) ON DELETE CASCADE`
- **Check:** `kind IN ('upsell','cross_sell','renewal')`
- **Check:** `evidence_class IN ('observed','inferred','ai_estimate')`
- **Check:** `status IN ('open','accepted','dismissed','converted','expired')`
- **Check:** `length(rationale) > 0`
- **FK support:** `(tenant_id, customer_profile_id)`

### `customer_expansion.recommendation_evidence`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, recommendation_id) → customer_expansion.recommendations(tenant_id, id) ON DELETE CASCADE`
- **FK support:** `(tenant_id, recommendation_id)`

### `customer_expansion.revenue_opportunities`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, recommendation_id) → customer_expansion.recommendations(tenant_id, id) ON DELETE RESTRICT`
- **Unique:** `(tenant_id, recommendation_id)`
- **Check:** `status IN ('open','in_deal','won','lost')`
- **FK support:** `(tenant_id, recommendation_id)`

## `sales_intelligence`

### `sales_intelligence.fact_lead_events`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **Unique:** `(tenant_id, source_event_id)`
- **Index:** `(tenant_id, occurred_at)`

### `sales_intelligence.fact_outreach_events`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **Unique:** `(tenant_id, source_event_id)`
- **Index:** `(tenant_id, campaign_ref, occurred_at)`

### `sales_intelligence.fact_deal_events`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **Unique:** `(tenant_id, source_event_id)`
- **Index:** `(tenant_id, deal_ref, occurred_at)`

### `sales_intelligence.fact_revenue_events`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **Unique:** `(tenant_id, source_event_id)`
- **Check:** `movement IN ('new','expansion','contraction','churn','reactivation')`
- **Index:** `(tenant_id, occurred_at)`

### `sales_intelligence.cost_entries`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **Check:** `period_end >= period_start`
- **Check:** `amount_minor >= 0`

### `sales_intelligence.metric_snapshots`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **Unique:** `(tenant_id, metric_key, period_grain, period_start, dimension, dimension_value, value_kind)`
- **Check:** `period_grain IN ('day','week','month','quarter')`
- **Check:** `value_kind IN ('actual','estimated','forecast')`

### `sales_intelligence.forecast_runs`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **Check:** `kind IN ('revenue','pipeline')`
- **Check:** `status IN ('queued','running','completed','failed')`

### `sales_intelligence.forecast_points`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, run_id) → sales_intelligence.forecast_runs(tenant_id, id) ON DELETE CASCADE`
- **Unique:** `(tenant_id, run_id, period_start)`
- **FK support:** `(tenant_id, run_id)`

### `sales_intelligence.report_definitions`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`

### `sales_intelligence.report_exports`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `(tenant_id, definition_id) → sales_intelligence.report_definitions(tenant_id, id) ON DELETE SET NULL (definition_id)`
- **Check:** `status IN ('queued','running','ready','failed')`
- **FK support:** `(tenant_id, definition_id)`

### `sales_intelligence.ai_insights`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`

## `integration`

### `integration.providers`

- **Primary key:** `(id)`
- **Unique:** `(key)`
- **Check:** `kind IN ('ai','email','calendar','company_data','payment','esign','business_identification','fx')`
- **Check:** `status IN ('disabled','enabled','degraded')`

### `integration.platform_credentials`

- **Primary key:** `(id)`
- **FK:** `provider_id → integration.providers(id) ON DELETE RESTRICT`
- **Unique:** `(provider_id, label)`
- **FK support:** `(provider_id)`

### `integration.connections`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `provider_id → integration.providers(id) ON DELETE RESTRICT`
- **Check:** `status IN ('pending','connected','needs_reauth','error','disconnected')`
- **FK support:** `(tenant_id, provider_id)`

### `integration.webhook_endpoints`

- **Primary key:** `(id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE CASCADE`
- **FK:** `provider_id → integration.providers(id) ON DELETE RESTRICT`
- **FK:** `connection_id → integration.connections(id) ON DELETE CASCADE`
- **Unique:** `(path_token)`
- **Check:** `status IN ('active','disabled')`
- **FK support:** `(provider_id)`
- **FK support:** `(connection_id)`

### `integration.webhook_deliveries`

- **Primary key:** `(id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE SET NULL`
- **FK:** `endpoint_id → integration.webhook_endpoints(id) ON DELETE CASCADE`
- **Unique:** `(endpoint_id, provider_event_id)`
- **Check:** `status IN ('received','rejected','dispatched','failed')`
- **FK support:** `(endpoint_id)`

### `integration.provider_call_logs`

- **Primary key:** `(id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE SET NULL`
- **FK:** `provider_id → integration.providers(id) ON DELETE RESTRICT`
- **Check:** `outcome IN ('success','retryable_error','permanent_error','rate_limited','circuit_open')`
- **Index:** `(provider_id, created_at DESC)`
- **FK support:** `(provider_id)`

### `integration.ai_model_routes`

- **Primary key:** `(id)`
- **FK:** `provider_id → integration.providers(id) ON DELETE RESTRICT`
- **FK:** `fallback_route_id → integration.ai_model_routes(id) ON DELETE SET NULL`
- **Unique:** `(task_key, provider_id, model)`
- **Check:** `status IN ('active','disabled')`
- **FK support:** `(provider_id)`
- **FK support:** `(fallback_route_id)`

### `integration.prompt_templates`

- **Primary key:** `(id)`
- **Unique:** `(key)`
- **Check:** `action_class IN ('informational','recommendation','human_approval_required','prohibited_without_authorization')`
- **Check:** `status IN ('active','retired')`

### `integration.prompt_versions`

- **Primary key:** `(id)`
- **FK:** `template_id → integration.prompt_templates(id) ON DELETE RESTRICT`
- **Unique:** `(template_id, version)`
- **Check:** `status IN ('draft','active','retired')`
- **FK support:** `(template_id)`

### `integration.ai_requests`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **FK:** `prompt_version_id → integration.prompt_versions(id) ON DELETE RESTRICT`
- **FK:** `provider_id → integration.providers(id) ON DELETE RESTRICT`
- **Unique:** `(tenant_id, idempotency_key)`
- **Check:** `outcome IN ('success','provider_error','validation_failed','quota_exceeded','policy_blocked','timeout')`
- **Index:** `(tenant_id, module_key, created_at DESC)`
- **FK support:** `(tenant_id, prompt_version_id)`
- **FK support:** `(tenant_id, provider_id)`

## `audit`

### `audit.events`

- **Primary key:** `(id, occurred_at)`
- **Check:** `actor_type IN ('user','platform_admin','system','public')`
- **Check:** `outcome IN ('success','denied','failed')`
- **Index:** `(tenant_id, occurred_at DESC)`
- **Index:** `(resource_type, resource_id, occurred_at DESC)`
- **Partitioning:** `RANGE (occurred_at) monthly`

### `audit.security_events`

- **Primary key:** `(id, occurred_at)`
- **Check:** `severity IN ('info','warning','high','critical')`
- **Index:** `(event_type, occurred_at DESC)`
- **Partitioning:** `RANGE (occurred_at) monthly`

### `audit.support_access_grants`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **Check:** `scope IN ('metadata','read_only')`

## `notification`

### `notification.templates`

- **Primary key:** `(id)`
- **Unique:** `(key, channel, locale, version)`
- **Check:** `channel IN ('in_app','email')`

### `notification.preferences`

- **Primary key:** `(id)`
- **Tenant key:** `UNIQUE (tenant_id, id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **Unique:** `(tenant_id, user_id, notification_key, channel)`

### `notification.notifications`

- **Primary key:** `(id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE CASCADE`
- **Unique:** `(recipient_id, idempotency_key)`
- **Check:** `recipient_type IN ('user','platform_admin')`
- **Index:** `(recipient_id, read_at, created_at DESC)`

### `notification.delivery_attempts`

- **Primary key:** `(id)`
- **FK:** `notification_id → notification.notifications(id) ON DELETE CASCADE`
- **Check:** `status IN ('queued','sent','delivered','failed','bounced')`
- **FK support:** `(notification_id)`

## `jobs`

### `jobs.jobs`

- **Primary key:** `(id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE CASCADE`
- **Unique:** `(tenant_id, kind, idempotency_key) NULLS NOT DISTINCT`
- **Check:** `status IN ('queued','running','retry_scheduled','succeeded','failed','cancelled','dead')`
- **Index:** `(queue, priority, run_at) WHERE status IN ('queued','retry_scheduled')`
- **Index:** `(tenant_id, status)`
- **Index:** `(lock_expires_at) WHERE status = 'running'`

### `jobs.job_attempts`

- **Primary key:** `(id)`
- **FK:** `job_id → jobs.jobs(id) ON DELETE CASCADE`
- **Unique:** `(job_id, attempt)`
- **FK support:** `(job_id)`

### `jobs.job_schedules`

- **Primary key:** `(id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE CASCADE`
- **Unique:** `(tenant_id, key) NULLS NOT DISTINCT`
- **Index:** `(next_run_at) WHERE enabled`

### `jobs.dead_letters`

- **Primary key:** `(id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE SET NULL`
- **Check:** `origin IN ('job','event_delivery')`
- **Check:** `resolution IS NULL OR resolution IN ('requeued','discarded')`

### `jobs.outbox_events`

- **Primary key:** `(event_id, occurred_at)`
- **Index:** `(sequence) WHERE published_at IS NULL`
- **Index:** `(aggregate_type, aggregate_id, sequence)`
- **Partitioning:** `RANGE (occurred_at) monthly`

### `jobs.event_deliveries`

- **Primary key:** `(id)`
- **Unique:** `(event_id, consumer)`
- **Check:** `status IN ('pending','processing','processed','failed','dead')`
- **Index:** `(consumer, status, next_attempt_at)`

## `files`

### `files.objects`

- **Primary key:** `(id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE RESTRICT`
- **Unique:** `(bucket, object_key)`
- **Check:** `status IN ('pending_upload','scanning','clean','quarantined','deleted')`
- **Check:** `classification IN ('public_asset','tenant_private','sensitive')`
- **Index:** `(tenant_id, owner_module)`
- **Index:** `(retention_until) WHERE deleted_at IS NULL`

### `files.object_references`

- **Primary key:** `(id)`
- **FK:** `object_id → files.objects(id) ON DELETE CASCADE`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE CASCADE`
- **Unique:** `(object_id, owner_module, owner_type, owner_id)`
- **FK support:** `(object_id)`

### `files.access_policies`

- **Primary key:** `(id)`
- **Unique:** `(key)`

### `files.access_grants`

- **Primary key:** `(id)`
- **FK:** `tenant_id → identity.tenants(id) ON DELETE CASCADE`
- **FK:** `object_id → files.objects(id) ON DELETE CASCADE`
- **FK:** `policy_id → files.access_policies(id) ON DELETE RESTRICT`
- **Unique:** `(token_hash)`
- **Check:** `download_count >= 0`
- **FK support:** `(object_id)`
- **FK support:** `(policy_id)`
