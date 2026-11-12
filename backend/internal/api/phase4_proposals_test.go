package api_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"mspengine/internal/kernel/jobs"
	"mspengine/internal/platform/integration"
)

// salesTenant has CRM and Proposals.
func salesTenant(t *testing.T, name string) (*Client, string) {
	t.Helper()
	c, tenant := crmTenant(t, name)
	adminClient(t, "super_admin").must(200, http.MethodPost, "/api/v1/admin/tenants/"+tenant+"/entitlements/proposals/activate", map[string]any{"reason": "phase 4 test"})
	return c, tenant
}

var linkRe = regexp.MustCompile(`/p/([A-Za-z0-9_-]+)`)

func future() string { return time.Now().AddDate(0, 1, 0).Format(time.DateOnly) }

// draftProposal creates a proposal for a deal, with the design's worked example as its only line.
func draftProposal(t *testing.T, c *Client, title string, unit int) (proposal map[string]any, deal map[string]any) {
	t.Helper()
	company, deal := newDeal(t, c, title, nil)
	p := c.must(201, http.MethodPost, "/api/v1/proposals/proposals", map[string]any{"title": title, "crm_company_id": company["id"], "deal_id": deal["id"]})
	c.must(200, http.MethodPatch, "/api/v1/proposals/proposals/"+p["id"].(string)+"/versions/1", map[string]any{
		"sections": []any{map[string]any{"heading": "Overview", "body": "Managed IT for 50 staff."}}, "term_months": 36, "currency": "GBP", "valid_until": future(),
		"lines": []any{map[string]any{"name": "Managed users", "pricing_model": "per_user", "quantity": 50, "unit_price_minor": unit, "setup_fee_minor": 50000, "discount_bps": 1000}},
	})
	return p, deal
}

func issue(c *Client, path string, hash, email string) (int, map[string]any) {
	c.headers = map[string]string{"Idempotency-Key": "issue-" + path}
	defer func() { c.headers = nil }()
	return c.Do(http.MethodPost, path+"/issue", map[string]any{"recipients": []any{map[string]any{"name": "Robin Client", "email": email}}, "content_hash": hash})
}

func publicGet(t *testing.T, path string) (int, map[string]any) {
	t.Helper()
	return newClient(t).Do(http.MethodGet, path, nil)
}

