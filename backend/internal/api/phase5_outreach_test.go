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

	"mspengine/internal/kernel/jobs"
	"mspengine/internal/platform/integration"
)

// outreachTenant is a workspace with CRM and Outreach, and the jurisdiction rules the tests use:
// GB allows opt-out email, DE needs consent, and FR has no rule, so it is blocked.
func outreachTenant(t *testing.T, name string) (*Client, string) {
	t.Helper()
	c, tenant := crmTenant(t, name)
	super := adminClient(t, "super_admin")
	super.must(200, http.MethodPost, "/api/v1/admin/tenants/"+tenant+"/entitlements/outreach/activate", map[string]any{"reason": "phase 5 test"})
	super.must(204, http.MethodPut, "/api/v1/admin/outreach/jurisdiction-rules/gb", map[string]any{"rule": "opt_out_allowed", "note": "test"})
	super.must(204, http.MethodPut, "/api/v1/admin/outreach/jurisdiction-rules/de", map[string]any{"rule": "consent_required"})
	if _, err := env.Owner.Exec(context.Background(), `DELETE FROM outreach.jurisdiction_rules WHERE country_code = 'FR'`); err != nil {
		t.Fatal(err)
	}
	return c, tenant
}

// tenantExec arranges test state as the application role, under a tenant's RLS context.
func tenantExec(t *testing.T, tenant, query string, args ...any) {
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
	tag, err := tx.Exec(ctx, query, args...)
	if err != nil {
		t.Fatal(err)
	}
	if tag.RowsAffected() != 1 {
		t.Fatalf("%s: %d rows", query, tag.RowsAffected())
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

// person adds a CRM contact with a country, and returns its shared contact ID.
func person(t *testing.T, c *Client, company, name, email, country string) string {
	t.Helper()
	body := map[string]any{"full_name": name, "crm_company_id": company}
	if email != "" {
		body["email"] = email
	}
	if country != "" {
		body["country_code"] = strings.ToLower(country)
	}
	p := c.must(201, http.MethodPost, "/api/v1/crm/contacts", body)
	if country != "" && p["country_code"] != country {
		t.Fatalf("country not stored: %v", p)
	}
	return p["contact_id"].(string)
}

type campaignSetup struct {
	id, sender, hash string
}

// newCampaign builds an approved-ready campaign: an authenticated .example domain, a sender, a
// two-step sequence with no delay, an always-open window, and an audience.
func newCampaign(t *testing.T, c *Client, slug string, contacts []string, limit int, window map[string]any) campaignSetup {
	t.Helper()
	dom := c.must(201, http.MethodPost, "/api/v1/outreach/domains", map[string]any{"domain": slug + ".example"})
	if dom["authenticated"] != true {
		t.Fatalf("domain: %v", dom)
	}
	snd := c.must(201, http.MethodPost, "/api/v1/outreach/senders", map[string]any{"sending_domain_id": dom["id"], "from_email": "sam@" + slug + ".example", "from_name": "Sam Sender", "daily_limit": limit})
	c.must(201, http.MethodPost, "/api/v1/outreach/templates", map[string]any{"name": slug + " first", "subject": "Hello {{first_name}}", "body": "Hi {{first_name}},\nA note from {{sender_name}}."})
	c.must(201, http.MethodPost, "/api/v1/outreach/templates", map[string]any{"name": slug + " second", "subject": "Following up", "body": "Just checking in."})
	versions := make([]string, 2)
	for _, tpl := range c.must(200, http.MethodGet, "/api/v1/outreach/templates", nil)["data"].([]any) {
		m := tpl.(map[string]any)
		switch m["name"] {
		case slug + " first":
			versions[0] = m["current_version_id"].(string)
		case slug + " second":
			versions[1] = m["current_version_id"].(string)
		}
	}
	aud := c.must(201, http.MethodPost, "/api/v1/outreach/audiences", map[string]any{"name": slug})
	c.must(200, http.MethodPost, "/api/v1/outreach/audiences/"+aud["id"].(string)+"/members", map[string]any{"contact_ids": contacts})
	if window == nil {
		window = map[string]any{"days": []int{0, 1, 2, 3, 4, 5, 6}, "start": "00:00", "end": "23:59"}
	}
	camp := c.must(201, http.MethodPost, "/api/v1/outreach/campaigns", map[string]any{"name": slug, "sender_account_id": snd["id"], "audience_id": aud["id"],
		"timezone": "UTC", "sending_window": window, "purpose": "Introduce managed IT support", "postal_address": "1 Test Street, Testville, TE1 1ST"})
	id := camp["id"].(string)
	c.must(204, http.MethodPut, "/api/v1/outreach/campaigns/"+id+"/steps", map[string]any{"steps": []any{
		map[string]any{"template_version_id": versions[0], "delay_minutes": 0},
		map[string]any{"template_version_id": versions[1], "delay_minutes": 0},
	}})
	return campaignSetup{id: id, sender: snd["id"].(string)}
}

func approveAndStart(t *testing.T, c *Client, cs *campaignSetup) {
	t.Helper()
	sub := c.must(200, http.MethodPost, "/api/v1/outreach/campaigns/"+cs.id+"/submit", nil)
	cs.hash = sub["content_hash"].(string)
	c.must(204, http.MethodPost, "/api/v1/outreach/campaigns/"+cs.id+"/approve", map[string]any{"content_hash": cs.hash})
	c.must(200, http.MethodPost, "/api/v1/outreach/campaigns/"+cs.id+"/start", nil)
}

// sendRound runs one dispatch tick and every send job it queued.
func sendRound(t *testing.T) {
	t.Helper()
	if err := graph.Outreach.HandleDispatch(context.Background(), jobs.Job{}); err != nil {
		t.Fatal(err)
	}
	work(t, jobs.QueueEmailSend)
}

func mailsTo(to string) int {
	env.Mail.mu.Lock()
	defer env.Mail.mu.Unlock()
	n := 0
	for _, m := range env.Mail.sent {
		if m.To == to {
			n++
		}
	}
	return n
}

func enrollmentReasons(t *testing.T, c *Client, campaign string) map[string]int {
	t.Helper()
	out := map[string]int{}
	for _, e := range c.must(200, http.MethodGet, "/api/v1/outreach/campaigns/"+campaign, nil)["enrollments"].([]any) {
		m := e.(map[string]any)
		out[m["status"].(string)+":"+m["stop_reason"].(string)] += int(m["count"].(float64))
	}
	return out
}

var unsubRe = regexp.MustCompile(`http://app\.test/u/([A-Za-z0-9_-]+)`)

func TestOutreachSendsOnlyWhenEveryGatePasses(t *testing.T) {
	c, tenant := outreachTenant(t, "Gates Tenant")
	co := c.must(201, http.MethodPost, "/api/v1/crm/companies", map[string]any{"name": "Gates Buyer", "domain": "gates-buyer.example"})["id"].(string)
	gb := person(t, c, co, "Alex Allowed", "alex@gates-buyer.example", "GB")
	deNo := person(t, c, co, "Dana Noconsent", "dana@gates-buyer.example", "DE")
	deYes := person(t, c, co, "Dirk Consented", "dirk@gates-buyer.example", "DE")
	fr := person(t, c, co, "Fran Norule", "fran@gates-buyer.example", "FR")
	noEmail := person(t, c, co, "Nemo Noemail", "", "GB")
	sup := person(t, c, co, "Sue Suppressed", "sue@gates-buyer.example", "GB")
	tenantExec(t, tenant, `INSERT INTO shared.contact_consents (tenant_id, contact_id, purpose, status, evidence, captured_at) VALUES ($1, $2, 'marketing_email', 'granted', '{"source":"test"}', now())`, tenant, deYes)
	c.must(204, http.MethodPost, "/api/v1/outreach/suppressions", map[string]any{"email": "SUE@gates-buyer.example"})
	cs := newCampaign(t, c, "gates-sender", []string{gb, deNo, deYes, fr, noEmail, sup}, 50, nil)

	pf := c.must(200, http.MethodGet, "/api/v1/outreach/campaigns/"+cs.id+"/preflight", nil)
	aud := pf["audience"].(map[string]any)
	if len(pf["blockers"].([]any)) != 0 || aud["sendable"] != 2.0 || aud["blocked"] != 2.0 || aud["suppressed"] != 1.0 || aud["no_email"] != 1.0 {
		t.Fatalf("preflight: %v", pf)
	}
	// Nothing can start before approval, and approval must confirm what was reviewed.
	c.wantCode(409, "not_approved", http.MethodPost, "/api/v1/outreach/campaigns/"+cs.id+"/start", nil)
	sub := c.must(200, http.MethodPost, "/api/v1/outreach/campaigns/"+cs.id+"/submit", nil)
	c.wantCode(409, "content_changed", http.MethodPost, "/api/v1/outreach/campaigns/"+cs.id+"/approve", map[string]any{"content_hash": strings.Repeat("0", 64)})
	c.must(204, http.MethodPost, "/api/v1/outreach/campaigns/"+cs.id+"/approve", map[string]any{"content_hash": sub["content_hash"]})
	if n := c.must(200, http.MethodPost, "/api/v1/outreach/campaigns/"+cs.id+"/start", nil)["count"]; n != 6.0 {
		t.Fatalf("enrolled %v", n)
	}

	sendRound(t)
	m := env.Mail.waitFor(t, "alex@gates-buyer.example", "Hello Alex")
	if !strings.Contains(m.Body, "Hi Alex,") || !strings.Contains(m.Body, "Sam Sender") || !strings.Contains(m.Body, "1 Test Street") || !unsubRe.MatchString(m.Body) {
		t.Fatalf("body: %q", m.Body)
	}
	if !strings.HasPrefix(m.Headers["List-Unsubscribe"], "<http://app.test/api/v1/public/outreach/unsubscribe/") || m.Headers["List-Unsubscribe-Post"] != "List-Unsubscribe=One-Click" {
		t.Fatalf("headers: %v", m.Headers)
	}
	if !strings.Contains(m.From, "sam@gates-sender.example") {
		t.Fatalf("from %q", m.From)
	}
	env.Mail.waitFor(t, "dirk@gates-buyer.example", "Hello Dirk")
	for _, blocked := range []string{"dana@gates-buyer.example", "fran@gates-buyer.example", "sue@gates-buyer.example"} {
		if mailsTo(blocked) != 0 {
			t.Fatalf("sent to %s", blocked)
		}
	}
	reasons := enrollmentReasons(t, c, cs.id)
	if reasons["stopped:jurisdiction_block"] != 2 || reasons["stopped:suppressed"] != 1 || reasons["stopped:missing_email"] != 1 || reasons["active:"] != 2 {
		t.Fatalf("reasons: %v", reasons)
	}

	// The second step goes out on the next tick, then the sequence completes. More ticks send nothing.
	sendRound(t)
	env.Mail.waitFor(t, "alex@gates-buyer.example", "Following up")
	sendRound(t)
	if mailsTo("alex@gates-buyer.example") != 2 || mailsTo("dirk@gates-buyer.example") != 2 {
		t.Fatal("each person should get exactly two emails")
	}
	if r := enrollmentReasons(t, c, cs.id); r["completed:"] != 2 {
		t.Fatalf("after sequence: %v", r)
	}
	totals := c.must(200, http.MethodGet, "/api/v1/outreach/campaigns/"+cs.id, nil)["totals"].(map[string]any)
	if totals["sent"] != 4.0 {
		t.Fatalf("totals %v", totals)
	}
	// Sends appear on the CRM timeline of the contact's company.
	dispatch(t)
	tl := c.must(200, http.MethodGet, "/api/v1/crm/activities?crm_company_id="+co, nil)
	if !strings.Contains(asJSON(tl), "Outreach email sent") {
		t.Fatalf("timeline: %s", asJSON(tl))
	}
}

func TestEditingAnApprovedCampaignNeedsNewApproval(t *testing.T) {
	c, _ := outreachTenant(t, "Edit Tenant")
	co := c.must(201, http.MethodPost, "/api/v1/crm/companies", map[string]any{"name": "Edit Buyer", "domain": "edit-buyer.example"})["id"].(string)
	p := person(t, c, co, "Eve Edit", "eve@edit-buyer.example", "GB")
	cs := newCampaign(t, c, "edit-sender", []string{p}, 50, nil)
	sub := c.must(200, http.MethodPost, "/api/v1/outreach/campaigns/"+cs.id+"/submit", nil)
	c.must(204, http.MethodPost, "/api/v1/outreach/campaigns/"+cs.id+"/approve", map[string]any{"content_hash": sub["content_hash"]})
	c.must(204, http.MethodPatch, "/api/v1/outreach/campaigns/"+cs.id, map[string]any{"postal_address": "2 Other Road, Elsewhere"})
	got := c.must(200, http.MethodGet, "/api/v1/outreach/campaigns/"+cs.id, nil)
	if got["status"] != "draft" || got["approved"] != false {
		t.Fatalf("after edit: %v %v", got["status"], got["approved"])
	}
	c.wantCode(409, "not_approved", http.MethodPost, "/api/v1/outreach/campaigns/"+cs.id+"/start", nil)
	// The old hash no longer approves anything.
	c.must(200, http.MethodPost, "/api/v1/outreach/campaigns/"+cs.id+"/submit", nil)
	c.wantCode(409, "content_changed", http.MethodPost, "/api/v1/outreach/campaigns/"+cs.id+"/approve", map[string]any{"content_hash": sub["content_hash"]})
}

func TestUnsubscribeStopsTheSequenceAtOnce(t *testing.T) {
	c, _ := outreachTenant(t, "Unsub Tenant")
	co := c.must(201, http.MethodPost, "/api/v1/crm/companies", map[string]any{"name": "Unsub Buyer", "domain": "unsub-buyer.example"})["id"].(string)
	p := person(t, c, co, "Uma Unsub", "uma@unsub-buyer.example", "GB")
	cs := newCampaign(t, c, "unsub-sender", []string{p}, 50, nil)
	approveAndStart(t, c, &cs)
	sendRound(t)
	m := env.Mail.waitFor(t, "uma@unsub-buyer.example", "Hello Uma")
	token := unsubRe.FindStringSubmatch(m.Body)[1]

	// Looking at the page, as a link scanner would, changes nothing.
	st, body := publicGet(t, "/api/v1/public/outreach/unsubscribe/"+token)
	if st != 200 || body["done"] != false || body["sender"] != "Sam Sender" {
		t.Fatalf("status %d %v", st, body)
	}
	if st, _ := publicGet(t, "/api/v1/public/outreach/unsubscribe/"+token[:len(token)-2]+"AA"); st != 404 {
		t.Fatalf("tampered token: %d", st)
	}
	// One-click unsubscribe as a mail client sends it: a form post with no session or origin.
	for range 2 {
		resp, err := http.Post(env.URL+"/api/v1/public/outreach/unsubscribe/"+token, "application/x-www-form-urlencoded", strings.NewReader("List-Unsubscribe=One-Click"))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 204 {
			t.Fatalf("unsubscribe %d", resp.StatusCode)
		}
	}
	if _, body := publicGet(t, "/api/v1/public/outreach/unsubscribe/"+token); body["done"] != true {
		t.Fatalf("after: %v", body)
	}
	sendRound(t)
	if mailsTo("uma@unsub-buyer.example") != 1 {
		t.Fatal("a second email went out after unsubscribing")
	}
	if r := enrollmentReasons(t, c, cs.id); r["stopped:unsubscribe"] != 1 {
		t.Fatalf("reasons %v", r)
	}
	// The opt-out holds for every later campaign of the tenant.
	cs2 := newCampaign(t, c, "unsub-sender-two", []string{p}, 50, nil)
	if aud := c.must(200, http.MethodGet, "/api/v1/outreach/campaigns/"+cs2.id+"/preflight", nil)["audience"].(map[string]any); aud["suppressed"] != 1.0 {
		t.Fatalf("second campaign: %v", aud)
	}
	// Opt-outs cannot be removed by the tenant.
	sups := c.must(200, http.MethodGet, "/api/v1/outreach/suppressions", nil)["data"].([]any)
	c.wantCode(409, "not_removable", http.MethodDelete, "/api/v1/outreach/suppressions/"+sups[0].(map[string]any)["id"].(string), nil)
	if strings.Contains(asJSON(sups), "uma@") {
		t.Fatal("suppression list shows addresses")
	}
}

func TestPausedCampaignsSendNothing(t *testing.T) {
	c, _ := outreachTenant(t, "Pause Tenant")
	co := c.must(201, http.MethodPost, "/api/v1/crm/companies", map[string]any{"name": "Pause Buyer", "domain": "pause-buyer.example"})["id"].(string)
	p := person(t, c, co, "Pat Pause", "pat@pause-buyer.example", "GB")
	cs := newCampaign(t, c, "pause-sender", []string{p}, 50, nil)
	approveAndStart(t, c, &cs)
	c.must(204, http.MethodPost, "/api/v1/outreach/campaigns/"+cs.id+"/pause", nil)
	sendRound(t)
	if mailsTo("pat@pause-buyer.example") != 0 {
		t.Fatal("sent while paused")
	}
	c.must(204, http.MethodPost, "/api/v1/outreach/campaigns/"+cs.id+"/resume", nil)
	sendRound(t)
	env.Mail.waitFor(t, "pat@pause-buyer.example", "Hello Pat")
	c.must(204, http.MethodPost, "/api/v1/outreach/campaigns/"+cs.id+"/cancel", nil)
	sendRound(t)
	if mailsTo("pat@pause-buyer.example") != 1 {
		t.Fatal("sent after cancelling")
	}
}

func TestDailyLimitAndSendingWindowHoldMessages(t *testing.T) {
	c, _ := outreachTenant(t, "Limit Tenant")
	co := c.must(201, http.MethodPost, "/api/v1/crm/companies", map[string]any{"name": "Limit Buyer", "domain": "limit-buyer.example"})["id"].(string)
	a := person(t, c, co, "Lee One", "lee1@limit-buyer.example", "GB")
	b := person(t, c, co, "Lee Two", "lee2@limit-buyer.example", "GB")
	cs := newCampaign(t, c, "limit-sender", []string{a, b}, 1, nil)
	approveAndStart(t, c, &cs)
	sendRound(t)
	sendRound(t)
	if n := mailsTo("lee1@limit-buyer.example") + mailsTo("lee2@limit-buyer.example"); n != 1 {
		t.Fatalf("%d emails with a daily limit of one", n)
	}

	// A window that excludes today holds everything.
	other := (int(time.Now().UTC().Weekday()) + 3) % 7
	w := person(t, c, co, "Wyn Window", "wyn@limit-buyer.example", "GB")
	cw := newCampaign(t, c, "window-sender", []string{w}, 50, map[string]any{"days": []int{other}, "start": "09:00", "end": "17:00"})
	approveAndStart(t, c, &cw)
	sendRound(t)
	if mailsTo("wyn@limit-buyer.example") != 0 {
		t.Fatal("sent outside the window")
	}
	if r := enrollmentReasons(t, c, cw.id); r["active:"] != 1 {
		t.Fatalf("window enrollment %v", r)
	}
}

func postEmailEvent(t *testing.T, ep map[string]any, body string) {
	t.Helper()
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	r, _ := http.NewRequest(http.MethodPost, env.URL+"/api/v1/webhooks/email/"+ep["path_token"].(string), bytes.NewReader([]byte(body)))
	r.Header.Set("X-Webhook-Timestamp", ts)
	r.Header.Set("X-Webhook-Signature", hex.EncodeToString(integration.SignHMAC(ep["secret"].(string), ts, []byte(body))))
	r.Header.Set("X-Webhook-Id", strconv.FormatInt(time.Now().UnixNano(), 10))
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("webhook %d", resp.StatusCode)
	}
}

func TestProviderEventsStopSendingAndSuppress(t *testing.T) {
	c, _ := outreachTenant(t, "Events Tenant")
	co := c.must(201, http.MethodPost, "/api/v1/crm/companies", map[string]any{"name": "Events Buyer", "domain": "events-buyer.example"})["id"].(string)
	replier := person(t, c, co, "Rae Reply", "rae@events-buyer.example", "GB")
	bouncer := person(t, c, co, "Bo Bounce", "bo@events-buyer.example", "GB")
	complainer := person(t, c, co, "Cal Complain", "cal@events-buyer.example", "GB")
	cs := newCampaign(t, c, "events-sender", []string{replier, bouncer, complainer}, 50, nil)
	approveAndStart(t, c, &cs)
	sendRound(t)
	msgs := map[string]string{}
	for _, m := range c.must(200, http.MethodGet, "/api/v1/outreach/messages?campaign_id="+cs.id, nil)["data"].([]any) {
		mm := m.(map[string]any)
		msgs[mm["contact_id"].(string)] = "sink-" + mm["id"].(string)
	}
	ep := adminClient(t, "integration_admin").must(201, http.MethodPost, "/api/v1/admin/integrations/webhook-endpoints", map[string]any{"provider_key": "sink_email"})
	before := adminClient(t, "super_admin").must(200, http.MethodGet, "/api/v1/admin/outreach/global-suppressions", nil)["count"].(float64)
	postEmailEvent(t, ep, `{"event_id":"r-`+cs.id+`","type":"reply","message_id":"`+msgs[replier]+`","snippet":"Yes, tell me more"}`)
	postEmailEvent(t, ep, `{"event_id":"r-`+cs.id+`","type":"reply","message_id":"`+msgs[replier]+`","snippet":"Yes, tell me more"}`)
	postEmailEvent(t, ep, `{"event_id":"b-`+cs.id+`","type":"hard_bounce","message_id":"`+msgs[bouncer]+`"}`)
	postEmailEvent(t, ep, `{"event_id":"c-`+cs.id+`","type":"complaint","message_id":"`+msgs[complainer]+`"}`)
	work(t, jobs.QueueCritical)

	sendRound(t)
	for _, to := range []string{"rae@events-buyer.example", "bo@events-buyer.example", "cal@events-buyer.example"} {
		if mailsTo(to) != 1 {
			t.Fatalf("%s got a second email after a provider event", to)
		}
	}
	r := enrollmentReasons(t, c, cs.id)
	if r["stopped:reply"] != 1 || r["stopped:hard_bounce"] != 1 || r["stopped:complaint"] != 1 {
		t.Fatalf("reasons %v", r)
	}
	replies := c.must(200, http.MethodGet, "/api/v1/outreach/replies", nil)["data"].([]any)
	if len(replies) != 1 || replies[0].(map[string]any)["snippet"] != "Yes, tell me more" {
		t.Fatalf("replies %v", replies)
	}
	if after := adminClient(t, "super_admin").must(200, http.MethodGet, "/api/v1/admin/outreach/global-suppressions", nil)["count"].(float64); after != before+1 {
		t.Fatalf("global suppressions %v -> %v", before, after)
	}
	// A complaint suppresses the address for every tenant, without revealing which tenant it came from.
	c2, _ := outreachTenant(t, "Other Events Tenant")
	co2 := c2.must(201, http.MethodPost, "/api/v1/crm/companies", map[string]any{"name": "Events Buyer", "domain": "events-buyer.example"})["id"].(string)
	p2 := person(t, c2, co2, "Cal Complain", "cal@events-buyer.example", "GB")
	cs2 := newCampaign(t, c2, "events-other", []string{p2}, 50, nil)
	if aud := c2.must(200, http.MethodGet, "/api/v1/outreach/campaigns/"+cs2.id+"/preflight", nil)["audience"].(map[string]any); aud["suppressed"] != 1.0 {
		t.Fatalf("other tenant: %v", aud)
	}
	// Labelling a reply as an unsubscribe request opts the person out.
	c.must(204, http.MethodPost, "/api/v1/outreach/replies/"+replies[0].(map[string]any)["id"].(string)+"/classify", map[string]any{"classification": "unsubscribe_request"})
	if n := len(c.must(200, http.MethodGet, "/api/v1/outreach/suppressions", nil)["data"].([]any)); n != 3 {
		t.Fatalf("%d suppressions", n)
	}
}

func TestOutreachIsIsolatedBetweenTenants(t *testing.T) {
	a, _ := outreachTenant(t, "Iso Out A")
	b, _ := outreachTenant(t, "Iso Out B")
	co := a.must(201, http.MethodPost, "/api/v1/crm/companies", map[string]any{"name": "Iso Buyer", "domain": "iso-out-buyer.example"})["id"].(string)
	p := person(t, a, co, "Ira Iso", "ira@iso-out-buyer.example", "GB")
	cs := newCampaign(t, a, "iso-out-sender", []string{p}, 50, nil)
	approveAndStart(t, a, &cs)
	sendRound(t)
	msg := a.must(200, http.MethodGet, "/api/v1/outreach/messages", nil)["data"].([]any)[0].(map[string]any)["id"].(string)
	for _, path := range []string{"/api/v1/outreach/campaigns/" + cs.id, "/api/v1/outreach/messages/" + msg, "/api/v1/outreach/senders/" + cs.sender} {
		if st, _ := b.Do(http.MethodGet, path, nil); st != 404 && st != 405 {
			t.Fatalf("B reads %s: %d", path, st)
		}
	}
	for _, op := range [][2]string{{http.MethodPost, "/pause"}, {http.MethodPost, "/cancel"}, {http.MethodPatch, ""}} {
		var body any
		if op[0] == http.MethodPatch {
			body = map[string]any{"name": "taken"}
		}
		if st, _ := b.Do(op[0], "/api/v1/outreach/campaigns/"+cs.id+op[1], body); st != 404 {
			t.Fatalf("B %s %s: %d", op[0], op[1], st)
		}
	}
	b.wantCode(404, "not_found", http.MethodDelete, "/api/v1/outreach/senders/"+cs.sender, nil)
	for _, list := range []string{"campaigns", "messages", "senders", "domains", "templates", "audiences", "replies", "suppressions"} {
		if s := asJSON(b.must(200, http.MethodGet, "/api/v1/outreach/"+list, nil)); strings.Contains(s, "iso-out") || strings.Contains(s, cs.id) {
			t.Fatalf("B's %s show A's data: %s", list, s)
		}
	}
	// B cannot put A's contacts in its audiences.
	aud := b.must(201, http.MethodPost, "/api/v1/outreach/audiences", map[string]any{"name": "stolen"})
	b.wantCode(422, "validation_failed", http.MethodPost, "/api/v1/outreach/audiences/"+aud["id"].(string)+"/members", map[string]any{"contact_ids": []string{p}})
	if r := enrollmentReasons(t, a, cs.id); r["active:"] != 1 && r["completed:"] != 1 {
		t.Fatalf("A's campaign affected: %v", r)
	}
}

func TestContactCountriesAreCheckedAgainstReferenceData(t *testing.T) {
	c, _ := outreachTenant(t, "Country Tenant")
	co := c.must(201, http.MethodPost, "/api/v1/crm/companies", map[string]any{"name": "Country Buyer", "domain": "country-buyer.example"})["id"].(string)
	c.wantCode(422, "validation_failed", http.MethodPost, "/api/v1/crm/contacts", map[string]any{"full_name": "Zed", "email": "zed@country-buyer.example", "crm_company_id": co, "country_code": "zz"})
}

func TestJurisdictionRulesAreAdminOnly(t *testing.T) {
	support := adminClient(t, "platform_support")
	support.must(200, http.MethodGet, "/api/v1/admin/outreach/jurisdiction-rules", nil)
	if st, _ := support.Do(http.MethodPut, "/api/v1/admin/outreach/jurisdiction-rules/gb", map[string]any{"rule": "blocked"}); st != 403 {
		t.Fatalf("support changed a rule: %d", st)
	}
	super := adminClient(t, "super_admin")
	super.wantCode(422, "validation_failed", http.MethodPut, "/api/v1/admin/outreach/jurisdiction-rules/zz", map[string]any{"rule": "opt_out_allowed"})
	c, _ := outreachTenant(t, "Rule Tenant")
	if st, _ := c.Do(http.MethodGet, "/api/v1/admin/outreach/jurisdiction-rules", nil); st != 401 && st != 403 {
		t.Fatalf("tenant read admin rules: %d", st)
	}
}
