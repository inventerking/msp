-- +goose Up
CREATE TABLE platform.settings (
    key text NOT NULL,
    value jsonb NOT NULL,
    updated_by uuid,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (key)
);

CREATE TABLE platform.modules (
    id uuid NOT NULL DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    key text NOT NULL,
    name text NOT NULL,
    description text NOT NULL,
    status text NOT NULL,
    default_limits jsonb NOT NULL DEFAULT '{}'::jsonb,
    sort_order int NOT NULL DEFAULT 0,
    PRIMARY KEY (id),
    CONSTRAINT modules_ck1 CHECK (status IN ('available','hidden','retired'))
);

CREATE TABLE platform.module_dependencies (
    module_id uuid NOT NULL,
    depends_on_module_id uuid NOT NULL,
    kind text NOT NULL,
    PRIMARY KEY (module_id, depends_on_module_id),
    CONSTRAINT module_dependencies_ck1 CHECK (kind IN ('requires','enhances')),
    CONSTRAINT module_dependencies_ck2 CHECK (module_id <> depends_on_module_id)
);

CREATE TABLE platform.products (
    id uuid NOT NULL DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    key text NOT NULL,
    kind text NOT NULL,
    name text NOT NULL,
    description text NOT NULL,
    status text NOT NULL,
    PRIMARY KEY (id),
    CONSTRAINT products_ck1 CHECK (kind IN ('module','bundle','suite','addon')),
    CONSTRAINT products_ck2 CHECK (status IN ('draft','active','retired'))
);

CREATE TABLE platform.product_versions (
    id uuid NOT NULL DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    product_id uuid NOT NULL,
    version int NOT NULL DEFAULT 1,
    status text NOT NULL,
    effective_from timestamptz,
    effective_to timestamptz,
    PRIMARY KEY (id),
    CONSTRAINT product_versions_ck1 CHECK (status IN ('draft','published','retired'))
);

CREATE TABLE platform.product_version_modules (
    product_version_id uuid NOT NULL,
    module_id uuid NOT NULL,
    PRIMARY KEY (product_version_id, module_id)
);

CREATE TABLE platform.plans (
    id uuid NOT NULL DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    product_version_id uuid NOT NULL,
    key text NOT NULL,
    name text NOT NULL,
    billing_period text NOT NULL,
    trial_days int NOT NULL DEFAULT 0,
    usage_limits jsonb NOT NULL DEFAULT '{}'::jsonb,
    included_credits jsonb NOT NULL DEFAULT '{}'::jsonb,
    status text NOT NULL,
    PRIMARY KEY (id),
    CONSTRAINT plans_ck1 CHECK (billing_period IN ('monthly','annual','custom')),
    CONSTRAINT plans_ck2 CHECK (trial_days >= 0),
    CONSTRAINT plans_ck3 CHECK (status IN ('draft','active','retired'))
);

CREATE TABLE platform.prices (
    id uuid NOT NULL DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    plan_id uuid NOT NULL,
    currency char(3) NOT NULL,
    amount_minor bigint NOT NULL,
    valid_from timestamptz NOT NULL,
    valid_to timestamptz,
    PRIMARY KEY (id),
    CONSTRAINT prices_ck1 CHECK (amount_minor >= 0)
);

CREATE TABLE platform.promotions (
    id uuid NOT NULL DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    code citext NOT NULL,
    kind text NOT NULL,
    percent_off numeric(5,2),
    amount_off_minor bigint,
    currency char(3),
    max_redemptions int,
    valid_from timestamptz NOT NULL,
    valid_to timestamptz,
    status text NOT NULL,
    PRIMARY KEY (id),
    CONSTRAINT promotions_ck1 CHECK (kind IN ('percent','fixed','trial_extension')),
    CONSTRAINT promotions_ck2 CHECK (status IN ('draft','active','retired'))
);

