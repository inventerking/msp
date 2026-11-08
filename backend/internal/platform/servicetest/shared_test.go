package servicetest

import (
	"errors"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/db"
	"mspengine/internal/kernel/jobs"
	"mspengine/internal/platform/shared"
)

func uniqueDomain() string { return "co" + strings.ReplaceAll(uuid.NewString()[:12], "-", "") + ".com" }

func TestConcurrentResolveCreatesOneCompany(t *testing.T) {
	d := uniqueDomain()
	var wg sync.WaitGroup
	ids := make([]uuid.UUID, 8)
	for i := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := sh.ResolveOrCreate(ctx, shared.Signal{Domain: "https://www." + d + "/x"})
			if err != nil {
				t.Error(err)
			}
			ids[i] = id
		}()
	}
	wg.Wait()
	for _, id := range ids {
		if id != ids[0] {
			t.Fatalf("got different companies: %v", ids)
		}
	}
	id, found, err := sh.ResolveByDomain(ctx, d)
	if err != nil || !found || id != ids[0] {
		t.Fatalf("%v %v %v", id, found, err)
	}
}

func TestFreeMailCannotBecomeACompany(t *testing.T) {
	if _, err := sh.ResolveOrCreate(ctx, shared.Signal{Domain: "someone@gmail.com"}); err == nil {
		t.Fatal("gmail.com became a company")
	}
}

