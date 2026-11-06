package identity

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base32"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/crypto"
	"mspengine/internal/kernel/db"
	"mspengine/internal/kernel/mailer"
	"mspengine/internal/platform/audit"
	"mspengine/internal/platform/identity/repo"
)

const (
	totpPeriod        = 30
	recoveryCodeCount = 10
	issuer            = "MSP Growth Engine"
)

// matchTOTP checks a code for the current step and one step either side. It returns the matched
// step, so a used code can be recorded and refused if replayed, even within its validity window.
func matchTOTP(secret, code string, now time.Time) (time.Time, bool) {
	for _, offset := range []int{0, -1, 1} {
		step := now.Truncate(totpPeriod * time.Second).Add(time.Duration(offset*totpPeriod) * time.Second)
		want, err := totp.GenerateCodeCustom(secret, step, totp.ValidateOpts{Period: totpPeriod, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
		if err == nil && subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			return step, true
		}
	}
	return time.Time{}, false
}

func invalidCode() *apperr.Error {
	return apperr.New(http.StatusUnprocessableEntity, "mfa_code_invalid", "That code is not valid")
}

// Enrollment holds what the user needs to set up an authenticator app.
type Enrollment struct {
	Secret string
	URL    string
}

// EnrollTOTP creates an unverified factor. It takes effect only once confirmed with a code.
func (s *Service) EnrollTOTP(ctx context.Context, p authctx.Principal) (Enrollment, error) {
	k, err := totp.Generate(totp.GenerateOpts{Issuer: issuer, AccountName: p.Email, Period: totpPeriod, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
	if err != nil {
		return Enrollment{}, err
	}
	sealed, err := s.keys.Encrypt([]byte(k.Secret()), p.UserID[:])
	if err != nil {
		return Enrollment{}, err
	}
	err = db.Tx(ctx, s.pools.App, func(tx pgx.Tx) error {
		q := repo.New(tx)
		if _, err := q.GetTOTPFactor(ctx, repo.GetTOTPFactorParams{UserID: p.UserID, Verified: true}); err == nil {
			return apperr.Conflict("mfa_already_enabled", "Two-step verification is already on")
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err := q.DeleteUnverifiedFactors(ctx, p.UserID); err != nil {
			return err
		}
		_, err := q.CreateMFAFactor(ctx, repo.CreateMFAFactorParams{UserID: p.UserID, SecretCiphertext: sealed, KeyID: strPtr(crypto.KeyID)})
		return err
	})
	if err != nil {
		return Enrollment{}, err
	}
	return Enrollment{Secret: k.Secret(), URL: k.URL()}, nil
}

// ConfirmTOTP activates the factor and returns recovery codes, shown only this once.
func (s *Service) ConfirmTOTP(ctx context.Context, p authctx.Principal, code string) ([]string, error) {
	codes := make([]string, recoveryCodeCount)
	for i := range codes {
		c, err := newRecoveryCode()
		if err != nil {
			return nil, err
		}
		codes[i] = c
	}
	err := db.Tx(ctx, s.pools.App, func(tx pgx.Tx) error {
		q := repo.New(tx)
		f, err := q.GetTOTPFactor(ctx, repo.GetTOTPFactorParams{UserID: p.UserID, Verified: false})
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.Conflict("mfa_not_enrolling", "Start setup first")
		}
		if err != nil {
			return err
		}
		secret, err := s.keys.Decrypt(f.SecretCiphertext, p.UserID[:])
		if err != nil {
			return wrap("decrypt factor", err)
		}
		step, ok := matchTOTP(string(secret), code, time.Now())
		if !ok {
			return invalidCode()
		}
		// Record the code's own time step, not the clock, so replay checks compare like with like.
		if err := q.MarkFactorVerified(ctx, repo.MarkFactorVerifiedParams{ID: f.ID, UsedAt: &step}); err != nil {
			return err
		}
		if err := q.DeleteRecoveryCodes(ctx, p.UserID); err != nil {
			return err
		}
		for _, c := range codes {
			if err := q.CreateRecoveryCode(ctx, repo.CreateRecoveryCodeParams{UserID: p.UserID, CodeHash: s.keys.HMACValue("recovery", c)}); err != nil {
				return err
			}
		}
		if err := q.SetSessionMFAVerified(ctx, p.SessionID); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{ActorType: "user", ActorID: &p.UserID, Action: "mfa.enabled", ResourceType: "user", ResourceID: &p.UserID})
	})
	if err != nil {
		return nil, err
	}
	s.sendAsync(ctx, mailer.Message{To: p.Email, Subject: "Two-step verification turned on",
		Body: "Two-step verification was turned on for your account. If this was not you, reset your password immediately.\n"})
	return codes, nil
}

func newRecoveryCode() (string, error) {
	b := make([]byte, 10)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)), nil
}

