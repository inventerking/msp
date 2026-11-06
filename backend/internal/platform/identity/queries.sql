-- name: CreateUser :one
INSERT INTO identity.users (email, display_name, locale, timezone, status)
VALUES (@email, @display_name, 'en', 'UTC', 'pending_verification') RETURNING id;

-- name: GetUserByEmail :one
SELECT id, email, email_verified_at, display_name, status FROM identity.users
WHERE email = @email AND deleted_at IS NULL;

-- name: GetUserByID :one
SELECT id, email, email_verified_at, display_name, status FROM identity.users
WHERE id = @id AND deleted_at IS NULL;

-- name: MarkEmailVerified :exec
UPDATE identity.users SET email_verified_at = now(), status = 'active'
WHERE id = @id AND status = 'pending_verification';

-- name: TouchLastLogin :exec
UPDATE identity.users SET last_login_at = now() WHERE id = @id;

-- name: UpsertCredential :exec
INSERT INTO identity.user_credentials (user_id, password_hash, password_changed_at, must_change, updated_at)
VALUES (@user_id, @password_hash, now(), false, now())
ON CONFLICT (user_id) DO UPDATE SET password_hash = EXCLUDED.password_hash, password_changed_at = now(), updated_at = now();

-- name: GetCredential :one
SELECT password_hash FROM identity.user_credentials WHERE user_id = @user_id;

-- name: CreateSession :one
INSERT INTO identity.sessions (user_id, token_hash, ip_hash, user_agent, last_auth_at, expires_at, idle_expires_at, active_tenant_id)
VALUES (@user_id, @token_hash, sqlc.narg(ip_hash), sqlc.narg(user_agent), now(), @expires_at, @idle_expires_at, sqlc.narg(active_tenant_id))
RETURNING id;

-- name: GetSessionByToken :one
SELECT s.id, s.user_id, s.active_tenant_id, s.mfa_verified_at, s.last_auth_at, s.expires_at, s.idle_expires_at,
       u.email, u.display_name, u.status AS user_status,
       EXISTS (SELECT 1 FROM identity.mfa_factors f WHERE f.user_id = s.user_id AND f.verified_at IS NOT NULL) AS has_mfa
FROM identity.sessions s JOIN identity.users u ON u.id = s.user_id
WHERE s.token_hash = @token_hash AND s.revoked_at IS NULL AND s.expires_at > now() AND s.idle_expires_at > now();

-- name: TouchSession :exec
UPDATE identity.sessions SET idle_expires_at = @idle_expires_at WHERE id = @id;

-- name: SetSessionTenant :exec
UPDATE identity.sessions SET active_tenant_id = sqlc.narg(tenant_id) WHERE id = @id;

-- name: SetSessionMFAVerified :exec
UPDATE identity.sessions SET mfa_verified_at = now(), last_auth_at = now() WHERE id = @id;

-- name: SetSessionLastAuth :exec
UPDATE identity.sessions SET last_auth_at = now() WHERE id = @id;

-- name: RevokeSession :execrows
UPDATE identity.sessions SET revoked_at = now(), revoke_reason = @reason
WHERE id = @id AND user_id = @user_id AND revoked_at IS NULL;

-- name: RevokeUserSessions :exec
UPDATE identity.sessions SET revoked_at = now(), revoke_reason = @reason
WHERE user_id = @user_id AND revoked_at IS NULL AND (sqlc.narg(except_id)::uuid IS NULL OR id <> sqlc.narg(except_id)::uuid);

-- name: ListUserSessions :many
SELECT id, user_agent, created_at, expires_at FROM identity.sessions
WHERE user_id = @user_id AND revoked_at IS NULL AND expires_at > now() AND idle_expires_at > now()
ORDER BY created_at DESC;

-- name: CreateVerificationToken :exec
INSERT INTO identity.verification_tokens (user_id, purpose, token_hash, expires_at)
VALUES (@user_id, @purpose, @token_hash, @expires_at);

-- name: ConsumeVerificationToken :one
UPDATE identity.verification_tokens SET consumed_at = now()
WHERE token_hash = @token_hash AND purpose = @purpose AND consumed_at IS NULL AND expires_at > now()
RETURNING user_id;

-- name: CreateMFAFactor :one
INSERT INTO identity.mfa_factors (user_id, kind, secret_ciphertext, key_id)
VALUES (@user_id, 'totp', @secret_ciphertext, @key_id) RETURNING id;

