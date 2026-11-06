package api_test

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// adminClient creates an admin, signs in and completes mandatory MFA enrolment.
func adminClient(t *testing.T, roles ...string) *Client {
	t.Helper()
	email := unique("admin")
	if _, err := env.IDs.CreateAdmin(context.Background(), nil, email, "Admin", testPassword, roles); err != nil {
		t.Fatal(err)
	}
	c := newClient(t)
	out := c.must(200, http.MethodPost, "/api/v1/admin/auth/login", map[string]any{"email": email, "password": testPassword})
	if out["mfa_enrolled"] != false {
		t.Fatal("new admin should not be enrolled")
	}
	c.refreshCSRF("/api/v1/admin/me")
	secret := c.must(200, http.MethodPost, "/api/v1/admin/auth/mfa/enroll", nil)["secret"].(string)
	c.must(204, http.MethodPost, "/api/v1/admin/auth/mfa/confirm", map[string]any{"code": code(t, secret, time.Now())})
	return c
}

func TestAdminMFAIsMandatory(t *testing.T) {
	email := unique("adminmfa")
	if _, err := env.IDs.CreateAdmin(context.Background(), nil, email, "A", testPassword, []string{"super_admin"}); err != nil {
		t.Fatal(err)
	}
	c := newClient(t)
	c.must(200, http.MethodPost, "/api/v1/admin/auth/login", map[string]any{"email": email, "password": testPassword})
	out := c.refreshCSRF("/api/v1/admin/me")
	if out["mfa_verified"] != false {
		t.Fatal("session should not be verified yet")
	}
	c.wantCode(401, "mfa_required", http.MethodGet, "/api/v1/admin/tenants", nil)
	c.wantCode(401, "mfa_required", http.MethodGet, "/api/v1/admin/modules", nil)
	secret := c.must(200, http.MethodPost, "/api/v1/admin/auth/mfa/enroll", nil)["secret"].(string)
	c.wantCode(422, "mfa_code_invalid", http.MethodPost, "/api/v1/admin/auth/mfa/confirm", map[string]any{"code": "000000"})
	c.must(204, http.MethodPost, "/api/v1/admin/auth/mfa/confirm", map[string]any{"code": code(t, secret, time.Now())})
	c.must(200, http.MethodGet, "/api/v1/admin/tenants", nil)

	// Once enrolled, a password alone cannot enrol a new factor to take the account over.
	thief := newClient(t)
	if out := thief.must(200, http.MethodPost, "/api/v1/admin/auth/login", map[string]any{"email": email, "password": testPassword}); out["mfa_enrolled"] != true {
		t.Fatal("expected enrolled")
	}
	thief.refreshCSRF("/api/v1/admin/me")
	thief.wantCode(409, "mfa_already_enabled", http.MethodPost, "/api/v1/admin/auth/mfa/enroll", nil)
	thief.wantCode(401, "mfa_required", http.MethodGet, "/api/v1/admin/tenants", nil)
}

func TestAdminCanSignInRightAfterEnrolling(t *testing.T) {
	email := unique("adminagain")
	if _, err := env.IDs.CreateAdmin(context.Background(), nil, email, "A", testPassword, []string{"super_admin"}); err != nil {
		t.Fatal(err)
	}
	c := newClient(t)
	c.must(200, http.MethodPost, "/api/v1/admin/auth/login", map[string]any{"email": email, "password": testPassword})
	c.refreshCSRF("/api/v1/admin/me")
	secret := c.must(200, http.MethodPost, "/api/v1/admin/auth/mfa/enroll", nil)["secret"].(string)
	c.must(204, http.MethodPost, "/api/v1/admin/auth/mfa/confirm", map[string]any{"code": code(t, secret, time.Now().Add(-30*time.Second))})
	again := newClient(t)
	again.must(200, http.MethodPost, "/api/v1/admin/auth/login", map[string]any{"email": email, "password": testPassword})
	again.refreshCSRF("/api/v1/admin/me")
	again.must(204, http.MethodPost, "/api/v1/admin/auth/mfa/verify", map[string]any{"code": code(t, secret, time.Now())})
	again.must(200, http.MethodGet, "/api/v1/admin/tenants", nil)
}