func TestProposalApprovalIssueAndPublicView(t *testing.T) {
	c, tenant := salesTenant(t, "Proposal Tenant")
	p, deal := draftProposal(t, c, "Harbor Managed IT", 2000)
	vpath := "/api/v1/proposals/proposals/" + p["id"].(string) + "/versions/1"
	v := c.must(200, http.MethodGet, vpath, nil)
	if v["mrr_total_minor"] != float64(90000) || v["setup_total_minor"] != float64(50000) || v["tcv_minor"] != float64(3290000) || v["tax_note"] == "" {
		t.Fatalf("totals: %v", v)
	}
	if v["needs_approval"] != true || v["approved"] != false {
		t.Fatalf("a large proposal must need approval: %v", v)
	}
	hash := v["content_hash"].(string)
	email := "robin@" + strconv.FormatInt(time.Now().UnixNano(), 36) + ".example"
	if s, b := issue(c, vpath, hash, email); s != 409 || b["code"] != "approval_required" {
		t.Fatalf("issued without approval: %d %v", s, b)
	}
	c.must(200, http.MethodPost, vpath+"/submit", nil)
	c.wantCode(409, "content_changed", http.MethodPost, vpath+"/approve", map[string]any{"content_hash": strings.Repeat("0", 64)})
	if a := c.must(200, http.MethodPost, vpath+"/approve", map[string]any{"content_hash": hash}); a["status"] != "approved" || a["approved"] != true {
		t.Fatalf("approve: %v", a)
	}
	for range 2 { // Issuing twice returns the issued version.
		if s, b := issue(c, vpath, hash, email); s != 200 || b["status"] != "issued" {
			t.Fatalf("issue: %d %v", s, b)
		}
	}
	work(t, jobs.QueueDefault)
	m := env.Mail.waitFor(t, email, "proposal")
	token := linkRe.FindStringSubmatch(m.Body)[1]
	if got := c.must(200, http.MethodGet, vpath, nil); got["has_pdf"] != true {
		t.Fatalf("no PDF after delivery: %v", got)
	}

	// The customer views it without an account, and sees only the snapshot.
	s, pub := publicGet(t, "/api/v1/public/proposals/"+token)
	if s != 200 || pub["title"] != "Harbor Managed IT" || pub["superseded"] != false || !strings.Contains(asJSON(pub["totals"]), `"tcv_minor":3290000`) {
		t.Fatalf("public view %d: %v", s, pub)
	}
	for _, leak := range []string{"approved", "content_hash", "crm_company_id", "deal_id", tenant} {
		if strings.Contains(asJSON(pub), leak) {
			t.Errorf("the public view exposes %q", leak)
		}
	}
	if s, _, body := newClient(t).raw(http.MethodGet, "/api/v1/public/proposals/"+token+"/pdf", nil); s != 200 || !bytes.HasPrefix(body, []byte("%PDF")) {
		t.Fatalf("public pdf %d", s)
	}
	publicGet(t, "/api/v1/public/proposals/"+token) // Throttled: one view recorded.
	act := c.must(200, http.MethodGet, "/api/v1/proposals/proposals/"+p["id"].(string)+"/activity", nil)
	if len(act["views"].([]any)) != 1 || len(act["links"].([]any)) != 1 {
		t.Fatalf("activity: %v", act)
	}
	if got := c.must(200, http.MethodGet, "/api/v1/proposals/proposals/"+p["id"].(string), nil); got["status"] != "viewed" {
		t.Fatalf("status %v", got["status"])
	}
	dispatch(t)
	if d := c.must(200, http.MethodGet, "/api/v1/crm/deals/"+deal["id"].(string), nil); d["stage_name"] != "Proposal sent" {
		t.Fatalf("CRM did not record the proposal: %v", d["stage_name"])
	}

	// Revoked and unknown links show nothing.
	link := act["links"].([]any)[0].(map[string]any)["id"].(string)
	c.must(204, http.MethodPost, "/api/v1/proposals/share-links/"+link+"/revoke", nil)
	if s, b := publicGet(t, "/api/v1/public/proposals/"+token); s != 404 || b["code"] != "link_unavailable" {
		t.Fatalf("revoked link: %d %v", s, b)
	}
	if s, _ := publicGet(t, "/api/v1/public/proposals/"+strings.Repeat("A", 43)); s != 404 {
		t.Fatalf("unknown token %d", s)
	}
}