CREATE TABLE platform.promotion_redemptions (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    promotion_id uuid NOT NULL,
    purchase_request_id uuid NOT NULL,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id)
);

CREATE TABLE platform.purchase_requests (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    plan_id uuid NOT NULL,
    promotion_id uuid,
    quantity int NOT NULL,
    quote_snapshot jsonb NOT NULL,
    status text NOT NULL,
    idempotency_key text NOT NULL,
    requested_by uuid NOT NULL,
    reviewed_by uuid,
    reviewed_at timestamptz,
    rejection_reason text,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT purchase_requests_ck1 CHECK (status IN ('draft','submitted','payment_pending','payment_verified','under_review','approved','rejected','cancelled')),
    CONSTRAINT purchase_requests_ck2 CHECK (quantity > 0)
);

CREATE TABLE platform.subscriptions (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    plan_id uuid NOT NULL,
    purchase_request_id uuid,
    status text NOT NULL,
    price_snapshot jsonb NOT NULL DEFAULT '{}'::jsonb,
    currency char(3) NOT NULL,
    current_period_start timestamptz,
    current_period_end timestamptz,
    trial_ends_at timestamptz,
    cancel_at timestamptz,
    provider_subscription_ref text,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT subscriptions_ck1 CHECK (status IN ('pending_activation','trialing','active','past_due','suspended','cancelled','expired'))
);

CREATE TABLE platform.entitlement_grants (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    module_id uuid NOT NULL,
    subscription_id uuid,
    source text NOT NULL,
    status text NOT NULL,
    starts_at timestamptz,
    ends_at timestamptz,
    activated_by uuid,
    status_reason text,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT entitlement_grants_ck1 CHECK (source IN ('subscription','trial','admin_override')),
    CONSTRAINT entitlement_grants_ck2 CHECK (status IN ('pending','active','suspended','expired','revoked')),
    CONSTRAINT entitlement_grants_ck3 CHECK (source <> 'subscription' OR subscription_id IS NOT NULL)
);

CREATE TABLE platform.tenant_module_entitlements (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    module_id uuid NOT NULL,
    status text NOT NULL,
    effective_from timestamptz,
    effective_until timestamptz,
    entitlement_version bigint NOT NULL DEFAULT 0,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT tenant_module_entitlements_ck1 CHECK (status IN ('inactive','active','suspended','expired','revoked'))
);

CREATE TABLE platform.entitlement_status_history (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    module_id uuid NOT NULL,
    grant_id uuid,
    from_status text,
    to_status text NOT NULL,
    actor_type text NOT NULL,
    actor_id uuid,
    reason text,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT entitlement_status_history_ck1 CHECK (actor_type IN ('platform_admin','system'))
);

CREATE TABLE platform.invoices (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    subscription_id uuid,
    number text NOT NULL,
    status text NOT NULL,
    currency char(3) NOT NULL,
    subtotal_minor bigint NOT NULL,
    tax_minor bigint NOT NULL,
    total_minor bigint NOT NULL,
    issued_at timestamptz,
    due_at timestamptz,
    paid_at timestamptz,
    provider_invoice_ref text,
    pdf_file_id uuid,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT invoices_ck1 CHECK (status IN ('draft','issued','paid','void','uncollectible')),
    CONSTRAINT invoices_ck2 CHECK (total_minor = subtotal_minor + tax_minor)
);

CREATE TABLE platform.invoice_lines (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    invoice_id uuid NOT NULL,
    description text NOT NULL,
    quantity int NOT NULL,
    unit_amount_minor bigint NOT NULL,
    amount_minor bigint NOT NULL,
    period_start timestamptz,
    period_end timestamptz,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id)
);

