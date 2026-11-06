// Package identity owns the identity schema: MSP users, sessions, MFA, tenants, memberships,
// and the separate platform administrator domain.
package identity

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/crypto"
	"mspengine/internal/kernel/db"
	"mspengine/internal/kernel/mailer"
	"mspengine/internal/platform/audit"
	"mspengine/internal/platform/identity/repo"
)

// Session lifetimes, from the authentication design.
const (
	sessionAbsolute      = 7 * 24 * time.Hour
	sessionIdle          = 24 * time.Hour
	adminSessionAbsolute = 8 * time.Hour
	adminSessionIdle     = 30 * time.Minute
	verifyTokenTTL       = 24 * time.Hour
	resetTokenTTL        = 30 * time.Minute
	inviteTTL            = 7 * 24 * time.Hour
	failureWindow        = 15 * time.Minute
	maxEmailFailures     = 5
	maxIPFailures        = 50
	minPasswordLen       = 12
	maxPasswordLen       = 256
	dataRegion           = "eu" // Working assumption until D-13 is decided.
)

// Service implements identity use cases.
type Service struct {
	pools   *db.Pools
	keys    crypto.Keys
	mail    mailer.Mailer
	baseURL string
	// dummyHash keeps login timing the same whether or not the account exists.
	dummyHash string
}

// New creates the service.
func New(pools *db.Pools, keys crypto.Keys, m mailer.Mailer, baseURL string) (*Service, error) {
	h, err := crypto.HashPassword("placeholder-for-constant-time-login")
	if err != nil {
		return nil, err
	}
	return &Service{pools: pools, keys: keys, mail: m, baseURL: baseURL, dummyHash: h}, nil
}

// Keys exposes the key set for CSRF checks in the HTTP layer.
func (s *Service) Keys() crypto.Keys { return s.keys }

func normalizeEmail(e string) string { return strings.ToLower(strings.TrimSpace(e)) }

func validEmail(e string) bool {
	a, err := mail.ParseAddress(e)
	return err == nil && a.Address == e && strings.Contains(e, "@")
}

func checkPassword(field, pw string) error {
	if len(pw) < minPasswordLen || len(pw) > maxPasswordLen {
		return apperr.Validation(apperr.FieldError{Field: field, Code: "password_length"})
	}
	if isCommonPassword(pw) {
		return apperr.Validation(apperr.FieldError{Field: field, Code: "password_too_common"})
	}
	return nil
}

// sendAsync sends after the database work has finished, so an external call never holds a
// transaction open and a mail outage never fails the request. Users can ask for a resend.
func (s *Service) sendAsync(ctx context.Context, m mailer.Message) {
	bg := context.WithoutCancel(ctx)
	go func() {
		c, cancel := context.WithTimeout(bg, 15*time.Second)
		defer cancel()
		if err := s.mail.Send(c, m); err != nil {
			slog.ErrorContext(c, "send email failed", "subject", m.Subject, "error", err)
		}
	}()
}

func isUnique(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505"
}

// --- Registration and verification ---

// Register creates an unverified account. The response never reveals whether the email exists.
func (s *Service) Register(ctx context.Context, email, password, displayName string) error {
	email = normalizeEmail(email)
	displayName = strings.TrimSpace(displayName)
	if !validEmail(email) {
		return apperr.Validation(apperr.FieldError{Field: "email", Code: "invalid_email"})
	}
	if displayName == "" {
		return apperr.Validation(apperr.FieldError{Field: "display_name", Code: "required"})
	}
	if err := checkPassword("password", password); err != nil {
		return err
	}
	hash, err := crypto.HashPassword(password)
	if err != nil {
		return err
	}
	token, tokenHash, err := crypto.NewToken()
	if err != nil {
		return err
	}
	exists := false
	err = db.Tx(ctx, s.pools.App, func(tx pgx.Tx) error {
		q := repo.New(tx)
		id, err := q.CreateUser(ctx, repo.CreateUserParams{Email: email, DisplayName: displayName})
		if isUnique(err) {
			exists = true
			return errExists
		}
		if err != nil {
			return err
		}
		if err := q.UpsertCredential(ctx, repo.UpsertCredentialParams{UserID: id, PasswordHash: hash}); err != nil {
			return err
		}
		if err := q.CreateVerificationToken(ctx, repo.CreateVerificationTokenParams{UserID: id, Purpose: "email_verify", TokenHash: tokenHash, ExpiresAt: time.Now().Add(verifyTokenTTL)}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{ActorType: "public", Action: "user.registered", ResourceType: "user", ResourceID: &id})
	})
	if err != nil && !errors.Is(err, errExists) {
		return err
	}
	if exists {
		s.sendAsync(ctx, mailer.Message{To: email, Subject: "You already have an account",
			Body: "Someone tried to register with this email address. If it was you, sign in or reset your password:\n\n" + s.baseURL + "/forgot-password\n"})
		return nil
	}
	s.sendAsync(ctx, verifyMessage(email, s.baseURL, token))
	return nil
}

