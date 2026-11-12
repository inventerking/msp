package api_test

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/db"
	"mspengine/internal/kernel/events"
	"mspengine/internal/kernel/jobs"
)

// crmTenant is a workspace with CRM active.
func crmTenant(t *testing.T, name string) (*Client, string) {
	t.Helper()
	c, _, tenant := newTenant(t, name)
	adminClient(t, "super_admin").must(200, http.MethodPost, "/api/v1/admin/tenants/"+tenant+"/entitlements/crm/activate", map[string]any{"reason": "phase 4 test"})
	return c, tenant
}

func platformCount(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := appConn(t, "msp_platform", "dev_platform").QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func stageByKey(t *testing.T, c *Client, key string) (pipeline, stage string) {
	t.Helper()
	p := c.must(200, http.MethodGet, "/api/v1/crm/pipelines", nil)["data"].([]any)[0].(map[string]any)
	for _, s := range p["stages"].([]any) {
		if s.(map[string]any)["key"] == key {
			return p["id"].(string), s.(map[string]any)["id"].(string)
		}
	}
	t.Fatalf("no stage %s", key)
	return "", ""
}

func newDeal(t *testing.T, c *Client, title string, body map[string]any) (company, deal map[string]any) {
	t.Helper()
	company = c.must(201, http.MethodPost, "/api/v1/crm/companies", map[string]any{"name": title + " Ltd", "domain": strings.ToLower(strings.ReplaceAll(title, " ", "-")) + ".example"})
	in := map[string]any{"crm_company_id": company["id"], "title": title, "currency": "GBP"}
	for k, v := range body {
		in[k] = v
	}
	deal = c.must(201, http.MethodPost, "/api/v1/crm/deals", in)
	return company, deal
}

func TestDealLifecycle(t *testing.T) {
	c, _ := crmTenant(t, "Deal Tenant")
	pipeline, qualified := stageByKey(t, c, "qualified")
	_, won := stageByKey(t, c, "won")
	company, deal := newDeal(t, c, "Harbor Support", nil)
	id := deal["id"].(string)
	if deal["stage_name"] != "New lead" || deal["row_version"] != float64(1) || deal["probability"] != float64(5) {
		t.Fatalf("deal: %v", deal)
	}
	c.wantCode(422, "validation_failed", http.MethodPost, "/api/v1/crm/deals", map[string]any{"crm_company_id": company["id"], "title": "Bad", "currency": "gbp"})

	// Moving needs the current version. A stale one is a conflict, not a silent overwrite.
	moved := c.must(200, http.MethodPost, "/api/v1/crm/deals/"+id+"/move", map[string]any{"stage_id": qualified, "row_version": 1})
	c.wantCode(409, "stale_version", http.MethodPost, "/api/v1/crm/deals/"+id+"/move", map[string]any{"stage_id": qualified, "row_version": 1})
	c.wantCode(422, "validation_failed", http.MethodPost, "/api/v1/crm/deals/"+id+"/move", map[string]any{"stage_id": won, "row_version": moved["row_version"]})

	// Winning needs a value.
	c.wantCode(422, "validation_failed", http.MethodPost, "/api/v1/crm/deals/"+id+"/win", map[string]any{"row_version": moved["row_version"]})
	patched := c.must(200, http.MethodPatch, "/api/v1/crm/deals/"+id, map[string]any{"row_version": moved["row_version"], "expected_mrr_minor": 450000, "expected_close_date": "2026-12-31"})
	if patched["expected_close_date"] != "2026-12-31" {
		t.Fatalf("patch: %v", patched)
	}
	for range 2 { // A retried win changes nothing and publishes once.
		w := c.must(200, http.MethodPost, "/api/v1/crm/deals/"+id+"/win", map[string]any{"row_version": patched["row_version"]})
		if w["status"] != "won" || w["probability"] != float64(100) {
			t.Fatalf("win: %v", w)
		}
	}
	if n := platformCount(t, `SELECT count(*) FROM jobs.outbox_events WHERE event_type = 'crm.deal.won' AND aggregate_id = $1`, id); n != 1 {
		t.Fatalf("%d deal.won events", n)
	}
	if got := c.must(200, http.MethodGet, "/api/v1/crm/companies/"+company["id"].(string), nil); got["lifecycle"] != "customer" {
		t.Fatalf("winning did not make a customer: %v", got)
	}
	c.wantCode(409, "deal_closed", http.MethodPost, "/api/v1/crm/deals/"+id+"/move", map[string]any{"stage_id": qualified, "row_version": 99})

	// Reopening returns it to the last open stage, with history kept.
	re := c.must(200, http.MethodPost, "/api/v1/crm/deals/"+id+"/reopen", nil)
	if re["status"] != "open" || re["stage_id"] != qualified {
		t.Fatalf("reopen: %v", re)
	}
	c.wantCode(422, "validation_failed", http.MethodPost, "/api/v1/crm/deals/"+id+"/lose", map[string]any{"row_version": re["row_version"], "loss_reason": ""})
	lost := c.must(200, http.MethodPost, "/api/v1/crm/deals/"+id+"/lose", map[string]any{"row_version": re["row_version"], "loss_reason": "Chose a cheaper provider"})
	if lost["status"] != "lost" || lost["loss_reason"] != "Chose a cheaper provider" {
		t.Fatalf("lose: %v", lost)
	}
	detail := c.must(200, http.MethodGet, "/api/v1/crm/deals/"+id, nil)
	if len(detail["history"].([]any)) != 5 {
		t.Fatalf("history: %v", detail["history"])
	}

	board := c.must(200, http.MethodGet, "/api/v1/crm/pipelines/"+pipeline+"/board", nil)["stages"].([]any)
	if len(board) != 9 || !strings.Contains(asJSON(board), `"status":"lost"`) {
		t.Fatalf("board: %v", asJSON(board))
	}
}

func TestBoardTotalsStayPerCurrency(t *testing.T) {
	c, _ := crmTenant(t, "Currency Tenant")
	pipeline, _ := stageByKey(t, c, "new_lead")
	newDeal(t, c, "Pound Deal", map[string]any{"expected_mrr_minor": 1000})
	newDeal(t, c, "Dollar Deal", map[string]any{"currency": "USD", "expected_mrr_minor": 2000})
	first := c.must(200, http.MethodGet, "/api/v1/crm/pipelines/"+pipeline+"/board", nil)["stages"].([]any)[0].(map[string]any)
	got := map[string]float64{}
	for _, x := range first["totals"].([]any) {
		m := x.(map[string]any)
		got[m["currency"].(string)] = m["mrr_minor"].(float64)
	}
	if len(got) != 2 || got["GBP"] != 1000 || got["USD"] != 2000 {
		t.Fatalf("totals mixed currencies: %v", first["totals"])
	}
}

func TestConcurrentMovesGiveOneConflict(t *testing.T) {
	c, _ := crmTenant(t, "Race Tenant")
	_, a := stageByKey(t, c, "qualified")
	_, b := stageByKey(t, c, "contacted")
	_, deal := newDeal(t, c, "Race Deal", nil)
	var wg sync.WaitGroup
	codes := make([]int, 2)
	for i, stage := range []string{a, b} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i], _ = c.Do(http.MethodPost, "/api/v1/crm/deals/"+deal["id"].(string)+"/move", map[string]any{"stage_id": stage, "row_version": 1})
		}()
	}
	wg.Wait()
	if (codes[0] != 200 || codes[1] != 409) && (codes[0] != 409 || codes[1] != 200) {
		t.Fatalf("codes %v", codes)
	}
}

