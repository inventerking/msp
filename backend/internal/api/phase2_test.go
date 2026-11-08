package api_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/jobs"
	"mspengine/internal/platform/integration"
	"mspengine/internal/platform/shared"
)

// raw sends bytes rather than JSON.
func (c *Client) raw(method, path string, body []byte) (int, http.Header, []byte) {
	c.t.Helper()
	req, err := http.NewRequest(method, env.URL+path, bytes.NewReader(body))
	if err != nil {
		c.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	if c.csrf != "" {
		req.Header.Set("X-CSRF-Token", c.csrf)
	}
	resp, err := c.c.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		c.t.Fatal(err)
	}
	return resp.StatusCode, resp.Header, out
}

// worker runs one queue until it is empty.
func work(t *testing.T, queue string) {
	t.Helper()
	r, _ := graph.Worker("test-worker")
	r.TenantLimit = 1000
	for range 100 {
		did, err := r.RunOne(context.Background(), queue)
		if err != nil {
			t.Fatal(err)
		}
		if !did {
			return
		}
	}
}

func TestFileFlowThroughTheAPI(t *testing.T) {
	a, _, _ := newTenant(t, "Files A")
	b, _, _ := newTenant(t, "Files B")
	body := []byte("%PDF-1.7\nhello")
	up := a.must(201, http.MethodPost, "/api/v1/files/uploads", map[string]any{"purpose": "attachment", "file_name": `quote"; x=.pdf`, "size_bytes": len(body)})
	id, url := up["id"].(string), up["upload_url"].(string)

	if s, _, _ := b.raw(http.MethodPut, url, body); s != 404 {
		t.Fatalf("tenant B uploaded into tenant A's file: %d", s)
	}
	saved := a.csrf
	a.csrf = ""
	if s, _, _ := a.raw(http.MethodPut, url, body); s != 403 {
		t.Fatalf("an upload without a CSRF token got %d", s)
	}
	a.csrf = saved
	if s, _, out := a.raw(http.MethodPut, url, body); s != 204 {
		t.Fatalf("upload: %d %s", s, out)
	}
	a.wantCode(409, "file_not_ready", http.MethodGet, "/api/v1/files/"+id+"/download", nil)
	work(t, jobs.QueueDefault)
	if got := a.must(200, http.MethodGet, "/api/v1/files/"+id, nil); got["status"] != "clean" {
		t.Fatalf("status %v", got["status"])
	}
	s, h, out := a.raw(http.MethodGet, "/api/v1/files/"+id+"/download", nil)
	if s != 200 || !bytes.Equal(out, body) {
		t.Fatalf("%d %s", s, out)
	}
	if !strings.HasPrefix(h.Get("Content-Disposition"), "attachment") || !strings.Contains(h.Get("Content-Security-Policy"), "sandbox") || h.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("unsafe download headers: %v", h)
	}
	if strings.Contains(h.Get("Content-Disposition"), "\n") {
		t.Fatal("header injection through the file name")
	}
	b.wantCode(404, "not_found", http.MethodGet, "/api/v1/files/"+id, nil)
	b.wantCode(404, "not_found", http.MethodGet, "/api/v1/files/"+id+"/download", nil)

	// A file is too large by its declared size, and again by its real size.
	a.wantCode(413, "file_too_large", http.MethodPost, "/api/v1/files/uploads", map[string]any{"purpose": "attachment", "file_name": "x.pdf", "size_bytes": 1 << 30})
	small := a.must(201, http.MethodPost, "/api/v1/files/uploads", map[string]any{"purpose": "attachment", "file_name": "x.pdf", "size_bytes": 10})
	if s, _, _ := a.raw(http.MethodPut, small["upload_url"].(string), make([]byte, 21<<20)); s != 413 {
		t.Fatalf("an oversized upload got %d", s)
	}
}

