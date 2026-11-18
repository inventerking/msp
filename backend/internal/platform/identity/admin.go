package identity

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/crypto"
	"mspengine/internal/kernel/db"
	"mspengine/internal/kernel/events"
	"mspengine/internal/kernel/mailer"
	"mspengine/internal/platform/audit"
	auditrepo "mspengine/internal/platform/audit/repo"
	"mspengine/internal/platform/identity/repo"
)

// AuditRow is one audit event as read back.
type AuditRow = auditrepo.ListEventsRow

func adminEvent(a authctx.AdminPrincipal, tenantID *uuid.UUID, action, resType string, resID *uuid.UUID, after any) audit.Event {
	return audit.Event{TenantID: tenantID, ActorType: "platform_admin", ActorID: &a.AdminID, Action: action, ResourceType: resType, ResourceID: resID, After: after}
}

// AdminLogin verifies an admin's credentials. The session is unusable until MFA is verified.
func (s *Service) AdminLogin(ctx context.Context, email, password string) (string, bool, error) {
	email = normalizeEmail(email)
	emailHash := s.keys.HMACValue("login-email", email)
	m := authctx.MetaFrom(ctx)
	ipHash := m.IPHash
	if ipHash == nil {
		ipHash = s.keys.HMACValue("ip", "")
	}
	if err := s.checkThrottle(ctx, "platform", emailHash, ipHash); err != nil {
		return "", false, err
	}
	var admin repo.GetAdminByEmailRow
	err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		var err error
		admin, err = repo.New(tx).GetAdminByEmail(ctx, email)
		return err
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", false, err
	}
	known := err == nil
	stored := admin.PasswordHash
	if !known {
		stored = s.dummyHash
	}
	ok, _ := crypto.VerifyPassword(stored, password)
	if !known || !ok || admin.Status != "active" {
		s.recordFailure(ctx, emailHash, ipHash, "platform", "bad_credentials")
		return "", false, apperr.InvalidCredentials()
	}
	token, hash, err := crypto.NewToken()
	if err != nil {
		return "", false, err
	}
	enrolled := false
	err = db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		q := repo.New(tx)
		now := time.Now()
		var ua *string
		if m.UserAgent != "" {
			ua = &m.UserAgent
		}
		id, err := q.CreateAdminSession(ctx, repo.CreateAdminSessionParams{AdminID: admin.ID, TokenHash: hash, IpHash: ipHash, UserAgent: ua, ExpiresAt: now.Add(adminSessionAbsolute), IdleExpiresAt: now.Add(adminSessionIdle)})
		if err != nil {
			return err
		}
		if _, err := q.GetAdminFactor(ctx, repo.GetAdminFactorParams{AdminID: admin.ID, Verified: true}); err == nil {
			enrolled = true
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err := q.RecordLoginAttempt(ctx, repo.RecordLoginAttemptParams{EmailHash: emailHash, IpHash: ipHash, Domain: "platform", Succeeded: true}); err != nil {
			return err
		}
		if err := q.TouchAdminLogin(ctx, admin.ID); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{ActorType: "platform_admin", ActorID: &admin.ID, Action: "admin_session.created", ResourceType: "session", ResourceID: &id})
	})
	return token, enrolled, err
}

// ResolveAdminSession returns the live admin session for a token.
func (s *Service) ResolveAdminSession(ctx context.Context, token string) (authctx.AdminPrincipal, error) {
	var a authctx.AdminPrincipal
	err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		q := repo.New(tx)
		r, err := q.GetAdminSessionByToken(ctx, crypto.HashToken(token))
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.Unauthenticated()
		}
		if err != nil {
			return err
		}
		if r.Status != "active" {
			return apperr.Unauthenticated()
		}
		a = authctx.AdminPrincipal{AdminID: r.AdminID, SessionID: r.ID, Email: r.Email, DisplayName: r.DisplayName, Status: r.Status, Roles: r.Roles, HasMFA: r.HasMfa, MFAVerified: r.MfaVerifiedAt != nil}
		next := time.Now().Add(adminSessionIdle)
		if next.Sub(r.IdleExpiresAt) > time.Minute {
			return q.TouchAdminSession(ctx, repo.TouchAdminSessionParams{ID: r.ID, IdleExpiresAt: next})
		}
		return nil
	})
	return a, err
}