func TestAdminSessionCannotUseTenantRoutes(t *testing.T) {
	a := adminClient(t, "super_admin")
	a.wantCode(401, "authentication_required", http.MethodGet, "/api/v1/me/context", nil)
	a.wantCode(401, "authentication_required", http.MethodGet, "/api/v1/crm/status", nil)
}

func TestAdminRoleMatrix(t *testing.T) {
	_, _, tenant := newTenant(t, "Matrix Tenant")
	reason := map[string]any{"reason": "testing roles"}
	cases := []struct {
		role         string
		method, path string
		body         any
		allowed      bool
	}{
		{"platform_analyst", http.MethodGet, "/api/v1/admin/tenants", nil, true},
		{"platform_analyst", http.MethodPost, "/api/v1/admin/tenants/" + tenant + "/suspend", reason, false},
		{"platform_analyst", http.MethodPost, "/api/v1/admin/tenants/" + tenant + "/entitlements/crm/activate", reason, false},
		{"platform_analyst", http.MethodGet, "/api/v1/admin/admins", nil, false},
		{"platform_support", http.MethodGet, "/api/v1/admin/tenants/" + tenant + "/users", nil, true},
		{"platform_support", http.MethodPost, "/api/v1/admin/products", map[string]any{"key": "x_support", "kind": "bundle", "name": "XX", "description": "x", "module_keys": []string{"crm"}}, false},
		{"platform_support", http.MethodPost, "/api/v1/admin/tenants/" + tenant + "/entitlements/crm/activate", reason, false},
		{"billing_admin", http.MethodGet, "/api/v1/admin/products", nil, true},
		{"billing_admin", http.MethodGet, "/api/v1/admin/admins", nil, false},
		{"billing_admin", http.MethodPost, "/api/v1/admin/tenants/" + tenant + "/suspend", reason, false},
		{"integration_admin", http.MethodGet, "/api/v1/admin/tenants", nil, false},
		{"integration_admin", http.MethodGet, "/api/v1/admin/modules", nil, true},
		{"integration_admin", http.MethodPost, "/api/v1/admin/tenants/" + tenant + "/entitlements/crm/activate", reason, false},
		{"super_admin", http.MethodGet, "/api/v1/admin/admins", nil, true},
	}
	clients := map[string]*Client{}
	for _, tc := range cases {
		t.Run(tc.role+" "+tc.method+" "+tc.path, func(t *testing.T) {
			c := clients[tc.role]
			if c == nil {
				c = adminClient(t, tc.role)
				clients[tc.role] = c
			}
			c.t = t
			status, body := c.Do(tc.method, tc.path, tc.body)
			if tc.allowed && status >= 400 {
				t.Fatalf("expected allowed, got %d %v", status, body)
			}
			if !tc.allowed && (status != http.StatusForbidden || body["code"] != "insufficient_role") {
				t.Fatalf("expected insufficient_role, got %d %v", status, body)
			}
		})
	}
}

func moduleStatus(ctxOut map[string]any, key string) string {
	for _, m := range ctxOut["modules"].([]any) {
		mm := m.(map[string]any)
		if mm["key"] == key {
			return mm["status"].(string)
		}
	}
	return ""
}

// seedPlan creates a priced plan on a seeded product and returns the plan ID.
func seedPlan(t *testing.T, a *Client, productKey, planKey string) string {
	t.Helper()
	var version string
	for _, p := range a.must(200, http.MethodGet, "/api/v1/admin/products", nil)["data"].([]any) {
		pm := p.(map[string]any)
		if pm["key"] == productKey {
			version = pm["version_id"].(string)
		}
	}
	if version == "" {
		t.Fatalf("no product %s", productKey)
	}
	plan := a.must(201, http.MethodPost, "/api/v1/admin/plans", map[string]any{"product_version_id": version, "key": planKey, "name": "Plan " + planKey, "billing_period": "monthly", "trial_days": 0})
	id := plan["id"].(string)
	a.must(201, http.MethodPost, "/api/v1/admin/plans/"+id+"/prices", map[string]any{"currency": "USD", "amount_minor": 10000})
	return id
}

