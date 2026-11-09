package api_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"mspengine/internal/kernel/jobs"
	ciapp "mspengine/internal/modules/competitorintel/app"
)

// ciTenant is a workspace with both prospecting modules, AI credits and a GB ICP.
func ciTenant(t *testing.T, name string) (*Client, string) {
	t.Helper()
	c, tenant := leadTenant(t, name, 200)
	super := adminClient(t, "super_admin")
	super.must(200, http.MethodPost, "/api/v1/admin/tenants/"+tenant+"/entitlements/competitor_intelligence/activate", map[string]any{"reason": "phase 3 test"})
	return c, tenant
}

func fakeFetcher(t *testing.T) *ciapp.FakeFetcher {
	t.Helper()
	f, ok := graph.Competitors.Fetcher.(*ciapp.FakeFetcher)
	if !ok {
		t.Fatal("tests must use the fake fetcher")
	}
	return f
}

func observationsByType(t *testing.T, c *Client, competitor string, history bool) map[string][]map[string]any {
	t.Helper()
	path := "/api/v1/competitor-intelligence/competitors/" + competitor + "/observations"
	if history {
		path += "?history=true"
	}
	out := map[string][]map[string]any{}
	for _, o := range c.must(200, http.MethodGet, path, nil)["data"].([]any) {
		m := o.(map[string]any)
		out[m["attribute_type"].(string)] = append(out[m["attribute_type"].(string)], m)
	}
	return out
}

func TestCompetitorResearchFlow(t *testing.T) {
	c, _ := ciTenant(t, "Research Tenant")
	cands := c.must(200, http.MethodPost, "/api/v1/competitor-intelligence/discover", map[string]any{})["data"].([]any)
	if !strings.Contains(asJSON(cands), "brightbyte-it-services.example") || strings.Contains(asJSON(cands), "lakeside") {
		t.Fatalf("discovery should follow the ICP's countries: %v", asJSON(cands))
	}
	comp := c.must(201, http.MethodPost, "/api/v1/competitor-intelligence/competitors", map[string]any{"domain": "brightbyte-it-services.example", "display_name": "Brightbyte"})
	id := comp["id"].(string)
	c.wantCode(409, "competitor_exists", http.MethodPost, "/api/v1/competitor-intelligence/competitors", map[string]any{"domain": "brightbyte-it-services.example"})
	if got := c.must(200, http.MethodPost, "/api/v1/competitor-intelligence/discover", map[string]any{})["data"].([]any); strings.Contains(asJSON(got), "brightbyte") {
		t.Fatal("a tracked competitor was suggested again")
	}
	for _, bad := range []string{"http://127.0.0.1/admin", "http://localhost/", "file:///etc/passwd", "https://user:pw@brightbyte-it-services.example/", "http://2130706433/", "https://[::1]/", "https://intranet/"} {
		c.wantCode(422, "validation_failed", http.MethodPost, "/api/v1/competitor-intelligence/competitors/"+id+"/sources", map[string]any{"url": bad, "kind": "website"})
	}
	c.must(201, http.MethodPost, "/api/v1/competitor-intelligence/competitors/"+id+"/sources", map[string]any{"url": "https://no-robots.example/pricing", "kind": "pricing_page"})

	run := c.must(202, http.MethodPost, "/api/v1/competitor-intelligence/competitors/"+id+"/research-runs", nil)
	work(t, jobs.QueueResearch)
	got := c.must(200, http.MethodGet, "/api/v1/competitor-intelligence/research-runs/"+run["id"].(string), nil)
	if got["status"] != "completed" || got["pages_fetched"] != float64(1) {
		t.Fatalf("run: %v", got)
	}
	sources := asJSON(c.must(200, http.MethodGet, "/api/v1/competitor-intelligence/competitors/"+id+"/sources", nil))
	if !strings.Contains(sources, `"access_status":"disallowed_by_robots"`) || !strings.Contains(sources, `"access_status":"allowed"`) {
		t.Fatalf("sources: %s", sources)
	}
	obs := observationsByType(t, c, id, false)
	if len(obs["service"]) != 3 || len(obs["pricing"]) != 1 || len(obs["geography"]) != 1 {
		t.Fatalf("observations: %v", obs)
	}
	for _, list := range obs {
		for _, o := range list {
			if o["evidence_class"] != "observed" || o["source_url"] != "https://brightbyte-it-services.example/" || o["quote"] == nil {
				t.Fatalf("an observation lacks its label or source: %v", o)
			}
		}
	}
	if strings.Contains(asJSON(obs), "free") || strings.Contains(asJSON(obs), "secret") {
		t.Fatal("text from a script, or from a page robots.txt disallows, was analyzed")
	}
	changes := c.must(200, http.MethodGet, "/api/v1/competitor-intelligence/competitors/"+id+"/changes", nil)["data"].([]any)
	if len(changes) != 4 {
		t.Fatalf("first research should record each attribute once: %v", changes)
	}

	// Unchanged content creates nothing new.
	c.must(202, http.MethodPost, "/api/v1/competitor-intelligence/competitors/"+id+"/research-runs", nil)
	work(t, jobs.QueueResearch)
	if got := c.must(200, http.MethodGet, "/api/v1/competitor-intelligence/competitors/"+id+"/changes", nil)["data"].([]any); len(got) != 4 {
		t.Fatalf("an unchanged page made changes: %d", len(got))
	}

	// A price change is detected, and the old observation is kept as history.
	fakeFetcher(t).Set("https://brightbyte-it-services.example/", ciapp.Page{Status: 200, ContentType: "text/html", Body: []byte(
		"<p>Services: Managed IT, Cybersecurity, Cloud backup</p><p>Pricing: From £99 per user per month</p><p>Regions: GB</p>")})
	c.must(202, http.MethodPost, "/api/v1/competitor-intelligence/competitors/"+id+"/research-runs", nil)
	work(t, jobs.QueueResearch)
	changes = c.must(200, http.MethodGet, "/api/v1/competitor-intelligence/competitors/"+id+"/changes", nil)["data"].([]any)
	latest := changes[0].(map[string]any)
	if len(changes) != 5 || latest["attribute_type"] != "pricing" || !strings.Contains(latest["summary"].(string), "£99") {
		t.Fatalf("changes: %v", changes)
	}
	if cur := observationsByType(t, c, id, false); len(cur["pricing"]) != 1 || !strings.Contains(cur["pricing"][0]["value"].(string), "99") || len(cur["industry"]) != 1 {
		t.Fatalf("current: %v", cur)
	}
	if hist := observationsByType(t, c, id, true); len(hist["pricing"]) != 2 || hist["pricing"][1]["superseded_by_id"] == nil {
		t.Fatalf("history: %v", hist["pricing"])
	}
	c.must(204, http.MethodPost, "/api/v1/competitor-intelligence/changes/"+latest["id"].(string)+"/acknowledge", nil)
	if list := c.must(200, http.MethodGet, "/api/v1/competitor-intelligence/competitors", nil)["data"].([]any); list[0].(map[string]any)["open_changes"] != float64(4) {
		t.Fatalf("list: %v", list)
	}
}

