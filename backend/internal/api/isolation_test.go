package api_test

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/api/adminapi"
	"mspengine/internal/api/modulesapi"
	"mspengine/internal/api/tenantapi"
)

// schemaFaults inspects the database catalog and reports any tenant-owned table missing a required
// protection. It is a function, so a test can prove it catches a deliberately broken table.
func schemaFaults(ctx context.Context, q interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, schemas []string, allowNoTenantUnique []string) ([]string, error) {
	rows, err := q.Query(ctx, `
		SELECT n.nspname, c.relname, c.relrowsecurity, c.relforcerowsecurity,
		       EXISTS (SELECT 1 FROM pg_policy p WHERE p.polrelid = c.oid) AS has_policy,
		       EXISTS (SELECT 1 FROM pg_constraint k WHERE k.conrelid = c.oid AND k.contype = 'u'
		               AND (SELECT array_agg(a.attname::text ORDER BY a.attname) FROM pg_attribute a
		                    WHERE a.attrelid = c.oid AND a.attnum = ANY(k.conkey)) = ARRAY['id', 'tenant_id']) AS has_unique,
		       (SELECT a.attnotnull FROM pg_attribute a WHERE a.attrelid = c.oid AND a.attname = 'tenant_id') AS tenant_required
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relkind IN ('r', 'p') AND NOT c.relispartition
		  AND n.nspname = ANY($1)
		  AND EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid = c.oid AND a.attname = 'tenant_id' AND NOT a.attisdropped)`, schemas)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var faults []string
	for rows.Next() {
		var schema, table string
		var rls, force, policy, unique bool
		var required *bool
		if err := rows.Scan(&schema, &table, &rls, &force, &policy, &unique, &required); err != nil {
			return nil, err
		}
		name := schema + "." + table
		if required == nil || !*required {
			if slices.Contains(mixedTables, name) {
				continue
			}
			faults = append(faults, name+": tenant_id is nullable")
		}
		if !rls {
			faults = append(faults, name+": row-level security not enabled")
		}
		if !force {
			faults = append(faults, name+": row-level security not forced")
		}
		if !policy {
			faults = append(faults, name+": no policy")
		}
		if !unique && !slices.Contains(allowNoTenantUnique, name) {
			faults = append(faults, name+": no UNIQUE (tenant_id, id)")
		}
	}
	return faults, rows.Err()
}

// mixedTables hold platform-level rows with a null tenant, and have their own policies or none
// for tenant users. They are checked separately.
var mixedTables = []string{"identity.roles", "audit.events", "audit.security_events",
	"jobs.jobs", "jobs.job_schedules", "jobs.dead_letters", "jobs.outbox_events", "jobs.event_deliveries",
	"integration.webhook_endpoints", "integration.webhook_deliveries", "integration.provider_call_logs",
	"notification.notifications", "files.objects", "files.object_references", "files.access_grants"}

// Mixed tables must still have row-level security, even though a null tenant is allowed.
func TestMixedTablesHaveRLS(t *testing.T) {
	for _, name := range mixedTables {
		var rls, force bool
		if err := env.Owner.QueryRow(context.Background(), `SELECT relrowsecurity, relforcerowsecurity FROM pg_class WHERE oid = $1::regclass`, name).Scan(&rls, &force); err != nil {
			t.Fatal(err)
		}
		if !rls || !force {
			t.Errorf("%s: rls=%v forced=%v", name, rls, force)
		}
	}
}