// The Phase 1 acceptance flow: nothing is active until an admin acts, the bundle grants exactly its
// modules, and the backend blocks a suspended module whatever the client thinks.
func TestBundleActivationAndBackendEnforcement(t *testing.T) {
	admin := adminClient(t, "billing_admin")
	super := adminClient(t, "super_admin")
	owner, _, tenant := newTenant(t, "Activation Tenant")
	other, _, _ := newTenant(t, "Unrelated Tenant")

	owner.wantCode(403, "module_not_entitled", http.MethodGet, "/api/v1/crm/status", nil)
	if s := moduleStatus(owner.refreshCSRF("/api/v1/me/context"), "crm"); s != "inactive" {
		t.Fatalf("crm status %q", s)
	}

	// Recording a subscription grants nothing: access needs an admin's activation.
	plan := seedPlan(t, admin, "sales", "sales_monthly_test")
	admin.wantCode(422, "price_unavailable", http.MethodPost, "/api/v1/admin/tenants/"+tenant+"/subscriptions", map[string]any{"plan_id": plan, "currency": "EUR"})
	sub := admin.must(201, http.MethodPost, "/api/v1/admin/tenants/"+tenant+"/subscriptions", map[string]any{"plan_id": plan, "currency": "USD"})
	if sub["status"] != "pending_activation" {
		t.Fatalf("status %v", sub["status"])
	}
	owner.wantCode(403, "module_not_entitled", http.MethodGet, "/api/v1/crm/status", nil)
	if s := moduleStatus(owner.refreshCSRF("/api/v1/me/context"), "crm"); s != "pending" {
		t.Fatalf("crm status %q, want pending", s)
	}

	admin.must(200, http.MethodPost, "/api/v1/admin/subscriptions/"+sub["id"].(string)+"/activate", map[string]any{"reason": "payment verified"})
	admin.wantCode(409, "invalid_transition", http.MethodPost, "/api/v1/admin/subscriptions/"+sub["id"].(string)+"/activate", map[string]any{"reason": "payment verified"})

	// Sales grants exactly CRM, IT Assessment and Proposals.
	for path, want := range map[string]int{
		"/api/v1/crm/status": 200, "/api/v1/it-assessments/status": 200, "/api/v1/proposals/status": 200,
		"/api/v1/outreach/status": 403, "/api/v1/lead-generation/status": 403, "/api/v1/chatbot/status": 403,
		"/api/v1/competitor-intelligence/status": 403, "/api/v1/visitor-intelligence/status": 403,
		"/api/v1/customer-expansion/status": 403, "/api/v1/sales-intelligence/status": 403,
	} {
		if got, _ := owner.Do(http.MethodGet, path, nil); got != want {
			t.Errorf("%s: got %d, want %d", path, got, want)
		}
	}
	// Another tenant gains nothing.
	other.wantCode(403, "module_not_entitled", http.MethodGet, "/api/v1/crm/status", nil)

	// Suspension blocks at the API immediately, and reports why.
	reason := map[string]any{"reason": "testing suspension"}
	admin.must(200, http.MethodPost, "/api/v1/admin/tenants/"+tenant+"/entitlements/crm/suspend", reason)
	status, body := owner.Do(http.MethodGet, "/api/v1/crm/status", nil)
	if status != 403 || body["code"] != "module_not_entitled" || body["module_status"] != "suspended" {
		t.Fatalf("got %d %v", status, body)
	}
	owner.must(200, http.MethodGet, "/api/v1/proposals/status", nil) // Other modules are unaffected.

	admin.must(200, http.MethodPost, "/api/v1/admin/tenants/"+tenant+"/entitlements/crm/activate", reason)
	owner.must(200, http.MethodGet, "/api/v1/crm/status", nil)

	// Only a Super Admin can revoke, or restore after a revocation.
	admin.wantCode(403, "insufficient_role", http.MethodPost, "/api/v1/admin/tenants/"+tenant+"/entitlements/crm/revoke", reason)
	super.must(200, http.MethodPost, "/api/v1/admin/tenants/"+tenant+"/entitlements/crm/revoke", reason)
	if _, b := owner.Do(http.MethodGet, "/api/v1/crm/status", nil); b["module_status"] != "revoked" {
		t.Fatalf("got %v", b)
	}
	admin.wantCode(403, "insufficient_role", http.MethodPost, "/api/v1/admin/tenants/"+tenant+"/entitlements/crm/activate", reason)
	super.must(200, http.MethodPost, "/api/v1/admin/tenants/"+tenant+"/entitlements/crm/activate", reason)
	owner.must(200, http.MethodGet, "/api/v1/crm/status", nil)

	// Expiry also blocks.
	admin.must(200, http.MethodPost, "/api/v1/admin/tenants/"+tenant+"/entitlements/proposals/expire", reason)
	if _, b := owner.Do(http.MethodGet, "/api/v1/proposals/status", nil); b["module_status"] != "expired" {
		t.Fatalf("got %v", b)
	}

	// Every step above left an audit record, visible to the tenant and attributed to an admin.
	actions := map[string]bool{}
	for _, e := range owner.must(200, http.MethodGet, "/api/v1/tenant/audit-events?limit=100", nil)["data"].([]any) {
		m := e.(map[string]any)
		actions[m["action"].(string)] = true
		if m["action"] == "entitlement.suspend" && m["actor_type"] != "platform_admin" {
			t.Errorf("wrong actor: %v", m)
		}
	}
	for _, a := range []string{"subscription.created", "subscription.activated", "entitlement.suspend", "entitlement.activate", "entitlement.revoke", "entitlement.expire"} {
		if !actions[a] {
			t.Errorf("missing audit action %s", a)
		}
	}
}

