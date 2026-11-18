package api_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"mspengine/internal/kernel/jobs"
	"mspengine/internal/platform/integration"
)

// billingPlan creates a GBP plan on a seeded product, with optional seats and included AI credits.
func billingPlan(t *testing.T, productKey string, amount int, seats, credits, trial int) string {
	t.Helper()
	admin := adminClient(t, "super_admin")
	var version string
	for _, p := range admin.must(200, http.MethodGet, "/api/v1/admin/products", nil)["data"].([]any) {
		if pm := p.(map[string]any); pm["key"] == productKey {
			version = pm["version_id"].(string)
		}
	}
	body := map[string]any{"product_version_id": version, "key": "p" + strings.ReplaceAll(uuid.NewString(), "-", "")[:16], "name": "Plan " + productKey, "billing_period": "monthly", "trial_days": trial}
	if seats > 0 {
		body["seats"] = seats
	}
	if credits > 0 {
		body["included_credits"] = map[string]any{"ai_credits": credits}
	}
	id := admin.must(201, http.MethodPost, "/api/v1/admin/plans", body)["id"].(string)
	admin.must(201, http.MethodPost, "/api/v1/admin/plans/"+id+"/prices", map[string]any{"currency": "GBP", "amount_minor": amount})
	return id
}

func purchase(t *testing.T, c *Client, plan, method, key string) map[string]any {
	t.Helper()
	status, body := c.DoWith(http.MethodPost, "/api/v1/billing/purchases", map[string]any{"plan_id": plan, "currency": "GBP", "quantity": 1, "payment_method": method},
		map[string]string{"Idempotency-Key": key})
	if status != 201 {
		t.Fatalf("purchase: %d %v", status, body)
	}
	return body
}

// payCheckout completes the fake checkout behind a checkout URL with an outcome.
func payCheckout(t *testing.T, checkoutURL, outcome string) map[string]any {
	t.Helper()
	ref := checkoutURL[strings.LastIndex(checkoutURL, "/")+1:]
	return newClient(t).must(200, http.MethodPost, "/api/v1/public/fake-checkout/"+ref+"/complete", map[string]any{"outcome": outcome})
}

func billedModule(t *testing.T, c *Client, key string) string {
	t.Helper()
	for _, m := range c.refreshCSRF("/api/v1/me/context")["modules"].([]any) {
		if mm := m.(map[string]any); mm["key"] == key {
			return mm["status"].(string)
		}
	}
	return ""
}