// AdminLogout revokes the admin session.
func (s *Service) AdminLogout(ctx context.Context, a authctx.AdminPrincipal) error {
	return db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		if err := repo.New(tx).RevokeAdminSession(ctx, a.SessionID); err != nil {
			return err
		}
		return audit.Record(ctx, tx, adminEvent(a, nil, "admin_session.revoked", "session", &a.SessionID, nil))
	})
}

// AdminEnrollTOTP starts mandatory MFA setup. An existing factor cannot be replaced this way,
// so a stolen password alone cannot take over an enrolled admin account.
func (s *Service) AdminEnrollTOTP(ctx context.Context, a authctx.AdminPrincipal) (Enrollment, error) {
	if a.HasMFA {
		return Enrollment{}, apperr.Conflict("mfa_already_enabled", "Two-step verification is already on")
	}
	k, err := totp.Generate(totp.GenerateOpts{Issuer: issuer + " Admin", AccountName: a.Email, Period: totpPeriod, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
	if err != nil {
		return Enrollment{}, err
	}
	sealed, err := s.keys.Encrypt([]byte(k.Secret()), a.AdminID[:])
	if err != nil {
		return Enrollment{}, err
	}
	err = db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		_, err := repo.New(tx).CreateAdminFactor(ctx, repo.CreateAdminFactorParams{AdminID: a.AdminID, SecretCiphertext: sealed, KeyID: strPtr(s.keys.ID())})
		return err
	})
	if err != nil {
		return Enrollment{}, err
	}
	return Enrollment{Secret: k.Secret(), URL: k.URL()}, nil
}

// AdminConfirmTOTP activates the factor and completes MFA for this session.
func (s *Service) AdminConfirmTOTP(ctx context.Context, a authctx.AdminPrincipal, code string) error {
	return db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		q := repo.New(tx)
		f, err := q.GetAdminFactor(ctx, repo.GetAdminFactorParams{AdminID: a.AdminID, Verified: false})
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.Conflict("mfa_not_enrolling", "Start setup first")
		}
		if err != nil {
			return err
		}
		secret, err := s.keys.Decrypt(f.SecretCiphertext, a.AdminID[:])
		if err != nil {
			return wrap("decrypt factor", err)
		}
		step, ok := matchTOTP(string(secret), strings.TrimSpace(code), time.Now())
		if !ok {
			return invalidCode()
		}
		if err := q.MarkAdminFactorVerified(ctx, repo.MarkAdminFactorVerifiedParams{ID: f.ID, UsedAt: &step}); err != nil {
			return err
		}
		if err := q.SetAdminSessionMFAVerified(ctx, a.SessionID); err != nil {
			return err
		}
		return audit.Record(ctx, tx, adminEvent(a, nil, "admin_mfa.enabled", "platform_admin", &a.AdminID, nil))
	})
}

// AdminVerifyMFA completes login with an authenticator code.
func (s *Service) AdminVerifyMFA(ctx context.Context, a authctx.AdminPrincipal, code string) error {
	emailHash := s.keys.HMACValue("login-email", normalizeEmail(a.Email))
	ipHash := authctx.MetaFrom(ctx).IPHash
	if ipHash == nil {
		ipHash = s.keys.HMACValue("ip", "")
	}
	if err := s.checkThrottle(ctx, "platform", emailHash, ipHash); err != nil {
		return err
	}
	verified := false
	err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		q := repo.New(tx)
		f, err := q.GetAdminFactor(ctx, repo.GetAdminFactorParams{AdminID: a.AdminID, Verified: true})
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.Conflict("mfa_not_enabled", "Set up two-step verification first")
		}
		if err != nil {
			return err
		}
		secret, err := s.keys.Decrypt(f.SecretCiphertext, a.AdminID[:])
		if err != nil {
			return wrap("decrypt factor", err)
		}
		step, ok := matchTOTP(string(secret), strings.TrimSpace(code), time.Now())
		if !ok {
			return nil
		}
		n, err := q.MarkAdminFactorUsed(ctx, repo.MarkAdminFactorUsedParams{ID: f.ID, UsedAt: &step})
		if err != nil {
			return err
		}
		if n == 0 {
			return nil
		}
		verified = true
		if err := q.SetAdminSessionMFAVerified(ctx, a.SessionID); err != nil {
			return err
		}
		return audit.Record(ctx, tx, adminEvent(a, nil, "admin_mfa.verified", "session", &a.SessionID, nil))
	})
	if err != nil {
		return err
	}
	if !verified {
		s.recordFailure(ctx, emailHash, ipHash, "platform", "bad_mfa_code")
		return invalidCode()
	}
	return nil
}

