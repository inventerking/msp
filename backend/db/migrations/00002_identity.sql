-- +goose Up
CREATE TABLE identity.tenants (
    id uuid NOT NULL DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    slug citext NOT NULL,
    name text NOT NULL,
    status text NOT NULL,
    default_locale text NOT NULL,
    default_timezone text NOT NULL,
    reporting_currency char(3) NOT NULL,
    data_region text NOT NULL,
    owner_user_id uuid,
    suspended_at timestamptz,
    deleted_at timestamptz,
    PRIMARY KEY (id),
    CONSTRAINT tenants_ck1 CHECK (status IN ('pending','active','suspended','offboarding','deleted'))
);

CREATE TABLE identity.tenant_profiles (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    legal_name text,
    website_domain citext,
    country_code char(2),
    address jsonb,
    employee_band text,
    logo_file_id uuid,
    onboarding_status text NOT NULL,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT tenant_profiles_ck1 CHECK (onboarding_status IN ('not_started','in_progress','complete'))
);

CREATE TABLE identity.tenant_icps (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    name text NOT NULL,
    is_default boolean NOT NULL DEFAULT false,
    industries text[] NOT NULL DEFAULT '{}',
    countries text[] NOT NULL DEFAULT '{}',
    min_employees int,
    max_employees int,
    technologies text[] NOT NULL DEFAULT '{}',
    version int NOT NULL DEFAULT 1,
    created_by uuid,
    deleted_at timestamptz,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT tenant_icps_ck1 CHECK (min_employees IS NULL OR max_employees IS NULL OR min_employees <= max_employees)
);

CREATE TABLE identity.users (
    id uuid NOT NULL DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    email citext NOT NULL,
    email_verified_at timestamptz,
    display_name text NOT NULL,
    locale text NOT NULL,
    timezone text NOT NULL,
    status text NOT NULL,
    last_login_at timestamptz,
    deleted_at timestamptz,
    PRIMARY KEY (id),
    CONSTRAINT users_ck1 CHECK (status IN ('pending_verification','active','locked','disabled'))
);

CREATE TABLE identity.user_credentials (
    user_id uuid NOT NULL DEFAULT uuidv7(),
    password_hash text NOT NULL,
    password_changed_at timestamptz NOT NULL,
    must_change boolean NOT NULL DEFAULT false,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id)
);

CREATE TABLE identity.memberships (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    user_id uuid NOT NULL,
    role_id uuid NOT NULL,
    status text NOT NULL,
    invited_by uuid,
    joined_at timestamptz,
    removed_at timestamptz,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT memberships_ck1 CHECK (status IN ('invited','active','suspended','removed'))
);

CREATE TABLE identity.roles (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid,
    key text NOT NULL,
    name text NOT NULL,
    is_system boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id),
    CONSTRAINT roles_ck1 CHECK (is_system = (tenant_id IS NULL))
);

CREATE TABLE identity.permissions (
    id uuid NOT NULL DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    key text NOT NULL,
    description text NOT NULL,
    deprecated boolean NOT NULL DEFAULT false,
    PRIMARY KEY (id)
);

CREATE TABLE identity.role_permissions (
    role_id uuid NOT NULL,
    permission_id uuid NOT NULL,
    PRIMARY KEY (role_id, permission_id)
);

CREATE TABLE identity.invitations (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    email citext NOT NULL,
    role_id uuid NOT NULL,
    token_hash bytea NOT NULL,
    invited_by uuid NOT NULL,
    expires_at timestamptz NOT NULL,
    accepted_at timestamptz,
    revoked_at timestamptz,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id)
);

CREATE TABLE identity.sessions (
    id uuid NOT NULL DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    user_id uuid NOT NULL,
    active_tenant_id uuid,
    token_hash bytea NOT NULL,
    ip_hash bytea,
    user_agent text,
    mfa_verified_at timestamptz,
    last_auth_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    idle_expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    revoke_reason text,
    PRIMARY KEY (id)
);

