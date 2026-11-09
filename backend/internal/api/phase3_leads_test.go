package api_test

import (
	"bytes"
	"context"
	"encoding/csv"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"mspengine/internal/kernel/jobs"
)

// dispatch publishes and delivers events until none are left.
func dispatch(t *testing.T) {
	t.Helper()
	_, bus := graph.Worker("dispatch-test")
	for range 200 {
		pub, err := bus.PublishPending(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		del, err := bus.DeliverPending(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if pub == 0 && del == 0 {
			return
		}
	}
}

// leadTenant is a workspace with Lead Generation active, credits, and an ICP for legal and
// healthcare firms in Great Britain with 20 to 200 staff.
func leadTenant(t *testing.T, name string, credits int64) (*Client, string) {
	t.Helper()
	c, _, tenant := newTenant(t, name)
	super := adminClient(t, "super_admin")
	super.must(200, http.MethodPost, "/api/v1/admin/tenants/"+tenant+"/entitlements/lead_generation/activate", map[string]any{"reason": "phase 3 test"})
	if credits > 0 {
		for _, meter := range []string{"lead_credits", "ai_credits"} {
			if err := graph.Commercial.Grant(context.Background(), uuid.MustParse(tenant), meter, credits, "test-"+uuid.NewString(), nil, "test credits"); err != nil {
				t.Fatal(err)
			}
		}
	}
	c.must(200, http.MethodPut, "/api/v1/tenant/icp", map[string]any{"industries": []string{"legal_services", "healthcare"}, "countries": []string{"gb"}, "min_employees": 20, "max_employees": 200})
	return c, tenant
}

// tenantScalar reads one value as the application role, under a tenant's RLS context.
func tenantScalar(t *testing.T, tenant, query string, args ...any) string {
	t.Helper()
	ctx := context.Background()
	conn := appConn(t, "msp_app", "dev_app")
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenant); err != nil {
		t.Fatal(err)
	}
	var out string
	if err := tx.QueryRow(ctx, query, args...).Scan(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func balance(t *testing.T, tenant, meter string) int64 {
	t.Helper()
	rows, err := graph.Commercial.Balances(context.Background(), uuid.MustParse(tenant))
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.MeterKey == meter {
			return r.Balance
		}
	}
	return 0
}

func startSearch(t *testing.T, c *Client, key string, body map[string]any) map[string]any {
	t.Helper()
	c.headers = map[string]string{"Idempotency-Key": key}
	defer func() { c.headers = nil }()
	return c.must(202, http.MethodPost, "/api/v1/lead-generation/searches", body)
}

func TestICPValidation(t *testing.T) {
	c, _, _ := newTenant(t, "ICP Tenant")
	if got := c.must(200, http.MethodGet, "/api/v1/tenant/icp", nil); got["configured"] != false {
		t.Fatalf("got %v", got)
	}
	c.wantCode(422, "validation_failed", http.MethodPut, "/api/v1/tenant/icp", map[string]any{"countries": []string{"ZZ"}})
	c.wantCode(422, "validation_failed", http.MethodPut, "/api/v1/tenant/icp", map[string]any{"industries": []string{"astrology"}})
	c.wantCode(422, "validation_failed", http.MethodPut, "/api/v1/tenant/icp", map[string]any{"min_employees": 50, "max_employees": 10})
	got := c.must(200, http.MethodPut, "/api/v1/tenant/icp", map[string]any{"countries": []string{"de"}, "technologies": []string{" M365 "}})
	if asJSON(got["countries"]) != `["DE"]` || asJSON(got["technologies"]) != `["m365"]` || got["version"] != float64(1) {
		t.Fatalf("got %v", got)
	}
	if got := c.must(200, http.MethodPut, "/api/v1/tenant/icp", map[string]any{"countries": []string{"GB"}}); got["version"] != float64(2) {
		t.Fatalf("an update did not bump the version: %v", got)
	}
	ref := c.must(200, http.MethodGet, "/api/v1/reference", nil)
	if len(ref["countries"].([]any)) < 240 || len(ref["industries"].([]any)) < 10 {
		t.Fatalf("reference data missing: %d countries", len(ref["countries"].([]any)))
	}
}

func TestLeadSearchToScoredProspects(t *testing.T) {
	c, tenant := leadTenant(t, "Search Tenant", 100)
	body := map[string]any{"criteria": map[string]any{"countries": []string{"GB"}}, "max_results": 25}
	first := startSearch(t, c, "search-key-1", body)
	again := startSearch(t, c, "search-key-1", body)
	if first["id"] != again["id"] || first["status"] != "queued" {
		t.Fatalf("a repeated request made a second search: %v %v", first, again)
	}
	if got := balance(t, tenant, "lead_credits"); got != 100 {
		t.Fatalf("balance %d before the search ran", got)
	}
	id := first["id"].(string)
	work(t, jobs.QueueProviderIO)
	s := c.must(200, http.MethodGet, "/api/v1/lead-generation/searches/"+id, nil)
	if s["status"] != "completed" || s["result_count"] != float64(8) || s["credits_used"] != float64(8) {
		t.Fatalf("search: %v", s)
	}
	if got := balance(t, tenant, "lead_credits"); got != 92 {
		t.Fatalf("charged for %d credits, want 8", 100-got)
	}
	results := c.must(200, http.MethodGet, "/api/v1/lead-generation/searches/"+id+"/results", nil)["data"].([]any)
	if len(results) != 8 {
		t.Fatalf("%d results", len(results))
	}
	// A name licensed to one tenant is shown to it, but never enters the shared company record.
	var licensed map[string]any
	for _, r := range results {
		if r.(map[string]any)["licence_class"] == "licensed_tenant" {
			licensed = r.(map[string]any)
		}
	}
	var canonical string
	if err := env.Owner.QueryRow(context.Background(), `SELECT display_name FROM shared.companies WHERE id = $1`, licensed["company_id"]).Scan(&canonical); err != nil {
		t.Fatal(err)
	}
	if canonical == licensed["display_name"] || canonical != licensed["domain"] {
		t.Fatalf("a tenant-licensed name reached the shared record: %q", canonical)
	}

	p := c.must(200, http.MethodPost, "/api/v1/lead-generation/searches/"+id+"/results/promote", map[string]any{"all": true})
	if p["created"] != float64(8) || p["existing"] != float64(0) {
		t.Fatalf("promote: %v", p)
	}
	if p := c.must(200, http.MethodPost, "/api/v1/lead-generation/searches/"+id+"/results/promote", map[string]any{"all": true}); p["created"] != float64(0) || p["existing"] != float64(8) {
		t.Fatalf("promoting twice made duplicates: %v", p)
	}
	list := c.must(200, http.MethodGet, "/api/v1/lead-generation/prospects?limit=100", nil)["data"].([]any)
	byName := map[string]map[string]any{}
	for _, x := range list {
		byName[x.(map[string]any)["display_name"].(string)] = x.(map[string]any)
	}
	legal, factory := byName["Northwind Legal"], byName["Crestline Manufacturing"]
	if legal == nil || factory == nil {
		t.Fatalf("prospects: %v", asJSON(list))
	}
	if legal["score"].(float64) < 90 || factory["score"].(float64) > 40 || legal["data_quality"] != "high" {
		t.Fatalf("scores: legal %v, manufacturing %v", legal, factory)
	}
	sc := c.must(200, http.MethodGet, "/api/v1/lead-generation/prospects/"+factory["id"].(string)+"/score", nil)
	if sc["model_version"] != float64(1) || !strings.Contains(asJSON(sc["factors"]), `"state":"not_matched"`) {
		t.Fatalf("score: %v", sc)
	}
	if got := c.must(200, http.MethodGet, "/api/v1/lead-generation/prospects?min_score=90", nil)["data"].([]any); len(got) == 0 || len(got) >= 8 {
		t.Fatalf("min_score filter returned %d", len(got))
	}

	// An AI explanation is queued, produced by the fake provider, and labelled as AI.
	c.must(202, http.MethodPost, "/api/v1/lead-generation/prospects/"+legal["id"].(string)+"/explain", nil)
	work(t, jobs.QueueAI)
	if sc := c.must(200, http.MethodGet, "/api/v1/lead-generation/prospects/"+legal["id"].(string)+"/score", nil); sc["explanation"] == nil || sc["explanation_is_ai"] != true {
		t.Fatalf("explanation: %v", sc)
	}
	c.wantCode(409, "crm_unavailable", http.MethodPost, "/api/v1/lead-generation/prospects/"+legal["id"].(string)+"/convert", nil)
}

func TestNewScoringModelVersionChangesScores(t *testing.T) {
	c, _ := leadTenant(t, "Model Tenant", 50)
	p := c.must(201, http.MethodPost, "/api/v1/lead-generation/prospects", map[string]any{"domain": "https://www.summit-law-partners.example/about"})
	if p["domain"] != "summit-law-partners.example" {
		t.Fatalf("domain not normalized: %v", p)
	}
	c.wantCode(409, "prospect_exists", http.MethodPost, "/api/v1/lead-generation/prospects", map[string]any{"domain": "summit-law-partners.example"})
	id := p["id"].(string)
	before := c.must(200, http.MethodGet, "/api/v1/lead-generation/prospects/"+id+"/score", nil)
	if before["value"] != float64(50) || before["coverage"] != float64(0) {
		t.Fatalf("a prospect with no data must score a neutral 50: %v", before)
	}
	m := c.must(201, http.MethodPost, "/api/v1/lead-generation/scoring-models", map[string]any{"name": "Default", "weights": map[string]any{"industry": 70, "geography": 10, "employees": 20, "technology": 0}})
	if m["status"] != "draft" || m["version"] != float64(2) {
		t.Fatalf("model: %v", m)
	}
	c.must(200, http.MethodPost, "/api/v1/lead-generation/scoring-models/"+m["id"].(string)+"/publish", nil)
	c.wantCode(409, "model_not_draft", http.MethodPost, "/api/v1/lead-generation/scoring-models/"+m["id"].(string)+"/publish", nil)
	c.must(202, http.MethodPost, "/api/v1/lead-generation/prospects/"+id+"/enrich", map[string]any{"kinds": []string{"company"}})
	work(t, jobs.QueueProviderIO)
	after := c.must(200, http.MethodGet, "/api/v1/lead-generation/prospects/"+id+"/score", nil)
	if after["model_version"] != float64(2) || after["coverage"] != float64(1) {
		t.Fatalf("after enrichment and a new model: %v", after)
	}
	models := c.must(200, http.MethodGet, "/api/v1/lead-generation/scoring-models", nil)["data"].([]any)
	if !strings.Contains(asJSON(models), `"status":"retired"`) {
		t.Fatalf("the old model was not retired: %v", models)
	}
}

func TestEnrichmentCompanyContactsAndNoData(t *testing.T) {
	c, tenant := leadTenant(t, "Enrich Tenant", 10)
	p := c.must(201, http.MethodPost, "/api/v1/lead-generation/prospects", map[string]any{"domain": "riverside-clinic.example"})
	id := p["id"].(string)
	runs := c.must(202, http.MethodPost, "/api/v1/lead-generation/prospects/"+id+"/enrich", map[string]any{"kinds": []string{"company", "contact"}})["runs"].([]any)
	if len(runs) != 2 {
		t.Fatalf("runs: %v", runs)
	}
	work(t, jobs.QueueProviderIO)
	d := c.must(200, http.MethodGet, "/api/v1/lead-generation/prospects/"+id, nil)
	if d["display_name"] != "Riverside Clinic" || len(d["contacts"].([]any)) != 2 || !strings.Contains(asJSON(d["attributes"]), `"value":"healthcare"`) {
		t.Fatalf("detail: %v", asJSON(d))
	}
	for _, r := range d["enrichments"].([]any) {
		if r.(map[string]any)["status"] != "succeeded" {
			t.Fatalf("run: %v", r)
		}
	}
	if got := balance(t, tenant, "lead_credits"); got != 8 {
		t.Fatalf("balance %d, want 8", got)
	}
	// A company the provider does not know is "no data", which costs nothing.
	n := c.must(201, http.MethodPost, "/api/v1/lead-generation/prospects", map[string]any{"domain": "unknown-firm.example"})
	c.must(202, http.MethodPost, "/api/v1/lead-generation/prospects/"+n["id"].(string)+"/enrich", map[string]any{"kinds": []string{"company"}})
	work(t, jobs.QueueProviderIO)
	nd := c.must(200, http.MethodGet, "/api/v1/lead-generation/prospects/"+n["id"].(string), nil)
	if nd["enrichments"].([]any)[0].(map[string]any)["status"] != "no_data" {
		t.Fatalf("enrichment: %v", nd["enrichments"])
	}
	if got := balance(t, tenant, "lead_credits"); got != 8 {
		t.Fatalf("no data was charged: balance %d", got)
	}
}

func TestLeadCreditsAreRequired(t *testing.T) {
	c, _ := leadTenant(t, "Broke Tenant", 0)
	c.headers = map[string]string{"Idempotency-Key": "broke-search"}
	c.wantCode(402, "credits_exhausted", http.MethodPost, "/api/v1/lead-generation/searches", map[string]any{"criteria": map[string]any{}})
	c.headers = nil
	c.wantCode(422, "validation_failed", http.MethodPost, "/api/v1/lead-generation/searches", map[string]any{"criteria": map[string]any{}})
}

func TestProspectsAreIsolatedBetweenTenants(t *testing.T) {
	a, _ := leadTenant(t, "Leads Alpha", 50)
	b, _ := leadTenant(t, "Leads Beta", 50)
	pa := a.must(201, http.MethodPost, "/api/v1/lead-generation/prospects", map[string]any{"domain": "pioneer-logistics.example", "display_name": "Alpha's secret name"})
	pb := b.must(201, http.MethodPost, "/api/v1/lead-generation/prospects", map[string]any{"domain": "pioneer-logistics.example"})
	if pa["company_id"] != pb["company_id"] || pa["id"] == pb["id"] {
		t.Fatalf("both tenants should share the company but not the prospect: %v %v", pa, pb)
	}
	var shared string
	if err := env.Owner.QueryRow(context.Background(), `SELECT display_name FROM shared.companies WHERE id = $1`, pa["company_id"]).Scan(&shared); err != nil {
		t.Fatal(err)
	}
	if shared == "Alpha's secret name" {
		t.Fatal("a tenant-entered name reached the shared record")
	}
	s := startSearch(t, a, "alpha-search", map[string]any{"criteria": map[string]any{"countries": []string{"US"}}})
	list := a.must(201, http.MethodPost, "/api/v1/lead-generation/lists", map[string]any{"name": "Alpha list"})
	var bLog []string
	b.log = &bLog
	for _, path := range []string{"/prospects/" + pa["id"].(string), "/prospects/" + pa["id"].(string) + "/score", "/searches/" + s["id"].(string), "/searches/" + s["id"].(string) + "/results"} {
		b.wantCode(404, "not_found", http.MethodGet, "/api/v1/lead-generation"+path, nil)
	}
	b.wantCode(404, "not_found", http.MethodPatch, "/api/v1/lead-generation/prospects/"+pa["id"].(string), map[string]any{"status": "qualified"})
	b.wantCode(404, "not_found", http.MethodPost, "/api/v1/lead-generation/searches/"+s["id"].(string)+"/results/promote", map[string]any{"all": true})
	b.wantCode(404, "not_found", http.MethodPost, "/api/v1/lead-generation/lists/"+list["id"].(string)+"/members", map[string]any{"prospect_ids": []string{pb["id"].(string)}})
	own := b.must(201, http.MethodPost, "/api/v1/lead-generation/lists", map[string]any{"name": "Beta list"})
	b.wantCode(422, "validation_failed", http.MethodPost, "/api/v1/lead-generation/lists/"+own["id"].(string)+"/members", map[string]any{"prospect_ids": []string{pa["id"].(string)}})
	for _, path := range []string{"/prospects", "/searches", "/lists", "/saved-searches"} {
		b.must(200, http.MethodGet, "/api/v1/lead-generation"+path, nil)
	}
	for _, line := range bLog {
		for _, marker := range []string{pa["id"].(string), s["id"].(string), "Alpha's secret name", "Alpha list"} {
			if strings.Contains(line, marker) {
				t.Errorf("tenant B saw %q in %s", marker, line)
			}
		}
	}
}

func uploadCSV(t *testing.T, c *Client, body string) string {
	t.Helper()
	up := c.must(201, http.MethodPost, "/api/v1/files/uploads", map[string]any{"purpose": "csv_import", "file_name": "leads.csv", "size_bytes": len(body)})
	if s, _, out := c.raw(http.MethodPut, up["upload_url"].(string), []byte(body)); s != 204 {
		t.Fatalf("upload: %d %s", s, out)
	}
	work(t, jobs.QueueDefault)
	return up["id"].(string)
}

func TestImportAndExportCSV(t *testing.T) {
	c, _ := leadTenant(t, "CSV Tenant", 50)
	c.must(201, http.MethodPost, "/api/v1/lead-generation/prospects", map[string]any{"domain": "existing-firm.example"})
	file := uploadCSV(t, c, "Website,Company,Country,Sector,Staff\n"+
		"alpha-dental.example,=HYPERLINK(\"http://x\"),gb,healthcare,40\n"+
		"existing-firm.example,Already Here,GB,,\n"+
		"not a domain,Bad Row,GB,,\n"+
		"beta-law.example,Beta Law,ZZ,legal_services,12\n"+
		"alpha-dental.example,Again,GB,,\n")
	c.wantCode(422, "validation_failed", http.MethodPost, "/api/v1/lead-generation/imports", map[string]any{"file_id": file, "mapping": map[string]any{"domain": "Website"}, "lawful_source_attested": false})
	list := c.must(201, http.MethodPost, "/api/v1/lead-generation/lists", map[string]any{"name": "Imported"})
	imp := c.must(202, http.MethodPost, "/api/v1/lead-generation/imports", map[string]any{"file_id": file, "lawful_source_attested": true, "list_id": list["id"],
		"mapping": map[string]any{"domain": "Website", "name": "Company", "country": "Country", "industry": "Sector", "employees": "Staff"}})
	work(t, jobs.QueueDefault)
	got := c.must(200, http.MethodGet, "/api/v1/lead-generation/imports/"+imp["id"].(string), nil)
	if got["status"] != "completed" {
		_, _, report := c.raw(http.MethodGet, "/api/v1/lead-generation/imports/"+imp["id"].(string)+"/report", nil)
		t.Fatalf("import failed: %s", report)
	}
	if got["rows_total"] != float64(5) || got["rows_imported"] != float64(1) || got["rows_duplicate"] != float64(2) || got["rows_rejected"] != float64(2) {
		t.Fatalf("import: %v", got)
	}
	s, _, report := c.raw(http.MethodGet, "/api/v1/lead-generation/imports/"+imp["id"].(string)+"/report", nil)
	if s != 200 || !strings.Contains(string(report), "invalid_domain") || !strings.Contains(string(report), "unknown_country") {
		t.Fatalf("report %d: %s", s, report)
	}
	lists := c.must(200, http.MethodGet, "/api/v1/lead-generation/lists", nil)["data"].([]any)
	if lists[0].(map[string]any)["member_count"] != float64(2) {
		t.Fatalf("lists: %v", lists)
	}

	// Add a provider record licensed to this tenant, whose facts must not leave in an export.
	search := startSearch(t, c, "csv-search", map[string]any{"criteria": map[string]any{"countries": []string{"GB"}, "industries": []string{"healthcare"}}})
	work(t, jobs.QueueProviderIO)
	c.must(200, http.MethodPost, "/api/v1/lead-generation/searches/"+search["id"].(string)+"/results/promote", map[string]any{"all": true})

	exp := c.must(202, http.MethodPost, "/api/v1/lead-generation/exports", map[string]any{})
	c.wantCode(409, "export_not_ready", http.MethodGet, "/api/v1/lead-generation/exports/"+exp["id"].(string)+"/download", nil)
	work(t, jobs.QueueDefault)
	e := c.must(200, http.MethodGet, "/api/v1/lead-generation/exports/"+exp["id"].(string), nil)
	if e["status"] != "completed" || e["rows_total"] != float64(3) || e["values_withheld"] != float64(5) {
		t.Fatalf("export: %v", e)
	}
	s, h, out := c.raw(http.MethodGet, "/api/v1/lead-generation/exports/"+exp["id"].(string)+"/download", nil)
	if s != 200 || !strings.HasPrefix(h.Get("Content-Disposition"), "attachment") {
		t.Fatalf("download %d %v", s, h)
	}
	rows, err := csv.NewReader(bytes.NewReader(out)).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		for _, cell := range r {
			if strings.HasPrefix(cell, "=") {
				t.Fatalf("formula reached the export: %q", cell)
			}
		}
		if r[1] == "harbor-dental-group.example" && (r[0] != "" || r[3] != "") {
			t.Fatalf("tenant-licensed values were exported: %v", r)
		}
	}
	if !strings.Contains(string(out), "'=HYPERLINK") {
		t.Fatalf("export: %s", out)
	}
}

func TestMergeRepointsProspectsInEveryTenant(t *testing.T) {
	a, _ := leadTenant(t, "Merge Alpha", 10)
	b, _ := leadTenant(t, "Merge Beta", 10)
	old := a.must(201, http.MethodPost, "/api/v1/lead-generation/prospects", map[string]any{"domain": "old-brand.example"})
	keep := a.must(201, http.MethodPost, "/api/v1/lead-generation/prospects", map[string]any{"domain": "new-brand.example"})
	bOld := b.must(201, http.MethodPost, "/api/v1/lead-generation/prospects", map[string]any{"domain": "old-brand.example"})
	super := adminClient(t, "super_admin")
	super.must(204, http.MethodPost, "/api/v1/admin/companies/merge", map[string]any{"source_id": old["company_id"], "target_id": keep["company_id"], "reason": "rebrand"})
	dispatch(t)
	// B had only the old company, so its prospect now points at the survivor.
	if got := b.must(200, http.MethodGet, "/api/v1/lead-generation/prospects/"+bOld["id"].(string), nil); got["company_id"] != keep["company_id"] {
		t.Fatalf("not repointed: %v", got)
	}
	// A had both, so the duplicate is retired and the survivor stays.
	a.wantCode(404, "not_found", http.MethodGet, "/api/v1/lead-generation/prospects/"+old["id"].(string), nil)
	a.must(200, http.MethodGet, "/api/v1/lead-generation/prospects/"+keep["id"].(string), nil)
}

func TestLosingTheModuleCancelsOpenSearches(t *testing.T) {
	c, tenant := leadTenant(t, "Revoked Tenant", 50)
	s := startSearch(t, c, "revoked-search", map[string]any{"criteria": map[string]any{}})
	super := adminClient(t, "super_admin")
	super.must(200, http.MethodPost, "/api/v1/admin/tenants/"+tenant+"/entitlements/lead_generation/suspend", map[string]any{"reason": "testing cancellation"})
	dispatch(t)
	work(t, jobs.QueueProviderIO)
	c.wantCode(403, "module_not_entitled", http.MethodGet, "/api/v1/lead-generation/searches/"+s["id"].(string), nil)
	status := tenantScalar(t, tenant, `SELECT status FROM lead_generation.search_jobs WHERE id = $1`, s["id"])
	if status != "cancelled" {
		t.Fatalf("status %s", status)
	}
}

// Module jobs go to these queues, so a worker that did not poll one would leave them waiting forever.
func TestWorkerPollsEveryModuleQueue(t *testing.T) {
	r, _ := graph.Worker("queue-check")
	for _, q := range []string{jobs.QueueDefault, jobs.QueueProviderIO, jobs.QueueResearch, jobs.QueueAI} {
		if !r.Polls(q) {
			t.Errorf("the worker does not poll %s", q)
		}
	}
}