// Only public or shareable data may enter canonical records.
func TestTenantLicensedFactsCannotEnterCanonicalData(t *testing.T) {
	for _, l := range []string{shared.LicenceTenantOnly, shared.LicenceTenantProvided, shared.LicenceSubjectProvided, ""} {
		_, err := sh.ResolveOrCreate(ctx, shared.Signal{Domain: uniqueDomain(), Facts: []shared.Fact{{Attribute: "revenue", Value: 1, SourceKey: "p", Licence: l, ObservedAt: time.Now()}}})
		if err == nil {
			t.Errorf("licence %q was accepted", l)
		}
	}
	// The database refuses it too, even if the service check were bypassed.
	id, err := sh.ResolveOrCreate(ctx, shared.Signal{Domain: uniqueDomain()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = env.Pools.App.Exec(ctx, `INSERT INTO shared.company_source_facts (company_id, attribute, value, source_key, licence_class, confidence, observed_at) VALUES ($1, 'x', '1', 's', 'licensed_tenant', 0.5, now())`, id)
	if err == nil {
		t.Fatal("the database accepted a tenant-licensed fact in a global table")
	}
}

func TestCompanyHasNoTraceOfWhoCreatedIt(t *testing.T) {
	rows, err := env.Super.Query(ctx, `SELECT column_name FROM information_schema.columns WHERE table_schema = 'shared' AND table_name IN ('companies', 'company_identifiers', 'company_source_facts') AND column_name IN ('tenant_id', 'created_by', 'created_by_tenant', 'notes')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var c string
		_ = rows.Scan(&c)
		t.Errorf("canonical company data has a column %q, which could reveal tenant interest", c)
	}
}

func TestMergeFollowsAndNotifies(t *testing.T) {
	a, b := uniqueDomain(), uniqueDomain()
	source, err := sh.ResolveOrCreate(ctx, shared.Signal{Domain: a, Facts: []shared.Fact{{Attribute: "industry", Value: "legal", SourceKey: "p", Licence: shared.LicencePublic, Confidence: 0.9, ObservedAt: time.Now()}}})
	if err != nil {
		t.Fatal(err)
	}
	target, err := sh.ResolveOrCreate(ctx, shared.Signal{Domain: b})
	if err != nil {
		t.Fatal(err)
	}
	admin := authctx.AdminPrincipal{AdminID: uuid.New()}
	if err := sh.MergeCompanies(ctx, admin, source, source, "self"); err == nil {
		t.Fatal("merged a company into itself")
	}
	if err := sh.MergeCompanies(ctx, admin, source, target, "duplicate record"); err != nil {
		t.Fatal(err)
	}
	id, found, err := sh.ResolveByDomain(ctx, a)
	if err != nil || !found || id != target {
		t.Fatalf("merged domain resolves to %v %v %v", id, found, err)
	}
	if err := sh.MergeCompanies(ctx, admin, source, target, "again"); err == nil {
		t.Fatal("merged an already merged company")
	}
	var n int
	if err := env.Pools.System.QueryRow(ctx, `SELECT count(*) FROM jobs.outbox_events WHERE event_type = 'shared.company.merged' AND aggregate_id = $1`, target).Scan(&n); err != nil || n != 1 {
		t.Fatalf("events %d %v", n, err)
	}
	var facts int
	if err := env.Super.QueryRow(ctx, `SELECT count(*) FROM shared.company_source_facts WHERE company_id = $1`, target).Scan(&facts); err != nil || facts != 1 {
		t.Fatalf("facts did not move: %d %v", facts, err)
	}
}

func TestOnlyPlatformRoleCanMerge(t *testing.T) {
	_, err := env.Pools.App.Exec(ctx, `UPDATE shared.companies SET status = 'dissolved'`)
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("got %v", err)
	}
}

func TestSearchFindsCanonicalCompanies(t *testing.T) {
	marker := "zq" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	if _, err := sh.ResolveOrCreate(ctx, shared.Signal{Domain: marker + ".com", SourceName: "Acme " + marker}); err != nil {
		t.Fatal(err)
	}
	rows, err := sh.SearchCompanies(ctx, marker)
	if err != nil || len(rows) != 1 {
		t.Fatalf("%d %v", len(rows), err)
	}
	if _, err := sh.SearchCompanies(ctx, "a"); err == nil {
		t.Fatal("a one-letter search was accepted")
	}
	// LIKE wildcards in the query match literally rather than dumping the table.
	if rows, err := sh.SearchCompanies(ctx, "'; DROP TABLE x; --"); err != nil || len(rows) != 0 {
		t.Fatalf("%d %v", len(rows), err)
	}
}

func contact(t *testing.T, tenant uuid.UUID, email string) uuid.UUID {
	t.Helper()
	id, err := sh.ResolveOrCreateContact(ctx, tenant, shared.ContactInput{Email: email, FullName: "Ann Example", JobTitle: "CIO", SourceKey: "csv", Licence: shared.LicenceTenantProvided})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestContactsAreTenantScoped(t *testing.T) {
	a, b := newTenant(t), newTenant(t)
	email := uuid.NewString()[:8] + "@example.com"
	ca := contact(t, a, email)
	if again := contact(t, a, strings.ToUpper(email)); again != ca {
		t.Fatal("the same email made a second contact in one tenant")
	}
	cb := contact(t, b, email)
	if ca == cb {
		t.Fatal("two tenants share one contact row")
	}
	// The hash differs per tenant, so hashes cannot be matched across tenants.
	var ha, hb []byte
	if err := env.Super.QueryRow(ctx, `SELECT email_hash FROM shared.contacts WHERE id = $1`, ca).Scan(&ha); err != nil {
		t.Fatal(err)
	}
	if err := env.Super.QueryRow(ctx, `SELECT email_hash FROM shared.contacts WHERE id = $1`, cb).Scan(&hb); err != nil {
		t.Fatal(err)
	}
	if string(ha) == string(hb) {
		t.Fatal("the same email hashed the same in two tenants")
	}
	// Tenant B cannot read or consent for tenant A's contact.
	err := db.TenantTx(ctx, env.Pools.App, b, uuid.Nil, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM shared.contacts WHERE id = $1`, ca).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			t.Error("tenant B can see tenant A's contact")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := sh.RecordConsent(ctx, b, ca, shared.ConsentInput{Purpose: "marketing_email", Status: "granted"}); err == nil {
		t.Fatal("tenant B recorded consent on tenant A's contact")
	}
}

func TestContactNeedsASource(t *testing.T) {
	if _, err := sh.ResolveOrCreateContact(ctx, newTenant(t), shared.ContactInput{Email: "a@example.com"}); err == nil {
		t.Fatal("a contact was created with no recorded source")
	}
}

type fakeEraser struct {
	name    string
	failFor int
	calls   int
}

func (f *fakeEraser) Name() string { return f.name }
func (f *fakeEraser) EraseContact(context.Context, uuid.UUID, uuid.UUID) error {
	f.calls++
	if f.calls <= f.failFor {
		return errors.New("module temporarily failing")
	}
	return nil
}
func (f *fakeEraser) ExportContact(context.Context, uuid.UUID, uuid.UUID) (any, error) {
	return map[string]string{"from": f.name}, nil
}

func TestErasureReachesModulesAndRetries(t *testing.T) {
	tenant := newTenant(t)
	svc := shared.New(env.Pools, keys)
	mod := &fakeEraser{name: "crm", failFor: 1}
	svc.RegisterEraser(mod)
	email := uuid.NewString()[:8] + "@example.com"
	id, err := svc.ResolveOrCreateContact(ctx, tenant, shared.ContactInput{Email: email, FullName: "Ann Example", SourceKey: "csv", Licence: shared.LicenceTenantProvided})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RecordConsent(ctx, tenant, id, shared.ConsentInput{Purpose: "marketing_email", Status: "granted", Evidence: map[string]any{"ip": "1.2.3.4", "form": "web"}}); err != nil {
		t.Fatal(err)
	}
	p := authctx.Principal{TenantID: tenant, UserID: uuid.New()}
	exp, err := svc.ExportContact(ctx, p, id)
	if err != nil || exp.Contact.Email == nil || *exp.Contact.Email != email || len(exp.Sources) != 1 || len(exp.Consents) != 1 || exp.Modules["crm"] == nil {
		t.Fatalf("export wrong: %+v %v", exp, err)
	}
	job, err := svc.RequestErasure(ctx, p, id)
	if err != nil {
		t.Fatal(err)
	}
	if other, err := svc.RequestErasure(ctx, p, id); err != nil || other != job {
		t.Fatalf("a repeat request should find the same job: %v %v", other, err)
	}
	if _, err := svc.RequestErasure(ctx, authctx.Principal{TenantID: newTenant(t), UserID: uuid.New()}, id); err == nil {
		t.Fatal("another tenant requested erasure")
	}
	r := jobs.NewRunner(env.Pools.System, "erase")
	r.BackoffBase, r.BackoffMax = time.Millisecond, time.Millisecond
	r.Register(shared.JobEraseContact, svc.HandleErase)
	for range 3 {
		if _, err := r.RunOne(ctx, jobs.QueueDefault); err != nil {
			t.Fatal(err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if mod.calls != 2 {
		t.Fatalf("module erase calls %d", mod.calls)
	}
	exp, err = svc.ExportContact(ctx, p, id)
	if err != nil {
		t.Fatal(err)
	}
	c := exp.Contact
	if c.Status != "erased" || c.Email != nil || c.FullName != nil || c.JobTitle != nil || len(exp.Sources) != 0 {
		t.Fatalf("personal data remains: %+v", exp)
	}
	var evidence string
	if err := env.Super.QueryRow(ctx, `SELECT evidence::text FROM shared.contact_consents WHERE contact_id = $1`, id).Scan(&evidence); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(evidence, "1.2.3.4") {
		t.Fatal("consent evidence kept personal data after erasure")
	}
	var n int
	if err := env.Pools.System.QueryRow(ctx, `SELECT count(*) FROM jobs.outbox_events WHERE event_type = 'shared.contact.erased' AND aggregate_id = $1`, id).Scan(&n); err != nil || n != 1 {
		t.Fatalf("events %d %v", n, err)
	}
	// The row and ID stay, so records in other modules are not left dangling.
	if _, err := svc.ExportContact(ctx, p, id); err != nil {
		t.Fatal(err)
	}
}

func TestFXRateLookup(t *testing.T) {
	if _, err := env.Super.Exec(ctx, `INSERT INTO shared.fx_rates (base_currency, quote_currency, rate, rate_date, source_key) VALUES ('EUR', 'USD', 1.10, '2026-01-01', 'test'), ('EUR', 'USD', 1.20, '2026-02-01', 'test')`); err != nil {
		t.Fatal(err)
	}
	for on, want := range map[string]float64{"2026-01-15": 1.10, "2026-03-01": 1.20} {
		d, _ := time.Parse("2006-01-02", on)
		rate, found, err := sh.Rate(ctx, "EUR", "USD", d)
		if err != nil || !found || rate != want {
			t.Errorf("%s: %v %v %v", on, rate, found, err)
		}
	}
	if rate, found, _ := sh.Rate(ctx, "USD", "USD", time.Now()); !found || rate != 1 {
		t.Error("the same currency should convert at 1")
	}
	if _, found, _ := sh.Rate(ctx, "GBP", "JPY", time.Now()); found {
		t.Error("an unknown pair should report no rate, and never guess")
	}
}