func TestOverrideWithoutSubscriptionNeedsSuperAdmin(t *testing.T) {
	billing := adminClient(t, "billing_admin")
	super := adminClient(t, "super_admin")
	owner, _, tenant := newTenant(t, "Override Tenant")
	reason := map[string]any{"reason": "testing override"}
	billing.wantCode(403, "insufficient_role", http.MethodPost, "/api/v1/admin/tenants/"+tenant+"/entitlements/chatbot/activate", reason)
	super.must(200, http.MethodPost, "/api/v1/admin/tenants/"+tenant+"/entitlements/chatbot/activate", reason)
	owner.must(200, http.MethodGet, "/api/v1/chatbot/status", nil)
	billing.wantCode(404, "not_found", http.MethodPost, "/api/v1/admin/tenants/"+tenant+"/entitlements/no_such_module/suspend", reason)
	super.wantCode(422, "validation_failed", http.MethodPost, "/api/v1/admin/tenants/"+tenant+"/entitlements/crm/delete", reason)
}

func TestEntitlementEndDateIsEnforced(t *testing.T) {
	super := adminClient(t, "super_admin")
	owner, _, tenant := newTenant(t, "Dated Tenant")
	until := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	super.must(200, http.MethodPost, "/api/v1/admin/tenants/"+tenant+"/entitlements/crm/activate", map[string]any{"reason": "dated grant", "effective_until": until})
	owner.must(200, http.MethodGet, "/api/v1/crm/status", nil)
	// Move the end date into the past. The status column still says active, and the read must not trust it.
	tag, err := appConn(t, "msp_platform", "dev_platform").Exec(context.Background(), `UPDATE platform.tenant_module_entitlements SET effective_until = now() - interval '1 minute' WHERE tenant_id = $1`, tenant)
	if err != nil {
		t.Fatal(err)
	}
	if tag.RowsAffected() == 0 {
		t.Fatal("arranged no rows, so this test would prove nothing")
	}
	status, body := owner.Do(http.MethodGet, "/api/v1/crm/status", nil)
	if status != 403 || body["module_status"] != "expired" {
		t.Fatalf("got %d %v", status, body)
	}
	if s := moduleStatus(owner.must(200, http.MethodGet, "/api/v1/me/context", nil), "crm"); s != "expired" {
		t.Fatalf("context reports %q", s)
	}
}