CREATE TABLE platform.payments (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    invoice_id uuid,
    purchase_request_id uuid,
    provider text NOT NULL,
    provider_payment_ref text,
    method text NOT NULL,
    status text NOT NULL,
    amount_minor bigint NOT NULL,
    currency char(3) NOT NULL,
    verified_by uuid,
    verified_at timestamptz,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT payments_ck1 CHECK (method IN ('card','bank_transfer','manual','other')),
    CONSTRAINT payments_ck2 CHECK (status IN ('pending','succeeded','failed','refunded','disputed'))
);

CREATE TABLE platform.usage_meters (
    id uuid NOT NULL DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    key text NOT NULL,
    name text NOT NULL,
    unit text NOT NULL,
    status text NOT NULL,
    PRIMARY KEY (id),
    CONSTRAINT usage_meters_ck1 CHECK (status IN ('active','retired'))
);

CREATE TABLE platform.credit_accounts (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    meter_id uuid NOT NULL,
    balance bigint NOT NULL DEFAULT 0,
    reserved bigint NOT NULL DEFAULT 0,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT credit_accounts_ck1 CHECK (balance >= 0 AND reserved >= 0 AND reserved <= balance)
);

CREATE TABLE platform.credit_ledger (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    account_id uuid NOT NULL,
    entry_type text NOT NULL,
    amount bigint NOT NULL,
    source_module text,
    reference_type text,
    reference_id uuid,
    idempotency_key text NOT NULL,
    expires_at timestamptz,
    actor_type text NOT NULL,
    actor_id uuid,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT credit_ledger_ck1 CHECK (entry_type IN ('grant','reservation','commit','release','expiry','adjustment','refund'))
);

CREATE TABLE platform.tenant_usage_limits (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    meter_id uuid NOT NULL,
    period text NOT NULL,
    limit_value bigint NOT NULL,
    hard_stop boolean NOT NULL DEFAULT false,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT tenant_usage_limits_ck1 CHECK (period IN ('day','month','billing_period'))
);

CREATE TABLE platform.usage_rollups (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    meter_id uuid NOT NULL,
    module_key text NOT NULL,
    period_start date NOT NULL,
    quantity bigint NOT NULL,
    cost_estimate_minor bigint,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id)
);

