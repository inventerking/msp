package api_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// visitorTenant is a workspace with CRM and Visitor Intelligence.
func visitorTenant(t *testing.T, name string) (*Client, string) {
	t.Helper()
	c, tenant := crmTenant(t, name)
	adminClient(t, "super_admin").must(200, http.MethodPost, "/api/v1/admin/tenants/"+tenant+"/entitlements/visitor_intelligence/activate", map[string]any{"reason": "phase 5 test"})
	return c, tenant
}

// collect sends a tracker hit the way the tracker does: text/plain, from the site's page.
func collect(t *testing.T, origin, ua string, hit map[string]any) int {
	t.Helper()
	b, _ := json.Marshal(hit)
	r, _ := http.NewRequest(http.MethodPost, env.URL+"/api/v1/public/collect", strings.NewReader(string(b)))
	r.Header.Set("Content-Type", "text/plain;charset=UTF-8")
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if ua == "" {
		ua = "Mozilla/5.0 (X11; Linux x86_64) Firefox/140.0"
	}
	r.Header.Set("User-Agent", ua)
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if o := resp.Header.Get("Access-Control-Allow-Origin"); origin != "" && o != origin {
		t.Fatalf("CORS origin %q", o)
	}
	return resp.StatusCode
}

func report(t *testing.T, c *Client, site string) map[string]any {
	t.Helper()
	return c.must(200, http.MethodGet, "/api/v1/visitor-intelligence/sites/"+site+"/report?days=7", nil)
}

func TestTrackingFollowsTheSiteConsentMode(t *testing.T) {
	c, tenant := visitorTenant(t, "Track Tenant")
	site := c.must(201, http.MethodPost, "/api/v1/visitor-intelligence/sites", map[string]any{"domain": "track-site.example"})
	if site["consent_mode"] != "opt_in" || site["ip_handling"] != "discard" {
		t.Fatalf("defaults: %v", site)
	}
	key, id, origin := site["site_key"].(string), site["id"].(string), "https://track-site.example"
	hit := func(extra map[string]any) map[string]any {
		h := map[string]any{"site_key": key, "type": "page_view", "url": "https://track-site.example/pricing?email=pat%40x.example&utm_source=news&token=abc#top",
			"referrer": "https://search.example/q?x=1", "anonymous_id": "anon-1", "session_id": "sess-1"}
		for k, v := range extra {
			h[k] = v
		}
		return h
	}
	// Opt-in without consent: an anonymous page view only.
	if st := collect(t, origin, "", hit(nil)); st != 204 {
		t.Fatalf("collect %d", st)
	}
	r := report(t, c, id)
	if r["page_views"] != 1.0 || r["sessions"] != 0.0 || r["visitors"] != 0.0 {
		t.Fatalf("without consent: %v", r)
	}
	// With consent, a session and a visitor are recorded.
	collect(t, origin, "", map[string]any{"site_key": key, "type": "consent", "consent": "granted", "anonymous_id": "anon-1", "policy_version": "v1"})
	collect(t, origin, "", hit(map[string]any{"consent": "granted"}))
	collect(t, origin, "", hit(map[string]any{"consent": "granted", "url": "https://www.track-site.example/about"}))
	r = report(t, c, id)
	if r["page_views"] != 3.0 || r["sessions"] != 1.0 || r["visitors"] != 1.0 {
		t.Fatalf("with consent: %v", r)
	}
	// Query strings keep only utm_ parameters, so emails and tokens in links are not stored.
	pages := asJSON(r["pages"])
	if !strings.Contains(pages, "https://track-site.example/pricing?utm_source=news") || strings.Contains(pages, "pat") || strings.Contains(pages, "token") || strings.Contains(pages, "#top") {
		t.Fatalf("pages: %s", pages)
	}
	if !strings.Contains(asJSON(r["sources"]), "search.example") {
		t.Fatalf("sources: %v", r["sources"])
	}
	// Other origins, other domains, bots and unknown keys are refused or ignored.
	if st := collect(t, "https://evil.example", "", hit(nil)); st != 403 {
		t.Fatalf("foreign origin %d", st)
	}
	collect(t, origin, "", hit(map[string]any{"url": "https://other-site.example/"}))
	collect(t, origin, "Googlebot/2.1", hit(nil))
	if st := collect(t, origin, "", hit(map[string]any{"site_key": "vi_unknown"})); st != 404 {
		t.Fatalf("unknown key %d", st)
	}
	if r := report(t, c, id); r["page_views"] != 3.0 {
		t.Fatalf("ignored hits were counted: %v", r)
	}
	// No IP address or raw browser ID is stored anywhere.
	for _, table := range []string{"sessions", "events", "visitors", "consent_records"} {
		dump := tenantScalar(t, tenant, `SELECT coalesce(string_agg(t::text, ' '), '') FROM visitor_intelligence.`+table+` t`)
		if strings.Contains(dump, "127.0.0.1") || strings.Contains(dump, "anon-1") || strings.Contains(dump, "sess-1") {
			t.Fatalf("%s holds raw identifiers: %s", table, dump)
		}
	}
	// A paused site's key stops working.
	c.must(200, http.MethodPatch, "/api/v1/visitor-intelligence/sites/"+id, map[string]any{"status": "paused"})
	if st := collect(t, origin, "", hit(nil)); st != 404 {
		t.Fatalf("paused site %d", st)
	}
}

