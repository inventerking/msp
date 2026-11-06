-- +goose Up
-- Row-level security and least-privilege grants for Phase 1 schemas.
-- msp_app serves tenant requests. msp_platform serves platform admins. msp_system runs housekeeping.

GRANT USAGE ON SCHEMA identity, platform, audit, shared TO msp_app, msp_platform, msp_system;
GRANT EXECUTE ON FUNCTION shared.touch_updated_at() TO msp_app, msp_platform, msp_system;

ALTER TABLE identity.tenant_profiles ENABLE ROW LEVEL SECURITY;
ALTER TABLE identity.tenant_profiles FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON identity.tenant_profiles TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY platform_access ON identity.tenant_profiles TO msp_platform USING (true) WITH CHECK (true);

ALTER TABLE identity.tenant_icps ENABLE ROW LEVEL SECURITY;
ALTER TABLE identity.tenant_icps FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON identity.tenant_icps TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY platform_access ON identity.tenant_icps TO msp_platform USING (true) WITH CHECK (true);

ALTER TABLE identity.memberships ENABLE ROW LEVEL SECURITY;
ALTER TABLE identity.memberships FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON identity.memberships TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid OR user_id = NULLIF(current_setting('app.user_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY platform_access ON identity.memberships TO msp_platform USING (true) WITH CHECK (true);

ALTER TABLE identity.invitations ENABLE ROW LEVEL SECURITY;
ALTER TABLE identity.invitations FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON identity.invitations TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY platform_access ON identity.invitations TO msp_platform USING (true) WITH CHECK (true);

ALTER TABLE platform.promotion_redemptions ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.promotion_redemptions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON platform.promotion_redemptions TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY platform_access ON platform.promotion_redemptions TO msp_platform USING (true) WITH CHECK (true);

ALTER TABLE platform.purchase_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.purchase_requests FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON platform.purchase_requests TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY platform_access ON platform.purchase_requests TO msp_platform USING (true) WITH CHECK (true);

ALTER TABLE platform.subscriptions ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.subscriptions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON platform.subscriptions TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY platform_access ON platform.subscriptions TO msp_platform USING (true) WITH CHECK (true);

ALTER TABLE platform.entitlement_grants ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.entitlement_grants FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON platform.entitlement_grants TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY platform_access ON platform.entitlement_grants TO msp_platform USING (true) WITH CHECK (true);

ALTER TABLE platform.tenant_module_entitlements ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.tenant_module_entitlements FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON platform.tenant_module_entitlements TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY platform_access ON platform.tenant_module_entitlements TO msp_platform USING (true) WITH CHECK (true);

ALTER TABLE platform.entitlement_status_history ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.entitlement_status_history FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON platform.entitlement_status_history TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY platform_access ON platform.entitlement_status_history TO msp_platform USING (true) WITH CHECK (true);

ALTER TABLE platform.invoices ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.invoices FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON platform.invoices TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY platform_access ON platform.invoices TO msp_platform USING (true) WITH CHECK (true);

ALTER TABLE platform.invoice_lines ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.invoice_lines FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON platform.invoice_lines TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY platform_access ON platform.invoice_lines TO msp_platform USING (true) WITH CHECK (true);

ALTER TABLE platform.payments ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.payments FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON platform.payments TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY platform_access ON platform.payments TO msp_platform USING (true) WITH CHECK (true);

ALTER TABLE platform.credit_accounts ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.credit_accounts FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON platform.credit_accounts TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY platform_access ON platform.credit_accounts TO msp_platform USING (true) WITH CHECK (true);

ALTER TABLE platform.credit_ledger ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.credit_ledger FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON platform.credit_ledger TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY platform_access ON platform.credit_ledger TO msp_platform USING (true) WITH CHECK (true);

ALTER TABLE platform.tenant_usage_limits ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.tenant_usage_limits FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON platform.tenant_usage_limits TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY platform_access ON platform.tenant_usage_limits TO msp_platform USING (true) WITH CHECK (true);

ALTER TABLE platform.usage_rollups ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.usage_rollups FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON platform.usage_rollups TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY platform_access ON platform.usage_rollups TO msp_platform USING (true) WITH CHECK (true);

ALTER TABLE audit.support_access_grants ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit.support_access_grants FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON audit.support_access_grants TO msp_app USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY platform_access ON audit.support_access_grants TO msp_platform USING (true) WITH CHECK (true);

-- A tenant row is visible to its own context, or to a user who is a member of it.
ALTER TABLE identity.tenants ENABLE ROW LEVEL SECURITY;
ALTER TABLE identity.tenants FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_visibility ON identity.tenants TO msp_app
    USING (
        id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
        OR EXISTS (
            SELECT 1 FROM identity.memberships m
            WHERE m.tenant_id = identity.tenants.id
              AND m.user_id = NULLIF(current_setting('app.user_id', true), '')::uuid
              AND m.status = 'active'
        )
    )
    WITH CHECK (id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY platform_access ON identity.tenants TO msp_platform USING (true) WITH CHECK (true);

-- System roles have a null tenant and are readable by everyone; tenant-defined roles are private.
ALTER TABLE identity.roles ENABLE ROW LEVEL SECURITY;
ALTER TABLE identity.roles FORCE ROW LEVEL SECURITY;
CREATE POLICY role_visibility ON identity.roles FOR SELECT TO msp_app
    USING (tenant_id IS NULL OR tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY platform_access ON identity.roles TO msp_platform USING (true) WITH CHECK (true);
-- Migrations seed the system roles.
CREATE POLICY owner_seed ON identity.roles TO msp_owner USING (true) WITH CHECK (true);

-- Audit: tenants read only their own rows. Events with no tenant are write-only for msp_app.
ALTER TABLE audit.events ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit.events FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_read ON audit.events FOR SELECT TO msp_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY tenant_write ON audit.events FOR INSERT TO msp_app
    WITH CHECK (tenant_id IS NULL OR tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE POLICY platform_access ON audit.events TO msp_platform USING (true) WITH CHECK (true);
-- The hash-chain trigger runs as the owner and must read earlier rows.
CREATE POLICY owner_chain_read ON audit.events FOR SELECT TO msp_owner USING (true);
CREATE POLICY owner_chain_write ON audit.events FOR INSERT TO msp_owner WITH CHECK (true);

ALTER TABLE audit.security_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit.security_events FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_write ON audit.security_events FOR INSERT TO msp_app WITH CHECK (true);
CREATE POLICY platform_access ON audit.security_events TO msp_platform USING (true) WITH CHECK (true);

-- msp_app: tenant-facing access. It has no rights on platform_admin* tables at all.
-- A tenant may rename itself but can never change its own status, so a bug cannot lift a suspension.
GRANT SELECT, INSERT ON identity.tenants TO msp_app;
GRANT UPDATE (name, default_locale, default_timezone, reporting_currency) ON identity.tenants TO msp_app;
GRANT SELECT, INSERT, UPDATE ON identity.tenant_profiles, identity.tenant_icps,
    identity.users, identity.memberships, identity.invitations, identity.sessions,
    identity.verification_tokens, identity.mfa_factors, identity.user_credentials TO msp_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON identity.mfa_recovery_codes TO msp_app;
GRANT DELETE ON identity.mfa_factors TO msp_app;
GRANT SELECT, INSERT ON identity.login_attempts TO msp_app;
GRANT SELECT ON identity.roles, identity.permissions, identity.role_permissions TO msp_app;

-- msp_app can read but never change commercial state, so no tenant request can grant itself access.
GRANT SELECT ON platform.modules, platform.module_dependencies, platform.products,
    platform.product_versions, platform.product_version_modules, platform.plans, platform.prices,
    platform.subscriptions, platform.entitlement_grants, platform.tenant_module_entitlements,
    platform.entitlement_status_history TO msp_app;

GRANT SELECT, INSERT ON audit.events TO msp_app;
GRANT INSERT ON audit.security_events TO msp_app;

-- msp_platform: admin use cases. It cannot read user password hashes or MFA secrets.
GRANT SELECT, INSERT, UPDATE ON identity.platform_admins, identity.platform_admin_sessions,
    identity.platform_admin_mfa_factors TO msp_platform;
GRANT SELECT, INSERT, DELETE ON identity.platform_admin_role_assignments TO msp_platform;
GRANT SELECT, INSERT ON identity.login_attempts TO msp_platform;
GRANT SELECT ON identity.tenants TO msp_platform;
GRANT UPDATE (status, suspended_at) ON identity.tenants TO msp_platform;
GRANT SELECT ON identity.tenant_profiles, identity.memberships, identity.roles TO msp_platform;
GRANT SELECT (id, email, email_verified_at, display_name, status, last_login_at, created_at) ON identity.users TO msp_platform;
GRANT SELECT, INSERT, UPDATE ON ALL TABLES IN SCHEMA platform TO msp_platform;
GRANT SELECT, INSERT ON audit.events, audit.security_events, audit.support_access_grants TO msp_platform;

-- msp_system: expired-session housekeeping only.
GRANT SELECT, DELETE ON identity.sessions, identity.platform_admin_sessions, identity.verification_tokens TO msp_system;
GRANT DELETE ON identity.login_attempts TO msp_system;

-- +goose Down
REVOKE ALL ON ALL TABLES IN SCHEMA identity, platform, audit FROM msp_app, msp_platform, msp_system;
REVOKE USAGE ON SCHEMA identity, platform, audit, shared FROM msp_app, msp_platform, msp_system;