func TestCardPurchaseIsReviewedThenActivates(t *testing.T) {
	c, _, tenant := newTenant(t, "Billing Card")
	plan := billingPlan(t, "crm", 4900, 0, 200, 0)
	offers := c.must(200, http.MethodGet, "/api/v1/billing/plans", nil)
	if offers["card_available"] != true || !strings.Contains(offers["tax_note"].(string), "exclude tax") {
		t.Fatalf("offers: %v", offers)
	}
	p := purchase(t, c, plan, "card", "card-purchase-1")
	if again := purchase(t, c, plan, "card", "card-purchase-1"); again["id"] != p["id"] {
		t.Fatal("the same key must return the same purchase")
	}
	url, _ := p["checkout_url"].(string)
	if p["status"] != "payment_pending" || url == "" || p["quote"].(map[string]any)["total_minor"].(float64) != 4900 {
		t.Fatalf("purchase: %v", p)
	}
	if done := payCheckout(t, url, "pay"); done["status"] != "succeeded" {
		t.Fatalf("checkout: %v", done)
	}
	ref := url[strings.LastIndex(url, "/")+1:]
	newClient(t).wantCode(409, "checkout_closed", http.MethodPost, "/api/v1/public/fake-checkout/"+ref+"/complete", map[string]any{"outcome": "pay"})
	p = c.must(200, http.MethodGet, "/api/v1/billing/purchases/"+p["id"].(string), nil)
	if p["status"] != "under_review" {
		t.Fatalf("after payment: %v", p["status"])
	}
	if inv := c.must(200, http.MethodGet, "/api/v1/billing/invoices/"+p["invoice_id"].(string), nil)["invoice"].(map[string]any); inv["status"] != "paid" || !strings.HasPrefix(inv["number"].(string), "INV-") {
		t.Fatalf("invoice: %v", inv)
	}
	if billedModule(t, c, "crm") == "active" {
		t.Fatal("payment alone must not grant access before review")
	}
	admin := adminClient(t, "billing_admin")
	queue := admin.must(200, http.MethodGet, "/api/v1/admin/billing/purchases?status=under_review", nil)["data"].([]any)
	found := false
	for _, q := range queue {
		found = found || q.(map[string]any)["id"] == p["id"]
	}
	if !found {
		t.Fatal("the purchase is not in the review queue")
	}
	sub := admin.must(200, http.MethodPost, "/api/v1/admin/billing/purchases/"+p["id"].(string)+"/approve", map[string]any{"reason": "Looks fine"})
	again := admin.must(200, http.MethodPost, "/api/v1/admin/billing/purchases/"+p["id"].(string)+"/approve", map[string]any{"reason": "Looks fine"})
	if sub["subscription_id"] != again["subscription_id"] {
		t.Fatal("approving twice must return the same subscription")
	}
	if billedModule(t, c, "crm") != "active" {
		t.Fatal("approval must activate the plan's module")
	}
	usage := c.must(200, http.MethodGet, "/api/v1/usage", nil)["meters"].([]any)
	credited := false
	for _, m := range usage {
		mm := m.(map[string]any)
		credited = credited || mm["meter_key"] == "ai_credits" && mm["balance"].(float64) == 200
	}
	if !credited {
		t.Fatalf("included credits: %v", usage)
	}
	if n := tenantScalar(t, tenant, `SELECT count(*)::text FROM notification.notifications WHERE tenant_id = $1 AND notification_key = 'billing.purchase_approved'`, tenant); n != "1" {
		t.Fatalf("approval notices: %s", n)
	}
}

func TestRejectingAPaidPurchaseRefundsIt(t *testing.T) {
	c, _, _ := newTenant(t, "Billing Reject")
	plan := billingPlan(t, "proposals", 2500, 0, 0, 0)
	p := purchase(t, c, plan, "card", "reject-1-key")
	payCheckout(t, p["checkout_url"].(string), "pay")
	admin := adminClient(t, "billing_admin")
	admin.wantCode(422, "validation_failed", http.MethodPost, "/api/v1/admin/billing/purchases/"+p["id"].(string)+"/reject", map[string]any{"reason": ""})
	admin.must(204, http.MethodPost, "/api/v1/admin/billing/purchases/"+p["id"].(string)+"/reject", map[string]any{"reason": "Cannot serve this region yet"})
	admin.wantCode(409, "purchase_not_reviewable", http.MethodPost, "/api/v1/admin/billing/purchases/"+p["id"].(string)+"/approve", map[string]any{"reason": "changed mind"})
	got := c.must(200, http.MethodGet, "/api/v1/billing/purchases/"+p["id"].(string), nil)
	if got["status"] != "rejected" || got["rejection_reason"] != "Cannot serve this region yet" {
		t.Fatalf("purchase: %v", got)
	}
	pays := c.must(200, http.MethodGet, "/api/v1/billing/invoices/"+p["invoice_id"].(string), nil)["payments"].([]any)
	if len(pays) != 1 || pays[0].(map[string]any)["status"] != "refunded" || pays[0].(map[string]any)["refund_status"] != "succeeded" {
		t.Fatalf("payments: %v", pays)
	}
	if n := platformScalar(t, `SELECT refunded_minor::text FROM integration.fake_payment_ledger WHERE ref = $1`, p["checkout_url"].(string)[strings.LastIndex(p["checkout_url"].(string), "/")+1:]); n != "2500" {
		t.Fatalf("provider refund: %s", n)
	}
}