func TestSchemaProtectsEveryTenantTable(t *testing.T) {
	faults, err := schemaFaults(context.Background(), env.Owner, []string{"identity", "platform", "audit", "shared", "jobs", "files", "integration", "notification"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range faults {
		t.Error(f)
	}
}

// The catalog check must really catch a broken table, or passing means nothing.
func TestSchemaCheckCatchesPlantedFault(t *testing.T) {
	ctx := context.Background()
	if _, err := env.Owner.Exec(ctx, `CREATE TABLE audit.planted_fault (id uuid PRIMARY KEY, tenant_id uuid NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = env.Owner.Exec(ctx, `DROP TABLE audit.planted_fault`) })
	faults, err := schemaFaults(ctx, env.Owner, []string{"audit"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"not enabled", "not forced", "no policy", "no UNIQUE"} {
		found := false
		for _, f := range faults {
			found = found || (strings.HasPrefix(f, "audit.planted_fault: ") && strings.Contains(f, want))
		}
		if !found {
			t.Errorf("planted fault not reported as %q; got: %v", want, faults)
		}
	}
}

func TestRuntimeRolesCannotBypass(t *testing.T) {
	rows, err := env.Owner.Query(context.Background(), `
		SELECT rolname, rolsuper, rolbypassrls FROM pg_roles WHERE rolname IN ('msp_app', 'msp_platform', 'msp_system')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var name string
		var super, bypass bool
		if err := rows.Scan(&name, &super, &bypass); err != nil {
			t.Fatal(err)
		}
		n++
		if super || bypass {
			t.Errorf("%s can bypass row-level security", name)
		}
	}
	if n != 3 {
		t.Fatalf("found %d roles", n)
	}
}

func appConn(t *testing.T, role, pw string) *pgx.Conn {
	t.Helper()
	c, err := pgx.Connect(context.Background(), env.DSN(role, pw))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c
}

func TestRLSEnforcedAtDatabase(t *testing.T) {
	_, _, tenantA := newTenant(t, "RLS A")
	_, _, tenantB := newTenant(t, "RLS B")
	ctx := context.Background()
	c := appConn(t, "msp_app", "dev_app")

	count := func(tenant string) int {
		t.Helper()
		tx, err := c.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if tenant != "" {
			if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenant); err != nil {
				t.Fatal(err)
			}
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM identity.memberships WHERE tenant_id = $1`, tenantA).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := count(""); n != 0 {
		t.Errorf("no tenant context saw %d of tenant A's rows", n)
	}
	if n := count(tenantB); n != 0 {
		t.Errorf("tenant B saw %d of tenant A's rows", n)
	}
	if n := count(tenantA); n != 1 {
		t.Errorf("tenant A saw %d of its own rows, want 1", n)
	}

	// Context ends with the transaction, so it cannot leak to the connection's next user.
	if n := count(""); n != 0 {
		t.Errorf("tenant context leaked past its transaction: saw %d rows", n)
	}

	// A write into another tenant is refused by the database.
	tx, err := c.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantB); err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO identity.tenant_profiles (tenant_id, onboarding_status) VALUES ($1, 'complete')`, tenantA)
	if err == nil {
		t.Error("tenant B wrote a row for tenant A")
	}
}

func TestCompositeForeignKeyBlocksCrossTenantReference(t *testing.T) {
	_, _, tenantA := newTenant(t, "FK A")
	_, _, tenantB := newTenant(t, "FK B")
	ctx := context.Background()
	var subID string
	// Arrange through the platform role: a subscription belonging to tenant A.
	p := appConn(t, "msp_platform", "dev_platform")
	err := p.QueryRow(ctx, `
		WITH pl AS (INSERT INTO platform.plans (product_version_id, key, name, billing_period, status)
		            SELECT id, 'fk_plan', 'FK', 'monthly', 'active' FROM platform.product_versions LIMIT 1 RETURNING id)
		INSERT INTO platform.subscriptions (tenant_id, plan_id, status, currency)
		SELECT $1, id, 'active', 'USD' FROM pl RETURNING id`, tenantA).Scan(&subID)
	if err != nil {
		t.Fatal(err)
	}
	// A grant for tenant B pointing at tenant A's subscription must be refused by the constraint itself.
	_, err = p.Exec(ctx, `
		INSERT INTO platform.entitlement_grants (tenant_id, module_id, subscription_id, source, status)
		SELECT $1, id, $2, 'subscription', 'pending' FROM platform.modules LIMIT 1`, tenantB, subID)
	if err == nil || !strings.Contains(err.Error(), "23503") {
		t.Fatalf("cross-tenant reference was not blocked by a foreign key: %v", err)
	}
}

func TestLeastPrivilegeGrants(t *testing.T) {
	ctx := context.Background()
	app := appConn(t, "msp_app", "dev_app")
	platform := appConn(t, "msp_platform", "dev_platform")
	denied := []struct {
		name string
		c    *pgx.Conn
		sql  string
	}{
		{"app reads admins", app, `SELECT * FROM identity.platform_admins`},
		{"app reads admin sessions", app, `SELECT * FROM identity.platform_admin_sessions`},
		{"app writes modules", app, `UPDATE platform.modules SET name = 'x'`},
		{"app grants itself access", app, `INSERT INTO platform.tenant_module_entitlements (tenant_id, module_id, status) SELECT gen_random_uuid(), id, 'active' FROM platform.modules`},
		{"app changes a grant", app, `UPDATE platform.entitlement_grants SET status = 'active'`},
		{"app rewrites audit", app, `UPDATE audit.events SET action = 'x'`},
		{"app deletes audit", app, `DELETE FROM audit.events`},
		{"app reads security events", app, `SELECT * FROM audit.security_events`},
		{"app lifts its own suspension", app, `UPDATE identity.tenants SET status = 'active'`},
		{"app clears its suspension date", app, `UPDATE identity.tenants SET suspended_at = NULL`},
		{"app changes its tenant owner", app, `UPDATE identity.tenants SET owner_user_id = NULL`},
		{"app reads an audit partition directly", app, `SELECT * FROM audit.events_default`},
		{"app creates partitions", app, `SELECT audit.ensure_partitions(1)`},
		{"app verifies chains", app, `SELECT * FROM audit.verify_chain(NULL)`},
		{"app rewrites audit hashes", app, `SELECT audit.event_hash(NULL, NULL)`},
		{"platform renames a tenant", platform, `UPDATE identity.tenants SET name = 'x'`},
		{"platform reads password hashes", platform, `SELECT * FROM identity.user_credentials`},
		{"platform reads MFA secrets", platform, `SELECT * FROM identity.mfa_factors`},
		{"platform reads user sessions", platform, `SELECT * FROM identity.sessions`},
		{"platform rewrites audit", platform, `UPDATE audit.events SET action = 'x'`},
	}
	for _, d := range denied {
		t.Run(d.name, func(t *testing.T) {
			_, err := d.c.Exec(ctx, d.sql)
			if err == nil || !strings.Contains(err.Error(), "permission denied") {
				t.Fatalf("expected permission denied, got %v", err)
			}
		})
	}
}

// operations lists every operation in a contract, with placeholder path values.
func operations(t *testing.T, get func() (*openapi3.T, error), base string) []op {
	t.Helper()
	spec, err := get()
	if err != nil {
		t.Fatal(err)
	}
	var out []op
	const placeholder = "01a1b22d-0000-7000-8000-000000000000"
	for path, item := range spec.Paths.Map() {
		for method, o := range item.Operations() {
			p := path
			for _, seg := range strings.Split(path, "/") {
				if strings.HasPrefix(seg, "{") {
					v := placeholder
					if seg == "{action}" {
						v = "activate"
					}
					if seg == "{moduleKey}" {
						v = "crm"
					}
					p = strings.Replace(p, seg, v, 1)
				}
			}
			access, _ := o.Extensions["x-access"].(string)
			perm, _ := o.Extensions["x-permission"].(string)
			mod, _ := o.Extensions["x-module"].(string)
			out = append(out, op{method: method, path: base + p, access: access, permission: perm, module: mod, id: o.OperationID})
		}
	}
	return out
}

type op struct{ method, path, access, permission, module, id string }

func allTenantOps(t *testing.T) []op {
	return append(operations(t, tenantapi.GetSwagger, "/api/v1"), operations(t, modulesapi.GetSwagger, "/api/v1")...)
}

func TestEveryRouteDeclaresAccess(t *testing.T) {
	ops := append(allTenantOps(t), operations(t, adminapi.GetSwagger, "/api/v1/admin")...)
	if len(ops) < 90 {
		t.Fatalf("only %d operations found", len(ops))
	}
	for _, o := range ops {
		if o.access == "" {
			t.Errorf("%s %s declares no access", o.method, o.path)
		}
	}
}

// Generated from the contract, so a new route is covered without anyone remembering to add a test.
func TestEveryProtectedRouteRejectsAnonymous(t *testing.T) {
	anon := newClient(t)
	for _, o := range append(allTenantOps(t), operations(t, adminapi.GetSwagger, "/api/v1/admin")...) {
		if o.access == "public" {
			continue
		}
		t.Run(o.id, func(t *testing.T) {
			// No query parameters are sent, so routes that require them also prove that
			// authentication is checked before validation.
			status, body := anon.Do(o.method, o.path, map[string]any{})
			if status != http.StatusUnauthorized || body["code"] != "authentication_required" {
				t.Fatalf("%s %s: got %d %v", o.method, o.path, status, body["code"])
			}
		})
	}
}

func TestTenantSessionCannotReachAdminRoutes(t *testing.T) {
	c, _, _ := newTenant(t, "Not An Admin")
	for _, o := range operations(t, adminapi.GetSwagger, "/api/v1/admin") {
		if o.access == "public" {
			continue
		}
		t.Run(o.id, func(t *testing.T) {
			if status, body := c.Do(o.method, o.path, map[string]any{}); status != http.StatusUnauthorized {
				t.Fatalf("%s %s: got %d %v", o.method, o.path, status, body["code"])
			}
		})
	}
}

// Generated: every module route is blocked at the API when the tenant has no entitlement, even for
// an owner who holds every permission. Hiding navigation is not what stops them.
func TestEveryModuleRouteRequiresEntitlement(t *testing.T) {
	c, _, _ := newTenant(t, "No Modules")
	n := 0
	for _, o := range operations(t, modulesapi.GetSwagger, "/api/v1") {
		if o.module == "" {
			t.Errorf("%s has no x-module", o.path)
			continue
		}
		n++
		status, body := c.Do(o.method, o.path, nil)
		if status != http.StatusForbidden || body["code"] != "module_not_entitled" || body["module_key"] != o.module {
			t.Errorf("%s: got %d %v", o.path, status, body)
		}
	}
	if n != 10 {
		t.Fatalf("checked %d modules, want 10", n)
	}
}

// Generated: a Viewer is denied every route whose declared permission the role lacks.
func TestPermissionsEnforcedForEveryDeclaredRoute(t *testing.T) {
	owner, _, _ := newTenant(t, "Perm Tenant")
	viewer, viewerEmail := signUp(t, "viewerperm")
	inv := owner.must(201, http.MethodPost, "/api/v1/tenant/invitations", map[string]any{"email": viewerEmail, "role": "viewer"})
	_ = inv
	m := env.Mail.waitFor(t, viewerEmail, "invited")
	viewer.must(200, http.MethodPost, "/api/v1/invitations/accept", map[string]any{"token": inviteRe.FindStringSubmatch(m.Body)[1]})

	var viewerPerms []string
	ctxOut := viewer.refreshCSRF("/api/v1/me/context")
	for _, p := range ctxOut["permissions"].([]any) {
		viewerPerms = append(viewerPerms, p.(string))
	}
	checked := 0
	for _, o := range operations(t, tenantapi.GetSwagger, "/api/v1") {
		if o.permission == "" || slices.Contains(viewerPerms, o.permission) {
			continue
		}
		checked++
		status, body := viewer.Do(o.method, o.path, map[string]any{})
		if status != http.StatusForbidden || body["code"] != "insufficient_permission" {
			t.Errorf("%s %s (%s): got %d %v", o.method, o.path, o.permission, status, body["code"])
		}
	}
	if checked < 8 {
		t.Fatalf("only %d routes checked", checked)
	}
}

// Tenant B acts on Tenant A's objects by ID, and every attempt must look like the object is absent.
func TestCrossTenantObjectAccessLooksNotFound(t *testing.T) {
	a, aEmail, tenantA := newTenant(t, "Canary Corp Alpha")
	b, _, _ := newTenant(t, "Other Corp Beta")
	var bLog []string
	b.log = &bLog

	members := a.must(200, http.MethodGet, "/api/v1/tenant/members", nil)["data"].([]any)
	memberA := members[0].(map[string]any)["id"].(string)
	inv := a.must(201, http.MethodPost, "/api/v1/tenant/invitations", map[string]any{"email": unique("invitee"), "role": "viewer"})
	sessions := a.must(200, http.MethodGet, "/api/v1/auth/sessions", nil)["data"].([]any)
	sessionA := sessions[0].(map[string]any)["id"].(string)

	b.wantCode(404, "not_found", http.MethodPatch, "/api/v1/tenant/members/"+memberA, map[string]any{"role": "viewer"})
	b.wantCode(404, "not_found", http.MethodDelete, "/api/v1/tenant/members/"+memberA, nil)
	b.wantCode(404, "not_found", http.MethodDelete, "/api/v1/tenant/invitations/"+inv["id"].(string), nil)
	b.wantCode(404, "not_found", http.MethodDelete, "/api/v1/auth/sessions/"+sessionA, nil)
	b.wantCode(404, "not_found", http.MethodPost, "/api/v1/me/active-tenant", map[string]any{"tenant_id": tenantA})

	// A's session and data are untouched by those attempts.
	a.must(200, http.MethodGet, "/api/v1/tenant", nil)

	// B's own views of everything must contain nothing of A's.
	for _, path := range []string{"/api/v1/me/context", "/api/v1/tenant", "/api/v1/tenant/members", "/api/v1/tenant/invitations", "/api/v1/tenant/audit-events", "/api/v1/entitlements", "/api/v1/subscriptions", "/api/v1/auth/sessions"} {
		b.must(200, http.MethodGet, path, nil)
	}
	for _, line := range bLog {
		for _, marker := range []string{"Canary Corp Alpha", aEmail, tenantA, memberA, sessionA} {
			if strings.Contains(line, marker) {
				t.Errorf("tenant B received tenant A's %q in: %s", marker, line)
			}
		}
	}
	if len(bLog) < 13 {
		t.Fatalf("canary scan covered only %d responses", len(bLog))
	}
}

func TestAuditIsTamperEvidentAndScoped(t *testing.T) {
	a, _, tenantA := newTenant(t, "Audit Tenant")
	a.must(200, http.MethodPatch, "/api/v1/tenant", map[string]any{"name": "Audit Tenant Renamed"})
	events := a.must(200, http.MethodGet, "/api/v1/tenant/audit-events", nil)["data"].([]any)
	if len(events) < 2 {
		t.Fatalf("expected audit events, got %d", len(events))
	}
	actions := map[string]bool{}
	for _, e := range events {
		m := e.(map[string]any)
		actions[m["action"].(string)] = true
		if m["tenant_id"] != tenantA {
			t.Errorf("foreign audit row %v", m)
		}
	}
	if !actions["tenant.created"] || !actions["tenant.updated"] {
		t.Errorf("missing actions: %v", actions)
	}
	ctx := context.Background()
	verify := func() (string, string) {
		t.Helper()
		platform := appConn(t, "msp_platform", "dev_platform")
		rows, err := platform.Query(ctx, `SELECT chain_seq::text, problem FROM audit.verify_chain($1)`, tenantA)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		if !rows.Next() {
			return "", ""
		}
		var seq, problem string
		if err := rows.Scan(&seq, &problem); err != nil {
			t.Fatal(err)
		}
		return seq, problem
	}
	if seq, problem := verify(); problem != "" {
		t.Fatalf("intact chain reported %s at %s", problem, seq)
	}

	// Tamper as the database superuser, the only role able to, and check the chain notices.
	super := appConn(t, "postgres", "test_superuser")
	if _, err := super.Exec(ctx, `UPDATE audit.events SET action = 'tenant.renamed_by_nobody' WHERE tenant_id = $1 AND chain_seq = 1`, tenantA); err != nil {
		t.Fatal(err)
	}
	if seq, problem := verify(); problem != "altered_row" || seq != "1" {
		t.Fatalf("altered row not detected: %q at %q", problem, seq)
	}
	if _, err := super.Exec(ctx, `UPDATE audit.events SET action = 'tenant.created' WHERE tenant_id = $1 AND chain_seq = 1`, tenantA); err != nil {
		t.Fatal(err)
	}
	if _, problem := verify(); problem != "" {
		t.Fatalf("restoring the row should restore the chain, got %q", problem)
	}
	if _, err := super.Exec(ctx, `DELETE FROM audit.events WHERE tenant_id = $1 AND chain_seq = 1`, tenantA); err != nil {
		t.Fatal(err)
	}
	if _, problem := verify(); problem != "missing_row" {
		t.Fatalf("deleted row not detected: %q", problem)
	}
}

// Concurrent writers must still produce a single unbroken chain.
func TestAuditChainSurvivesConcurrentWrites(t *testing.T) {
	a, _, tenantA := newTenant(t, "Concurrent Audit")
	var wg sync.WaitGroup
	for i := range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a.must(200, http.MethodPatch, "/api/v1/tenant", map[string]any{"name": fmt.Sprintf("Concurrent %d", i)})
		}()
	}
	wg.Wait()
	platform := appConn(t, "msp_platform", "dev_platform")
	var broken int
	if err := platform.QueryRow(context.Background(), `SELECT count(*) FROM audit.verify_chain($1)`, tenantA).Scan(&broken); err != nil {
		t.Fatal(err)
	}
	if broken != 0 {
		t.Fatal("concurrent writes broke the audit chain")
	}
	var n int
	if err := platform.QueryRow(context.Background(), `SELECT count(*) FROM audit.events WHERE tenant_id = $1`, tenantA).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 13 {
		t.Fatalf("got %d events, want 13", n)
	}
}