func TestPipelineConfiguration(t *testing.T) {
	c, _ := crmTenant(t, "Pipeline Tenant")
	p := c.must(200, http.MethodGet, "/api/v1/crm/pipelines", nil)["data"].([]any)[0].(map[string]any)
	stages := p["stages"].([]any)
	newDeal(t, c, "Used Stage", nil) // Puts a deal in the first stage.
	var keep []map[string]any
	for _, s := range stages {
		m := s.(map[string]any)
		keep = append(keep, map[string]any{"id": m["id"], "key": m["key"], "name": m["name"], "kind": m["kind"], "default_probability": m["default_probability"]})
	}
	keep[0]["name"] = "Fresh lead"
	got := c.must(200, http.MethodPut, "/api/v1/crm/pipelines/"+p["id"].(string)+"/stages", map[string]any{"stages": keep})
	if !strings.Contains(asJSON(got), "Fresh lead") {
		t.Fatalf("rename: %v", got)
	}
	c.wantCode(409, "stage_in_use", http.MethodPut, "/api/v1/crm/pipelines/"+p["id"].(string)+"/stages", map[string]any{"stages": keep[1:]})
	c.wantCode(422, "validation_failed", http.MethodPut, "/api/v1/crm/pipelines/"+p["id"].(string)+"/stages", map[string]any{"stages": append(keep[:7:7], keep[7])}) // No lost stage.
	c.wantCode(409, "default_pipeline", http.MethodPatch, "/api/v1/crm/pipelines/"+p["id"].(string), map[string]any{"status": "archived"})
}