ALTER TABLE platform.module_dependencies ADD CONSTRAINT module_dependencies_module_id_fk FOREIGN KEY (module_id) REFERENCES platform.modules (id) ON DELETE CASCADE;
ALTER TABLE platform.module_dependencies ADD CONSTRAINT module_dependencies_depends_on_module_id_fk FOREIGN KEY (depends_on_module_id) REFERENCES platform.modules (id) ON DELETE RESTRICT;
ALTER TABLE platform.product_versions ADD CONSTRAINT product_versions_product_id_fk FOREIGN KEY (product_id) REFERENCES platform.products (id) ON DELETE RESTRICT;
ALTER TABLE platform.product_version_modules ADD CONSTRAINT product_version_modules_product_version_id_fk FOREIGN KEY (product_version_id) REFERENCES platform.product_versions (id) ON DELETE CASCADE;
ALTER TABLE platform.product_version_modules ADD CONSTRAINT product_version_modules_module_id_fk FOREIGN KEY (module_id) REFERENCES platform.modules (id) ON DELETE RESTRICT;
ALTER TABLE platform.plans ADD CONSTRAINT plans_product_version_id_fk FOREIGN KEY (product_version_id) REFERENCES platform.product_versions (id) ON DELETE RESTRICT;
ALTER TABLE platform.prices ADD CONSTRAINT prices_plan_id_fk FOREIGN KEY (plan_id) REFERENCES platform.plans (id) ON DELETE RESTRICT;
ALTER TABLE platform.promotion_redemptions ADD CONSTRAINT promotion_redemptions_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE platform.promotion_redemptions ADD CONSTRAINT promotion_redemptions_promotion_id_fk FOREIGN KEY (promotion_id) REFERENCES platform.promotions (id) ON DELETE RESTRICT;
ALTER TABLE platform.promotion_redemptions ADD CONSTRAINT promotion_redemptions_purchase_request_id_fk FOREIGN KEY (tenant_id, purchase_request_id) REFERENCES platform.purchase_requests (tenant_id, id) ON DELETE RESTRICT;
ALTER TABLE platform.purchase_requests ADD CONSTRAINT purchase_requests_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE platform.purchase_requests ADD CONSTRAINT purchase_requests_plan_id_fk FOREIGN KEY (plan_id) REFERENCES platform.plans (id) ON DELETE RESTRICT;
ALTER TABLE platform.purchase_requests ADD CONSTRAINT purchase_requests_promotion_id_fk FOREIGN KEY (promotion_id) REFERENCES platform.promotions (id) ON DELETE RESTRICT;
ALTER TABLE platform.subscriptions ADD CONSTRAINT subscriptions_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE platform.subscriptions ADD CONSTRAINT subscriptions_plan_id_fk FOREIGN KEY (plan_id) REFERENCES platform.plans (id) ON DELETE RESTRICT;
ALTER TABLE platform.subscriptions ADD CONSTRAINT subscriptions_purchase_request_id_fk FOREIGN KEY (tenant_id, purchase_request_id) REFERENCES platform.purchase_requests (tenant_id, id) ON DELETE RESTRICT;
ALTER TABLE platform.entitlement_grants ADD CONSTRAINT entitlement_grants_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE platform.entitlement_grants ADD CONSTRAINT entitlement_grants_module_id_fk FOREIGN KEY (module_id) REFERENCES platform.modules (id) ON DELETE RESTRICT;
ALTER TABLE platform.entitlement_grants ADD CONSTRAINT entitlement_grants_subscription_id_fk FOREIGN KEY (tenant_id, subscription_id) REFERENCES platform.subscriptions (tenant_id, id) ON DELETE RESTRICT;
ALTER TABLE platform.tenant_module_entitlements ADD CONSTRAINT tenant_module_entitlements_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE platform.tenant_module_entitlements ADD CONSTRAINT tenant_module_entitlements_module_id_fk FOREIGN KEY (module_id) REFERENCES platform.modules (id) ON DELETE RESTRICT;
ALTER TABLE platform.entitlement_status_history ADD CONSTRAINT entitlement_status_history_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE platform.entitlement_status_history ADD CONSTRAINT entitlement_status_history_module_id_fk FOREIGN KEY (module_id) REFERENCES platform.modules (id) ON DELETE RESTRICT;
ALTER TABLE platform.entitlement_status_history ADD CONSTRAINT entitlement_status_history_grant_id_fk FOREIGN KEY (tenant_id, grant_id) REFERENCES platform.entitlement_grants (tenant_id, id) ON DELETE RESTRICT;
ALTER TABLE platform.invoices ADD CONSTRAINT invoices_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE platform.invoices ADD CONSTRAINT invoices_subscription_id_fk FOREIGN KEY (tenant_id, subscription_id) REFERENCES platform.subscriptions (tenant_id, id) ON DELETE RESTRICT;
ALTER TABLE platform.invoice_lines ADD CONSTRAINT invoice_lines_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE platform.invoice_lines ADD CONSTRAINT invoice_lines_invoice_id_fk FOREIGN KEY (tenant_id, invoice_id) REFERENCES platform.invoices (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE platform.payments ADD CONSTRAINT payments_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE platform.payments ADD CONSTRAINT payments_invoice_id_fk FOREIGN KEY (tenant_id, invoice_id) REFERENCES platform.invoices (tenant_id, id) ON DELETE RESTRICT;
ALTER TABLE platform.payments ADD CONSTRAINT payments_purchase_request_id_fk FOREIGN KEY (tenant_id, purchase_request_id) REFERENCES platform.purchase_requests (tenant_id, id) ON DELETE RESTRICT;
ALTER TABLE platform.credit_accounts ADD CONSTRAINT credit_accounts_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE platform.credit_accounts ADD CONSTRAINT credit_accounts_meter_id_fk FOREIGN KEY (meter_id) REFERENCES platform.usage_meters (id) ON DELETE RESTRICT;
ALTER TABLE platform.credit_ledger ADD CONSTRAINT credit_ledger_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE platform.credit_ledger ADD CONSTRAINT credit_ledger_account_id_fk FOREIGN KEY (tenant_id, account_id) REFERENCES platform.credit_accounts (tenant_id, id) ON DELETE RESTRICT;
ALTER TABLE platform.tenant_usage_limits ADD CONSTRAINT tenant_usage_limits_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE platform.tenant_usage_limits ADD CONSTRAINT tenant_usage_limits_meter_id_fk FOREIGN KEY (meter_id) REFERENCES platform.usage_meters (id) ON DELETE RESTRICT;
ALTER TABLE platform.usage_rollups ADD CONSTRAINT usage_rollups_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE platform.usage_rollups ADD CONSTRAINT usage_rollups_meter_id_fk FOREIGN KEY (meter_id) REFERENCES platform.usage_meters (id) ON DELETE RESTRICT;

CREATE TRIGGER touch_updated_at BEFORE UPDATE ON platform.settings FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX modules_uq1 ON platform.modules (key);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON platform.modules FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE INDEX module_dependencies_depends_on_module_id_idx ON platform.module_dependencies (depends_on_module_id);
CREATE UNIQUE INDEX products_uq1 ON platform.products (key);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON platform.products FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX product_versions_uq1 ON platform.product_versions (product_id, version);
CREATE INDEX product_versions_product_id_idx ON platform.product_versions (product_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON platform.product_versions FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE INDEX product_version_modules_module_id_idx ON platform.product_version_modules (module_id);
CREATE UNIQUE INDEX plans_uq1 ON platform.plans (key);
CREATE INDEX plans_product_version_id_idx ON platform.plans (product_version_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON platform.plans FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX prices_uq1 ON platform.prices (plan_id, currency, valid_from);
CREATE INDEX prices_plan_id_idx ON platform.prices (plan_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON platform.prices FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX promotions_uq1 ON platform.promotions (code);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON platform.promotions FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX promotion_redemptions_uq1 ON platform.promotion_redemptions (tenant_id, promotion_id, purchase_request_id);
CREATE INDEX promotion_redemptions_promotion_id_idx ON platform.promotion_redemptions (tenant_id, promotion_id);
CREATE INDEX promotion_redemptions_purchase_request_id_idx ON platform.promotion_redemptions (tenant_id, purchase_request_id);
CREATE UNIQUE INDEX purchase_requests_uq1 ON platform.purchase_requests (tenant_id, idempotency_key);
CREATE INDEX purchase_requests_ix1 ON platform.purchase_requests (status, created_at);
CREATE INDEX purchase_requests_plan_id_idx ON platform.purchase_requests (tenant_id, plan_id);
CREATE INDEX purchase_requests_promotion_id_idx ON platform.purchase_requests (tenant_id, promotion_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON platform.purchase_requests FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE INDEX subscriptions_ix1 ON platform.subscriptions (status, current_period_end);
CREATE INDEX subscriptions_plan_id_idx ON platform.subscriptions (tenant_id, plan_id);
CREATE INDEX subscriptions_purchase_request_id_idx ON platform.subscriptions (tenant_id, purchase_request_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON platform.subscriptions FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX entitlement_grants_uq1 ON platform.entitlement_grants (tenant_id, module_id, subscription_id) NULLS DISTINCT;
CREATE INDEX entitlement_grants_module_id_idx ON platform.entitlement_grants (tenant_id, module_id);
CREATE INDEX entitlement_grants_subscription_id_idx ON platform.entitlement_grants (tenant_id, subscription_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON platform.entitlement_grants FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX tenant_module_entitlements_uq1 ON platform.tenant_module_entitlements (tenant_id, module_id);
CREATE INDEX tenant_module_entitlements_module_id_idx ON platform.tenant_module_entitlements (tenant_id, module_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON platform.tenant_module_entitlements FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE INDEX entitlement_status_history_module_id_idx ON platform.entitlement_status_history (tenant_id, module_id);
CREATE INDEX entitlement_status_history_grant_id_idx ON platform.entitlement_status_history (tenant_id, grant_id);
CREATE UNIQUE INDEX invoices_uq1 ON platform.invoices (number);
CREATE INDEX invoices_subscription_id_idx ON platform.invoices (tenant_id, subscription_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON platform.invoices FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE INDEX invoice_lines_invoice_id_idx ON platform.invoice_lines (tenant_id, invoice_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON platform.invoice_lines FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX payments_uq1 ON platform.payments (provider, provider_payment_ref);
CREATE INDEX payments_invoice_id_idx ON platform.payments (tenant_id, invoice_id);
CREATE INDEX payments_purchase_request_id_idx ON platform.payments (tenant_id, purchase_request_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON platform.payments FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX usage_meters_uq1 ON platform.usage_meters (key);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON platform.usage_meters FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX credit_accounts_uq1 ON platform.credit_accounts (tenant_id, meter_id);
CREATE INDEX credit_accounts_meter_id_idx ON platform.credit_accounts (tenant_id, meter_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON platform.credit_accounts FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX credit_ledger_uq1 ON platform.credit_ledger (tenant_id, idempotency_key);
CREATE INDEX credit_ledger_account_id_idx ON platform.credit_ledger (tenant_id, account_id);
CREATE UNIQUE INDEX tenant_usage_limits_uq1 ON platform.tenant_usage_limits (tenant_id, meter_id, period);
CREATE INDEX tenant_usage_limits_meter_id_idx ON platform.tenant_usage_limits (tenant_id, meter_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON platform.tenant_usage_limits FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX usage_rollups_uq1 ON platform.usage_rollups (tenant_id, meter_id, module_key, period_start);
CREATE INDEX usage_rollups_meter_id_idx ON platform.usage_rollups (tenant_id, meter_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON platform.usage_rollups FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();

-- +goose Down
DROP TABLE IF EXISTS platform.usage_rollups CASCADE;
DROP TABLE IF EXISTS platform.tenant_usage_limits CASCADE;
DROP TABLE IF EXISTS platform.credit_ledger CASCADE;
DROP TABLE IF EXISTS platform.credit_accounts CASCADE;
DROP TABLE IF EXISTS platform.usage_meters CASCADE;
DROP TABLE IF EXISTS platform.payments CASCADE;
DROP TABLE IF EXISTS platform.invoice_lines CASCADE;
DROP TABLE IF EXISTS platform.invoices CASCADE;
DROP TABLE IF EXISTS platform.entitlement_status_history CASCADE;
DROP TABLE IF EXISTS platform.tenant_module_entitlements CASCADE;
DROP TABLE IF EXISTS platform.entitlement_grants CASCADE;
DROP TABLE IF EXISTS platform.subscriptions CASCADE;
DROP TABLE IF EXISTS platform.purchase_requests CASCADE;
DROP TABLE IF EXISTS platform.promotion_redemptions CASCADE;
DROP TABLE IF EXISTS platform.promotions CASCADE;
DROP TABLE IF EXISTS platform.prices CASCADE;
DROP TABLE IF EXISTS platform.plans CASCADE;
DROP TABLE IF EXISTS platform.product_version_modules CASCADE;
DROP TABLE IF EXISTS platform.product_versions CASCADE;
DROP TABLE IF EXISTS platform.products CASCADE;
DROP TABLE IF EXISTS platform.module_dependencies CASCADE;
DROP TABLE IF EXISTS platform.modules CASCADE;
DROP TABLE IF EXISTS platform.settings CASCADE;