func TestBankTransferPurchase(t *testing.T) {
	c, _, _ := newTenant(t, "Billing Bank")
	plan := billingPlan(t, "chatbot", 9900, 0, 0, 0)
	admin := adminClient(t, "billing_admin")
	admin.must(200, http.MethodPut, "/api/v1/admin/billing/bank-instructions", map[string]any{"text": "Pay to the test account. Use the invoice number as the reference."})
	p := purchase(t, c, plan, "bank_transfer", "bank-1-key")
	if p["status"] != "payment_pending" || p["checkout_url"] != nil || !strings.Contains(p["bank_instructions"].(string), "test account") {
		t.Fatalf("purchase: %v", p)
	}
	var payment string
	for _, b := range admin.must(200, http.MethodGet, "/api/v1/admin/billing/bank-transfers", nil)["data"].([]any) {
		if bm := b.(map[string]any); bm["invoice_id"] == p["invoice_id"] {
			payment = bm["payment_id"].(string)
		}
	}
	if payment == "" {
		t.Fatal("the transfer is not listed")
	}
	admin.wantCode(422, "validation_failed", http.MethodPost, "/api/v1/admin/billing/payments/"+payment+"/mark-paid", map[string]any{"reference": ""})
	admin.must(204, http.MethodPost, "/api/v1/admin/billing/payments/"+payment+"/mark-paid", map[string]any{"reference": "BANK-REF-1"})
	if got := c.must(200, http.MethodGet, "/api/v1/billing/purchases/"+p["id"].(string), nil); got["status"] != "under_review" {
		t.Fatalf("after confirming: %v", got["status"])
	}
	// Rejecting needs a refund by hand, which an admin records.
	admin.must(204, http.MethodPost, "/api/v1/admin/billing/purchases/"+p["id"].(string)+"/reject", map[string]any{"reason": "Duplicate order"})
	admin.wantCode(409, "manual_refund", http.MethodPost, "/api/v1/admin/billing/payments/"+payment+"/retry-refund", nil)
	admin.must(204, http.MethodPost, "/api/v1/admin/billing/payments/"+payment+"/mark-refunded", map[string]any{"note": "Returned by bank, ref R-1"})
	admin.wantCode(409, "no_refund_due", http.MethodPost, "/api/v1/admin/billing/payments/"+payment+"/mark-refunded", map[string]any{"note": "again"})
}

func TestUnpaidPurchaseCanBeCancelledAndVoided(t *testing.T) {
	c, _, _ := newTenant(t, "Billing Cancel")
	plan := billingPlan(t, "outreach", 1000, 0, 0, 0)
	p := purchase(t, c, plan, "card", "cancel-1-key")
	c.must(204, http.MethodPost, "/api/v1/billing/purchases/"+p["id"].(string)+"/cancel", nil)
	if inv := c.must(200, http.MethodGet, "/api/v1/billing/invoices/"+p["invoice_id"].(string), nil)["invoice"].(map[string]any); inv["status"] != "void" {
		t.Fatalf("invoice: %v", inv["status"])
	}
	c.wantCode(409, "purchase_not_cancellable", http.MethodPost, "/api/v1/billing/purchases/"+p["id"].(string)+"/cancel", nil)
	newClient(t).wantCode(409, "checkout_closed", http.MethodPost, "/api/v1/public/fake-checkout/"+p["checkout_url"].(string)[strings.LastIndex(p["checkout_url"].(string), "/")+1:]+"/complete", map[string]any{"outcome": "pay"})
}

// approved buys a plan with a card outcome and approves it, returning the subscription.
func approved(t *testing.T, c *Client, plan, outcome, key string) string {
	t.Helper()
	p := purchase(t, c, plan, "card", key)
	payCheckout(t, p["checkout_url"].(string), outcome)
	return adminClient(t, "billing_admin").must(200, http.MethodPost, "/api/v1/admin/billing/purchases/"+p["id"].(string)+"/approve", map[string]any{"reason": "approved"})["subscription_id"].(string)
}