func TestTasksNotesMeetingsAndReminders(t *testing.T) {
	c, tenant := crmTenant(t, "Work Tenant")
	company, deal := newDeal(t, c, "Busy Deal", nil)
	person := c.must(201, http.MethodPost, "/api/v1/crm/contacts", map[string]any{"full_name": "Jo Buyer", "email": "jo@busy-deal.example", "crm_company_id": company["id"], "is_primary": true})
	if person["full_name"] != "Jo Buyer" || person["is_primary"] != true {
		t.Fatalf("contact: %v", person)
	}
	task := c.must(201, http.MethodPost, "/api/v1/crm/tasks", map[string]any{"title": "Send pricing", "deal_id": deal["id"], "remind_at": time.Now().Add(-time.Minute).Format(time.RFC3339)})
	for range 2 {
		if err := graph.CRM.HandleReminders(context.Background(), jobs.Job{}); err != nil {
			t.Fatal(err)
		}
	}
	notes := c.must(200, http.MethodGet, "/api/v1/notifications", nil)
	if notes["unread"] != float64(1) || !strings.Contains(asJSON(notes), "Send pricing") {
		t.Fatalf("reminder: %v", notes)
	}
	c.must(200, http.MethodPost, "/api/v1/crm/tasks/"+task["id"].(string)+"/complete", nil)
	c.must(201, http.MethodPost, "/api/v1/crm/notes", map[string]any{"subject_type": "deal", "subject_id": deal["id"], "body": "<script>alert(1)</script> Budget approved"})
	m := c.must(201, http.MethodPost, "/api/v1/crm/meetings", map[string]any{"title": "Kick-off", "deal_id": deal["id"], "contact_ids": []any{person["id"]},
		"starts_at": time.Now().Add(24 * time.Hour).Format(time.RFC3339), "ends_at": time.Now().Add(25 * time.Hour).Format(time.RFC3339)})
	if len(m["attendees"].([]any)) != 2 || m["crm_company_id"] != company["id"] {
		t.Fatalf("meeting: %v", m)
	}
	c.must(204, http.MethodPost, "/api/v1/crm/activities", map[string]any{"activity_type": "call", "subject_type": "contact", "subject_id": person["id"], "summary": "Discussed timing"})
	timeline := asJSON(c.must(200, http.MethodGet, "/api/v1/crm/activities?crm_company_id="+company["id"].(string), nil))
	for _, want := range []string{"Completed task: Send pricing", "Added a note", "Meeting: Kick-off", "Discussed timing"} {
		if !strings.Contains(timeline, want) {
			t.Errorf("timeline lacks %q: %s", want, timeline)
		}
	}
	notesList := asJSON(c.must(200, http.MethodGet, "/api/v1/crm/notes?subject_type=deal&subject_id="+deal["id"].(string), nil))
	if !strings.Contains(notesList, "Budget approved") {
		t.Fatalf("notes: %s", notesList)
	}
	_ = tenant
}