-- name: DeleteUnverifiedFactors :exec
DELETE FROM identity.mfa_factors WHERE user_id = @user_id AND verified_at IS NULL;

-- name: DeleteAllFactors :exec
DELETE FROM identity.mfa_factors WHERE user_id = @user_id;

-- name: GetTOTPFactor :one
SELECT id, secret_ciphertext, key_id, verified_at, last_used_at FROM identity.mfa_factors
WHERE user_id = @user_id AND kind = 'totp' AND (verified_at IS NOT NULL) = @verified::boolean
ORDER BY created_at DESC LIMIT 1;

-- name: MarkFactorVerified :exec
UPDATE identity.mfa_factors SET verified_at = now(), last_used_at = @used_at WHERE id = @id;

-- name: MarkFactorUsed :execrows
UPDATE identity.mfa_factors SET last_used_at = @used_at
WHERE id = @id AND (last_used_at IS NULL OR last_used_at < @used_at);

-- name: CreateRecoveryCode :exec
INSERT INTO identity.mfa_recovery_codes (user_id, code_hash) VALUES (@user_id, @code_hash);

-- name: DeleteRecoveryCodes :exec
DELETE FROM identity.mfa_recovery_codes WHERE user_id = @user_id;

-- name: UseRecoveryCode :execrows
UPDATE identity.mfa_recovery_codes SET used_at = now()
WHERE user_id = @user_id AND code_hash = @code_hash AND used_at IS NULL;

-- name: RecordLoginAttempt :exec
INSERT INTO identity.login_attempts (email_hash, ip_hash, domain, succeeded, failure_reason)
VALUES (@email_hash, @ip_hash, @domain, @succeeded, sqlc.narg(failure_reason));

-- name: CountRecentFailures :one
SELECT
  count(*) FILTER (WHERE email_hash = @email_hash)::int AS by_email,
  count(*) FILTER (WHERE ip_hash = @ip_hash)::int AS by_ip
FROM identity.login_attempts
WHERE domain = @domain AND NOT succeeded AND created_at > @since AND (email_hash = @email_hash OR ip_hash = @ip_hash);

-- name: CreateTenant :one
INSERT INTO identity.tenants (id, slug, name, status, default_locale, default_timezone, reporting_currency, data_region, owner_user_id)
VALUES (@id, @slug, @name, 'active', 'en', 'UTC', 'USD', @data_region, @owner_user_id)
RETURNING id, slug, name, status;

-- name: GetTenant :one
SELECT id, slug, name, status, default_locale, default_timezone, reporting_currency, suspended_at, created_at
FROM identity.tenants WHERE id = @id AND deleted_at IS NULL;

-- name: UpdateTenant :exec
UPDATE identity.tenants
SET name = COALESCE(sqlc.narg(name), name),
    default_locale = COALESCE(sqlc.narg(default_locale), default_locale),
    default_timezone = COALESCE(sqlc.narg(default_timezone), default_timezone),
    reporting_currency = COALESCE(sqlc.narg(reporting_currency), reporting_currency)
WHERE id = @id;

-- name: CreateTenantProfile :exec
INSERT INTO identity.tenant_profiles (tenant_id, onboarding_status) VALUES (@tenant_id, 'not_started');

-- name: GetTenantProfile :one
SELECT legal_name, website_domain, country_code, onboarding_status FROM identity.tenant_profiles WHERE tenant_id = @tenant_id;

-- name: UpdateTenantProfile :exec
UPDATE identity.tenant_profiles
SET legal_name = COALESCE(sqlc.narg(legal_name), legal_name),
    website_domain = COALESCE(sqlc.narg(website_domain), website_domain),
    country_code = COALESCE(sqlc.narg(country_code), country_code),
    onboarding_status = 'in_progress'
WHERE tenant_id = @tenant_id;

-- name: GetRoleByKey :one
SELECT id, key, name FROM identity.roles WHERE key = @key AND tenant_id IS NULL;

-- name: ListRoles :many
SELECT id, key, name FROM identity.roles ORDER BY created_at, key;

-- name: CreateMembership :one
INSERT INTO identity.memberships (tenant_id, user_id, role_id, status, invited_by, joined_at)
VALUES (@tenant_id, @user_id, @role_id, 'active', sqlc.narg(invited_by), now())
ON CONFLICT (tenant_id, user_id) DO UPDATE SET role_id = EXCLUDED.role_id, status = 'active', joined_at = now(), removed_at = NULL
RETURNING id;