func platformExec(t *testing.T, query string, args ...any) {
	t.Helper()
	if _, err := appConn(t, "msp_platform", "dev_platform").Exec(context.Background(), query, args...); err != nil {
		t.Fatal(err)
	}
}

func tick(t *testing.T) {
	t.Helper()
	if err := graph.Commercial.HandleBillingTick(context.Background(), jobs.Job{}); err != nil {
		t.Fatal(err)
	}
}

func TestRenewalDunningSuspensionAndRecovery(t *testing.T) {
	c, _, tenant := newTenant(t, "Billing Dunning")
	plan := billingPlan(t, "crm", 3000, 0, 50, 0)
	sub := approved(t, c, plan, "pay_card_declines_later", "dunning-1-key")
	// The period ends: a renewal invoice is issued and the saved card is charged, and declines.
	platformExec(t, `UPDATE platform.subscriptions SET current_period_end = now() - interval '1 minute' WHERE id = $1`, sub)
	tick(t)
	state := func() string {
		return platformScalar(t, `SELECT status || ':' || dunning_attempts FROM platform.subscriptions WHERE id = $1`, sub)
	}
	if got := state(); got != "past_due:1" {
		t.Fatalf("after a declined renewal: %s", got)
	}
	tick(t) // Nothing more is due yet.
	if got := state(); got != "past_due:1" {
		t.Fatalf("an early tick must not retry: %s", got)
	}
	if n := tenantScalar(t, tenant, `SELECT count(*)::text FROM notification.notifications WHERE tenant_id = $1 AND notification_key = 'billing.payment_failed'`, tenant); n != "1" {
		t.Fatalf("failure notices: %s", n)
	}
	// A retry is due and declines again.
	platformExec(t, `UPDATE platform.subscriptions SET next_retry_at = now() - interval '1 minute' WHERE id = $1`, sub)
	tick(t)
	if got := state(); got != "past_due:2" {
		t.Fatalf("after a retry: %s", got)
	}
	// The grace period passes: access is suspended.
	platformExec(t, `UPDATE platform.subscriptions SET past_due_since = now() - interval '15 days', next_retry_at = now() + interval '1 day' WHERE id = $1`, sub)
	tick(t)
	if got := state(); !strings.HasPrefix(got, "suspended") || billedModule(t, c, "crm") != "suspended" {
		t.Fatalf("after the grace period: %s, crm %s", got, billedModule(t, c, "crm"))
	}
	// The tenant pays the open invoice with another card: access returns and dunning stops.
	var open string
	for _, i := range c.must(200, http.MethodGet, "/api/v1/billing/invoices", nil)["data"].([]any) {
		if im := i.(map[string]any); im["status"] == "issued" {
			open = im["id"].(string)
		}
	}
	link := c.must(200, http.MethodPost, "/api/v1/billing/invoices/"+open+"/pay", nil)["checkout_url"].(string)
	if again := c.must(200, http.MethodPost, "/api/v1/billing/invoices/"+open+"/pay", nil)["checkout_url"]; again != link {
		t.Fatal("paying twice the same day must reuse the checkout")
	}
	payCheckout(t, link, "pay")
	if got := state(); got != "active:0" || billedModule(t, c, "crm") != "active" {
		t.Fatalf("after paying: %s, crm %s", got, billedModule(t, c, "crm"))
	}
}

func TestUnpaidSubscriptionExpires(t *testing.T) {
	c, _, _ := newTenant(t, "Billing Expiry")
	plan := billingPlan(t, "proposals", 3000, 0, 0, 0)
	sub := approved(t, c, plan, "pay_card_declines_later", "expiry-1-key")
	platformExec(t, `UPDATE platform.subscriptions SET current_period_end = now() - interval '1 minute' WHERE id = $1`, sub)
	tick(t)
	platformExec(t, `UPDATE platform.subscriptions SET past_due_since = now() - interval '31 days' WHERE id = $1`, sub)
	tick(t)
	if got := platformScalar(t, `SELECT status FROM platform.subscriptions WHERE id = $1`, sub); got != "expired" || billedModule(t, c, "proposals") == "active" {
		t.Fatalf("after 31 days: %s", got)
	}
	if got := platformScalar(t, `SELECT string_agg(status, ',' ORDER BY created_at) FROM platform.invoices WHERE subscription_id = $1`, sub); got != "uncollectible" {
		t.Fatalf("renewal invoice: %s", got)
	}
}