var errExists = errors.New("user exists")

func verifyMessage(email, baseURL, token string) mailer.Message {
	return mailer.Message{To: email, Subject: "Verify your email address",
		Body: "Confirm your email address to finish creating your account:\n\n" + baseURL + "/verify-email?token=" + token + "\n\nThis link expires in 24 hours.\n"}
}

// VerifyEmail consumes a verification token.
func (s *Service) VerifyEmail(ctx context.Context, token string) error {
	return db.Tx(ctx, s.pools.App, func(tx pgx.Tx) error {
		q := repo.New(tx)
		uid, err := q.ConsumeVerificationToken(ctx, repo.ConsumeVerificationTokenParams{TokenHash: crypto.HashToken(token), Purpose: "email_verify"})
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.New(http.StatusUnprocessableEntity, "token_invalid", "This link is invalid or has expired")
		}
		if err != nil {
			return err
		}
		if err := q.MarkEmailVerified(ctx, uid); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{ActorType: "user", ActorID: &uid, Action: "user.email_verified", ResourceType: "user", ResourceID: &uid})
	})
}

// ResendVerification sends a new link if the account is unverified, and never reveals which.
func (s *Service) ResendVerification(ctx context.Context, email string) error {
	email = normalizeEmail(email)
	token, tokenHash, err := crypto.NewToken()
	if err != nil {
		return err
	}
	send := false
	err = db.Tx(ctx, s.pools.App, func(tx pgx.Tx) error {
		q := repo.New(tx)
		u, err := q.GetUserByEmail(ctx, email)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if u.Status != "pending_verification" {
			return nil
		}
		send = true
		return q.CreateVerificationToken(ctx, repo.CreateVerificationTokenParams{UserID: u.ID, Purpose: "email_verify", TokenHash: tokenHash, ExpiresAt: time.Now().Add(verifyTokenTTL)})
	})
	if err != nil {
		return err
	}
	if send {
		s.sendAsync(ctx, verifyMessage(email, s.baseURL, token))
	}
	return nil
}

// --- Login and sessions ---

// LoginResult holds a new session token, which is only ever returned here.
type LoginResult struct {
	Token       string
	MFARequired bool
}

