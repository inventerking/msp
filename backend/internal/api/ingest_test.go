package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"mspengine/internal/api"
	"mspengine/internal/kernel/config"
	"mspengine/internal/kernel/ratelimit"
)

// The ingest process records tracker hits exactly as the API does, and serves nothing else.
func TestIngestProcessServesOnlyTheTracker(t *testing.T) {
	c, _ := visitorTenant(t, "Ingest Tenant")
	site := c.must(201, http.MethodPost, "/api/v1/visitor-intelligence/sites", map[string]any{"domain": "ingest-site.example"})
	srv := httptest.NewServer(api.NewIngest(api.IngestDeps{Config: config.Config{Env: "test", AuthRatePerMinute: 1_000_000}, Keys: graph.Keys,
		Limiter: ratelimit.New(nil), DB: graph.Pools.App, Metrics: graph.IngestMetrics(), Collect: graph.CollectHandler()}))
	defer srv.Close()

	post := func(path string, body any) int {
		b, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPost, srv.URL+path, bytes.NewReader(b))
		req.Header.Set("Content-Type", "text/plain;charset=UTF-8")
		req.Header.Set("Origin", "https://ingest-site.example")
		req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) Firefox/140.0")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if o := resp.Header.Get("Access-Control-Allow-Origin"); path == "/api/v1/public/collect" && o != "https://ingest-site.example" {
			t.Fatalf("CORS origin %q", o)
		}
		return resp.StatusCode
	}
	if st := post("/api/v1/public/collect", map[string]any{"site_key": site["site_key"], "type": "page_view", "url": "https://ingest-site.example/pricing"}); st != 204 {
		t.Fatalf("collect: %d", st)
	}
	if r := report(t, c, site["id"].(string)); r["page_views"] != 1.0 {
		t.Fatalf("hit not recorded: %v", r)
	}
	for _, path := range []string{"/api/v1/auth/login", "/api/v1/public/chat/x/messages", "/api/v1/admin/auth/login"} {
		if st := post(path, map[string]any{}); st != 404 {
			t.Fatalf("%s served by ingest: %d", path, st)
		}
	}
	if resp, err := http.Get(srv.URL + "/readyz"); err != nil || resp.StatusCode != 200 {
		t.Fatalf("readyz: %v %v", resp, err)
	}
}