func TestSeatLimitIsEnforced(t *testing.T) {
	c, _, _ := newTenant(t, "Billing Seats")
	plan := billingPlan(t, "crm", 1000, 2, 0, 0)
	c.must(201, http.MethodPost, "/api/v1/tenant/invitations", map[string]any{"email": unique("before"), "role": "viewer"})
	c.must(201, http.MethodPost, "/api/v1/tenant/invitations", map[string]any{"email": unique("before2"), "role": "viewer"})
	approved(t, c, plan, "pay", "seats-1-key")
	// The owner and two open invitations already use more than two seats.
	c.wantCode(409, "seat_limit_reached", http.MethodPost, "/api/v1/tenant/invitations", map[string]any{"email": unique("after"), "role": "viewer"})
}

func TestPaymentWebhookAndReconciliation(t *testing.T) {
	c, _, _ := newTenant(t, "Billing Recon")
	plan := billingPlan(t, "crm", 1500, 0, 0, 0)
	p := purchase(t, c, plan, "card", "recon-1-key")
	ref := p["checkout_url"].(string)[strings.LastIndex(p["checkout_url"].(string), "/")+1:]
	// The provider's signed webhook path, as a real provider would use it.
	ep := adminClient(t, "integration_admin").must(201, http.MethodPost, "/api/v1/admin/integrations/webhook-endpoints", map[string]any{"provider_key": "fake_payments"})
	platformExec(t, `UPDATE integration.fake_payment_ledger SET status = 'succeeded', method_ref = 'fpm_x' WHERE ref = $1`, ref)
	body := []byte(`{"EventID":"evt-` + ref + `","Type":"payment.succeeded","PaymentRef":"` + ref + `","MethodRef":"fpm_x"}`)
	for range 2 {
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		r, _ := http.NewRequest(http.MethodPost, env.URL+"/api/v1/webhooks/payments/"+ep["path_token"].(string), bytes.NewReader(body))
		r.Header.Set("X-Webhook-Timestamp", ts)
		r.Header.Set("X-Webhook-Signature", hex.EncodeToString(integration.SignHMAC(ep["secret"].(string), ts, body)))
		r.Header.Set("X-Webhook-Id", "evt-"+ref)
		resp, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("webhook %d", resp.StatusCode)
		}
	}
	// Earlier tests can leave other work on the queue, so keep going until this event is handled.
	workUntil(t, jobs.QueueCritical, func() bool {
		return c.must(200, http.MethodGet, "/api/v1/billing/purchases/"+p["id"].(string), nil)["status"] == "under_review"
	})
	// Our record disagrees with the provider's: reconciliation reports it, once.
	platformExec(t, `UPDATE platform.payments SET amount_minor = 1400 WHERE provider_payment_ref = $1`, ref)
	admin := adminClient(t, "billing_admin")
	admin.must(200, http.MethodPost, "/api/v1/admin/billing/reconciliation/run", nil)
	view := admin.must(200, http.MethodPost, "/api/v1/admin/billing/reconciliation/run", nil)
	var issue string
	n := 0
	for _, i := range view["issues"].([]any) {
		if im := i.(map[string]any); im["provider_ref"] == ref && im["kind"] == "amount_mismatch" && im["status"] == "open" {
			issue = im["id"].(string)
			n++
		}
	}
	if n != 1 {
		t.Fatalf("mismatch issues: %d", n)
	}
	admin.must(204, http.MethodPost, "/api/v1/admin/billing/reconciliation/issues/"+issue+"/resolve", map[string]any{"note": "Corrected by hand"})
}

