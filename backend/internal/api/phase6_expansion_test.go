package api_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"mspengine/internal/kernel/events"
	"mspengine/internal/kernel/jobs"
)

// growthTenant is a workspace with CRM, Customer Expansion and Sales Intelligence.
func growthTenant(t *testing.T, name string, modules ...string) (*Client, string) {
	t.Helper()
	c, tenant := crmTenant(t, name)
	super := adminClient(t, "super_admin")
	if len(modules) == 0 {
		modules = []string{"customer_expansion", "sales_intelligence"}
	}
	for _, m := range modules {
		super.must(200, http.MethodPost, "/api/v1/admin/tenants/"+tenant+"/entitlements/"+m+"/activate", map[string]any{"reason": "phase 6 test"})
	}
	// Report in the currency the tests' deals use, so amounts need no exchange rate.
	c.must(200, http.MethodPatch, "/api/v1/tenant", map[string]any{"reporting_currency": "GBP"})
	return c, tenant
}

// winDeal opens a deal with an MRR, wins it and lets the consumers run.
func winDeal(t *testing.T, c *Client, title string, mrr int) (company, deal string) {
	t.Helper()
	co, d := newDeal(t, c, title, map[string]any{"expected_mrr_minor": mrr})
	c.must(200, http.MethodPost, "/api/v1/crm/deals/"+d["id"].(string)+"/win", map[string]any{"row_version": d["row_version"]})
	dispatch(t)
	dispatch(t)
	return co["id"].(string), d["id"].(string)
}

func customerFor(t *testing.T, c *Client, crmCompany string) map[string]any {
	t.Helper()
	for _, x := range c.must(200, http.MethodGet, "/api/v1/customer-expansion/customers", nil)["data"].([]any) {
		cu := x.(map[string]any)
		if cu["crm_company"].(map[string]any)["id"] == crmCompany {
			return cu
		}
	}
	t.Fatalf("no customer for %s", crmCompany)
	return nil
}