// TestOtherModulesMoveDealsForwardOnly sends the events Proposals and Assessments publish, and
// checks CRM records each once and never moves a deal backward.
func TestOtherModulesMoveDealsForwardOnly(t *testing.T) {
	c, tenant := crmTenant(t, "Timeline Tenant")
	company, deal := newDeal(t, c, "Event Deal", nil)
	publish := func(typ string, payload map[string]any) {
		err := db.TenantTx(context.Background(), graph.Pools.App, uuid.MustParse(tenant), uuid.Nil, func(tx pgx.Tx) error {
			tid := uuid.MustParse(tenant)
			return events.Append(context.Background(), tx, events.Event{Type: typ, TenantID: &tid, SourceModule: strings.Split(typ, ".")[0], AggregateType: "test", AggregateID: uuid.New()}, payload)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	publish("proposals.version.issued", map[string]any{"crm_company_id": company["id"], "deal_id": deal["id"], "title": "Managed services"})
	dispatch(t)
	dispatch(t) // A second pass must not add anything.
	d := c.must(200, http.MethodGet, "/api/v1/crm/deals/"+deal["id"].(string), nil)
	if d["stage_name"] != "Proposal sent" {
		t.Fatalf("stage %v", d["stage_name"])
	}
	publish("it_assessment.assessment.completed", map[string]any{"crm_company_id": company["id"], "score": 72})
	dispatch(t)
	if d := c.must(200, http.MethodGet, "/api/v1/crm/deals/"+deal["id"].(string), nil); d["stage_name"] != "Proposal sent" {
		t.Fatalf("a late assessment moved the deal back to %v", d["stage_name"])
	}
	publish("proposals.proposal.signed", map[string]any{"crm_company_id": company["id"], "deal_id": deal["id"]})
	dispatch(t)
	d = c.must(200, http.MethodGet, "/api/v1/crm/deals/"+deal["id"].(string), nil)
	if d["status"] != "open" || d["stage_name"] != "Negotiation" {
		t.Fatalf("signing must suggest, not win: %v", d)
	}
	timeline := asJSON(c.must(200, http.MethodGet, "/api/v1/crm/activities?deal_id="+deal["id"].(string), nil))
	if strings.Count(timeline, "Proposal sent: Managed services") != 1 || !strings.Contains(timeline, "Proposal signed") {
		t.Fatalf("timeline: %s", timeline)
	}
	if !strings.Contains(asJSON(c.must(200, http.MethodGet, "/api/v1/crm/activities?crm_company_id="+company["id"].(string), nil)), "IT assessment completed (score 72)") {
		t.Fatal("assessment activity missing")
	}
}

func TestCRMRecordsAreIsolated(t *testing.T) {
	a, _ := crmTenant(t, "CRM Alpha")
	b, _ := crmTenant(t, "CRM Beta")
	company, deal := newDeal(t, a, "Secret Deal", map[string]any{"expected_mrr_minor": 999})
	note := a.must(201, http.MethodPost, "/api/v1/crm/notes", map[string]any{"subject_type": "deal", "subject_id": deal["id"], "body": "Alpha's private note"})
	var bLog []string
	b.log = &bLog
	for _, path := range []string{"/companies/" + company["id"].(string), "/deals/" + deal["id"].(string), "/notes?subject_type=deal&subject_id=" + deal["id"].(string)} {
		b.wantCode(404, "not_found", http.MethodGet, "/api/v1/crm"+path, nil)
	}
	b.wantCode(404, "not_found", http.MethodPatch, "/api/v1/crm/notes/"+note["id"].(string), map[string]any{"body": "x"})
	b.wantCode(422, "validation_failed", http.MethodPost, "/api/v1/crm/deals", map[string]any{"crm_company_id": company["id"], "title": "Steal", "currency": "GBP"})
	b.wantCode(422, "validation_failed", http.MethodPost, "/api/v1/crm/notes", map[string]any{"subject_type": "deal", "subject_id": deal["id"], "body": "x"})
	for _, path := range []string{"/companies", "/deals", "/tasks", "/activities", "/meetings", "/contacts"} {
		b.must(200, http.MethodGet, "/api/v1/crm"+path, nil)
	}
	for _, line := range bLog {
		for _, marker := range []string{company["id"].(string), deal["id"].(string), "Secret Deal", "Alpha's private note"} {
			if strings.Contains(line, marker) {
				t.Errorf("tenant B saw %q in %s", marker, line)
			}
		}
	}
}

// The registry works without CRM (D-02): a prospect converted before CRM is bought is there once
// CRM is activated.
func TestRegistryWorksBeforeCRMIsActive(t *testing.T) {
	c, tenant := leadTenant(t, "Registry Tenant", 10)
	p := c.must(201, http.MethodPost, "/api/v1/lead-generation/prospects", map[string]any{"domain": "granite-schools-trust.example", "display_name": "Granite Schools"})
	conv := c.must(200, http.MethodPost, "/api/v1/lead-generation/prospects/"+p["id"].(string)+"/convert", nil)
	c.wantCode(403, "module_not_entitled", http.MethodGet, "/api/v1/crm/companies", nil)
	adminClient(t, "super_admin").must(200, http.MethodPost, "/api/v1/admin/tenants/"+tenant+"/entitlements/crm/activate", map[string]any{"reason": "later purchase"})
	got := c.must(200, http.MethodGet, "/api/v1/crm/companies/"+conv["crm_company_id"].(string), nil)
	if got["name"] != "Granite Schools" || got["source_module"] != "lead_generation" {
		t.Fatalf("company: %v", got)
	}
	if !strings.Contains(asJSON(c.must(200, http.MethodGet, "/api/v1/crm/activities?crm_company_id="+got["id"].(string), nil)), "Converted from a Lead Generation prospect") {
		t.Fatal("conversion not on the timeline")
	}
}

func TestErasureKeepsDealTotals(t *testing.T) {
	c, _ := crmTenant(t, "Erase Tenant")
	company, deal := newDeal(t, c, "Erase Deal", map[string]any{"expected_mrr_minor": 5000})
	person := c.must(201, http.MethodPost, "/api/v1/crm/contacts", map[string]any{"full_name": "Pat Private", "email": "pat@erase-deal.example", "crm_company_id": company["id"]})
	c.must(200, http.MethodPatch, "/api/v1/crm/deals/"+deal["id"].(string), map[string]any{"row_version": 1, "primary_contact_id": person["id"]})
	c.must(202, http.MethodPost, "/api/v1/privacy/contacts/"+person["contact_id"].(string)+"/erase", nil)
	work(t, "default")
	c.wantCode(404, "not_found", http.MethodGet, "/api/v1/crm/contacts/"+person["id"].(string), nil)
	d := c.must(200, http.MethodGet, "/api/v1/crm/deals/"+deal["id"].(string), nil)
	if d["primary_contact_id"] != nil || d["expected_mrr_minor"] != float64(5000) {
		t.Fatalf("deal after erasure: %v", d)
	}
}

// D-03: a won deal records a contract even without Customer Expansion, and reopening ends it.
func TestWonDealRecordsContract(t *testing.T) {
	c, tenant := crmTenant(t, "Contract Tenant")
	_, deal := newDeal(t, c, "Contract Deal", map[string]any{"expected_mrr_minor": 120000})
	id := deal["id"].(string)
	won := c.must(200, http.MethodPost, "/api/v1/crm/deals/"+id+"/win", map[string]any{"row_version": 1})
	dispatch(t)
	dispatch(t)
	status := func() string {
		return tenantScalar(t, tenant, `SELECT status || ':' || mrr_minor || ':' || currency FROM customer_expansion.contracts WHERE deal_id = $1`, id)
	}
	if got := status(); got != "active:120000:GBP" {
		t.Fatalf("contract %s", got)
	}
	c.must(200, http.MethodPost, "/api/v1/crm/deals/"+id+"/reopen", nil)
	dispatch(t)
	if got := status(); !strings.HasPrefix(got, "cancelled") {
		t.Fatalf("after reopening: %s", got)
	}
	_ = won
}