func TestWebhookThroughTheAPI(t *testing.T) {
	admin := adminClient(t, "integration_admin")
	ep := admin.must(201, http.MethodPost, "/api/v1/admin/integrations/webhook-endpoints", map[string]any{"provider_key": "fake_webhook"})
	token, secret := ep["path_token"].(string), ep["secret"].(string)
	var calls int
	graph.Integration.RegisterWebhook("fake_webhook", integration.VerifyHMAC, func(context.Context, *uuid.UUID, []byte) error { calls++; return nil })

	post := func(sigSecret string, body []byte, id string) int {
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		req, _ := http.NewRequest(http.MethodPost, env.URL+"/api/v1/webhooks/email/"+token, bytes.NewReader(body))
		req.Header.Set("X-Webhook-Timestamp", ts)
		req.Header.Set("X-Webhook-Signature", hex.EncodeToString(integration.SignHMAC(sigSecret, ts, body)))
		req.Header.Set("X-Webhook-Id", id)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}
	body := []byte(`{"event":"delivered"}`)
	if s := post("forged-secret", body, "w1"); s != 401 {
		t.Fatalf("a forged webhook got %d", s)
	}
	for range 2 {
		if s := post(secret, body, "w1"); s != 200 {
			t.Fatalf("a genuine webhook got %d", s)
		}
	}
	work(t, jobs.QueueCritical)
	if calls != 1 {
		t.Fatalf("handled %d times", calls)
	}
	if s := post(secret, make([]byte, integration.MaxWebhookBytes+1), "w2"); s != 413 {
		t.Fatalf("an oversized webhook got %d", s)
	}
	secretsLeak := admin.must(200, http.MethodGet, "/api/v1/admin/integrations/providers", nil)
	raw := strings.ToLower(asJSON(secretsLeak))
	if strings.Contains(raw, "ciphertext") || strings.Contains(raw, secret) {
		t.Fatal("the provider list exposes secret material")
	}
}

