package api_test

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"mspengine/internal/kernel/jobs"
)

// rollup delivers pending events, then computes one tenant's metrics. It does not wait on the
// analytics queue, which other tests' tenants also fill.
func rollup(t *testing.T, tenant string) {
	t.Helper()
	dispatch(t)
	if err := graph.SalesIntel.Rollup(context.Background(), uuid.MustParse(tenant)); err != nil {
		t.Fatal(err)
	}
}

// workUntil runs a queue until done reports true.
func workUntil(t *testing.T, queue string, done func() bool) {
	t.Helper()
	for range 50 {
		if done() {
			return
		}
		work(t, queue)
	}
	t.Fatal("the queue did not finish the work")
}

func thisMonth() string { return time.Now().UTC().Format("2006-01") + "-01" }

// seriesValue finds a metric's value for a month and dimension value in an analytics view.
func seriesValue(t *testing.T, view map[string]any, key, dim, dimValue, month string) (float64, bool) {
	t.Helper()
	for _, x := range view["series"].([]any) {
		s := x.(map[string]any)
		if s["key"] != key || s["dimension"] != dim {
			continue
		}
		for _, y := range s["points"].([]any) {
			p := y.(map[string]any)
			dv, _ := p["dimension_value"].(string)
			if p["period"] == month && dv == dimValue {
				v, ok := p["value"].(float64)
				return v, ok
			}
		}
	}
	return 0, false
}