func TestCookielessSitesNeverTrackVisitors(t *testing.T) {
	c, _ := visitorTenant(t, "Cookieless Tenant")
	site := c.must(201, http.MethodPost, "/api/v1/visitor-intelligence/sites", map[string]any{"domain": "cookieless.example", "consent_mode": "cookieless_only"})
	collect(t, "https://cookieless.example", "", map[string]any{"site_key": site["site_key"], "type": "page_view", "url": "https://cookieless.example/", "anonymous_id": "a", "session_id": "s", "consent": "granted"})
	if r := report(t, c, site["id"].(string)); r["page_views"] != 1.0 || r["sessions"] != 0.0 {
		t.Fatalf("cookieless: %v", r)
	}
}

func TestGoalsCountOncePerSession(t *testing.T) {
	c, _ := visitorTenant(t, "Goal Tenant")
	site := c.must(201, http.MethodPost, "/api/v1/visitor-intelligence/sites", map[string]any{"domain": "goal-site.example", "consent_mode": "opt_out"})
	id, key := site["id"].(string), site["site_key"]
	c.must(201, http.MethodPost, "/api/v1/visitor-intelligence/sites/"+id+"/goals", map[string]any{"name": "Contact form", "kind": "url_match", "value": "/thanks"})
	c.must(201, http.MethodPost, "/api/v1/visitor-intelligence/sites/"+id+"/goals", map[string]any{"name": "Demo booked", "kind": "custom_event", "value": "demo_booked"})
	for range 2 {
		collect(t, "https://goal-site.example", "", map[string]any{"site_key": key, "type": "page_view", "url": "https://goal-site.example/thanks", "anonymous_id": "g", "session_id": "gs"})
	}
	collect(t, "https://goal-site.example", "", map[string]any{"site_key": key, "type": "custom", "name": "demo_booked", "url": "https://goal-site.example/demo", "anonymous_id": "g", "session_id": "gs"})
	if r := report(t, c, id); r["conversions"] != 2.0 {
		t.Fatalf("conversions: %v", r)
	}
	// Opting out stops the session from being recorded.
	collect(t, "https://goal-site.example", "", map[string]any{"site_key": key, "type": "page_view", "url": "https://goal-site.example/", "anonymous_id": "h", "session_id": "hs", "consent": "denied"})
	if r := report(t, c, id); r["sessions"] != 1.0 {
		t.Fatalf("opt-out recorded a session: %v", r)
	}
}