// CreateAdmin adds a platform administrator. creator is nil only for the bootstrap command.
func (s *Service) CreateAdmin(ctx context.Context, creator *authctx.AdminPrincipal, email, name, password string, roles []string) (uuid.UUID, error) {
	email = normalizeEmail(email)
	if !validEmail(email) {
		return uuid.Nil, apperr.Validation(apperr.FieldError{Field: "email", Code: "invalid_email"})
	}
	if err := checkPassword("password", password); err != nil {
		return uuid.Nil, err
	}
	hash, err := crypto.HashPassword(password)
	if err != nil {
		return uuid.Nil, err
	}
	var id uuid.UUID
	err = db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		q := repo.New(tx)
		var err error
		id, err = q.CreateAdmin(ctx, repo.CreateAdminParams{Email: email, DisplayName: name, PasswordHash: hash})
		if isUnique(err) {
			return apperr.Conflict("admin_exists", "An admin with this email already exists")
		}
		if err != nil {
			return err
		}
		var by *uuid.UUID
		actor := "system"
		if creator != nil {
			by = &creator.AdminID
			actor = "platform_admin"
		}
		for _, r := range roles {
			if err := q.AddAdminRole(ctx, repo.AddAdminRoleParams{AdminID: id, Role: r, GrantedBy: by}); err != nil {
				return err
			}
		}
		return audit.Record(ctx, tx, audit.Event{ActorType: actor, ActorID: by, Action: "platform_admin.created", ResourceType: "platform_admin", ResourceID: &id, After: map[string]any{"roles": roles}})
	})
	return id, err
}

// AdminChangePassword sets a new password for the signed-in admin and ends their other sessions.
func (s *Service) AdminChangePassword(ctx context.Context, a authctx.AdminPrincipal, current, next string) error {
	if err := checkPassword("new_password", next); err != nil {
		return err
	}
	emailHash := s.keys.HMACValue("login-email", normalizeEmail(a.Email))
	ipHash := authctx.MetaFrom(ctx).IPHash
	if ipHash == nil {
		ipHash = s.keys.HMACValue("ip", "")
	}
	if err := s.checkThrottle(ctx, "platform", emailHash, ipHash); err != nil {
		return err
	}
	var row repo.GetAdminByIDRow
	err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		var err error
		row, err = repo.New(tx).GetAdminByID(ctx, a.AdminID)
		return err
	})
	if err != nil {
		return err
	}
	if ok, _ := crypto.VerifyPassword(row.PasswordHash, current); !ok {
		s.recordFailure(ctx, emailHash, ipHash, "platform", "bad_current_password")
		return apperr.InvalidCredentials()
	}
	hash, err := crypto.HashPassword(next)
	if err != nil {
		return err
	}
	return db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		q := repo.New(tx)
		if err := q.SetAdminPassword(ctx, repo.SetAdminPasswordParams{ID: a.AdminID, PasswordHash: hash}); err != nil {
			return err
		}
		if err := q.RevokeAdminSessions(ctx, repo.RevokeAdminSessionsParams{AdminID: a.AdminID, ExceptID: &a.SessionID}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, adminEvent(a, nil, "platform_admin.password_changed", "platform_admin", &a.AdminID, nil))
	})
}

// RecoverAdmin handles a lost password or authenticator: it sets a new password, removes the MFA
// factor and ends all sessions, so the admin must enrol again at next sign-in. actor is nil only
// for the break-glass command, which needs server access. Nobody can recover themselves through
// the API, because a stolen session could otherwise remove its own account's MFA.
func (s *Service) RecoverAdmin(ctx context.Context, actor *authctx.AdminPrincipal, target uuid.UUID, password, reason string) error {
	if actor != nil && actor.AdminID == target {
		return apperr.Conflict("cannot_recover_self", "Ask another Super Admin to do this")
	}
	if err := checkPassword("new_password", password); err != nil {
		return err
	}
	hash, err := crypto.HashPassword(password)
	if err != nil {
		return err
	}
	var email string
	err = db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		q := repo.New(tx)
		row, err := q.GetAdminByID(ctx, target)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		email = row.Email
		if err := q.SetAdminPassword(ctx, repo.SetAdminPasswordParams{ID: target, PasswordHash: hash}); err != nil {
			return err
		}
		if err := q.DeleteAdminFactors(ctx, target); err != nil {
			return err
		}
		if err := q.RevokeAdminSessions(ctx, repo.RevokeAdminSessionsParams{AdminID: target}); err != nil {
			return err
		}
		ev := audit.Event{ActorType: "system", Action: "platform_admin.recovered", ResourceType: "platform_admin", ResourceID: &target, After: map[string]any{"reason": reason}}
		if actor != nil {
			ev.ActorType, ev.ActorID = "platform_admin", &actor.AdminID
		}
		return audit.Record(ctx, tx, ev)
	})
	if err != nil {
		return err
	}
	s.sendAsync(ctx, mailer.Message{To: email, Subject: "Your administrator account was recovered",
		Body: "Your platform administrator password and two-step verification were reset, and all your sessions were signed out. If you did not ask for this, contact another Super Admin immediately.\n"})
	return nil
}