// Login verifies credentials and creates a new session.
func (s *Service) Login(ctx context.Context, email, password string) (LoginResult, error) {
	email = normalizeEmail(email)
	emailHash := s.keys.HMACValue("login-email", email)
	m := authctx.MetaFrom(ctx)
	ipHash := m.IPHash
	if ipHash == nil {
		ipHash = s.keys.HMACValue("ip", "")
	}
	if err := s.checkThrottle(ctx, "tenant", emailHash, ipHash); err != nil {
		return LoginResult{}, err
	}
	var user repo.GetUserByEmailRow
	var stored string
	err := db.Tx(ctx, s.pools.App, func(tx pgx.Tx) error {
		q := repo.New(tx)
		var err error
		user, err = q.GetUserByEmail(ctx, email)
		if err != nil {
			return err
		}
		stored, err = q.GetCredential(ctx, user.ID)
		return err
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return LoginResult{}, err
	}
	known := err == nil
	if !known {
		stored = s.dummyHash
	}
	ok, rehash := crypto.VerifyPassword(stored, password)
	if !known || !ok || user.Status == "locked" || user.Status == "disabled" {
		s.recordFailure(ctx, emailHash, ipHash, "tenant", "bad_credentials")
		return LoginResult{}, apperr.InvalidCredentials()
	}
	if user.Status == "pending_verification" {
		return LoginResult{}, apperr.New(http.StatusForbidden, "email_not_verified", "Please verify your email address first")
	}
	token, tokenHash, err := crypto.NewToken()
	if err != nil {
		return LoginResult{}, err
	}
	var newHash string
	if rehash {
		if newHash, err = crypto.HashPassword(password); err != nil {
			return LoginResult{}, err
		}
	}
	res := LoginResult{Token: token}
	err = db.UserTx(ctx, s.pools.App, user.ID, func(tx pgx.Tx) error {
		q := repo.New(tx)
		if newHash != "" {
			if err := q.UpsertCredential(ctx, repo.UpsertCredentialParams{UserID: user.ID, PasswordHash: newHash}); err != nil {
				return err
			}
		}
		memberships, err := q.ListUserMemberships(ctx, user.ID)
		if err != nil {
			return err
		}
		var active *uuid.UUID
		if len(memberships) > 0 {
			active = &memberships[0].TenantID
		}
		now := time.Now()
		var ua *string
		if m.UserAgent != "" {
			ua = &m.UserAgent
		}
		id, err := q.CreateSession(ctx, repo.CreateSessionParams{UserID: user.ID, TokenHash: tokenHash, IpHash: ipHash, UserAgent: ua, ExpiresAt: now.Add(sessionAbsolute), IdleExpiresAt: now.Add(sessionIdle), ActiveTenantID: active})
		if err != nil {
			return err
		}
		if _, err := q.GetTOTPFactor(ctx, repo.GetTOTPFactorParams{UserID: user.ID, Verified: true}); err == nil {
			res.MFARequired = true
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err := q.RecordLoginAttempt(ctx, repo.RecordLoginAttemptParams{EmailHash: emailHash, IpHash: ipHash, Domain: "tenant", Succeeded: true}); err != nil {
			return err
		}
		if err := q.TouchLastLogin(ctx, user.ID); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{ActorType: "user", ActorID: &user.ID, Action: "session.created", ResourceType: "session", ResourceID: &id})
	})
	return res, err
}

func (s *Service) checkThrottle(ctx context.Context, domain string, emailHash, ipHash []byte) error {
	pool := s.pools.App
	if domain == "platform" {
		pool = s.pools.Platform
	}
	var c repo.CountRecentFailuresRow
	err := db.Tx(ctx, pool, func(tx pgx.Tx) error {
		var err error
		c, err = repo.New(tx).CountRecentFailures(ctx, repo.CountRecentFailuresParams{EmailHash: emailHash, IpHash: ipHash, Domain: domain, Since: time.Now().Add(-failureWindow)})
		return err
	})
	if err != nil {
		return err
	}
	if c.ByEmail >= maxEmailFailures || c.ByIp >= maxIPFailures {
		if err := audit.Security(ctx, pool, "login.throttled", "warning", nil, map[string]any{"domain": domain}); err != nil {
			slog.ErrorContext(ctx, "record security event failed", "error", err)
		}
		return apperr.RateLimited(int(failureWindow.Seconds()))
	}
	return nil
}

func (s *Service) recordFailure(ctx context.Context, emailHash, ipHash []byte, domain, reason string) {
	pool := s.pools.App
	if domain == "platform" {
		pool = s.pools.Platform
	}
	err := db.Tx(ctx, pool, func(tx pgx.Tx) error {
		return repo.New(tx).RecordLoginAttempt(ctx, repo.RecordLoginAttemptParams{EmailHash: emailHash, IpHash: ipHash, Domain: domain, Succeeded: false, FailureReason: &reason})
	})
	if err != nil {
		slog.ErrorContext(ctx, "record login attempt failed", "error", err)
	}
	if err := audit.Security(ctx, pool, "login.failed", "info", nil, map[string]any{"domain": domain}); err != nil {
		slog.ErrorContext(ctx, "record security event failed", "error", err)
	}
}

// SessionView is what a verified session token resolves to.
type SessionView struct {
	ID           uuid.UUID
	UserID       uuid.UUID
	ActiveTenant *uuid.UUID
	Email        string
	DisplayName  string
	HasMFA       bool
	MFAVerified  bool
	LastAuthAt   time.Time
}

// ResolveSession returns the live session for a token, or an unauthenticated error.
func (s *Service) ResolveSession(ctx context.Context, token string) (SessionView, error) {
	var v SessionView
	err := db.Tx(ctx, s.pools.App, func(tx pgx.Tx) error {
		q := repo.New(tx)
		r, err := q.GetSessionByToken(ctx, crypto.HashToken(token))
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.Unauthenticated()
		}
		if err != nil {
			return err
		}
		if r.UserStatus != "active" {
			return apperr.Unauthenticated()
		}
		v = SessionView{ID: r.ID, UserID: r.UserID, ActiveTenant: r.ActiveTenantID, Email: r.Email, DisplayName: r.DisplayName, HasMFA: r.HasMfa, MFAVerified: r.MfaVerifiedAt != nil, LastAuthAt: r.LastAuthAt}
		// Extend the idle window at most once every five minutes to limit writes.
		next := time.Now().Add(sessionIdle)
		if next.Sub(r.IdleExpiresAt) > 5*time.Minute {
			next = minTime(next, r.ExpiresAt)
			return q.TouchSession(ctx, repo.TouchSessionParams{ID: r.ID, IdleExpiresAt: next})
		}
		return nil
	})
	return v, err
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// TenantAccess is a user's standing in a tenant.
type TenantAccess struct {
	RoleKey     string
	TenantState string
	TenantName  string
	TenantSlug  string
	Permissions map[string]bool
}

// Access loads the membership and permissions under tenant RLS. No membership means not found.
func (s *Service) Access(ctx context.Context, userID, tenantID uuid.UUID) (TenantAccess, error) {
	var a TenantAccess
	err := db.TenantTx(ctx, s.pools.App, tenantID, userID, func(tx pgx.Tx) error {
		q := repo.New(tx)
		m, err := q.GetActiveMembership(ctx, repo.GetActiveMembershipParams{TenantID: tenantID, UserID: userID})
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		perms, err := q.ListRolePermissions(ctx, m.RoleID)
		if err != nil {
			return err
		}
		a = TenantAccess{RoleKey: m.RoleKey, TenantState: m.TenantStatus, TenantName: m.TenantName, TenantSlug: m.TenantSlug, Permissions: make(map[string]bool, len(perms))}
		for _, p := range perms {
			a.Permissions[p] = true
		}
		return nil
	})
	return a, err
}

// Logout revokes the current session.
func (s *Service) Logout(ctx context.Context, p authctx.Principal) error {
	return db.Tx(ctx, s.pools.App, func(tx pgx.Tx) error {
		if _, err := repo.New(tx).RevokeSession(ctx, repo.RevokeSessionParams{ID: p.SessionID, UserID: p.UserID, Reason: strPtr("logout")}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{ActorType: "user", ActorID: &p.UserID, Action: "session.revoked", ResourceType: "session", ResourceID: &p.SessionID})
	})
}

// RevokeSession revokes one of the user's own sessions. Another user's session is not found.
func (s *Service) RevokeSession(ctx context.Context, p authctx.Principal, id uuid.UUID) error {
	return db.Tx(ctx, s.pools.App, func(tx pgx.Tx) error {
		n, err := repo.New(tx).RevokeSession(ctx, repo.RevokeSessionParams{ID: id, UserID: p.UserID, Reason: strPtr("user_revoked")})
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.NotFound()
		}
		return audit.Record(ctx, tx, audit.Event{ActorType: "user", ActorID: &p.UserID, Action: "session.revoked", ResourceType: "session", ResourceID: &id})
	})
}