func TestIssuedVersionsAreImmutable(t *testing.T) {
	c, tenant := salesTenant(t, "Immutable Tenant")
	item := c.must(201, http.MethodPost, "/api/v1/proposals/catalog", map[string]any{"name": "Backup", "pricing_model": "per_device", "currency": "GBP", "unit_price_minor": 300})
	company, _ := newDeal(t, c, "Frozen Deal", nil)
	p := c.must(201, http.MethodPost, "/api/v1/proposals/proposals", map[string]any{"title": "Small backup", "crm_company_id": company["id"]})
	vpath := "/api/v1/proposals/proposals/" + p["id"].(string) + "/versions/1"
	v := c.must(200, http.MethodPatch, vpath, map[string]any{"sections": []any{}, "term_months": 12, "currency": "GBP", "valid_until": future(),
		"lines": []any{map[string]any{"catalog_item_id": item["id"], "name": "Backup", "pricing_model": "per_device", "quantity": 10, "unit_price_minor": 300}}})
	if v["needs_approval"] != false || v["tcv_minor"] != float64(36000) {
		t.Fatalf("small proposal: %v", v)
	}
	if s, b := issue(c, vpath, v["content_hash"].(string), "buyer@frozen-deal.example"); s != 200 {
		t.Fatalf("issue %d %v", s, b)
	}
	c.wantCode(409, "version_issued", http.MethodPatch, vpath, map[string]any{"sections": []any{}, "term_months": 12, "currency": "GBP", "lines": []any{map[string]any{"name": "Cheap", "pricing_model": "fixed", "quantity": 1, "unit_price_minor": 1}}})

	// Even direct SQL through the application role cannot change it.
	ctx := context.Background()
	conn := appConn(t, "msp_app", "dev_app")
	for _, stmt := range []string{
		`UPDATE proposals.proposal_versions SET mrr_total_minor = 1, tcv_minor = 12 WHERE id = $1`,
		`UPDATE proposals.proposal_versions SET content = '{}' WHERE id = $1`,
		`UPDATE proposals.proposal_line_items SET unit_price_minor = 1 WHERE version_id = $1`,
		`DELETE FROM proposals.proposal_versions WHERE id = $1`,
	} {
		tx, err := conn.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenant); err != nil {
			t.Fatal(err)
		}
		_, err = tx.Exec(ctx, stmt, v["id"])
		_ = tx.Rollback(ctx)
		if err == nil || !strings.Contains(err.Error(), "cannot be") {
			t.Errorf("%s: %v", stmt, err)
		}
	}
	// A later catalog price change leaves the issued version alone.
	c.must(200, http.MethodPatch, "/api/v1/proposals/catalog/"+item["id"].(string), map[string]any{"unit_price_minor": 999})
	if got := c.must(200, http.MethodGet, vpath, nil); got["mrr_total_minor"] != float64(3000) || got["content_hash"] != v["content_hash"] {
		t.Fatalf("issued version changed: %v", got)
	}

	// A correction is a new version, and the old one is marked superseded.
	nv := c.must(201, http.MethodPost, "/api/v1/proposals/proposals/"+p["id"].(string)+"/versions", nil)
	if nv["version"] != float64(2) || nv["status"] != "draft" || len(nv["lines"].([]any)) != 1 {
		t.Fatalf("new version: %v", nv)
	}
	c.wantCode(409, "draft_exists", http.MethodPost, "/api/v1/proposals/proposals/"+p["id"].(string)+"/versions", nil)
	v2 := c.must(200, http.MethodPatch, "/api/v1/proposals/proposals/"+p["id"].(string)+"/versions/2", map[string]any{"sections": []any{}, "term_months": 12, "currency": "GBP", "valid_until": future(),
		"lines": []any{map[string]any{"name": "Backup", "pricing_model": "per_device", "quantity": 12, "unit_price_minor": 300}}})
	issue(c, "/api/v1/proposals/proposals/"+p["id"].(string)+"/versions/2", v2["content_hash"].(string), "buyer@frozen-deal.example")
	if got := c.must(200, http.MethodGet, vpath, nil); got["status"] != "superseded" {
		t.Fatalf("v1 is %v", got["status"])
	}
	c.wantCode(409, "proposal_issued", http.MethodDelete, "/api/v1/proposals/proposals/"+p["id"].(string), nil)
}

func TestAIDraftNeedsApproval(t *testing.T) {
	c, _ := salesTenant(t, "AI Proposal Tenant")
	if err := graph.Commercial.Grant(context.Background(), uuid.UUID(mustUUID(t, c)), "ai_credits", 20, "ai-"+time.Now().String(), nil, "test"); err != nil {
		t.Fatal(err)
	}
	p, _ := draftProposal(t, c, "AI Deal", 10) // Small enough not to need approval by value.
	vpath := "/api/v1/proposals/proposals/" + p["id"].(string) + "/versions/1"
	if v := c.must(200, http.MethodGet, vpath, nil); v["needs_approval"] != false {
		t.Fatalf("%v", v)
	}
	d := c.must(200, http.MethodPost, vpath+"/ai-draft", map[string]any{"section": "Why us", "notes": "Fast response times"})
	if d["ai_generated"] != true || d["body"] == "" {
		t.Fatalf("draft: %v", d)
	}
	v := c.must(200, http.MethodGet, vpath, nil)
	if v["ai_assisted"] != true || v["needs_approval"] != true {
		t.Fatalf("AI-assisted text must need approval: %v", v)
	}
	if s, b := issue(c, vpath, v["content_hash"].(string), "x@ai-deal.example"); s != 409 || b["code"] != "approval_required" {
		t.Fatalf("issued AI text without approval: %d %v", s, b)
	}
}

func mustUUID(t *testing.T, c *Client) [16]byte {
	t.Helper()
	ctx := c.must(200, http.MethodGet, "/api/v1/me/context", nil)
	id := ctx["tenant"].(map[string]any)["id"].(string)
	b, err := hex.DecodeString(strings.ReplaceAll(id, "-", ""))
	if err != nil {
		t.Fatal(err)
	}
	var out [16]byte
	copy(out[:], b)
	return out
}