-- name: GetActiveMembership :one
SELECT m.id, m.role_id, r.key AS role_key, t.status AS tenant_status, t.name AS tenant_name, t.slug AS tenant_slug
FROM identity.memberships m
JOIN identity.roles r ON r.id = m.role_id
JOIN identity.tenants t ON t.id = m.tenant_id
WHERE m.tenant_id = @tenant_id AND m.user_id = @user_id AND m.status = 'active' AND t.deleted_at IS NULL;

-- name: ListRolePermissions :many
SELECT p.key FROM identity.role_permissions rp JOIN identity.permissions p ON p.id = rp.permission_id
WHERE rp.role_id = @role_id ORDER BY p.key;

-- name: ListUserMemberships :many
SELECT m.tenant_id, t.name AS tenant_name, r.key AS role_key
FROM identity.memberships m
JOIN identity.tenants t ON t.id = m.tenant_id
JOIN identity.roles r ON r.id = m.role_id
WHERE m.user_id = @user_id AND m.status = 'active' AND t.deleted_at IS NULL
ORDER BY t.name;

-- name: ListMembers :many
SELECT m.id, m.user_id, u.email, u.display_name, r.key AS role_key, m.status
FROM identity.memberships m
JOIN identity.users u ON u.id = m.user_id
JOIN identity.roles r ON r.id = m.role_id
WHERE m.tenant_id = @tenant_id AND m.status <> 'removed'
ORDER BY u.display_name;

-- name: GetMember :one
SELECT m.id, m.user_id, u.email, u.display_name, r.key AS role_key, m.status
FROM identity.memberships m
JOIN identity.users u ON u.id = m.user_id
JOIN identity.roles r ON r.id = m.role_id
WHERE m.tenant_id = @tenant_id AND m.id = @id AND m.status <> 'removed'
FOR UPDATE OF m;

-- name: UpdateMemberRole :exec
UPDATE identity.memberships SET role_id = @role_id WHERE tenant_id = @tenant_id AND id = @id;

-- name: RemoveMember :exec
UPDATE identity.memberships SET status = 'removed', removed_at = now() WHERE tenant_id = @tenant_id AND id = @id;

-- name: CountOwners :one
SELECT count(*)::int FROM identity.memberships m JOIN identity.roles r ON r.id = m.role_id
WHERE m.tenant_id = @tenant_id AND m.status = 'active' AND r.key = 'owner';

-- name: LockTenantOwners :exec
SELECT pg_advisory_xact_lock(hashtextextended('owners:' || (@tenant_id::uuid)::text, 0));

-- name: CreateInvitation :one
INSERT INTO identity.invitations (tenant_id, email, role_id, token_hash, invited_by, expires_at)
VALUES (@tenant_id, @email, @role_id, @token_hash, @invited_by, @expires_at)
RETURNING id, expires_at;

-- name: ListInvitations :many
SELECT i.id, i.email, r.key AS role_key, i.expires_at
FROM identity.invitations i JOIN identity.roles r ON r.id = i.role_id
WHERE i.tenant_id = @tenant_id AND i.accepted_at IS NULL AND i.revoked_at IS NULL AND i.expires_at > now()
ORDER BY i.created_at DESC;

-- name: GetInvitationByHash :one
SELECT id, tenant_id, email, role_id FROM identity.invitations
WHERE tenant_id = @tenant_id AND token_hash = @token_hash AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > now();

-- name: AcceptInvitation :exec
UPDATE identity.invitations SET accepted_at = now() WHERE tenant_id = @tenant_id AND id = @id;

-- name: RevokeInvitation :execrows
UPDATE identity.invitations SET revoked_at = now()
WHERE tenant_id = @tenant_id AND id = @id AND accepted_at IS NULL AND revoked_at IS NULL;

-- name: CreateAdmin :one
INSERT INTO identity.platform_admins (email, display_name, password_hash, status)
VALUES (@email, @display_name, @password_hash, 'active') RETURNING id;

-- name: GetAdminByEmail :one
SELECT id, email, display_name, password_hash, status FROM identity.platform_admins WHERE email = @email;

