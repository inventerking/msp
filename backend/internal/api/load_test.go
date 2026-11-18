//go:build loadtest

package api_test

// Load test against the stated profile in docs/architecture/observability-and-reliability.md.
// It is not part of the normal suite. Run it with:
//
//	go test -tags loadtest -run TestLoadProfile -timeout 90m -v ./internal/api/ 2>&1 | grep -v '"msg":"request"'
//
// Knobs (environment): LOAD_TENANTS (500), LOAD_RPS (100), LOAD_INGEST_EPS (2000),
// LOAD_DURATION (2m), LOAD_BIG_COMPANIES (100000), LOAD_BIG_ACTIVITIES (500000), LOAD_SITES (50),
// LOAD_REPORT (file to write the Markdown result to).
//
// It runs the API in-process against a real PostgreSQL container, with the production pool sizes.
// The load generator shares the machine, so results are a floor for one API process, not a
// statement about production capacity.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"net/http/httptest"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mspengine/internal/api"
	"mspengine/internal/app"
	"mspengine/internal/kernel/config"
	"mspengine/internal/kernel/crypto"
	"mspengine/internal/kernel/db"
	"mspengine/internal/kernel/ratelimit"
	"mspengine/internal/platform/files"
)

func knob(name string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(name)); err == nil && v > 0 {
		return v
	}
	return def
}

type loadTenant struct {
	c         *Client
	id        string
	companies []string
}

type sample struct {
	op     string
	status int
	dur    time.Duration
}

type recorder struct {
	mu      sync.Mutex
	samples []sample
}

func (r *recorder) add(s sample) { r.mu.Lock(); r.samples = append(r.samples, s); r.mu.Unlock() }

type stats struct {
	op                 string
	n, errors, limited int
	p50, p95, p99, max time.Duration
}

func (r *recorder) summary() []stats {
	r.mu.Lock()
	defer r.mu.Unlock()
	by := map[string][]sample{}
	for _, s := range r.samples {
		by[s.op] = append(by[s.op], s)
		by["all CRM"] = append(by["all CRM"], s)
	}
	delete(by, "all CRM")
	all := []sample{}
	for op, ss := range by {
		if op != "ingest" {
			all = append(all, ss...)
		}
	}
	if len(all) > 0 {
		by["all CRM"] = all
	}
	var out []stats
	for op, ss := range by {
		d := make([]time.Duration, len(ss))
		st := stats{op: op, n: len(ss)}
		for i, s := range ss {
			d[i] = s.dur
			switch {
			case s.status == 429:
				st.limited++
			case s.status >= 400 || s.status == 0:
				st.errors++
			}
		}
		sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
		pct := func(p float64) time.Duration { return d[min(len(d)-1, int(float64(len(d))*p))] }
		st.p50, st.p95, st.p99, st.max = pct(.50), pct(.95), pct(.99), d[len(d)-1]
		out = append(out, st)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].op < out[j].op })
	return out
}

// send makes one request without failing the test, so it can run from any goroutine.
func send(hc *http.Client, base, csrf, method, path string, body []byte, headers map[string]string) (int, time.Duration) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, _ := http.NewRequest(method, base+path, rd)
	if body != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	start := time.Now()
	resp, err := hc.Do(req)
	if err != nil {
		return 0, time.Since(start)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return resp.StatusCode, time.Since(start)
}

// crmRequest picks one ordinary database-backed request, weighted towards reads as in normal use.
func crmRequest(rng *rand.Rand, tn *loadTenant) (op, method, path string, body []byte) {
	co := tn.companies[rng.IntN(len(tn.companies))]
	switch r := rng.IntN(100); {
	case r < 30:
		return "list companies", http.MethodGet, "/api/v1/crm/companies?limit=25", nil
	case r < 45:
		return "search companies", http.MethodGet, "/api/v1/crm/companies?limit=25&q=" + fmt.Sprintf("Synthetic+%d", rng.IntN(1000)), nil
	case r < 65:
		return "get company", http.MethodGet, "/api/v1/crm/companies/" + co, nil
	case r < 80:
		return "company activities", http.MethodGet, "/api/v1/crm/activities?company_id=" + co, nil
	case r < 90:
		b, _ := json.Marshal(map[string]any{"activity_type": "note", "subject_type": "company", "subject_id": co, "summary": "Load test note"})
		return "log activity", http.MethodPost, "/api/v1/crm/activities", b
	default:
		return "session context", http.MethodGet, "/api/v1/me/context", nil
	}
}