func TestBillingIsIsolatedBetweenTenants(t *testing.T) {
	a, _, _ := newTenant(t, "Billing Iso A")
	b, _, _ := newTenant(t, "Billing Iso B")
	plan := billingPlan(t, "crm", 1000, 0, 0, 0)
	p := purchase(t, a, plan, "card", "iso-1-key")
	b.wantCode(404, "not_found", http.MethodGet, "/api/v1/billing/purchases/"+p["id"].(string), nil)
	b.wantCode(404, "not_found", http.MethodPost, "/api/v1/billing/purchases/"+p["id"].(string)+"/cancel", nil)
	b.wantCode(404, "not_found", http.MethodGet, "/api/v1/billing/invoices/"+p["invoice_id"].(string), nil)
	b.wantCode(404, "not_found", http.MethodPost, "/api/v1/billing/invoices/"+p["invoice_id"].(string)+"/pay", nil)
	if list := b.must(200, http.MethodGet, "/api/v1/billing/purchases", nil)["data"].([]any); len(list) != 0 {
		t.Fatalf("B sees purchases: %v", list)
	}
	// A purchase's key is per tenant: B's same key makes B's own purchase.
	if own := purchase(t, b, plan, "card", "iso-1-key"); own["id"] == p["id"] {
		t.Fatal("idempotency keys must not cross tenants")
	}
}

func TestDunningPolicyValidation(t *testing.T) {
	admin := adminClient(t, "billing_admin")
	admin.wantCode(422, "validation_failed", http.MethodPut, "/api/v1/admin/billing/dunning-policy", map[string]any{"retry_days": []int{3, 1}, "suspend_after_days": 14, "expire_after_days": 30, "bank_transfer_due_days": 14, "review_target_hours": 24})
	admin.wantCode(422, "validation_failed", http.MethodPut, "/api/v1/admin/billing/dunning-policy", map[string]any{"retry_days": []int{1}, "suspend_after_days": 30, "expire_after_days": 14, "bank_transfer_due_days": 14, "review_target_hours": 24})
	got := admin.must(200, http.MethodPut, "/api/v1/admin/billing/dunning-policy", map[string]any{"retry_days": []int{1, 3, 7}, "suspend_after_days": 14, "expire_after_days": 30, "bank_transfer_due_days": 14, "review_target_hours": 24})
	if got["expire_after_days"].(float64) != 30 {
		t.Fatalf("policy: %v", got)
	}
	adminClient(t, "platform_support").wantCode(403, "insufficient_role", http.MethodPut, "/api/v1/admin/billing/dunning-policy", got)
}