func TestMRRReconcilesWithContracts(t *testing.T) {
	c, tenant := growthTenant(t, "Intel Reconcile")
	company, _ := winDeal(t, c, "Reconcile One", 120000)
	winDeal(t, c, "Reconcile Two", 30000)
	profile := customerFor(t, c, company)["id"].(string)
	extra := c.must(201, http.MethodPost, "/api/v1/customer-expansion/customers/"+profile+"/contracts", map[string]any{
		"start_date": time.Now().Format(time.DateOnly), "currency": "GBP", "mrr_minor": 10000})["id"].(string)
	c.must(204, http.MethodPatch, "/api/v1/customer-expansion/contracts/"+extra, map[string]any{"mrr_minor": 4000})
	rollup(t, tenant)
	view := c.must(200, http.MethodGet, "/api/v1/sales-intelligence/revenue", nil)
	if view["available"] != true || view["currency"] == "" || view["as_of"] == nil {
		t.Fatalf("revenue view: %v", view)
	}
	contracts, _ := strconv.ParseFloat(tenantScalar(t, tenant, `SELECT COALESCE(sum(mrr_minor), 0)::text FROM customer_expansion.contracts WHERE status IN ('active','renewing')`), 64)
	mrr, ok := seriesValue(t, view, "mrr", "all", "", thisMonth())
	if !ok || contracts != 154000 || mrr != contracts {
		t.Fatalf("MRR %v does not reconcile with contracts %v", mrr, contracts)
	}
	if arr, _ := seriesValue(t, view, "arr", "all", "", thisMonth()); arr != mrr*12 {
		t.Fatalf("ARR %v", arr)
	}
	if n, _ := seriesValue(t, view, "new_customers", "all", "", thisMonth()); n != 2 {
		t.Fatalf("new customers %v", n)
	}
	// A second contract for an existing customer is expansion; lowering it is contraction.
	if e, _ := seriesValue(t, view, "expansion_mrr", "all", "", thisMonth()); e != 10000 {
		t.Fatalf("expansion %v", e)
	}
	if e, _ := seriesValue(t, view, "contraction_mrr", "all", "", thisMonth()); e != 6000 {
		t.Fatalf("contraction %v", e)
	}
	for _, x := range view["series"].([]any) {
		s := x.(map[string]any)
		for _, y := range s["points"].([]any) {
			if y.(map[string]any)["value_kind"] != s["value_kind"] {
				t.Fatalf("a series mixes value kinds: %v", s)
			}
		}
	}

	// Replaying every revenue event adds no facts, so MRR is unchanged.
	_, bus := graph.Worker("replay-intel")
	if _, err := bus.Replay(context.Background(), "sales_intelligence.revenue_facts", "customer_expansion.contract.activated", time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	rollup(t, tenant)
	if n := tenantScalar(t, tenant, `SELECT count(*)::text FROM sales_intelligence.fact_revenue_events`); n != "4" {
		t.Fatalf("revenue facts after replay: %s", n)
	}
	view = c.must(200, http.MethodGet, "/api/v1/sales-intelligence/revenue", nil)
	if again, _ := seriesValue(t, view, "mrr", "all", "", thisMonth()); again != mrr {
		t.Fatalf("MRR after replay %v, was %v", again, mrr)
	}
}

func TestLateActivationShowsHistory(t *testing.T) {
	c, tenant := crmTenant(t, "Intel Late")
	c.must(200, http.MethodPatch, "/api/v1/tenant", map[string]any{"reporting_currency": "GBP"})
	winDeal(t, c, "Late One", 50000)
	_, lost := newDeal(t, c, "Late Lost", nil)
	c.must(200, http.MethodPost, "/api/v1/crm/deals/"+lost["id"].(string)+"/lose", map[string]any{"row_version": lost["row_version"], "loss_reason": "price"})
	rollup(t, tenant)
	c.wantCode(403, "module_not_entitled", http.MethodGet, "/api/v1/sales-intelligence/dashboard", nil)
	// Facts were recorded although the module was not bought (ADR-014).
	adminClient(t, "super_admin").must(200, http.MethodPost, "/api/v1/admin/tenants/"+tenant+"/entitlements/sales_intelligence/activate", map[string]any{"reason": "late"})
	d := c.must(200, http.MethodGet, "/api/v1/sales-intelligence/dashboard", nil)
	var revenue, wins map[string]any
	for _, x := range d["sections"].([]any) {
		s := x.(map[string]any)
		switch s["key"] {
		case "revenue":
			revenue = s
		case "win_loss":
			wins = s
		}
	}
	if revenue["available"] != true || revenue["cards"].([]any)[0].(map[string]any)["value"].(float64) != 50000 {
		t.Fatalf("revenue after late activation: %v", revenue)
	}
	for _, x := range wins["cards"].([]any) {
		card := x.(map[string]any)
		if card["key"] == "win_rate" && (card["value"].(float64) != 0.5 || card["value_kind"] != "actual") {
			t.Fatalf("win rate card: %v", card)
		}
	}
}

func TestUnavailableSectionsSayWhy(t *testing.T) {
	c, _, tenant := newTenant(t, "Intel Only")
	adminClient(t, "super_admin").must(200, http.MethodPost, "/api/v1/admin/tenants/"+tenant+"/entitlements/sales_intelligence/activate", map[string]any{"reason": "test"})
	d := c.must(200, http.MethodGet, "/api/v1/sales-intelligence/dashboard", nil)
	reasons := map[string]any{}
	for _, x := range d["sections"].([]any) {
		s := x.(map[string]any)
		if s["available"] == false {
			reasons[s["key"].(string)] = s["reason"]
		}
	}
	for _, k := range []string{"revenue", "leads", "campaigns", "funnel", "win_loss"} {
		if reasons[k] != "module_not_active" {
			t.Errorf("%s: %v", k, reasons[k])
		}
	}
	if reasons["costs"] != "no_cost_data" {
		t.Errorf("costs: %v", reasons["costs"])
	}
	c.wantCode(422, "insufficient_history", http.MethodPost, "/api/v1/sales-intelligence/forecasts", map[string]any{"kind": "revenue"})
	defs := c.must(200, http.MethodGet, "/api/v1/sales-intelligence/definitions", nil)
	if len(defs["data"].([]any)) < 30 || defs["attribution_days"].(float64) != 90 {
		t.Fatalf("definitions: %v", defs)
	}
}

func TestTeamViewIsAudited(t *testing.T) {
	c, tenant := growthTenant(t, "Intel Team")
	winDeal(t, c, "Team Deal", 1000)
	rollup(t, tenant)
	view := c.must(200, http.MethodGet, "/api/v1/sales-intelligence/team", nil)
	owner := ""
	for _, x := range view["series"].([]any) {
		s := x.(map[string]any)
		if s["key"] == "deals_won" {
			if s["dimension"] != "owner" {
				t.Fatalf("team view shows %s", s["dimension"])
			}
			owner, _ = s["points"].([]any)[0].(map[string]any)["dimension_value"].(string)
		}
	}
	if owner == "" {
		t.Fatalf("no owner breakdown: %v", view)
	}
	if n := tenantScalar(t, tenant, `SELECT count(*)::text FROM audit.events WHERE action = 'sales_intel.team_viewed' AND tenant_id = $1`, tenant); n != "1" {
		t.Fatalf("team views audited: %s", n)
	}
	// Owner breakdowns are not reachable through general views.
	c.wantCode(422, "validation_failed", http.MethodGet, "/api/v1/sales-intelligence/metrics/deals_won?dimension=owner", nil)
	wl := c.must(200, http.MethodGet, "/api/v1/sales-intelligence/win-loss", nil)
	for _, x := range wl["series"].([]any) {
		if x.(map[string]any)["dimension"] == "owner" {
			t.Fatal("win/loss leaks the owner breakdown")
		}
	}
}

func TestForecasts(t *testing.T) {
	c, tenant := growthTenant(t, "Intel Forecast")
	// Three earlier months of contract history, recorded as facts.
	contract := uuid.New()
	for i, m := range []int{-3, -2, -1} {
		when := time.Now().UTC().AddDate(0, m, 0)
		movement, delta := "expansion", int64(1000)
		if i == 0 {
			movement, delta = "new", 10000
		}
		tenantExec(t, tenant, `INSERT INTO sales_intelligence.fact_revenue_events (tenant_id, source_event_id, movement, contract_ref, mrr_delta_minor, contract_mrr_minor,
			currency, reporting_mrr_delta_minor, reporting_currency, occurred_at) VALUES ($1, $2, $3, $4, $5, $5, 'GBP', $5, 'GBP', $6)`,
			tenant, uuid.New(), movement, contract, delta, when)
	}
	_, open := newDeal(t, c, "Forecast Deal", map[string]any{"expected_mrr_minor": 4000, "contract_value_minor": 48000, "probability": 50,
		"expected_close_date": time.Now().UTC().AddDate(0, 1, 0).Format(time.DateOnly)})
	_ = open
	rollup(t, tenant)

	run := c.must(202, http.MethodPost, "/api/v1/sales-intelligence/forecasts", map[string]any{"kind": "revenue", "horizon_months": 3})
	same := c.must(202, http.MethodPost, "/api/v1/sales-intelligence/forecasts", map[string]any{"kind": "revenue", "horizon_months": 3})
	if run["id"] != same["id"] || run["value_kind"] != "forecast" {
		t.Fatalf("same-day request: %v %v", run["id"], same["id"])
	}
	done := func(id string) func() bool {
		return func() bool {
			st := c.must(200, http.MethodGet, "/api/v1/sales-intelligence/forecasts/"+id, nil)["status"]
			return st == "completed" || st == "failed"
		}
	}
	workUntil(t, jobs.QueueAnalytics, done(run["id"].(string)))
	got := c.must(200, http.MethodGet, "/api/v1/sales-intelligence/forecasts/"+run["id"].(string), nil)
	points := got["points"].([]any)
	if got["status"] != "completed" || len(points) != 3 || got["assumptions"].(map[string]any)["method_version"] != "v1" {
		t.Fatalf("forecast: %v", got)
	}
	for _, x := range points {
		p := x.(map[string]any)
		if p["value_kind"] != "forecast" || p["low"].(float64) > p["expected"].(float64) || p["high"].(float64) < p["expected"].(float64) {
			t.Fatalf("point: %v", p)
		}
	}
	pipe := c.must(202, http.MethodPost, "/api/v1/sales-intelligence/forecasts", map[string]any{"kind": "pipeline", "horizon_months": 2})
	workUntil(t, jobs.QueueAnalytics, done(pipe["id"].(string)))
	pipe = c.must(200, http.MethodGet, "/api/v1/sales-intelligence/forecasts/"+pipe["id"].(string), nil)
	if pipe["status"] != "completed" || pipe["points"].([]any)[1].(map[string]any)["expected"].(float64) != 24000 {
		t.Fatalf("pipeline forecast: %v", pipe)
	}
}

func TestCostsGiveCAC(t *testing.T) {
	c, tenant := growthTenant(t, "Intel Costs")
	winDeal(t, c, "CAC One", 1000)
	winDeal(t, c, "CAC Two", 1000)
	start := time.Now().UTC().Format("2006-01") + "-01"
	end := time.Now().UTC().AddDate(0, 1, -time.Now().UTC().Day()).Format(time.DateOnly)
	cost := c.must(201, http.MethodPost, "/api/v1/sales-intelligence/costs", map[string]any{"period_start": start, "period_end": end, "category": "advertising", "amount_minor": 50000, "currency": "GBP"})
	c.wantCode(422, "validation_failed", http.MethodPost, "/api/v1/sales-intelligence/costs", map[string]any{"period_start": end, "period_end": start, "category": "advertising", "amount_minor": 1, "currency": "GBP"})
	rollup(t, tenant)
	view := c.must(200, http.MethodGet, "/api/v1/sales-intelligence/costs-summary", nil)
	if cac, _ := seriesValue(t, view, "cac", "all", "", thisMonth()); cac != 25000 {
		t.Fatalf("CAC %v: %v", cac, view)
	}
	for _, x := range view["series"].([]any) {
		if s := x.(map[string]any); s["key"] == "cac" && s["value_kind"] != "estimated" {
			t.Fatalf("CAC must be estimated: %v", s)
		}
	}
	c.must(204, http.MethodDelete, "/api/v1/sales-intelligence/costs/"+cost["id"].(string), nil)
	if n := tenantScalar(t, tenant, `SELECT count(*)::text FROM audit.events WHERE action LIKE 'sales_intel.cost_%' AND tenant_id = $1`, tenant); n != "2" {
		t.Fatalf("cost changes audited: %s", n)
	}
}

func TestReportExportIsSafeAndShortLived(t *testing.T) {
	c, tenant := growthTenant(t, "Intel Export")
	campaign := uuid.New()
	tenantExec(t, tenant, `INSERT INTO sales_intelligence.dimension_labels (tenant_id, kind, ref, label) VALUES ($1, 'campaign', $2, '=HYPERLINK("http://x.example")')`, tenant, campaign)
	tenantExec(t, tenant, `INSERT INTO sales_intelligence.fact_outreach_events (tenant_id, source_event_id, event_type, campaign_ref, occurred_at) VALUES ($1, $2, 'outreach.message.sent', $3, now())`, tenant, uuid.New(), campaign)
	if err := graph.SalesIntel.Rollup(context.Background(), uuid.MustParse(tenant)); err != nil {
		t.Fatal(err)
	}
	report := c.must(201, http.MethodPost, "/api/v1/sales-intelligence/reports", map[string]any{"name": "Campaign sends",
		"definition": map[string]any{"metrics": []string{"messages_sent"}, "from": thisMonth(), "to": thisMonth(), "dimension": "campaign"}})
	exp := c.must(202, http.MethodPost, "/api/v1/sales-intelligence/reports/"+report["id"].(string)+"/export", nil)
	workUntil(t, jobs.QueueAnalytics, func() bool {
		st := c.must(200, http.MethodGet, "/api/v1/sales-intelligence/exports/"+exp["id"].(string), nil)["status"]
		return st == "ready" || st == "failed"
	})
	exp = c.must(200, http.MethodGet, "/api/v1/sales-intelligence/exports/"+exp["id"].(string), nil)
	url, _ := exp["download_url"].(string)
	if exp["status"] != "ready" || !strings.HasPrefix(url, "/api/v1/public/files/") || exp["expires_at"] == nil {
		t.Fatalf("export: %v", exp)
	}
	resp, err := http.Get(env.URL + url)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(body), "month,metric_key") || !strings.Contains(string(body), `'=HYPERLINK`) {
		t.Fatalf("download %d: %s", resp.StatusCode, body)
	}
	c.wantCode(422, "validation_failed", http.MethodPost, "/api/v1/sales-intelligence/reports", map[string]any{"name": "People",
		"definition": map[string]any{"metrics": []string{"deals_won"}, "from": thisMonth(), "to": thisMonth(), "dimension": "owner"}})
	if n := tenantScalar(t, tenant, `SELECT count(*)::text FROM audit.events WHERE action = 'sales_intel.report_exported' AND tenant_id = $1`, tenant); n != "1" {
		t.Fatalf("exports audited: %s", n)
	}
}