CREATE TABLE identity.verification_tokens (
    id uuid NOT NULL DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    user_id uuid NOT NULL,
    purpose text NOT NULL,
    token_hash bytea NOT NULL,
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    PRIMARY KEY (id),
    CONSTRAINT verification_tokens_ck1 CHECK (purpose IN ('email_verify','password_reset','email_change'))
);

CREATE TABLE identity.mfa_factors (
    id uuid NOT NULL DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    user_id uuid NOT NULL,
    kind text NOT NULL,
    secret_ciphertext bytea,
    key_id text,
    credential_id bytea,
    public_key bytea,
    verified_at timestamptz,
    last_used_at timestamptz,
    PRIMARY KEY (id),
    CONSTRAINT mfa_factors_ck1 CHECK (kind IN ('totp','webauthn'))
);

CREATE TABLE identity.mfa_recovery_codes (
    id uuid NOT NULL DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    user_id uuid NOT NULL,
    code_hash bytea NOT NULL,
    used_at timestamptz,
    PRIMARY KEY (id)
);

CREATE TABLE identity.login_attempts (
    id uuid NOT NULL DEFAULT uuidv7(),
    email_hash bytea NOT NULL,
    ip_hash bytea NOT NULL,
    domain text NOT NULL,
    succeeded boolean NOT NULL,
    failure_reason text,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id),
    CONSTRAINT login_attempts_ck1 CHECK (domain IN ('tenant','platform'))
);

CREATE TABLE identity.platform_admins (
    id uuid NOT NULL DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    email citext NOT NULL,
    display_name text NOT NULL,
    password_hash text NOT NULL,
    status text NOT NULL,
    last_login_at timestamptz,
    PRIMARY KEY (id),
    CONSTRAINT platform_admins_ck1 CHECK (status IN ('invited','active','disabled'))
);

CREATE TABLE identity.platform_admin_role_assignments (
    admin_id uuid NOT NULL,
    role text NOT NULL,
    granted_by uuid,
    granted_at timestamptz NOT NULL,
    PRIMARY KEY (admin_id, role),
    CONSTRAINT platform_admin_role_assignments_ck1 CHECK (role IN ('super_admin','platform_support','billing_admin','integration_admin','platform_analyst'))
);

CREATE TABLE identity.platform_admin_mfa_factors (
    id uuid NOT NULL DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    admin_id uuid NOT NULL,
    kind text NOT NULL,
    secret_ciphertext bytea,
    key_id text,
    credential_id bytea,
    public_key bytea,
    verified_at timestamptz,
    last_used_at timestamptz,
    PRIMARY KEY (id),
    CONSTRAINT platform_admin_mfa_factors_ck1 CHECK (kind IN ('totp','webauthn'))
);

CREATE TABLE identity.platform_admin_sessions (
    id uuid NOT NULL DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    admin_id uuid NOT NULL,
    token_hash bytea NOT NULL,
    ip_hash bytea,
    user_agent text,
    mfa_verified_at timestamptz,
    last_auth_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    idle_expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    PRIMARY KEY (id)
);