func TestLegalDocumentsAndAcceptance(t *testing.T) {
	admin := adminClient(t, "super_admin")
	adminClient(t, "platform_support").wantCode(403, "insufficient_role", http.MethodPost, "/api/v1/admin/legal/documents", map[string]any{"kind": "terms", "title": "Terms", "body": "x", "requires_acceptance": true})
	draft := admin.must(201, http.MethodPost, "/api/v1/admin/legal/documents", map[string]any{"kind": "acceptable_use", "title": "Acceptable use", "body": "Placeholder for counsel's text.", "requires_acceptance": true})
	admin.must(200, http.MethodPatch, "/api/v1/admin/legal/documents/"+draft["id"].(string), map[string]any{"kind": "acceptable_use", "title": "Acceptable use policy", "body": "Draft text v1.", "requires_acceptance": true})
	pub := admin.must(200, http.MethodPost, "/api/v1/admin/legal/documents/"+draft["id"].(string)+"/publish", map[string]any{"counsel_reviewed": false})
	if pub["status"] != "published" || pub["counsel_reviewed"] != false {
		t.Fatalf("published: %v", pub)
	}
	admin.wantCode(409, "not_a_draft", http.MethodPatch, "/api/v1/admin/legal/documents/"+draft["id"].(string), map[string]any{"kind": "acceptable_use", "title": "x", "body": "y", "requires_acceptance": true})

	c, _, _ := newTenant(t, "Legal Tenant")
	pending := func() []any { return c.must(200, http.MethodGet, "/api/v1/me/legal", nil)["data"].([]any) }
	due := func(id string) bool {
		for _, d := range pending() {
			if d.(map[string]any)["id"] == id {
				return true
			}
		}
		return false
	}
	if !due(draft["id"].(string)) {
		t.Fatal("a published document requiring acceptance must be pending")
	}
	c.wantCode(422, "validation_failed", http.MethodPost, "/api/v1/me/legal/accept", map[string]any{"document_ids": []string{uuid.NewString()}})
	c.must(204, http.MethodPost, "/api/v1/me/legal/accept", map[string]any{"document_ids": []string{draft["id"].(string)}})
	if due(draft["id"].(string)) {
		t.Fatal("accepted, so no longer pending")
	}
	// A new version needs accepting again.
	v2 := admin.must(201, http.MethodPost, "/api/v1/admin/legal/documents", map[string]any{"kind": "acceptable_use", "title": "Acceptable use policy", "body": "Draft text v2.", "requires_acceptance": true})
	admin.must(200, http.MethodPost, "/api/v1/admin/legal/documents/"+v2["id"].(string)+"/publish", map[string]any{"counsel_reviewed": true})
	if !due(v2["id"].(string)) {
		t.Fatal("a new version must be pending")
	}
	public := newClient(t).must(200, http.MethodGet, "/api/v1/legal/documents", nil)["data"].([]any)
	for _, d := range public {
		if dm := d.(map[string]any); dm["kind"] == "acceptable_use" && dm["version"].(float64) != v2["version"].(float64) {
			t.Fatalf("only the current version is public: %v", dm)
		}
	}
	if old := admin.must(200, http.MethodGet, "/api/v1/admin/legal/documents/"+draft["id"].(string), nil); old["status"] != "retired" || old["accepted_count"].(float64) < 1 {
		t.Fatalf("old version: %v", old)
	}
}

func TestAbuseReports(t *testing.T) {
	anon := newClient(t)
	anon.wantCode(422, "validation_failed", http.MethodPost, "/api/v1/public/abuse-reports", map[string]any{"category": "spam", "details": "x", "subject_url": "javascript:alert(1)"})
	anon.must(202, http.MethodPost, "/api/v1/public/abuse-reports", map[string]any{"category": "spam", "details": "Unwanted email from a tenant", "subject_url": "https://example.test/x"})
	admin := adminClient(t, "platform_support")
	var id string
	for _, r := range admin.must(200, http.MethodGet, "/api/v1/admin/abuse-reports?status=new", nil)["data"].([]any) {
		if rm := r.(map[string]any); rm["details"] == "Unwanted email from a tenant" {
			id = rm["id"].(string)
		}
	}
	if id == "" {
		t.Fatal("the report is not listed")
	}
	admin.wantCode(422, "validation_failed", http.MethodPatch, "/api/v1/admin/abuse-reports/"+id, map[string]any{"status": "actioned"})
	got := admin.must(200, http.MethodPatch, "/api/v1/admin/abuse-reports/"+id, map[string]any{"status": "dismissed", "decision": "Sender opted in; no action"})
	if got["status"] != "dismissed" {
		t.Fatalf("review: %v", got)
	}
	adminClient(t, "billing_admin").wantCode(403, "insufficient_role", http.MethodGet, "/api/v1/admin/abuse-reports", nil)
	// Reports are rate-limited per address.
	limited := false
	for range 6 {
		if s, _ := anon.Do(http.MethodPost, "/api/v1/public/abuse-reports", map[string]any{"category": "other", "details": "repeat"}); s == 429 {
			limited = true
		}
	}
	if !limited {
		t.Fatal("abuse reports must be rate-limited")
	}
}