// ListSessions returns the user's live sessions.
func (s *Service) ListSessions(ctx context.Context, userID uuid.UUID) ([]repo.ListUserSessionsRow, error) {
	var out []repo.ListUserSessionsRow
	err := db.Tx(ctx, s.pools.App, func(tx pgx.Tx) error {
		var err error
		out, err = repo.New(tx).ListUserSessions(ctx, userID)
		return err
	})
	return out, err
}

// checkCurrentPassword verifies a signed-in user's password. Failures count toward the login
// throttle, so a stolen session cannot be used to guess the password at speed.
func (s *Service) checkCurrentPassword(ctx context.Context, p authctx.Principal, password string) error {
	emailHash := s.keys.HMACValue("login-email", normalizeEmail(p.Email))
	ipHash := authctx.MetaFrom(ctx).IPHash
	if ipHash == nil {
		ipHash = s.keys.HMACValue("ip", "")
	}
	if err := s.checkThrottle(ctx, "tenant", emailHash, ipHash); err != nil {
		return err
	}
	var stored string
	err := db.Tx(ctx, s.pools.App, func(tx pgx.Tx) error {
		var err error
		stored, err = repo.New(tx).GetCredential(ctx, p.UserID)
		return err
	})
	if err != nil {
		return err
	}
	if ok, _ := crypto.VerifyPassword(stored, password); !ok {
		s.recordFailure(ctx, emailHash, ipHash, "tenant", "bad_current_password")
		return apperr.InvalidCredentials()
	}
	return nil
}