func TestBundleChangeDoesNotAffectExistingSubscribers(t *testing.T) {
	admin := adminClient(t, "billing_admin")
	owner, _, tenant := newTenant(t, "Stable Tenant")
	p := admin.must(201, http.MethodPost, "/api/v1/admin/products", map[string]any{"key": "stable_bundle", "kind": "bundle", "name": "Stable", "description": "x", "module_keys": []string{"crm", "chatbot"}})
	versionOne := p["version_id"].(string)
	admin.must(200, http.MethodPost, "/api/v1/admin/product-versions/"+versionOne+"/publish", nil)
	admin.wantCode(409, "invalid_transition", http.MethodPost, "/api/v1/admin/product-versions/"+versionOne+"/publish", nil)
	plan := admin.must(201, http.MethodPost, "/api/v1/admin/plans", map[string]any{"product_version_id": versionOne, "key": "stable_m", "name": "Stable M", "billing_period": "monthly", "trial_days": 0})["id"].(string)
	admin.must(201, http.MethodPost, "/api/v1/admin/plans/"+plan+"/prices", map[string]any{"currency": "USD", "amount_minor": 5000})
	sub := admin.must(201, http.MethodPost, "/api/v1/admin/tenants/"+tenant+"/subscriptions", map[string]any{"plan_id": plan, "currency": "USD"})
	admin.must(200, http.MethodPost, "/api/v1/admin/subscriptions/"+sub["id"].(string)+"/activate", map[string]any{"reason": "ok reason"})

	// A new version drops Chatbot and adds Outreach.
	v2 := admin.must(201, http.MethodPost, "/api/v1/admin/products/"+p["id"].(string)+"/versions", map[string]any{"module_keys": []string{"crm", "outreach"}})
	admin.must(200, http.MethodPost, "/api/v1/admin/product-versions/"+v2["version_id"].(string)+"/publish", nil)

	owner.must(200, http.MethodGet, "/api/v1/chatbot/status", nil)
	owner.wantCode(403, "module_not_entitled", http.MethodGet, "/api/v1/outreach/status", nil)

	// A price change also leaves the subscription's recorded price alone, and ends the old price.
	admin.must(201, http.MethodPost, "/api/v1/admin/plans/"+plan+"/prices", map[string]any{"currency": "USD", "amount_minor": 9000})
	var snapshotAmount int64
	if err := appConn(t, "msp_platform", "dev_platform").QueryRow(context.Background(), `SELECT (price_snapshot->>'amount_minor')::bigint FROM platform.subscriptions WHERE id = $1`, sub["id"]).Scan(&snapshotAmount); err != nil {
		t.Fatal(err)
	}
	if snapshotAmount != 5000 {
		t.Fatalf("recorded price changed to %d", snapshotAmount)
	}
	var open int
	if err := env.Owner.QueryRow(context.Background(), `SELECT count(*) FROM platform.prices WHERE plan_id = $1 AND valid_to IS NULL`, plan).Scan(&open); err != nil {
		t.Fatal(err)
	}
	if open != 1 {
		t.Fatalf("%d open prices", open)
	}
}

func TestTenantSuspensionBlocksWorkspace(t *testing.T) {
	super := adminClient(t, "super_admin")
	owner, _, tenant := newTenant(t, "Suspended Tenant")
	super.must(200, http.MethodPost, "/api/v1/admin/tenants/"+tenant+"/entitlements/crm/activate", map[string]any{"reason": "before suspension"})
	owner.must(200, http.MethodGet, "/api/v1/crm/status", nil)
	super.must(200, http.MethodPost, "/api/v1/admin/tenants/"+tenant+"/suspend", map[string]any{"reason": "testing tenant suspension"})
	owner.wantCode(403, "tenant_suspended", http.MethodGet, "/api/v1/crm/status", nil)
	owner.wantCode(403, "tenant_suspended", http.MethodGet, "/api/v1/tenant", nil)
	out := owner.must(200, http.MethodGet, "/api/v1/me/context", nil) // Enough to show a status page.
	if out["tenant"].(map[string]any)["status"] != "suspended" {
		t.Fatalf("got %v", out["tenant"])
	}
	super.wantCode(409, "invalid_transition", http.MethodPost, "/api/v1/admin/tenants/"+tenant+"/suspend", map[string]any{"reason": "already suspended"})
	super.must(200, http.MethodPost, "/api/v1/admin/tenants/"+tenant+"/reactivate", map[string]any{"reason": "testing reactivation"})
	owner.must(200, http.MethodGet, "/api/v1/crm/status", nil)
}

func TestAdminSeesTenantUsersWithoutCredentials(t *testing.T) {
	support := adminClient(t, "platform_support")
	_, email, tenant := newTenant(t, "Support Tenant")
	users := support.must(200, http.MethodGet, "/api/v1/admin/tenants/"+tenant+"/users", nil)["data"].([]any)
	if len(users) != 1 || users[0].(map[string]any)["email"] != email {
		t.Fatalf("got %v", users)
	}
	for k := range users[0].(map[string]any) {
		if k == "password_hash" || k == "secret_ciphertext" {
			t.Errorf("credential field exposed: %s", k)
		}
	}
}