// AdminIDByEmail finds an admin for the break-glass command.
func (s *Service) AdminIDByEmail(ctx context.Context, email string) (uuid.UUID, error) {
	var id uuid.UUID
	err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		row, err := repo.New(tx).GetAdminByEmail(ctx, normalizeEmail(email))
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound()
		}
		id = row.ID
		return err
	})
	return id, err
}

// ListAdmins returns all platform administrators.
func (s *Service) ListAdmins(ctx context.Context) ([]repo.ListAdminsRow, error) {
	var out []repo.ListAdminsRow
	err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		var err error
		out, err = repo.New(tx).ListAdmins(ctx)
		return err
	})
	return out, err
}

// AdminListTenants returns tenants, newest first.
func (s *Service) AdminListTenants(ctx context.Context, before *uuid.UUID, limit int32) ([]repo.AdminListTenantsRow, error) {
	var out []repo.AdminListTenantsRow
	err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		var err error
		out, err = repo.New(tx).AdminListTenants(ctx, repo.AdminListTenantsParams{BeforeID: before, RowLimit: limit})
		return err
	})
	return out, err
}

// AdminGetTenant returns one tenant.
func (s *Service) AdminGetTenant(ctx context.Context, id uuid.UUID) (repo.AdminGetTenantRow, error) {
	var out repo.AdminGetTenantRow
	err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		var err error
		out, err = repo.New(tx).AdminGetTenant(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound()
		}
		return err
	})
	return out, err
}

// AdminSetTenantState suspends or reactivates a tenant.
func (s *Service) AdminSetTenantState(ctx context.Context, a authctx.AdminPrincipal, id uuid.UUID, suspend bool, reason string) (repo.AdminGetTenantRow, error) {
	status, from, action := "active", "suspended", "tenant.reactivated"
	var at *time.Time
	if suspend {
		now := time.Now()
		status, from, action, at = "suspended", "active", "tenant.suspended", &now
	}
	var out repo.AdminGetTenantRow
	err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		q := repo.New(tx)
		if _, err := q.AdminGetTenant(ctx, id); errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound()
		} else if err != nil {
			return err
		}
		n, err := q.AdminSetTenantStatus(ctx, repo.AdminSetTenantStatusParams{ID: id, Status: status, FromStatus: from, SuspendedAt: at})
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.New(http.StatusConflict, "invalid_transition", "The tenant is not in a state that allows this")
		}
		if err := audit.Record(ctx, tx, adminEvent(a, &id, action, "tenant", &id, map[string]any{"reason": reason})); err != nil {
			return err
		}
		eventType := "identity.tenant.reactivated"
		if suspend {
			eventType = "identity.tenant.suspended"
		}
		if err := events.Append(ctx, tx, events.Event{Type: eventType, TenantID: &id, SourceModule: "identity", AggregateType: "tenant", AggregateID: id}, map[string]any{"tenant_id": id}); err != nil {
			return err
		}
		out, err = q.AdminGetTenant(ctx, id)
		return err
	})
	return out, err
}

// AdminListTenantUsers returns a tenant's members without any credential data.
func (s *Service) AdminListTenantUsers(ctx context.Context, tenantID uuid.UUID) ([]repo.AdminListTenantUsersRow, error) {
	var out []repo.AdminListTenantUsersRow
	err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		var err error
		out, err = repo.New(tx).AdminListTenantUsers(ctx, tenantID)
		return err
	})
	return out, err
}

// AdminListAudit returns audit events across tenants.
func (s *Service) AdminListAudit(ctx context.Context, before *uuid.UUID, limit int32) ([]AuditRow, error) {
	var out []AuditRow
	err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		var err error
		out, err = audit.List(ctx, tx, before, limit)
		return err
	})
	return out, err
}
