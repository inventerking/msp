package api_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

func code(t *testing.T, secret string, at time.Time) string {
	t.Helper()
	c, err := totp.GenerateCodeCustom(secret, at, totp.ValidateOpts{Period: 30, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestSignUpRequiresVerifiedEmail(t *testing.T) {
	email := unique("unverified")
	c := newClient(t)
	c.must(202, http.MethodPost, "/api/v1/auth/register", map[string]any{"email": email, "password": testPassword, "display_name": "U"})
	c.wantCode(403, "email_not_verified", http.MethodPost, "/api/v1/auth/login", map[string]any{"email": email, "password": testPassword})
	token := tokenRe.FindStringSubmatch(env.Mail.waitFor(t, email, "Verify").Body)[1]
	c.must(204, http.MethodPost, "/api/v1/auth/verify-email", map[string]any{"token": token})
	c.wantCode(422, "token_invalid", http.MethodPost, "/api/v1/auth/verify-email", map[string]any{"token": token}) // single use
	c.must(200, http.MethodPost, "/api/v1/auth/login", map[string]any{"email": email, "password": testPassword})
}

func TestRegistrationDoesNotRevealExistingAccounts(t *testing.T) {
	_, email := signUp(t, "existing")
	c := newClient(t)
	a := c.must(202, http.MethodPost, "/api/v1/auth/register", map[string]any{"email": email, "password": testPassword, "display_name": "X"})
	b := c.must(202, http.MethodPost, "/api/v1/auth/register", map[string]any{"email": unique("fresh"), "password": testPassword, "display_name": "X"})
	if a["status"] != b["status"] {
		t.Fatalf("responses differ: %v vs %v", a, b)
	}
	env.Mail.waitFor(t, email, "already have an account")
	c.must(202, http.MethodPost, "/api/v1/auth/password/forgot", map[string]any{"email": "nobody@example.test"})
	c.must(202, http.MethodPost, "/api/v1/auth/password/forgot", map[string]any{"email": email})
}

func TestWeakPasswordsRejected(t *testing.T) {
	c := newClient(t)
	for _, pw := range []string{"short", "password1234", "aaaaaaaaaaaaaaaa"} {
		c.wantCode(422, "validation_failed", http.MethodPost, "/api/v1/auth/register", map[string]any{"email": unique("weak"), "password": pw, "display_name": "W"})
	}
}

func TestLoginErrorsAreGenericAndThrottled(t *testing.T) {
	_, email := signUp(t, "throttle")
	c := newClient(t)
	unknown := unique("nosuchuser")
	for i := 0; i < 5; i++ {
		c.wantCode(401, "invalid_credentials", http.MethodPost, "/api/v1/auth/login", map[string]any{"email": email, "password": "wrong-password-value"})
	}
	c.wantCode(401, "invalid_credentials", http.MethodPost, "/api/v1/auth/login", map[string]any{"email": unknown, "password": "wrong-password-value"})
	// The correct password is also refused while the account is throttled.
	c.wantCode(429, "rate_limited", http.MethodPost, "/api/v1/auth/login", map[string]any{"email": email, "password": testPassword})
}

func TestCSRFRequiredOnStateChange(t *testing.T) {
	c, _, _ := newTenant(t, "CSRF Tenant")
	token := c.csrf
	c.csrf = ""
	c.wantCode(403, "csrf_failed", http.MethodPatch, "/api/v1/tenant", map[string]any{"name": "Changed"})
	c.csrf = "wrong"
	c.wantCode(403, "csrf_failed", http.MethodPatch, "/api/v1/tenant", map[string]any{"name": "Changed"})
	c.csrf = token
	c.must(200, http.MethodPatch, "/api/v1/tenant", map[string]any{"name": "Changed"})

	// A token from another session is refused.
	other, _, _ := newTenant(t, "CSRF Other")
	other.csrf = token
	other.wantCode(403, "csrf_failed", http.MethodPatch, "/api/v1/tenant", map[string]any{"name": "Changed"})
}

func TestForeignOriginRejected(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, env.URL+"/api/v1/auth/login", strings.NewReader(`{"email":"a@b.c","password":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestSessionCookieIsLockedDown(t *testing.T) {
	email := unique("cookie")
	c := newClient(t)
	c.must(202, http.MethodPost, "/api/v1/auth/register", map[string]any{"email": email, "password": testPassword, "display_name": "C"})
	token := tokenRe.FindStringSubmatch(env.Mail.waitFor(t, email, "Verify").Body)[1]
	c.must(204, http.MethodPost, "/api/v1/auth/verify-email", map[string]any{"token": token})
	resp, err := http.Post(env.URL+"/api/v1/auth/login", "application/json", strings.NewReader(`{"email":"`+email+`","password":"`+testPassword+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var found bool
	for _, ck := range resp.Cookies() {
		if ck.Name == "session" {
			found = true
			if !ck.HttpOnly || ck.SameSite != http.SameSiteLaxMode {
				t.Errorf("cookie flags wrong: %+v", ck)
			}
		}
	}
	if !found {
		t.Fatal("no session cookie")
	}
}

func TestPasswordResetRevokesSessions(t *testing.T) {
	c, email := signUp(t, "reset")
	c.must(200, http.MethodGet, "/api/v1/me/context", nil)
	anon := newClient(t)
	anon.must(202, http.MethodPost, "/api/v1/auth/password/forgot", map[string]any{"email": email})
	token := tokenRe.FindStringSubmatch(env.Mail.waitFor(t, email, "Reset your password").Body)[1]
	anon.must(204, http.MethodPost, "/api/v1/auth/password/reset", map[string]any{"token": token, "password": "a-brand-new-passphrase"})
	c.wantCode(401, "authentication_required", http.MethodGet, "/api/v1/me/context", nil)
	anon.wantCode(401, "invalid_credentials", http.MethodPost, "/api/v1/auth/login", map[string]any{"email": email, "password": testPassword})
	anon.must(200, http.MethodPost, "/api/v1/auth/login", map[string]any{"email": email, "password": "a-brand-new-passphrase"})
	anon.wantCode(422, "token_invalid", http.MethodPost, "/api/v1/auth/password/reset", map[string]any{"token": token, "password": "another-new-passphrase"})
}

func TestSessionRevocationAndLogout(t *testing.T) {
	c, email := signUp(t, "sessions")
	second := newClient(t)
	second.must(200, http.MethodPost, "/api/v1/auth/login", map[string]any{"email": email, "password": testPassword})
	second.refreshCSRF("/api/v1/me/context")
	list := c.must(200, http.MethodGet, "/api/v1/auth/sessions", nil)["data"].([]any)
	if len(list) != 2 {
		t.Fatalf("got %d sessions", len(list))
	}
	var other string
	for _, s := range list {
		m := s.(map[string]any)
		if m["current"] == false {
			other = m["id"].(string)
		}
	}
	c.must(204, http.MethodDelete, "/api/v1/auth/sessions/"+other, nil)
	second.wantCode(401, "authentication_required", http.MethodGet, "/api/v1/me/context", nil)
	c.must(204, http.MethodPost, "/api/v1/auth/logout", nil)
	c.wantCode(401, "authentication_required", http.MethodGet, "/api/v1/me/context", nil)
}

func TestMFAFlow(t *testing.T) {
	c, email := signUp(t, "mfa")
	enroll := c.must(200, http.MethodPost, "/api/v1/auth/mfa/totp/enroll", nil)
	secret := enroll["secret"].(string)
	c.wantCode(422, "mfa_code_invalid", http.MethodPost, "/api/v1/auth/mfa/totp/confirm", map[string]any{"code": "000000"})
	recovery := c.must(200, http.MethodPost, "/api/v1/auth/mfa/totp/confirm", map[string]any{"code": code(t, secret, time.Now())})["recovery_codes"].([]any)
	if len(recovery) != 10 {
		t.Fatalf("got %d recovery codes", len(recovery))
	}
	var stored []byte
	if err := env.Owner.QueryRow(context.Background(), `SELECT secret_ciphertext FROM identity.mfa_factors f JOIN identity.users u ON u.id = f.user_id WHERE u.email = $1`, email).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stored), secret) {
		t.Fatal("MFA secret is stored in clear text")
	}

	// A new login is held at MFA and learns nothing about the workspace meanwhile.
	login := newClient(t)
	out := login.must(200, http.MethodPost, "/api/v1/auth/login", map[string]any{"email": email, "password": testPassword})
	if out["mfa_required"] != true {
		t.Fatal("MFA not required")
	}
	ctxOut := login.refreshCSRF("/api/v1/me/context")
	if ctxOut["mfa_required"] != true || ctxOut["tenant"] != nil || len(ctxOut["memberships"].([]any)) != 0 {
		t.Fatalf("workspace data exposed before MFA: %v", ctxOut)
	}
	login.wantCode(401, "mfa_required", http.MethodGet, "/api/v1/auth/sessions", nil)
	login.wantCode(422, "mfa_code_invalid", http.MethodPost, "/api/v1/auth/mfa/verify", map[string]any{"code": "000000"})

	// A recovery code works exactly once.
	rc := recovery[0].(string)
	login.must(204, http.MethodPost, "/api/v1/auth/mfa/verify", map[string]any{"code": rc})
	login.must(200, http.MethodGet, "/api/v1/auth/sessions", nil)
	again := newClient(t)
	again.must(200, http.MethodPost, "/api/v1/auth/login", map[string]any{"email": email, "password": testPassword})
	again.refreshCSRF("/api/v1/me/context")
	again.wantCode(422, "mfa_code_invalid", http.MethodPost, "/api/v1/auth/mfa/verify", map[string]any{"code": rc})
}

func TestTOTPCodeCannotBeReplayed(t *testing.T) {
	c, email := signUp(t, "replay")
	secret := c.must(200, http.MethodPost, "/api/v1/auth/mfa/totp/enroll", nil)["secret"].(string)
	// Confirm with the previous window's code, so the current one is unused.
	c.must(200, http.MethodPost, "/api/v1/auth/mfa/totp/confirm", map[string]any{"code": code(t, secret, time.Now().Add(-30*time.Second))})
	current := code(t, secret, time.Now().Add(30*time.Second))
	first := newClient(t)
	first.must(200, http.MethodPost, "/api/v1/auth/login", map[string]any{"email": email, "password": testPassword})
	first.refreshCSRF("/api/v1/me/context")
	first.must(204, http.MethodPost, "/api/v1/auth/mfa/verify", map[string]any{"code": current})
	second := newClient(t)
	second.must(200, http.MethodPost, "/api/v1/auth/login", map[string]any{"email": email, "password": testPassword})
	second.refreshCSRF("/api/v1/me/context")
	second.wantCode(422, "mfa_code_invalid", http.MethodPost, "/api/v1/auth/mfa/verify", map[string]any{"code": current})
}

// Regression: enrolling must not block the code for the current time window, only the code used.
func TestSignInRightAfterEnrollingMFA(t *testing.T) {
	c, email := signUp(t, "enrollthenlogin")
	secret := c.must(200, http.MethodPost, "/api/v1/auth/mfa/totp/enroll", nil)["secret"].(string)
	now := time.Now()
	confirmCode := code(t, secret, now.Add(-30*time.Second))
	c.must(200, http.MethodPost, "/api/v1/auth/mfa/totp/confirm", map[string]any{"code": confirmCode})

	login := newClient(t)
	login.must(200, http.MethodPost, "/api/v1/auth/login", map[string]any{"email": email, "password": testPassword})
	login.refreshCSRF("/api/v1/me/context")
	// The code that confirmed enrolment has been used, so it is refused.
	login.wantCode(422, "mfa_code_invalid", http.MethodPost, "/api/v1/auth/mfa/verify", map[string]any{"code": confirmCode})
	// The current window's code has not been used, so it works straight away.
	login.must(204, http.MethodPost, "/api/v1/auth/mfa/verify", map[string]any{"code": code(t, secret, time.Now())})
}

func TestChangePassword(t *testing.T) {
	c, email := signUp(t, "changepw")
	other := newClient(t)
	other.must(200, http.MethodPost, "/api/v1/auth/login", map[string]any{"email": email, "password": testPassword})
	const next = "a-completely-new-passphrase"

	c.wantCode(401, "invalid_credentials", http.MethodPost, "/api/v1/auth/password/change", map[string]any{"current_password": "wrong-password-value", "new_password": next})
	c.wantCode(422, "validation_failed", http.MethodPost, "/api/v1/auth/password/change", map[string]any{"current_password": testPassword, "new_password": "password1234"})
	c.must(200, http.MethodGet, "/api/v1/me/context", nil) // Nothing has changed yet.
	other.must(200, http.MethodGet, "/api/v1/me/context", nil)

	c.must(204, http.MethodPost, "/api/v1/auth/password/change", map[string]any{"current_password": testPassword, "new_password": next})
	c.must(200, http.MethodGet, "/api/v1/me/context", nil) // This session stays signed in.
	other.wantCode(401, "authentication_required", http.MethodGet, "/api/v1/me/context", nil)
	env.Mail.waitFor(t, email, "password was changed")

	fresh := newClient(t)
	fresh.wantCode(401, "invalid_credentials", http.MethodPost, "/api/v1/auth/login", map[string]any{"email": email, "password": testPassword})
	fresh.must(200, http.MethodPost, "/api/v1/auth/login", map[string]any{"email": email, "password": next})
}

// A stolen session must not allow fast password guessing.
func TestPasswordChecksAreThrottled(t *testing.T) {
	c, _ := signUp(t, "guesser")
	for i := 0; i < 5; i++ {
		c.wantCode(401, "invalid_credentials", http.MethodPost, "/api/v1/auth/reauthenticate", map[string]any{"password": "wrong-password-value"})
	}
	c.wantCode(429, "rate_limited", http.MethodPost, "/api/v1/auth/password/change", map[string]any{"current_password": testPassword, "new_password": "a-completely-new-passphrase"})
}

func TestRecentAuthRequiredForSensitiveActions(t *testing.T) {
	c, email := signUp(t, "recent")
	if _, err := env.Owner.Exec(context.Background(), `UPDATE identity.sessions s SET last_auth_at = now() - interval '1 hour' FROM identity.users u WHERE u.id = s.user_id AND u.email = $1`, email); err != nil {
		t.Fatal(err)
	}
	c.wantCode(401, "recent_auth_required", http.MethodPost, "/api/v1/auth/mfa/totp/enroll", nil)
	c.wantCode(401, "invalid_credentials", http.MethodPost, "/api/v1/auth/reauthenticate", map[string]any{"password": "wrong-password-value"})
	c.must(204, http.MethodPost, "/api/v1/auth/reauthenticate", map[string]any{"password": testPassword})
	c.must(200, http.MethodPost, "/api/v1/auth/mfa/totp/enroll", nil)
}

func TestWorkspaceAndTeamRules(t *testing.T) {
	owner, _, tenantID := newTenant(t, "Team Tenant")

	admin, adminEmail := signUp(t, "teamadmin")
	owner.must(201, http.MethodPost, "/api/v1/tenant/invitations", map[string]any{"email": adminEmail, "role": "msp_admin"})
	token := inviteRe.FindStringSubmatch(env.Mail.waitFor(t, adminEmail, "invited").Body)[1]

	// An invitation is bound to the invited email.
	stranger, _ := signUp(t, "stranger")
	stranger.wantCode(404, "not_found", http.MethodPost, "/api/v1/invitations/accept", map[string]any{"token": token})
	stranger.wantCode(404, "not_found", http.MethodPost, "/api/v1/invitations/accept", map[string]any{"token": "not-a-token"})

	admin.must(200, http.MethodPost, "/api/v1/invitations/accept", map[string]any{"token": token})
	admin.wantCode(404, "not_found", http.MethodPost, "/api/v1/invitations/accept", map[string]any{"token": token}) // single use
	ctxOut := admin.refreshCSRF("/api/v1/me/context")
	if ctxOut["role"] != "msp_admin" || ctxOut["tenant"].(map[string]any)["id"] != tenantID {
		t.Fatalf("unexpected context %v", ctxOut)
	}

	// An MSP Admin cannot grant a role beyond their own permissions.
	admin.wantCode(403, "role_exceeds_own_permissions", http.MethodPost, "/api/v1/tenant/invitations", map[string]any{"email": unique("x"), "role": "owner"})

	members := owner.must(200, http.MethodGet, "/api/v1/tenant/members", nil)["data"].([]any)
	var ownerID, adminID string
	for _, m := range members {
		mm := m.(map[string]any)
		if mm["role"] == "owner" {
			ownerID = mm["id"].(string)
		} else {
			adminID = mm["id"].(string)
		}
	}
	admin.wantCode(403, "insufficient_permission", http.MethodPatch, "/api/v1/tenant/members/"+ownerID, map[string]any{"role": "viewer"})
	admin.wantCode(403, "insufficient_permission", http.MethodDelete, "/api/v1/tenant/members/"+ownerID, nil)
	admin.wantCode(403, "role_exceeds_own_permissions", http.MethodPatch, "/api/v1/tenant/members/"+adminID, map[string]any{"role": "owner"})

	// The last owner cannot be removed or demoted.
	owner.wantCode(409, "last_owner", http.MethodPatch, "/api/v1/tenant/members/"+ownerID, map[string]any{"role": "viewer"})
	owner.wantCode(409, "last_owner", http.MethodDelete, "/api/v1/tenant/members/"+ownerID, nil)

	// Removal ends access on the very next request, without waiting for the session to end.
	admin.must(200, http.MethodGet, "/api/v1/tenant", nil)
	owner.must(204, http.MethodDelete, "/api/v1/tenant/members/"+adminID, nil)
	admin.wantCode(403, "no_active_tenant", http.MethodGet, "/api/v1/tenant", nil)
}

func TestWorkspaceSwitching(t *testing.T) {
	c, email, first := newTenant(t, "First Space")
	owner2, _, second := newTenant(t, "Second Space")
	owner2.must(201, http.MethodPost, "/api/v1/tenant/invitations", map[string]any{"email": email, "role": "viewer"})
	token := inviteRe.FindStringSubmatch(env.Mail.waitFor(t, email, "invited").Body)[1]
	c.must(200, http.MethodPost, "/api/v1/invitations/accept", map[string]any{"token": token})
	out := c.refreshCSRF("/api/v1/me/context")
	if out["tenant"].(map[string]any)["id"] != second || out["role"] != "viewer" || len(out["memberships"].([]any)) != 2 {
		t.Fatalf("unexpected context %v", out)
	}
	c.must(204, http.MethodPost, "/api/v1/me/active-tenant", map[string]any{"tenant_id": first})
	out = c.refreshCSRF("/api/v1/me/context")
	if out["tenant"].(map[string]any)["id"] != first || out["role"] != "owner" {
		t.Fatalf("unexpected context %v", out)
	}
}

func TestValidationComesFromTheContract(t *testing.T) {
	c, _, _ := newTenant(t, "Validation Tenant")
	c.wantCode(422, "validation_failed", http.MethodPatch, "/api/v1/tenant", map[string]any{"reporting_currency": "TOOLONG"})
	c.wantCode(422, "validation_failed", http.MethodPost, "/api/v1/tenant/invitations", map[string]any{"role": "viewer"})
	c.wantCode(422, "validation_failed", http.MethodGet, "/api/v1/tenant/audit-events?limit=100000", nil)
	c.wantCode(422, "validation_failed", http.MethodPatch, "/api/v1/tenant", map[string]any{"default_timezone": "Not/AZone"})
	c.wantCode(404, "not_found", http.MethodGet, "/api/v1/does-not-exist", nil)
}

func TestErrorsDoNotLeakInternals(t *testing.T) {
	c := newClient(t)
	status, body := c.Do(http.MethodPost, "/api/v1/auth/login", map[string]any{"email": strings.Repeat("a", 300), "password": "x"})
	if status < 400 {
		t.Fatalf("got %d", status)
	}
	raw := strings.ToLower(strings.Join([]string{asString(body["title"]), asString(body["detail"])}, " "))
	for _, leak := range []string{"sql", "pgx", "panic", "goroutine", "constraint", "schema"} {
		if strings.Contains(raw, leak) {
			t.Errorf("error text leaks %q: %s", leak, raw)
		}
	}
	if body["request_id"] == nil {
		t.Error("no request ID on the error")
	}
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}