// appendEvent writes an event as if another module had, under the tenant's RLS context.
func appendEvent(t *testing.T, tenant string, e events.Event, payload any) {
	t.Helper()
	ctx := context.Background()
	tx, err := appConn(t, "msp_app", "dev_app").Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenant); err != nil {
		t.Fatal(err)
	}
	id := uuid.MustParse(tenant)
	e.TenantID = &id
	if err := events.Append(ctx, tx, e, payload); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestWonDealGivesCustomerAndContract(t *testing.T) {
	c, tenant := growthTenant(t, "Expansion Won")
	company, deal := winDeal(t, c, "Won Customer", 120000)
	cu := customerFor(t, c, company)
	if cu["active_mrr_minor"].(float64) != 120000 || cu["currency"] != "GBP" || cu["crm_company"].(map[string]any)["lifecycle"] != "customer" {
		t.Fatalf("customer: %v", cu)
	}
	contracts := c.must(200, http.MethodGet, "/api/v1/customer-expansion/customers/"+cu["id"].(string)+"/contracts", nil)["data"].([]any)
	if len(contracts) != 1 || contracts[0].(map[string]any)["source"] != "won_deal" || contracts[0].(map[string]any)["deal_id"] != deal {
		t.Fatalf("contracts: %v", contracts)
	}
	// The activation was published with its movement, once.
	if got := platformScalar(t, `SELECT count(*)::text || ':' || min(payload->>'movement') FROM jobs.outbox_events WHERE event_type = 'customer_expansion.contract.activated' AND tenant_id = $1`, tenant); got != "1:new" {
		t.Fatalf("activation events: %s", got)
	}

	// Replaying the won deal records nothing new.
	_, bus := graph.Worker("replay-test")
	if _, err := bus.Replay(context.Background(), "customer_expansion.contract_recorder", "crm.deal.won", time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	dispatch(t)
	if n := tenantScalar(t, tenant, `SELECT count(*)::text FROM customer_expansion.contracts`); n != "1" {
		t.Fatalf("contracts after replay: %s", n)
	}
	if got := platformScalar(t, `SELECT count(*)::text FROM jobs.outbox_events WHERE event_type = 'customer_expansion.contract.activated' AND tenant_id = $1`, tenant); got != "1" {
		t.Fatalf("activation events after replay: %s", got)
	}
}

func TestSignedProposalWinsOverDealAndFlagsMismatch(t *testing.T) {
	c, tenant := growthTenant(t, "Expansion Mismatch")
	co, d := newDeal(t, c, "Mismatch Co", map[string]any{"expected_mrr_minor": 100000})
	deal := d["id"].(string)
	appendEvent(t, tenant, events.Event{Type: "proposals.proposal.signed", SourceModule: "proposals", AggregateType: "proposal", AggregateID: uuid.New()},
		map[string]any{"proposal_id": uuid.New(), "version_id": uuid.New(), "crm_company_id": co["id"], "deal_id": deal, "currency": "GBP",
			"mrr_total_minor": 150000, "setup_total_minor": 50000, "term_months": 12})
	dispatch(t)
	if got := tenantScalar(t, tenant, `SELECT status || ':' || mrr_minor FROM customer_expansion.contracts WHERE deal_id = $1`, deal); got != "draft:150000" {
		t.Fatalf("before the win: %s", got)
	}
	d = c.must(200, http.MethodGet, "/api/v1/crm/deals/"+deal, nil) // Signing moved the deal on.
	c.must(200, http.MethodPost, "/api/v1/crm/deals/"+deal+"/win", map[string]any{"row_version": d["row_version"]})
	dispatch(t)
	dispatch(t)
	got := tenantScalar(t, tenant, `SELECT status || ':' || mrr_minor || ':' || setup_minor || ':' || review_reason || ':' || (end_date IS NOT NULL) FROM customer_expansion.contracts WHERE deal_id = $1`, deal)
	if got != "active:150000:50000:proposal_deal_mismatch:true" {
		t.Fatalf("after the win: %s", got)
	}
	// A person marks the difference as reviewed.
	id := tenantScalar(t, tenant, `SELECT id::text FROM customer_expansion.contracts WHERE deal_id = $1`, deal)
	c.must(204, http.MethodPatch, "/api/v1/customer-expansion/contracts/"+id, map[string]any{"clear_review": true})
	if got := tenantScalar(t, tenant, `SELECT COALESCE(review_reason, 'none') FROM customer_expansion.contracts WHERE id = $1`, id); got != "none" {
		t.Fatalf("review: %s", got)
	}
}

func TestContractChangesPublishMovements(t *testing.T) {
	c, tenant := growthTenant(t, "Expansion Moves")
	company, _ := winDeal(t, c, "Moves Co", 100000)
	cu := customerFor(t, c, company)
	profile := cu["id"].(string)
	second := c.must(201, http.MethodPost, "/api/v1/customer-expansion/customers/"+profile+"/contracts", map[string]any{
		"start_date": time.Now().Format(time.DateOnly), "currency": "GBP", "mrr_minor": 20000, "term_months": 12, "notice_days": 30})["id"].(string)
	c.must(204, http.MethodPatch, "/api/v1/customer-expansion/contracts/"+second, map[string]any{"mrr_minor": 30000})
	c.must(204, http.MethodPatch, "/api/v1/customer-expansion/contracts/"+second, map[string]any{"status": "ended"})
	c.wantCode(409, "invalid_transition", http.MethodPatch, "/api/v1/customer-expansion/contracts/"+second, map[string]any{"status": "active"})
	c.wantCode(422, "validation_failed", http.MethodPost, "/api/v1/customer-expansion/customers/"+profile+"/contracts", map[string]any{
		"start_date": "2026-05-01", "end_date": "2026-04-01", "currency": "GBP", "mrr_minor": 1})
	got := platformScalar(t, `SELECT string_agg(payload->>'movement' || ':' || (payload->>'mrr_delta_minor'), ',' ORDER BY occurred_at)
		FROM jobs.outbox_events WHERE tenant_id = $1 AND event_type LIKE 'customer_expansion.contract.%'`, tenant)
	if got != "new:100000,expansion:20000,expansion:10000,contraction:-30000" {
		t.Fatalf("movements: %s", got)
	}
	// A term with no end date stores the end it implies, so renewals can find it.
	if got := tenantScalar(t, tenant, `SELECT (end_date = start_date + interval '12 months' - interval '1 day')::text FROM customer_expansion.contracts WHERE id = $1`, second); got != "true" {
		t.Fatalf("end from term: %s", got)
	}
	if n := tenantScalar(t, tenant, `SELECT count(*)::text FROM audit.events WHERE action = 'expansion.contract_changed' AND tenant_id = $1`, tenant); n != "2" {
		t.Fatalf("contract changes audited: %s", n)
	}
}

func service(t *testing.T, c *Client, name, category string) string {
	t.Helper()
	return c.must(201, http.MethodPost, "/api/v1/tenant/services", map[string]any{"name": name, "category_key": category})["id"].(string)
}

func TestCoverageAnalysisNeverCallsUnknownMissing(t *testing.T) {
	c, tenant := growthTenant(t, "Expansion Analysis")
	backup := service(t, c, "Managed backup", "backup_dr")
	mail := service(t, c, "Email security", "email_security")
	voice := service(t, c, "Hosted voice", "voip")
	c.wantCode(409, "service_exists", http.MethodPost, "/api/v1/tenant/services", map[string]any{"name": "Managed backup"})
	_ = mail

	company, _ := winDeal(t, c, "Analysis Co", 50000)
	profile := customerFor(t, c, company)["id"].(string)
	// A peer customer pays for backup, which gives the estimate.
	peerCo, _ := winDeal(t, c, "Peer Co", 80000)
	peer := customerFor(t, c, peerCo)["id"].(string)
	c.must(200, http.MethodPut, "/api/v1/customer-expansion/customers/"+peer+"/services", map[string]any{"items": []any{
		map[string]any{"tenant_service_id": backup, "coverage": "active", "mrr_minor": 9000, "currency": "GBP"}}})

	lines := c.must(200, http.MethodPut, "/api/v1/customer-expansion/customers/"+profile+"/services", map[string]any{"items": []any{
		map[string]any{"tenant_service_id": backup, "coverage": "absent"},
		map[string]any{"tenant_service_id": voice, "coverage": "declined"},
	}})["data"].([]any)
	if len(lines) != 3 {
		t.Fatalf("coverage lines: %v", lines)
	}
	c.wantCode(422, "validation_failed", http.MethodPut, "/api/v1/customer-expansion/customers/"+profile+"/services", map[string]any{"items": []any{
		map[string]any{"tenant_service_id": uuid.NewString(), "coverage": "active"}}})

	a := c.must(200, http.MethodPost, "/api/v1/customer-expansion/customers/"+profile+"/analyze", nil)
	if a["created"].(float64) != 2 || a["health_status"] == "" {
		t.Fatalf("analysis: %v", a)
	}
	recs := c.must(200, http.MethodGet, "/api/v1/customer-expansion/recommendations?customer_id="+profile, nil)["data"].([]any)
	kinds := map[string]map[string]any{}
	for _, x := range recs {
		r := x.(map[string]any)
		kinds[r["kind"].(string)] = r
		if r["rationale"] == "" || len(r["evidence"].([]any)) == 0 {
			t.Errorf("recommendation without rationale or evidence: %v", r)
		}
	}
	cross, confirm := kinds["cross_sell"], kinds["confirm_coverage"]
	if cross == nil || cross["service"].(map[string]any)["id"] != backup || cross["estimated_mrr_minor"].(float64) != 9000 || cross["evidence_class"] != "observed" {
		t.Fatalf("cross-sell: %v", cross)
	}
	if confirm == nil || confirm["service"].(map[string]any)["id"] != mail || confirm["estimated_mrr_minor"] != nil ||
		strings.Contains(confirm["rationale"].(string), "does not have") || !strings.HasPrefix(confirm["rationale"].(string), "Confirm whether") {
		t.Fatalf("unknown coverage must be a question: %v", confirm)
	}
	if _, ok := kinds["upsell"]; ok {
		t.Error("declined services must not be suggested")
	}

	// Running again refreshes the same suggestions instead of adding more.
	again := c.must(200, http.MethodPost, "/api/v1/customer-expansion/customers/"+profile+"/analyze", nil)
	if again["created"].(float64) != 0 || again["refreshed"].(float64) != 2 {
		t.Fatalf("second run: %v", again)
	}
	// Without AI credits the wording job ends quietly and the rule text stays.
	work(t, jobs.QueueAI)
	if r := c.must(200, http.MethodGet, "/api/v1/customer-expansion/recommendations/"+cross["id"].(string), nil); r["ai_worded"] != false {
		t.Fatalf("worded without credits: %v", r)
	}
	_ = tenant
}

func TestAIWordingKeepsTheRuleFacts(t *testing.T) {
	c, tenant := growthTenant(t, "Expansion Wording")
	if err := graph.Commercial.Grant(context.Background(), uuid.MustParse(tenant), "ai_credits", 100, "test-"+uuid.NewString(), nil, "test credits"); err != nil {
		t.Fatal(err)
	}
	backup := service(t, c, "Cloud backup", "backup_dr")
	company, _ := winDeal(t, c, "Wording Co", 40000)
	profile := customerFor(t, c, company)["id"].(string)
	c.must(200, http.MethodPut, "/api/v1/customer-expansion/customers/"+profile+"/services", map[string]any{"items": []any{map[string]any{"tenant_service_id": backup, "coverage": "absent"}}})
	c.must(200, http.MethodPost, "/api/v1/customer-expansion/customers/"+profile+"/analyze", nil)
	work(t, jobs.QueueAI)
	r := c.must(200, http.MethodGet, "/api/v1/customer-expansion/recommendations?status=open&customer_id="+profile, nil)["data"].([]any)[0].(map[string]any)
	if r["ai_worded"] != true || !strings.HasPrefix(r["rationale"].(string), "Suggested: Wording Co Ltd does not have Cloud backup") {
		t.Fatalf("wording: %v", r)
	}
}

func TestAcceptOpensOneDealAndTracksRealisedMRR(t *testing.T) {
	c, tenant := growthTenant(t, "Expansion Accept")
	backup := service(t, c, "Offsite backup", "backup_dr")
	company, _ := winDeal(t, c, "Accept Co", 30000)
	profile := customerFor(t, c, company)["id"].(string)
	c.must(200, http.MethodPut, "/api/v1/customer-expansion/customers/"+profile+"/services", map[string]any{"items": []any{map[string]any{"tenant_service_id": backup, "coverage": "absent"}}})
	c.must(200, http.MethodPost, "/api/v1/customer-expansion/customers/"+profile+"/analyze", nil)
	rec := c.must(200, http.MethodGet, "/api/v1/customer-expansion/recommendations?customer_id="+profile, nil)["data"].([]any)[0].(map[string]any)["id"].(string)

	accept := func(key string) map[string]any {
		status, body := c.DoWith(http.MethodPost, "/api/v1/customer-expansion/recommendations/"+rec+"/accept", map[string]any{"create_deal": true}, map[string]string{"Idempotency-Key": key})
		if status != 200 {
			t.Fatalf("accept: %d %v", status, body)
		}
		return body
	}
	first, second := accept("accept-key-1"), accept("accept-key-2")
	if first["deal_id"] == nil || first["deal_id"] != second["deal_id"] || first["opportunity_id"] != second["opportunity_id"] {
		t.Fatalf("accept twice: %v then %v", first, second)
	}
	if n := tenantScalar(t, tenant, `SELECT count(*)::text FROM crm.deals WHERE source_module = 'customer_expansion'`); n != "1" {
		t.Fatalf("deals opened: %s", n)
	}
	c.wantCode(409, "recommendation_closed", http.MethodPost, "/api/v1/customer-expansion/recommendations/"+rec+"/dismiss", nil)

	// Winning the deal realises the opportunity, at the deal's MRR.
	deal := first["deal_id"].(string)
	d := c.must(200, http.MethodGet, "/api/v1/crm/deals/"+deal, nil)
	c.must(200, http.MethodPatch, "/api/v1/crm/deals/"+deal, map[string]any{"row_version": d["row_version"], "expected_mrr_minor": 7000})
	d = c.must(200, http.MethodGet, "/api/v1/crm/deals/"+deal, nil)
	c.must(200, http.MethodPost, "/api/v1/crm/deals/"+deal+"/win", map[string]any{"row_version": d["row_version"]})
	dispatch(t)
	dispatch(t)
	opps := c.must(200, http.MethodGet, "/api/v1/customer-expansion/opportunities?status=won", nil)["data"].([]any)
	if len(opps) != 1 || opps[0].(map[string]any)["realised_mrr_minor"].(float64) != 7000 {
		t.Fatalf("won opportunities: %v", opps)
	}
	if r := c.must(200, http.MethodGet, "/api/v1/customer-expansion/recommendations/"+rec, nil); r["status"] != "converted" {
		t.Fatalf("recommendation: %v", r["status"])
	}
	summary := c.must(200, http.MethodGet, "/api/v1/customer-expansion/opportunities/summary", nil)["data"].([]any)
	if len(summary) != 1 || summary[0].(map[string]any)["realised_mrr_minor"].(float64) != 7000 {
		t.Fatalf("summary: %v", summary)
	}
	// The won expansion deal is a second contract: the customer's MRR includes it.
	if cu := customerFor(t, c, company); cu["active_mrr_minor"].(float64) != 37000 {
		t.Fatalf("customer MRR: %v; contracts %s", cu, tenantScalar(t, tenant, `SELECT string_agg(status || ':' || mrr_minor || ':' || currency, ',') FROM customer_expansion.contracts`))
	}
}

func TestExpansionWorksWithoutCRM(t *testing.T) {
	c, _, tenant := newTenant(t, "Expansion Only")
	adminClient(t, "super_admin").must(200, http.MethodPost, "/api/v1/admin/tenants/"+tenant+"/entitlements/customer_expansion/activate", map[string]any{"reason": "phase 6 test"})
	svc := service(t, c, "Monitoring", "managed_it")
	cu := c.must(201, http.MethodPost, "/api/v1/customer-expansion/customers", map[string]any{"name": "Registry Only Ltd", "domain": "registry-only.example"})
	if cu["crm_company"].(map[string]any)["name"] != "Registry Only Ltd" {
		t.Fatalf("customer: %v", cu)
	}
	profile := cu["id"].(string)
	c.must(200, http.MethodPut, "/api/v1/customer-expansion/customers/"+profile+"/services", map[string]any{"items": []any{map[string]any{"tenant_service_id": svc, "coverage": "absent"}}})
	c.must(200, http.MethodPost, "/api/v1/customer-expansion/customers/"+profile+"/analyze", nil)
	rec := c.must(200, http.MethodGet, "/api/v1/customer-expansion/recommendations", nil)["data"].([]any)[0].(map[string]any)["id"].(string)
	status, body := c.DoWith(http.MethodPost, "/api/v1/customer-expansion/recommendations/"+rec+"/accept", map[string]any{"create_deal": true}, map[string]string{"Idempotency-Key": "no-crm-accept"})
	if status != 200 || body["deal_skipped"] != "crm_not_active" || body["deal_id"] != nil {
		t.Fatalf("accept without CRM: %d %v", status, body)
	}
}

func TestRenewalAlertsIncludeNoticePeriods(t *testing.T) {
	c, tenant := growthTenant(t, "Expansion Renewals")
	company, _ := winDeal(t, c, "Renewal Co", 10000)
	profile := customerFor(t, c, company)["id"].(string)
	end := time.Now().UTC().AddDate(0, 0, 25)
	contract := c.must(201, http.MethodPost, "/api/v1/customer-expansion/customers/"+profile+"/contracts", map[string]any{
		"start_date": time.Now().UTC().AddDate(-1, 0, 0).Format(time.DateOnly), "end_date": end.Format(time.DateOnly), "notice_days": 10,
		"currency": "GBP", "mrr_minor": 5000})["id"].(string)
	scan := func() {
		if err := graph.Expansion.HandleRenewalScan(context.Background(), jobs.Job{}); err != nil {
			t.Fatal(err)
		}
	}
	scan()
	scan()
	alerts := c.must(200, http.MethodGet, "/api/v1/customer-expansion/renewals", nil)["data"].([]any)
	kinds := map[string]float64{}
	for _, x := range alerts {
		a := x.(map[string]any)
		if a["contract_id"] == contract {
			kinds[a["kind"].(string)] = a["window_days"].(float64)
		}
	}
	if len(kinds) != 2 || kinds["contract_end"] != 30 || kinds["notice_deadline"] != 30 {
		t.Fatalf("alerts: %v", alerts)
	}
	if n := platformScalar(t, `SELECT count(*)::text FROM jobs.outbox_events WHERE tenant_id = $1 AND event_type = 'customer_expansion.renewal.due'`, tenant); n != "2" {
		t.Fatalf("renewal events: %s", n)
	}
	// The contract's owner, who created it, is told once per alert.
	if n := tenantScalar(t, tenant, `SELECT count(*)::text FROM notification.notifications WHERE tenant_id = $1 AND notification_key = 'expansion.renewal_due'`, tenant); n != "2" {
		t.Fatalf("notifications: %s", n)
	}
	id := alerts[0].(map[string]any)["id"].(string)
	c.must(204, http.MethodPost, "/api/v1/customer-expansion/renewal-alerts/"+id+"/acknowledge", nil)
	c.must(204, http.MethodPost, "/api/v1/customer-expansion/renewal-alerts/"+id+"/acknowledge", nil)
	// Changing the end date resolves the old alerts.
	c.must(204, http.MethodPatch, "/api/v1/customer-expansion/contracts/"+contract, map[string]any{"end_date": end.AddDate(1, 0, 0).Format(time.DateOnly)})
	open := c.must(200, http.MethodGet, "/api/v1/customer-expansion/renewals?status=open", nil)["data"].([]any)
	for _, x := range open {
		if x.(map[string]any)["contract_id"] == contract {
			t.Fatalf("a moved contract kept its alert: %v", x)
		}
	}
}

func TestExpansionDataIsIsolated(t *testing.T) {
	a, _ := growthTenant(t, "Expansion Iso A")
	b, _ := growthTenant(t, "Expansion Iso B")
	company, _ := winDeal(t, a, "Iso Co", 1000)
	profile := customerFor(t, a, company)["id"].(string)
	b.wantCode(404, "not_found", http.MethodGet, "/api/v1/customer-expansion/customers/"+profile, nil)
	b.wantCode(404, "not_found", http.MethodGet, "/api/v1/customer-expansion/customers/"+profile+"/contracts", nil)
	b.wantCode(404, "not_found", http.MethodPost, "/api/v1/customer-expansion/customers/"+profile+"/analyze", nil)
	b.wantCode(404, "not_found", http.MethodPatch, "/api/v1/customer-expansion/customers/"+profile, map[string]any{"health_status": "at_risk"})
	if list := b.must(200, http.MethodGet, "/api/v1/customer-expansion/customers", nil)["data"].([]any); len(list) != 0 {
		t.Fatalf("B sees customers: %v", list)
	}
	// B cannot cover A's customer with A's services, nor its own.
	svc := service(t, b, "B service", "managed_it")
	b.wantCode(404, "not_found", http.MethodPut, "/api/v1/customer-expansion/customers/"+profile+"/services", map[string]any{"items": []any{map[string]any{"tenant_service_id": svc, "coverage": "active"}}})
}

func TestHealthOverrideIsKeptUntilCleared(t *testing.T) {
	c, tenant := growthTenant(t, "Expansion Health")
	company, _ := winDeal(t, c, "Health Co", 1000)
	profile := customerFor(t, c, company)["id"].(string)
	cu := c.must(200, http.MethodPatch, "/api/v1/customer-expansion/customers/"+profile, map[string]any{"health_status": "at_risk"})
	if cu["health_status"] != "at_risk" || cu["health_source"] != "manual" {
		t.Fatalf("override: %v", cu)
	}
	c.must(200, http.MethodPost, "/api/v1/customer-expansion/customers/"+profile+"/analyze", nil)
	if cu := c.must(200, http.MethodGet, "/api/v1/customer-expansion/customers/"+profile, nil); cu["health_status"] != "at_risk" {
		t.Fatalf("analysis overwrote a manual status: %v", cu["health_status"])
	}
	cu = c.must(200, http.MethodPatch, "/api/v1/customer-expansion/customers/"+profile, map[string]any{"health_status": nil})
	if cu["health_source"] != "computed" {
		t.Fatalf("clear: %v", cu)
	}
	if n := tenantScalar(t, tenant, `SELECT count(*)::text FROM audit.events WHERE action = 'expansion.health_overridden' AND tenant_id = $1`, tenant); n != "2" {
		t.Fatalf("overrides audited: %s", n)
	}
}

// DoWith sends one request with extra headers, such as an Idempotency-Key.
func (c *Client) DoWith(method, path string, body any, headers map[string]string) (int, map[string]any) {
	c.t.Helper()
	saved := c.headers
	merged := map[string]string{}
	for k, v := range saved {
		merged[k] = v
	}
	for k, v := range headers {
		merged[k] = v
	}
	c.headers = merged
	defer func() { c.headers = saved }()
	return c.Do(method, path, body)
}

// platformScalar reads one value as the platform role, which may read across tenants.
func platformScalar(t *testing.T, query string, args ...any) string {
	t.Helper()
	var out string
	if err := appConn(t, "msp_platform", "dev_platform").QueryRow(context.Background(), query, args...).Scan(&out); err != nil {
		t.Fatal(err)
	}
	return out
}