// Reauthenticate refreshes the recent-authentication time for sensitive actions.
func (s *Service) Reauthenticate(ctx context.Context, p authctx.Principal, password string) error {
	if err := s.checkCurrentPassword(ctx, p, password); err != nil {
		return err
	}
	return db.Tx(ctx, s.pools.App, func(tx pgx.Tx) error {
		return repo.New(tx).SetSessionLastAuth(ctx, p.SessionID)
	})
}

// ChangePassword sets a new password and signs out every other session, which removes an
// attacker who may hold one. The current session stays signed in.
func (s *Service) ChangePassword(ctx context.Context, p authctx.Principal, current, next string) error {
	if err := checkPassword("new_password", next); err != nil {
		return err
	}
	if err := s.checkCurrentPassword(ctx, p, current); err != nil {
		return err
	}
	hash, err := crypto.HashPassword(next)
	if err != nil {
		return err
	}
	err = db.Tx(ctx, s.pools.App, func(tx pgx.Tx) error {
		q := repo.New(tx)
		if err := q.UpsertCredential(ctx, repo.UpsertCredentialParams{UserID: p.UserID, PasswordHash: hash}); err != nil {
			return err
		}
		if err := q.RevokeUserSessions(ctx, repo.RevokeUserSessionsParams{UserID: p.UserID, Reason: strPtr("password_changed"), ExceptID: &p.SessionID}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{ActorType: "user", ActorID: &p.UserID, Action: "user.password_changed", ResourceType: "user", ResourceID: &p.UserID})
	})
	if err != nil {
		return err
	}
	s.sendAsync(ctx, mailer.Message{To: p.Email, Subject: "Your password was changed",
		Body: "Your password was just changed and your other sessions were signed out. If this was not you, reset your password immediately.\n"})
	return nil
}

// --- Password reset ---

// ForgotPassword emails a reset link if the account exists, and never reveals which.
func (s *Service) ForgotPassword(ctx context.Context, email string) error {
	email = normalizeEmail(email)
	token, tokenHash, err := crypto.NewToken()
	if err != nil {
		return err
	}
	send := false
	err = db.Tx(ctx, s.pools.App, func(tx pgx.Tx) error {
		q := repo.New(tx)
		u, err := q.GetUserByEmail(ctx, email)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		send = true
		return q.CreateVerificationToken(ctx, repo.CreateVerificationTokenParams{UserID: u.ID, Purpose: "password_reset", TokenHash: tokenHash, ExpiresAt: time.Now().Add(resetTokenTTL)})
	})
	if err != nil {
		return err
	}
	if send {
		s.sendAsync(ctx, mailer.Message{To: email, Subject: "Reset your password",
			Body: "Use this link to choose a new password:\n\n" + s.baseURL + "/reset-password?token=" + token + "\n\nThis link expires in 30 minutes. If you did not ask for it, you can ignore this email.\n"})
	}
	return nil
}

// ResetPassword consumes a reset token, sets the password and revokes all sessions.
func (s *Service) ResetPassword(ctx context.Context, token, password string) error {
	if err := checkPassword("password", password); err != nil {
		return err
	}
	hash, err := crypto.HashPassword(password)
	if err != nil {
		return err
	}
	var email string
	err = db.Tx(ctx, s.pools.App, func(tx pgx.Tx) error {
		q := repo.New(tx)
		uid, err := q.ConsumeVerificationToken(ctx, repo.ConsumeVerificationTokenParams{TokenHash: crypto.HashToken(token), Purpose: "password_reset"})
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.New(http.StatusUnprocessableEntity, "token_invalid", "This link is invalid or has expired")
		}
		if err != nil {
			return err
		}
		u, err := q.GetUserByID(ctx, uid)
		if err != nil {
			return err
		}
		email = u.Email
		if err := q.UpsertCredential(ctx, repo.UpsertCredentialParams{UserID: uid, PasswordHash: hash}); err != nil {
			return err
		}
		if err := q.RevokeUserSessions(ctx, repo.RevokeUserSessionsParams{UserID: uid, Reason: strPtr("password_reset")}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{ActorType: "user", ActorID: &uid, Action: "user.password_reset", ResourceType: "user", ResourceID: &uid})
	})
	if err != nil {
		return err
	}
	s.sendAsync(ctx, mailer.Message{To: email, Subject: "Your password was changed",
		Body: "Your password was just changed and all sessions were signed out. If this was not you, reset your password immediately.\n"})
	return nil
}

func strPtr(s string) *string { return &s }

// Check is a helper for wrapping unexpected states with context.
func wrap(what string, err error) error { return fmt.Errorf("%s: %w", what, err) }