-- name: ListAdmins :many
SELECT a.id, a.email, a.display_name, a.status,
       COALESCE(ARRAY(SELECT ra.role FROM identity.platform_admin_role_assignments ra WHERE ra.admin_id = a.id ORDER BY ra.role), '{}')::text[] AS roles
FROM identity.platform_admins a ORDER BY a.email;

-- name: AddAdminRole :exec
INSERT INTO identity.platform_admin_role_assignments (admin_id, role, granted_by, granted_at)
VALUES (@admin_id, @role, sqlc.narg(granted_by), now());

-- name: TouchAdminLogin :exec
UPDATE identity.platform_admins SET last_login_at = now() WHERE id = @id;

-- name: CreateAdminSession :one
INSERT INTO identity.platform_admin_sessions (admin_id, token_hash, ip_hash, user_agent, last_auth_at, expires_at, idle_expires_at)
VALUES (@admin_id, @token_hash, sqlc.narg(ip_hash), sqlc.narg(user_agent), now(), @expires_at, @idle_expires_at) RETURNING id;

-- name: GetAdminSessionByToken :one
SELECT s.id, s.admin_id, s.mfa_verified_at, s.last_auth_at, s.idle_expires_at, a.email, a.display_name, a.status,
       COALESCE(ARRAY(SELECT ra.role FROM identity.platform_admin_role_assignments ra WHERE ra.admin_id = a.id ORDER BY ra.role), '{}')::text[] AS roles,
       EXISTS (SELECT 1 FROM identity.platform_admin_mfa_factors f WHERE f.admin_id = s.admin_id AND f.verified_at IS NOT NULL) AS has_mfa
FROM identity.platform_admin_sessions s JOIN identity.platform_admins a ON a.id = s.admin_id
WHERE s.token_hash = @token_hash AND s.revoked_at IS NULL AND s.expires_at > now() AND s.idle_expires_at > now();

-- name: TouchAdminSession :exec
UPDATE identity.platform_admin_sessions SET idle_expires_at = @idle_expires_at WHERE id = @id;

-- name: SetAdminSessionMFAVerified :exec
UPDATE identity.platform_admin_sessions SET mfa_verified_at = now(), last_auth_at = now() WHERE id = @id;

-- name: RevokeAdminSession :exec
UPDATE identity.platform_admin_sessions SET revoked_at = now() WHERE id = @id;

-- name: CreateAdminFactor :one
INSERT INTO identity.platform_admin_mfa_factors (admin_id, kind, secret_ciphertext, key_id)
VALUES (@admin_id, 'totp', @secret_ciphertext, @key_id) RETURNING id;

-- name: GetAdminFactor :one
SELECT id, secret_ciphertext, key_id, last_used_at FROM identity.platform_admin_mfa_factors
WHERE admin_id = @admin_id AND kind = 'totp' AND (verified_at IS NOT NULL) = @verified::boolean
ORDER BY created_at DESC LIMIT 1;

-- name: MarkAdminFactorVerified :exec
UPDATE identity.platform_admin_mfa_factors SET verified_at = now(), last_used_at = @used_at WHERE id = @id;

-- name: MarkAdminFactorUsed :execrows
UPDATE identity.platform_admin_mfa_factors SET last_used_at = @used_at
WHERE id = @id AND (last_used_at IS NULL OR last_used_at < @used_at);

-- name: AdminListTenants :many
SELECT id, slug, name, status, suspended_at, created_at FROM identity.tenants
WHERE deleted_at IS NULL AND (sqlc.narg(before_id)::uuid IS NULL OR id < sqlc.narg(before_id)::uuid)
ORDER BY id DESC LIMIT @row_limit;

-- name: AdminGetTenant :one
SELECT id, slug, name, status, suspended_at, created_at FROM identity.tenants WHERE id = @id AND deleted_at IS NULL;

-- name: AdminSetTenantStatus :execrows
UPDATE identity.tenants SET status = @status, suspended_at = sqlc.narg(suspended_at)
WHERE id = @id AND status = @from_status;

-- name: AdminListTenantUsers :many
SELECT m.user_id, u.email, u.display_name, r.key AS role_key, m.status
FROM identity.memberships m
JOIN identity.users u ON u.id = m.user_id
JOIN identity.roles r ON r.id = m.role_id
WHERE m.tenant_id = @tenant_id AND m.status <> 'removed'
ORDER BY u.display_name;
