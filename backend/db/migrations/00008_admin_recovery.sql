-- +goose Up
-- Recovering an administrator who lost an authenticator removes their factor.
GRANT DELETE ON identity.platform_admin_mfa_factors TO msp_platform;

-- +goose Down
REVOKE DELETE ON identity.platform_admin_mfa_factors FROM msp_platform;