ALTER TABLE identity.tenant_profiles ADD CONSTRAINT tenant_profiles_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE identity.tenant_icps ADD CONSTRAINT tenant_icps_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE identity.user_credentials ADD CONSTRAINT user_credentials_user_id_fk FOREIGN KEY (user_id) REFERENCES identity.users (id) ON DELETE CASCADE;
ALTER TABLE identity.memberships ADD CONSTRAINT memberships_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE identity.memberships ADD CONSTRAINT memberships_user_id_fk FOREIGN KEY (user_id) REFERENCES identity.users (id) ON DELETE RESTRICT;
ALTER TABLE identity.memberships ADD CONSTRAINT memberships_role_id_fk FOREIGN KEY (role_id) REFERENCES identity.roles (id) ON DELETE RESTRICT;
ALTER TABLE identity.roles ADD CONSTRAINT roles_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE CASCADE;
ALTER TABLE identity.role_permissions ADD CONSTRAINT role_permissions_role_id_fk FOREIGN KEY (role_id) REFERENCES identity.roles (id) ON DELETE CASCADE;
ALTER TABLE identity.role_permissions ADD CONSTRAINT role_permissions_permission_id_fk FOREIGN KEY (permission_id) REFERENCES identity.permissions (id) ON DELETE RESTRICT;
ALTER TABLE identity.invitations ADD CONSTRAINT invitations_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;
ALTER TABLE identity.invitations ADD CONSTRAINT invitations_role_id_fk FOREIGN KEY (role_id) REFERENCES identity.roles (id) ON DELETE RESTRICT;
ALTER TABLE identity.sessions ADD CONSTRAINT sessions_user_id_fk FOREIGN KEY (user_id) REFERENCES identity.users (id) ON DELETE CASCADE;
ALTER TABLE identity.sessions ADD CONSTRAINT sessions_active_tenant_id_fk FOREIGN KEY (active_tenant_id) REFERENCES identity.tenants (id) ON DELETE SET NULL;
ALTER TABLE identity.verification_tokens ADD CONSTRAINT verification_tokens_user_id_fk FOREIGN KEY (user_id) REFERENCES identity.users (id) ON DELETE CASCADE;
ALTER TABLE identity.mfa_factors ADD CONSTRAINT mfa_factors_user_id_fk FOREIGN KEY (user_id) REFERENCES identity.users (id) ON DELETE CASCADE;
ALTER TABLE identity.mfa_recovery_codes ADD CONSTRAINT mfa_recovery_codes_user_id_fk FOREIGN KEY (user_id) REFERENCES identity.users (id) ON DELETE CASCADE;
ALTER TABLE identity.platform_admin_role_assignments ADD CONSTRAINT platform_admin_role_assignments_admin_id_fk FOREIGN KEY (admin_id) REFERENCES identity.platform_admins (id) ON DELETE CASCADE;
ALTER TABLE identity.platform_admin_role_assignments ADD CONSTRAINT platform_admin_role_assignments_granted_by_fk FOREIGN KEY (granted_by) REFERENCES identity.platform_admins (id) ON DELETE SET NULL;
ALTER TABLE identity.platform_admin_mfa_factors ADD CONSTRAINT platform_admin_mfa_factors_admin_id_fk FOREIGN KEY (admin_id) REFERENCES identity.platform_admins (id) ON DELETE CASCADE;
ALTER TABLE identity.platform_admin_sessions ADD CONSTRAINT platform_admin_sessions_admin_id_fk FOREIGN KEY (admin_id) REFERENCES identity.platform_admins (id) ON DELETE CASCADE;