func TestIdentifiedCompaniesGoToCRM(t *testing.T) {
	c, _ := visitorTenant(t, "Identify Tenant")
	site := c.must(201, http.MethodPost, "/api/v1/visitor-intelligence/sites", map[string]any{"domain": "identify-site.example", "consent_mode": "opt_out", "ip_handling": "truncate_then_discard"})
	collect(t, "https://identify-site.example", "", map[string]any{"site_key": site["site_key"], "type": "page_view", "url": "https://identify-site.example/", "anonymous_id": "i", "session_id": "is"})
	companies := report(t, c, site["id"].(string))["companies"].([]any)
	if len(companies) != 1 || companies[0].(map[string]any)["domain"] != "loopback-visitor.example" {
		t.Fatalf("companies: %v", companies)
	}
	company := companies[0].(map[string]any)["company_id"].(string)
	crmCo := c.must(200, http.MethodPost, "/api/v1/visitor-intelligence/companies/"+company+"/send-to-crm", nil)["id"].(string)
	again := c.must(200, http.MethodPost, "/api/v1/visitor-intelligence/companies/"+company+"/send-to-crm", nil)["id"].(string)
	if crmCo != again {
		t.Fatal("sending twice made two CRM companies")
	}
	dispatch(t)
	if tl := asJSON(c.must(200, http.MethodGet, "/api/v1/crm/activities?crm_company_id="+crmCo, nil)); !strings.Contains(tl, "Website visits from this company") {
		t.Fatalf("timeline: %s", tl)
	}
	// A site that discards IP addresses identifies nobody.
	plain := c.must(201, http.MethodPost, "/api/v1/visitor-intelligence/sites", map[string]any{"domain": "discard-site.example", "consent_mode": "opt_out"})
	collect(t, "https://discard-site.example", "", map[string]any{"site_key": plain["site_key"], "type": "page_view", "url": "https://discard-site.example/", "anonymous_id": "d", "session_id": "ds"})
	if n := len(report(t, c, plain["id"].(string))["companies"].([]any)); n != 0 {
		t.Fatalf("%d companies identified with IP discard", n)
	}
}

func TestVisitorDataIsIsolated(t *testing.T) {
	a, _ := visitorTenant(t, "Iso Vis A")
	b, _ := visitorTenant(t, "Iso Vis B")
	site := a.must(201, http.MethodPost, "/api/v1/visitor-intelligence/sites", map[string]any{"domain": "iso-vis.example", "consent_mode": "opt_out", "ip_handling": "truncate_then_discard"})
	id := site["id"].(string)
	collect(t, "https://iso-vis.example", "", map[string]any{"site_key": site["site_key"], "type": "page_view", "url": "https://iso-vis.example/", "anonymous_id": "x", "session_id": "xs"})
	company := report(t, a, id)["companies"].([]any)[0].(map[string]any)["company_id"].(string)
	for _, path := range []string{"/sites/" + id, "/sites/" + id + "/report", "/sites/" + id + "/goals"} {
		if st, _ := b.Do(http.MethodGet, "/api/v1/visitor-intelligence"+path, nil); st != 404 {
			t.Fatalf("B reads %s: %d", path, st)
		}
	}
	b.wantCode(404, "not_found", http.MethodPatch, "/api/v1/visitor-intelligence/sites/"+id, map[string]any{"status": "paused"})
	b.wantCode(404, "not_found", http.MethodPost, "/api/v1/visitor-intelligence/companies/"+company+"/send-to-crm", nil)
	if s := asJSON(b.must(200, http.MethodGet, "/api/v1/visitor-intelligence/sites", nil)); strings.Contains(s, "iso-vis") {
		t.Fatalf("B sees A's site: %s", s)
	}
	// The same domain can be tracked by another workspace, with its own key.
	other := b.must(201, http.MethodPost, "/api/v1/visitor-intelligence/sites", map[string]any{"domain": "iso-vis.example"})
	if other["site_key"] == site["site_key"] {
		t.Fatal("shared key")
	}

}