func TestCompetitorReportsAndComparisons(t *testing.T) {
	c, _ := ciTenant(t, "Report Tenant")
	a := c.must(201, http.MethodPost, "/api/v1/competitor-intelligence/competitors", map[string]any{"domain": "cobalt-managed-services.example"})
	b := c.must(201, http.MethodPost, "/api/v1/competitor-intelligence/competitors", map[string]any{"domain": "lakeside-tech-support.example"})
	for _, x := range []map[string]any{a, b} {
		c.must(202, http.MethodPost, "/api/v1/competitor-intelligence/competitors/"+x["id"].(string)+"/research-runs", nil)
	}
	work(t, jobs.QueueResearch)
	cmp := c.must(201, http.MethodPost, "/api/v1/competitor-intelligence/comparisons", map[string]any{"name": "Local rivals", "competitor_ids": []any{a["id"], b["id"]}, "dimensions": []string{"pricing", "geography"}})
	members := cmp["members"].([]any)
	if len(members) != 2 || strings.Contains(asJSON(members), `"service":`) || !strings.Contains(asJSON(members), `"evidence_class":"observed"`) {
		t.Fatalf("comparison: %v", asJSON(cmp))
	}
	c.wantCode(422, "validation_failed", http.MethodPost, "/api/v1/competitor-intelligence/comparisons", map[string]any{"name": "Bad", "competitor_ids": []any{a["id"], uuid.NewString()}})

	rep := c.must(202, http.MethodPost, "/api/v1/competitor-intelligence/reports", map[string]any{"kind": "market", "title": "Market view", "competitor_ids": []any{a["id"], b["id"]}})
	c.wantCode(409, "report_not_ready", http.MethodGet, "/api/v1/competitor-intelligence/reports/"+rep["id"].(string)+"/pdf", nil)
	work(t, jobs.QueueAI)
	got := c.must(200, http.MethodGet, "/api/v1/competitor-intelligence/reports/"+rep["id"].(string), nil)
	if got["status"] != "ready" || got["summary_label"] != "ai_estimate" || got["notice"] == nil || got["has_pdf"] != true {
		t.Fatalf("report: %v", got)
	}
	for _, o := range got["opportunities"].([]any) {
		if o.(map[string]any)["evidence_class"] != "ai_estimate" {
			t.Fatalf("an AI opportunity is not labelled: %v", o)
		}
	}
	s, h, pdf := c.raw(http.MethodGet, "/api/v1/competitor-intelligence/reports/"+rep["id"].(string)+"/pdf", nil)
	if s != 200 || h.Get("Content-Type") != "application/pdf" || !strings.Contains(string(pdf), "ai_estimate") || !strings.Contains(string(pdf), "AI estimates are generated") {
		t.Fatalf("pdf %d: %s", s, pdf)
	}
	opps := c.must(200, http.MethodGet, "/api/v1/competitor-intelligence/opportunities", nil)["data"].([]any)
	if len(opps) == 0 {
		t.Fatal("no opportunities from the report")
	}
	mine := c.must(201, http.MethodPost, "/api/v1/competitor-intelligence/opportunities", map[string]any{"title": "Dental practices in Leeds", "rationale": "No local rival lists dental work", "industry": "healthcare"})
	if mine["evidence_class"] != "inferred" {
		t.Fatalf("a user's opportunity must be labelled inferred: %v", mine)
	}
	c.wantCode(422, "validation_failed", http.MethodPost, "/api/v1/competitor-intelligence/opportunities", map[string]any{"title": "Bad", "rationale": "Unknown industry", "industry": "astrology"})
	if got := c.must(200, http.MethodPatch, "/api/v1/competitor-intelligence/opportunities/"+mine["id"].(string), map[string]any{"status": "pursuing"}); got["status"] != "pursuing" {
		t.Fatalf("got %v", got)
	}
}