func TestInsightsCiteOnlyGivenMetrics(t *testing.T) {
	c, tenant := growthTenant(t, "Intel Insights")
	winDeal(t, c, "Insight Deal", 25000)
	rollup(t, tenant)
	c.wantCode(402, "credits_exhausted", http.MethodPost, "/api/v1/sales-intelligence/insights", map[string]any{"scope": "revenue"})
	if err := graph.Commercial.Grant(context.Background(), uuid.MustParse(tenant), "ai_credits", 50, "test-"+uuid.NewString(), nil, "test credits"); err != nil {
		t.Fatal(err)
	}
	in := c.must(201, http.MethodPost, "/api/v1/sales-intelligence/insights", map[string]any{"scope": "revenue"})
	cited := in["cited_metric_ids"].([]any)
	if len(cited) != 1 {
		t.Fatalf("insight: %v", in)
	}
	found := false
	for _, m := range in["metrics"].([]any) {
		mm := m.(map[string]any)
		if mm["id"] == cited[0] {
			found = strings.Contains(in["commentary"].(string), mm["display"].(string))
		}
	}
	if !found {
		t.Fatalf("the commentary does not quote the metric it cites: %v", in)
	}
	if list := c.must(200, http.MethodGet, "/api/v1/sales-intelligence/insights", nil)["data"].([]any); len(list) != 1 {
		t.Fatalf("insights: %v", list)
	}
}

func TestSalesFactsAreIsolated(t *testing.T) {
	a, tenantA := growthTenant(t, "Intel Iso A")
	b, tenantB := growthTenant(t, "Intel Iso B")
	winDeal(t, a, "Iso Revenue", 90000)
	rollup(t, tenantA)
	if v, ok := seriesValue(t, b.must(200, http.MethodGet, "/api/v1/sales-intelligence/revenue", nil), "mrr", "all", "", thisMonth()); ok && v != 0 {
		t.Fatalf("B sees A's MRR: %v", v)
	}
	run := a.must(202, http.MethodPost, "/api/v1/sales-intelligence/forecasts", map[string]any{"kind": "pipeline"})
	b.wantCode(404, "not_found", http.MethodGet, "/api/v1/sales-intelligence/forecasts/"+run["id"].(string), nil)
	if n := tenantScalar(t, tenantB, `SELECT count(*)::text FROM sales_intelligence.fact_revenue_events WHERE tenant_id = $1`, tenantA); n != "0" {
		t.Fatalf("B's database session reads A's facts: %s", n)
	}
}