func TestJobsThroughTheAPI(t *testing.T) {
	a, _, tenantA := newTenant(t, "Jobs A")
	b, _, _ := newTenant(t, "Jobs B")
	id := uuid.New()
	var jobID uuid.UUID
	tid := uuid.MustParse(tenantA)
	err := pgx.BeginFunc(context.Background(), graph.Pools.System, func(tx pgx.Tx) error {
		var err error
		jobID, _, err = jobs.Enqueue(context.Background(), tx, jobs.Spec{TenantID: &tid, Kind: "api-test-" + id.String()[:8], Queue: "api_test_queue"})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/jobs/" + jobID.String()
	if got := a.must(200, http.MethodGet, path, nil); got["status"] != "queued" {
		t.Fatalf("got %v", got)
	}
	b.wantCode(404, "not_found", http.MethodGet, path, nil)
	b.wantCode(404, "not_found", http.MethodPost, path+"/cancel", nil)
	if got := a.must(200, http.MethodPost, path+"/cancel", nil); got["status"] != "cancelled" {
		t.Fatalf("got %v", got)
	}
}

func TestPrivacyThroughTheAPI(t *testing.T) {
	a, _, tenantA := newTenant(t, "Privacy A")
	b, _, _ := newTenant(t, "Privacy B")
	email := uuid.NewString()[:8] + "@example.com"
	contact, err := graph.Shared.ResolveOrCreateContact(context.Background(), uuid.MustParse(tenantA), shared.ContactInput{Email: email, FullName: "Ann", SourceKey: "csv", Licence: shared.LicenceTenantProvided})
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/privacy/contacts/" + contact.String()
	if out := a.must(200, http.MethodGet, path+"/export", nil); !strings.Contains(asJSON(out), email) {
		t.Fatalf("export %v", out)
	}
	b.wantCode(404, "not_found", http.MethodGet, path+"/export", nil)
	b.wantCode(404, "not_found", http.MethodPost, path+"/erase", nil)
	job := a.must(202, http.MethodPost, path+"/erase", nil)["job_id"].(string)
	work(t, jobs.QueueDefault)
	if got := a.must(200, http.MethodGet, "/api/v1/jobs/"+job, nil); got["status"] != "succeeded" {
		t.Fatalf("job %v", got)
	}
	if out := a.must(200, http.MethodGet, path+"/export", nil); strings.Contains(asJSON(out), email) {
		t.Fatalf("erased data still exported: %v", out)
	}
}

func TestCompanySearchShowsNoTenantActivity(t *testing.T) {
	a, _, _ := newTenant(t, "Search A")
	marker := "zz" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	if _, err := graph.Shared.ResolveOrCreate(context.Background(), shared.Signal{Domain: marker + ".com"}); err != nil {
		t.Fatal(err)
	}
	out := a.must(200, http.MethodGet, "/api/v1/companies/search?q="+marker, nil)["data"].([]any)
	if len(out) != 1 {
		t.Fatalf("got %d", len(out))
	}
	for k := range out[0].(map[string]any) {
		switch k {
		case "id", "display_name", "primary_domain", "country_code", "employee_band":
		default:
			t.Errorf("search exposes %q", k)
		}
	}
	a.wantCode(422, "validation_failed", http.MethodGet, "/api/v1/companies/search?q=a", nil)
}

func TestCreditsThroughTheAPI(t *testing.T) {
	owner, _, tenant := newTenant(t, "Credits Tenant")
	billing := adminClient(t, "billing_admin")
	support := adminClient(t, "platform_support")
	body := map[string]any{"meter_key": "ai_credits", "amount": 500, "reason": "welcome credits", "idempotency_key": "grant-" + uuid.NewString()}
	path := "/api/v1/admin/tenants/" + tenant + "/credits"
	support.wantCode(403, "insufficient_role", http.MethodPost, path, body)
	for range 2 {
		billing.must(204, http.MethodPost, path, body) // A retried grant must count once.
	}
	if got := billing.must(200, http.MethodGet, path, nil); !strings.Contains(asJSON(got["balances"]), `"balance":500`) {
		t.Fatalf("got %v", got)
	}
	if got := owner.must(200, http.MethodGet, "/api/v1/usage", nil); !strings.Contains(asJSON(got["meters"]), `"balance":500`) {
		t.Fatalf("got %v", got)
	}
}

func TestDeadLetterDecisions(t *testing.T) {
	support := adminClient(t, "platform_support")
	analyst := adminClient(t, "platform_analyst")
	kind := "dl-" + uuid.NewString()[:8]
	var deadJob uuid.UUID
	err := pgx.BeginFunc(context.Background(), graph.Pools.System, func(tx pgx.Tx) error {
		var err error
		deadJob, _, err = jobs.Enqueue(context.Background(), tx, jobs.Spec{Kind: kind, Queue: "dl_queue", MaxAttempts: 1})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	r := jobs.NewRunner(graph.Pools.System, "dl")
	r.Register(kind, func(context.Context, jobs.Job) error { return io.ErrUnexpectedEOF })
	if _, err := r.RunOne(context.Background(), "dl_queue"); err != nil {
		t.Fatal(err)
	}
	analyst.wantCode(403, "insufficient_role", http.MethodGet, "/api/v1/admin/dead-letters", nil)
	var id string
	for _, d := range support.must(200, http.MethodGet, "/api/v1/admin/dead-letters", nil)["data"].([]any) {
		if d.(map[string]any)["kind"] == kind {
			id = d.(map[string]any)["id"].(string)
		}
	}
	if id == "" {
		t.Fatal("the dead letter is not listed")
	}
	reason := map[string]any{"reason": "provider has recovered"}
	support.must(204, http.MethodPost, "/api/v1/admin/dead-letters/"+id+"/requeue", reason)
	support.wantCode(409, "invalid_transition", http.MethodPost, "/api/v1/admin/dead-letters/"+id+"/discard", reason)
	var status string
	if err := graph.Pools.System.QueryRow(context.Background(), `SELECT status FROM jobs.jobs WHERE id = $1`, deadJob).Scan(&status); err != nil || status != "queued" {
		t.Fatalf("status %s %v", status, err)
	}
	if !strings.Contains(asJSON(support.must(200, http.MethodGet, "/api/v1/admin/audit-events?limit=100", nil)), "dead_letter.requeued") {
		t.Fatal("the decision was not audited")
	}
	stats := support.must(200, http.MethodGet, "/api/v1/admin/jobs/stats", nil)
	if stats["queues"] == nil {
		t.Fatalf("stats %v", stats)
	}
}

func TestAIAdminRoles(t *testing.T) {
	admin := adminClient(t, "integration_admin")
	billing := adminClient(t, "billing_admin")
	billing.wantCode(403, "insufficient_role", http.MethodGet, "/api/v1/admin/ai/routes", nil)
	routes := admin.must(200, http.MethodGet, "/api/v1/admin/ai/routes", nil)["data"].([]any)
	if len(routes) == 0 {
		t.Fatal("no routes")
	}
	prompts := admin.must(200, http.MethodGet, "/api/v1/admin/ai/prompts", nil)["data"].([]any)
	prompt := prompts[0].(map[string]any)
	if prompt["active_version"] != float64(1) || prompt["action_class"] != "informational" {
		t.Fatalf("prompt %v", prompt)
	}
	v := admin.must(201, http.MethodPost, "/api/v1/admin/ai/prompts/"+prompt["id"].(string)+"/versions", map[string]any{"system_text": "New system text for testing.", "user_text": "{{content}}"})["id"].(string)
	if got := admin.must(200, http.MethodGet, "/api/v1/admin/ai/prompts", nil)["data"].([]any)[0].(map[string]any); got["active_version"] != float64(1) {
		t.Fatalf("a draft went live: %v", got)
	}
	admin.must(204, http.MethodPost, "/api/v1/admin/ai/prompt-versions/"+v+"/activate", nil)
	if got := admin.must(200, http.MethodGet, "/api/v1/admin/ai/prompts", nil)["data"].([]any)[0].(map[string]any); got["active_version"] != float64(2) {
		t.Fatalf("activation failed: %v", got)
	}
}

// The tenant API must not offer a general way to reach AI. It is only reached through modules.
func TestNoGeneralAIEndpointExists(t *testing.T) {
	for _, o := range allTenantOps(t) {
		if strings.Contains(o.path, "/ai/") || strings.HasSuffix(o.path, "/ai") {
			t.Errorf("tenant route %s reaches AI directly", o.path)
		}
	}
}

func TestEntitlementChangeNotifiesWorkspace(t *testing.T) {
	owner, _, tenant := newTenant(t, "Notified Tenant")
	super := adminClient(t, "super_admin")
	other, _, _ := newTenant(t, "Unnotified Tenant")
	super.must(200, http.MethodPost, "/api/v1/admin/tenants/"+tenant+"/entitlements/crm/activate", map[string]any{"reason": "testing notifications"})
	_, bus := graph.Worker("notify-test")
	// Other tests leave events queued, so dispatch until nothing is left rather than a fixed number of rounds.
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
			break
		}
	}
	got := owner.must(200, http.MethodGet, "/api/v1/notifications", nil)
	if got["unread"] != float64(1) {
		t.Fatalf("got %v", got)
	}
	id := got["data"].([]any)[0].(map[string]any)["id"].(string)
	other.wantCode(404, "not_found", http.MethodPost, "/api/v1/notifications/"+id+"/read", nil)
	if got := other.must(200, http.MethodGet, "/api/v1/notifications", nil); got["unread"] != float64(0) {
		t.Fatalf("another tenant was notified: %v", got)
	}
	owner.must(204, http.MethodPost, "/api/v1/notifications/"+id+"/read", nil)
	if got := owner.must(200, http.MethodGet, "/api/v1/notifications", nil); got["unread"] != float64(0) {
		t.Fatalf("still unread: %v", got)
	}
}