func TestCompetitorsArePrivateToTheTenant(t *testing.T) {
	a, _ := ciTenant(t, "Rival Alpha")
	b, _ := ciTenant(t, "Rival Beta")
	comp := a.must(201, http.MethodPost, "/api/v1/competitor-intelligence/competitors", map[string]any{"domain": "kestrel-engineering.example", "notes": "Alpha's private notes"})
	a.must(202, http.MethodPost, "/api/v1/competitor-intelligence/competitors/"+comp["id"].(string)+"/research-runs", nil)
	work(t, jobs.QueueResearch)
	pa := a.must(201, http.MethodPost, "/api/v1/lead-generation/prospects", map[string]any{"domain": "kestrel-engineering.example"})
	pb := b.must(201, http.MethodPost, "/api/v1/lead-generation/prospects", map[string]any{"domain": "kestrel-engineering.example"})
	if got := a.must(200, http.MethodGet, "/api/v1/lead-generation/prospects/"+pa["id"].(string), nil); got["is_competitor"] != true {
		t.Fatalf("tenant A is not warned: %v", got["is_competitor"])
	}
	if got := b.must(200, http.MethodGet, "/api/v1/lead-generation/prospects/"+pb["id"].(string), nil); got["is_competitor"] != false {
		t.Fatal("tenant B learned that tenant A tracks this company")
	}
	var bLog []string
	b.log = &bLog
	id := comp["id"].(string)
	for _, path := range []string{"/competitors/" + id, "/competitors/" + id + "/observations", "/competitors/" + id + "/changes", "/competitors/" + id + "/sources", "/competitors/" + id + "/research-config"} {
		b.wantCode(404, "not_found", http.MethodGet, "/api/v1/competitor-intelligence"+path, nil)
	}
	b.wantCode(404, "not_found", http.MethodPost, "/api/v1/competitor-intelligence/competitors/"+id+"/research-runs", nil)
	b.wantCode(422, "validation_failed", http.MethodPost, "/api/v1/competitor-intelligence/reports", map[string]any{"kind": "competitor", "competitor_ids": []string{id}})
	b.must(200, http.MethodGet, "/api/v1/competitor-intelligence/competitors", nil)
	b.must(200, http.MethodGet, "/api/v1/competitor-intelligence/opportunities", nil)
	for _, line := range bLog {
		if strings.Contains(line, id) || strings.Contains(line, "Alpha's private notes") {
			t.Errorf("tenant B saw tenant A's competitor: %s", line)
		}
	}
}

func TestScheduledMonitoring(t *testing.T) {
	c, _ := ciTenant(t, "Monitor Tenant")
	comp := c.must(201, http.MethodPost, "/api/v1/competitor-intelligence/competitors", map[string]any{"domain": "coastal-realty.example", "monitoring_enabled": true})
	id := comp["id"].(string)
	cfg := c.must(200, http.MethodPut, "/api/v1/competitor-intelligence/competitors/"+id+"/research-config", map[string]any{"cadence": "weekly", "enabled": true})
	if cfg["next_run_at"] == nil {
		t.Fatalf("config: %v", cfg)
	}
	for range 2 { // A second tick finds nothing due, so only one run is queued.
		if err := graph.Competitors.HandleMonitorTick(context.Background(), jobs.Job{}); err != nil {
			t.Fatal(err)
		}
	}
	work(t, jobs.QueueResearch)
	runs := c.must(200, http.MethodGet, "/api/v1/competitor-intelligence/competitors/"+id+"/research-runs", nil)["data"].([]any)
	if len(runs) != 1 || runs[0].(map[string]any)["scheduled"] != true || runs[0].(map[string]any)["status"] != "completed" {
		t.Fatalf("runs: %v", runs)
	}
	next := c.must(200, http.MethodGet, "/api/v1/competitor-intelligence/competitors/"+id+"/research-config", nil)["next_run_at"].(string)
	if next <= cfg["next_run_at"].(string) {
		t.Fatalf("next run not advanced: %s", next)
	}
}