func TestSignatureWebhookSignsOnce(t *testing.T) {
	c, _ := salesTenant(t, "Sign Tenant")
	p, deal := draftProposal(t, c, "Sign Deal", 10)
	vpath := "/api/v1/proposals/proposals/" + p["id"].(string) + "/versions/1"
	v := c.must(200, http.MethodGet, vpath, nil)
	issue(c, vpath, v["content_hash"].(string), "signer@sign-deal.example")
	c.headers = map[string]string{"Idempotency-Key": "sign-request-1"}
	req := c.must(201, http.MethodPost, vpath+"/signature-requests", map[string]any{"recipients": []any{map[string]any{"name": "Signer", "email": "signer@sign-deal.example"}}})
	again := c.must(201, http.MethodPost, vpath+"/signature-requests", map[string]any{"recipients": []any{map[string]any{"name": "Signer", "email": "signer@sign-deal.example"}}})
	c.headers = nil
	if req["id"] != again["id"] || req["status"] != "sent" {
		t.Fatalf("signature request: %v %v", req, again)
	}
	ep := adminClient(t, "integration_admin").must(201, http.MethodPost, "/api/v1/admin/integrations/webhook-endpoints", map[string]any{"provider_key": "fake_esign"})
	body := []byte(`{"event_id":"evt-1","envelope_id":"` + req["envelope_id"].(string) + `","type":"signed"}`)
	for range 2 {
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		r, _ := http.NewRequest(http.MethodPost, env.URL+"/api/v1/webhooks/esign/"+ep["path_token"].(string), bytes.NewReader(body))
		r.Header.Set("X-Webhook-Timestamp", ts)
		r.Header.Set("X-Webhook-Signature", hex.EncodeToString(integration.SignHMAC(ep["secret"].(string), ts, body)))
		r.Header.Set("X-Webhook-Id", "evt-1")
		resp, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("webhook %d", resp.StatusCode)
		}
	}
	work(t, jobs.QueueCritical)
	if got := c.must(200, http.MethodGet, "/api/v1/proposals/proposals/"+p["id"].(string), nil); got["status"] != "signed" {
		t.Fatalf("status %v", got["status"])
	}
	if n := platformCount(t, `SELECT count(*) FROM jobs.outbox_events WHERE event_type = 'proposals.proposal.signed' AND aggregate_id = $1`, p["id"]); n != 1 {
		t.Fatalf("%d signed events", n)
	}
	dispatch(t)
	d := c.must(200, http.MethodGet, "/api/v1/crm/deals/"+deal["id"].(string), nil)
	if d["status"] != "open" || d["stage_name"] != "Negotiation" {
		t.Fatalf("deal after signing: %v", d)
	}
}

func TestProposalsAreIsolated(t *testing.T) {
	a, _ := salesTenant(t, "Prop Alpha")
	b, _ := salesTenant(t, "Prop Beta")
	p, _ := draftProposal(t, a, "Alpha Offer", 10)
	item := a.must(201, http.MethodPost, "/api/v1/proposals/catalog", map[string]any{"name": "Alpha secret service", "pricing_model": "fixed", "currency": "GBP", "unit_price_minor": 100})
	var bLog []string
	b.log = &bLog
	b.wantCode(404, "not_found", http.MethodGet, "/api/v1/proposals/proposals/"+p["id"].(string), nil)
	b.wantCode(404, "not_found", http.MethodGet, "/api/v1/proposals/proposals/"+p["id"].(string)+"/versions/1", nil)
	b.wantCode(404, "not_found", http.MethodPatch, "/api/v1/proposals/catalog/"+item["id"].(string), map[string]any{"unit_price_minor": 1})
	b.wantCode(422, "validation_failed", http.MethodPost, "/api/v1/proposals/proposals", map[string]any{"title": "Steal", "crm_company_id": p["crm_company_id"]})
	for _, path := range []string{"/proposals", "/catalog", "/templates", "/text-templates"} {
		b.must(200, http.MethodGet, "/api/v1/proposals"+path, nil)
	}
	for _, line := range bLog {
		if strings.Contains(line, p["id"].(string)) || strings.Contains(line, "Alpha secret service") || strings.Contains(line, "Alpha Offer") {
			t.Errorf("tenant B saw tenant A's proposal data: %s", line)
		}
	}
}