CREATE UNIQUE INDEX tenants_uq1 ON identity.tenants (slug);
CREATE INDEX tenants_ix1 ON identity.tenants (status);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON identity.tenants FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX tenant_profiles_uq1 ON identity.tenant_profiles (tenant_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON identity.tenant_profiles FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX tenant_icps_uq1 ON identity.tenant_icps (tenant_id) WHERE is_default AND deleted_at IS NULL;
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON identity.tenant_icps FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX users_uq1 ON identity.users (email) WHERE deleted_at IS NULL;
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON identity.users FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE INDEX user_credentials_user_id_idx ON identity.user_credentials (user_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON identity.user_credentials FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX memberships_uq1 ON identity.memberships (tenant_id, user_id);
CREATE INDEX memberships_ix1 ON identity.memberships (user_id, status);
CREATE INDEX memberships_user_id_idx ON identity.memberships (tenant_id, user_id);
CREATE INDEX memberships_role_id_idx ON identity.memberships (tenant_id, role_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON identity.memberships FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX roles_uq1 ON identity.roles (tenant_id, key) NULLS NOT DISTINCT;
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON identity.roles FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX permissions_uq1 ON identity.permissions (key);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON identity.permissions FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE INDEX role_permissions_permission_id_idx ON identity.role_permissions (permission_id);
CREATE UNIQUE INDEX invitations_uq1 ON identity.invitations (token_hash);
CREATE UNIQUE INDEX invitations_uq2 ON identity.invitations (tenant_id, email) WHERE accepted_at IS NULL AND revoked_at IS NULL;
CREATE INDEX invitations_role_id_idx ON identity.invitations (tenant_id, role_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON identity.invitations FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX sessions_uq1 ON identity.sessions (token_hash);
CREATE INDEX sessions_ix1 ON identity.sessions (user_id) WHERE revoked_at IS NULL;
CREATE INDEX sessions_ix2 ON identity.sessions (expires_at);
CREATE INDEX sessions_user_id_idx ON identity.sessions (user_id);
CREATE INDEX sessions_active_tenant_id_idx ON identity.sessions (active_tenant_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON identity.sessions FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX verification_tokens_uq1 ON identity.verification_tokens (token_hash);
CREATE INDEX verification_tokens_user_id_idx ON identity.verification_tokens (user_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON identity.verification_tokens FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE INDEX mfa_factors_user_id_idx ON identity.mfa_factors (user_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON identity.mfa_factors FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX mfa_recovery_codes_uq1 ON identity.mfa_recovery_codes (user_id, code_hash);
CREATE INDEX mfa_recovery_codes_user_id_idx ON identity.mfa_recovery_codes (user_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON identity.mfa_recovery_codes FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE INDEX login_attempts_ix1 ON identity.login_attempts (email_hash, created_at DESC);
CREATE INDEX login_attempts_ix2 ON identity.login_attempts (ip_hash, created_at DESC);
CREATE UNIQUE INDEX platform_admins_uq1 ON identity.platform_admins (email);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON identity.platform_admins FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE INDEX platform_admin_role_assignments_granted_by_idx ON identity.platform_admin_role_assignments (granted_by);
CREATE INDEX platform_admin_mfa_factors_admin_id_idx ON identity.platform_admin_mfa_factors (admin_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON identity.platform_admin_mfa_factors FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE UNIQUE INDEX platform_admin_sessions_uq1 ON identity.platform_admin_sessions (token_hash);
CREATE INDEX platform_admin_sessions_admin_id_idx ON identity.platform_admin_sessions (admin_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON identity.platform_admin_sessions FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();

-- +goose Down
DROP TABLE IF EXISTS identity.platform_admin_sessions CASCADE;
DROP TABLE IF EXISTS identity.platform_admin_mfa_factors CASCADE;
DROP TABLE IF EXISTS identity.platform_admin_role_assignments CASCADE;
DROP TABLE IF EXISTS identity.platform_admins CASCADE;
DROP TABLE IF EXISTS identity.login_attempts CASCADE;
DROP TABLE IF EXISTS identity.mfa_recovery_codes CASCADE;
DROP TABLE IF EXISTS identity.mfa_factors CASCADE;
DROP TABLE IF EXISTS identity.verification_tokens CASCADE;
DROP TABLE IF EXISTS identity.sessions CASCADE;
DROP TABLE IF EXISTS identity.invitations CASCADE;
DROP TABLE IF EXISTS identity.role_permissions CASCADE;
DROP TABLE IF EXISTS identity.permissions CASCADE;
DROP TABLE IF EXISTS identity.roles CASCADE;
DROP TABLE IF EXISTS identity.memberships CASCADE;
DROP TABLE IF EXISTS identity.user_credentials CASCADE;
DROP TABLE IF EXISTS identity.users CASCADE;
DROP TABLE IF EXISTS identity.tenant_icps CASCADE;
DROP TABLE IF EXISTS identity.tenant_profiles CASCADE;
DROP TABLE IF EXISTS identity.tenants CASCADE;