// VerifyMFA completes a login with an authenticator code or a recovery code. Failures count
// toward the login throttle, so codes cannot be guessed at speed.
func (s *Service) VerifyMFA(ctx context.Context, p authctx.Principal, code string) error {
	code = strings.ToLower(strings.TrimSpace(strings.ReplaceAll(code, " ", "")))
	emailHash := s.keys.HMACValue("login-email", normalizeEmail(p.Email))
	ipHash := authctx.MetaFrom(ctx).IPHash
	if ipHash == nil {
		ipHash = s.keys.HMACValue("ip", "")
	}
	if err := s.checkThrottle(ctx, "tenant", emailHash, ipHash); err != nil {
		return err
	}
	verified := false
	err := db.Tx(ctx, s.pools.App, func(tx pgx.Tx) error {
		q := repo.New(tx)
		f, err := q.GetTOTPFactor(ctx, repo.GetTOTPFactorParams{UserID: p.UserID, Verified: true})
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.Conflict("mfa_not_enabled", "Two-step verification is not on")
		}
		if err != nil {
			return err
		}
		secret, err := s.keys.Decrypt(f.SecretCiphertext, p.UserID[:])
		if err != nil {
			return wrap("decrypt factor", err)
		}
		if step, ok := matchTOTP(string(secret), code, time.Now()); ok {
			n, err := q.MarkFactorUsed(ctx, repo.MarkFactorUsedParams{ID: f.ID, UsedAt: &step})
			if err != nil {
				return err
			}
			verified = n > 0 // Zero rows means this code was already used.
		} else {
			n, err := q.UseRecoveryCode(ctx, repo.UseRecoveryCodeParams{UserID: p.UserID, CodeHash: s.keys.HMACValue("recovery", code)})
			if err != nil {
				return err
			}
			verified = n > 0
		}
		if !verified {
			return nil
		}
		if err := q.SetSessionMFAVerified(ctx, p.SessionID); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{ActorType: "user", ActorID: &p.UserID, Action: "mfa.verified", ResourceType: "session", ResourceID: &p.SessionID})
	})
	if err != nil {
		return err
	}
	if !verified {
		s.recordFailure(ctx, emailHash, ipHash, "tenant", "bad_mfa_code")
		return invalidCode()
	}
	return nil
}

// DisableMFA removes all factors. The route requires recent authentication.
func (s *Service) DisableMFA(ctx context.Context, p authctx.Principal) error {
	err := db.Tx(ctx, s.pools.App, func(tx pgx.Tx) error {
		q := repo.New(tx)
		if err := q.DeleteAllFactors(ctx, p.UserID); err != nil {
			return err
		}
		if err := q.DeleteRecoveryCodes(ctx, p.UserID); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{ActorType: "user", ActorID: &p.UserID, Action: "mfa.disabled", ResourceType: "user", ResourceID: &p.UserID})
	})
	if err != nil {
		return err
	}
	s.sendAsync(ctx, mailer.Message{To: p.Email, Subject: "Two-step verification turned off",
		Body: "Two-step verification was turned off for your account. If this was not you, reset your password immediately.\n"})
	return nil
}