// drive sends requests at a fixed arrival rate until the context ends, whatever the latency, so a
// slow server shows up as latency rather than as fewer requests.
func drive(ctx context.Context, rate int, fire func(rng *rand.Rand)) {
	if rate <= 0 {
		return
	}
	interval := time.Second / time.Duration(rate)
	sem := make(chan struct{}, 2500) // The profile's concurrent users at peak.
	var wg sync.WaitGroup
	next := time.Now()
	for i := uint64(0); ctx.Err() == nil; i++ {
		next = next.Add(interval)
		if d := time.Until(next); d > 0 {
			time.Sleep(d)
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(seed uint64) {
			defer func() { <-sem; wg.Done() }()
			fire(rand.New(rand.NewPCG(seed, 7)))
		}(i)
	}
	wg.Wait()
}

func TestLoadProfile(t *testing.T) {
	nTenants, rps, eps := knob("LOAD_TENANTS", 500), knob("LOAD_RPS", 100), knob("LOAD_INGEST_EPS", 2000)
	bigCompanies, bigActivities, nSites := knob("LOAD_BIG_COMPANIES", 100000), knob("LOAD_BIG_ACTIVITIES", 500000), knob("LOAD_SITES", 50)
	dur := time.Duration(knob("LOAD_DURATION_SECONDS", 120)) * time.Second
	ctx := context.Background()

	// 1. Tenants, each with CRM, a few companies, and some with a tracked site.
	t.Logf("seeding %d tenants", nTenants)
	seedStart := time.Now()
	tenants := make([]*loadTenant, nTenants)
	var sites []map[string]string
	var sitesMu sync.Mutex
	work := make(chan int)
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range work {
				c, id := crmTenant(t, fmt.Sprintf("Load %d T", i)) // The letter after the number keeps generated emails unique.
				tn := &loadTenant{c: c, id: id}
				for j := range 5 {
					co := c.must(201, http.MethodPost, "/api/v1/crm/companies", map[string]any{"name": fmt.Sprintf("Synthetic %d-%d", i, j)})
					tn.companies = append(tn.companies, co["id"].(string))
				}
				if i < nSites {
					adminClient(t, "super_admin").must(200, http.MethodPost, "/api/v1/admin/tenants/"+id+"/entitlements/visitor_intelligence/activate", map[string]any{"reason": "load test"})
					domain := fmt.Sprintf("load-%d.example", i)
					s := c.must(201, http.MethodPost, "/api/v1/visitor-intelligence/sites", map[string]any{"domain": domain})
					sitesMu.Lock()
					sites = append(sites, map[string]string{"key": s["site_key"].(string), "origin": "https://" + domain})
					sitesMu.Unlock()
				}
				tenants[i] = tn
			}
		}()
	}
	for i := range nTenants {
		work <- i
	}
	close(work)
	wg.Wait()
	for i, tn := range tenants {
		if tn == nil {
			t.Fatalf("tenant %d was not seeded", i)
		}
	}

	// 2. The largest tenant, written as the application role with its tenant set, so RLS applies.
	big := tenants[0]
	t.Logf("seeding the largest tenant: %d companies, %d activities", bigCompanies, bigActivities)
	conn := appConn(t, "msp_app", "dev_app")
	err := pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, big.id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO crm.companies (tenant_id, name, domain, lifecycle)
			SELECT $1, 'Synthetic ' || g, 'synthetic-' || g || '.example', (ARRAY['prospect','customer','partner','other'])[1 + g % 4]
			FROM generate_series(1, $2::int) g`, big.id, bigCompanies); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO crm.activities (tenant_id, activity_type, crm_company_id, summary, occurred_at)
			SELECT $1, (ARRAY['call','note','meeting','email_sent'])[1 + g % 4], c.ids[1 + g % cardinality(c.ids)], 'Synthetic activity ' || g, now() - (g % 10000) * interval '1 hour'
			FROM generate_series(1, $2::int) g, (SELECT array_agg(id) AS ids FROM crm.companies WHERE tenant_id = $1) c`, big.id, bigActivities)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.Owner.Exec(ctx, `ANALYZE crm.companies; ANALYZE crm.activities`); err != nil {
		t.Fatal(err)
	}
	// The largest tenant's sample companies, read as its own user, so the requests are realistic.
	list := big.c.must(200, http.MethodGet, "/api/v1/crm/companies?limit=100", nil)
	for _, it := range list["data"].([]any) {
		big.companies = append(big.companies, it.(map[string]any)["id"].(string))
	}
	seedTime := time.Since(seedStart)

	// The ingest process, as deployed: its own graph and pool. LOAD_SHARED_INGEST=1 sends tracking to
	// the API instead, as before the ingest role existed, for comparison.
	ingestURL := env.URL
	if os.Getenv("LOAD_SHARED_INGEST") != "1" {
		ingestURL = startIngest(t)
	}

	// 3. Phase A: CRM traffic alone. Phase B: the same with ingest at the stated rate.
	var reqs atomic.Int64
	run := func(withIngest bool) *recorder {
		rec := &recorder{}
		cctx, cancel := context.WithTimeout(ctx, dur)
		defer cancel()
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			drive(cctx, rps, func(rng *rand.Rand) {
				tn := tenants[rng.IntN(len(tenants))]
				if rng.IntN(4) == 0 { // A quarter of traffic is in the largest tenant.
					tn = big
				}
				op, method, path, body := crmRequest(rng, tn)
				st, d := send(tn.c.c, env.URL, tn.c.csrf, method, path, body, nil)
				reqs.Add(1)
				rec.add(sample{op, st, d})
			})
		}()
		if withIngest && len(sites) > 0 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				hc := &http.Client{Timeout: 20 * time.Second, Transport: &http.Transport{MaxIdleConnsPerHost: 2500}}
				drive(cctx, eps, func(rng *rand.Rand) {
					s := sites[rng.IntN(len(sites))]
					visitor := strconv.Itoa(rng.IntN(20000))
					b, _ := json.Marshal(map[string]any{"site_key": s["key"], "type": "page_view", "url": s["origin"] + "/page/" + strconv.Itoa(rng.IntN(50)),
						"anonymous_id": "anon-" + visitor, "session_id": "sess-" + visitor, "consent": "granted"})
					st, d := send(hc, ingestURL, "", http.MethodPost, "/api/v1/public/collect", b, map[string]string{"Content-Type": "text/plain;charset=UTF-8", "Origin": s["origin"],
						"User-Agent": "Mozilla/5.0 (X11; Linux x86_64) Firefox/140.0"})
					rec.add(sample{"ingest", st, d})
				})
			}()
		}
		wg.Wait()
		return rec
	}
	t.Logf("phase A: CRM at %d rps for %s", rps, dur)
	a := run(false)
	t.Logf("phase B: CRM at %d rps with ingest at %d events/s for %s", rps, eps, dur)
	b := run(true)

	// 4. Report.
	var out strings.Builder
	fmt.Fprintf(&out, "Profile: %d tenants; largest tenant %d companies and %d activities; CRM %d requests/s; ingest %d events/s to %d sites; %s per phase. Seeding took %s.\n\n",
		nTenants, bigCompanies, bigActivities, rps, eps, len(sites), dur, seedTime.Round(time.Second))
	if ingestURL == env.URL {
		out.WriteString("Ingest: served by the API process, sharing its pool.\n\n")
	} else {
		fmt.Fprintf(&out, "Ingest: its own process with a %d-connection pool.\n\n", knob("LOAD_INGEST_CONNS", 10))
	}
	table := func(title string, r *recorder) map[string]stats {
		fmt.Fprintf(&out, "**%s**\n\n| Operation | Requests | Errors | Rate limited | p50 | p95 | p99 | Max |\n|---|---|---|---|---|---|---|---|\n", title)
		m := map[string]stats{}
		for _, s := range r.summary() {
			m[s.op] = s
			fmt.Fprintf(&out, "| %s | %d | %d | %d | %s | %s | %s | %s |\n", s.op, s.n, s.errors, s.limited, ms(s.p50), ms(s.p95), ms(s.p99), ms(s.max))
		}
		out.WriteString("\n")
		return m
	}
	sa := table("Phase A: CRM only", a)
	sb := table("Phase B: CRM with visitor ingest", b)
	crmA, crmB, ing := sa["all CRM"], sb["all CRM"], sb["ingest"]
	achieved := float64(ing.n) / dur.Seconds()
	fmt.Fprintf(&out, "| Target | Result | Met? |\n|---|---|---|\n")
	fmt.Fprintf(&out, "| CRM p95 below 500 ms | %s alone, %s with ingest | %s |\n", ms(crmA.p95), ms(crmB.p95), yes(crmA.p95 < 500*time.Millisecond && crmB.p95 < 500*time.Millisecond))
	fmt.Fprintf(&out, "| CRM p95 within 10%% during ingest | %+.0f%% | %s |\n", 100*(float64(crmB.p95)/float64(crmA.p95)-1), yes(float64(crmB.p95) <= 1.1*float64(crmA.p95)))
	fmt.Fprintf(&out, "| Ingest sustains %d events/s | %.0f events/s accepted at p95 %s, %d errors | %s |\n", eps, achieved, ms(ing.p95), ing.errors, yes(ing.errors == 0 && achieved >= 0.95*float64(eps)))
	fmt.Fprintf(&out, "| No errors in CRM traffic | %d errors | %s |\n", crmA.errors+crmB.errors, yes(crmA.errors+crmB.errors == 0))
	t.Log("\n" + out.String())
	if p := os.Getenv("LOAD_REPORT"); p != "" {
		if err := os.WriteFile(p, []byte(out.String()), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// startIngest runs the ingest handler on its own server, with its own pools sized as in production.
func startIngest(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	open := func(role, pw string, n int32) *pgxpool.Pool {
		p, err := db.Open(ctx, env.DSN(role, pw), n)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(p.Close)
		return p
	}
	pools := &db.Pools{App: open("msp_app", "dev_app", int32(knob("LOAD_INGEST_CONNS", 10))), Platform: open("msp_platform", "dev_platform", 2), System: open("msp_system", "dev_system", 1)}
	keys := crypto.Keys{HMAC: bytes.Repeat([]byte{7}, 32), Encryption: bytes.Repeat([]byte{9}, 32)}
	g, err := app.Build(pools, keys, app.Options{Mailer: &fakeMailer{}, Store: &files.MemoryStore{}, Scanner: files.AllowAllScanner{}, Bucket: "test", BaseURL: "http://app.test"})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Env: "test", AuthRatePerMinute: 1_000_000}
	srv := httptest.NewServer(api.NewIngest(api.IngestDeps{Config: cfg, Keys: keys, Limiter: ratelimit.New(nil), DB: pools.App, Metrics: g.IngestMetrics(), Collect: g.CollectHandler()}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func ms(d time.Duration) string {
	return strconv.FormatFloat(float64(d.Microseconds())/1000, 'f', 1, 64) + " ms"
}

func yes(ok bool) string {
	if ok {
		return "Yes"
	}
	return "**No**"
}
