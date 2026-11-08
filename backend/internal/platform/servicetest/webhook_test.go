package servicetest

import (
	"context"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/db"
	"mspengine/internal/kernel/jobs"
	"mspengine/internal/platform/integration"
)

func signed(secret string, body []byte, at time.Time) http.Header {
	h := http.Header{}
	ts := strconv.FormatInt(at.Unix(), 10)
	h.Set("X-Webhook-Timestamp", ts)
	h.Set("X-Webhook-Signature", hex.EncodeToString(integration.SignHMAC(secret, ts, body)))
	return h
}

func TestWebhookInbox(t *testing.T) {
	ten := newTenant(t)
	svc := integration.New(env.Pools, keys)
	admin := authctx.AdminPrincipal{AdminID: uuid.New()}
	_, token, secret, err := svc.CreateEndpoint(ctx, admin, &ten, "fake_webhook")
	if err != nil {
		t.Fatal(err)
	}
	var calls int
	var gotTenant *uuid.UUID
	var gotBody string
	svc.RegisterWebhook("fake_webhook", integration.VerifyHMAC, func(_ context.Context, tenantID *uuid.UUID, payload []byte) error {
		calls++
		gotTenant, gotBody = tenantID, string(payload)
		return nil
	})
	body := []byte(`{"tenant_id":"` + uuid.NewString() + `","x":1}`)
	unauth := func(err error) bool {
		var e *apperr.Error
		return errors.As(err, &e) && e.Code == "signature_invalid"
	}

	if err := svc.ReceiveWebhook(ctx, token, http.Header{}, body, "e1"); !unauth(err) {
		t.Fatalf("unsigned: %v", err)
	}
	if err := svc.ReceiveWebhook(ctx, token, signed("wrong-secret", body, time.Now()), body, "e1"); !unauth(err) {
		t.Fatalf("wrong secret: %v", err)
	}
	if err := svc.ReceiveWebhook(ctx, token, signed(secret, []byte("other body"), time.Now()), body, "e1"); !unauth(err) {
		t.Fatalf("altered body: %v", err)
	}
	if err := svc.ReceiveWebhook(ctx, token, signed(secret, body, time.Now().Add(-time.Hour)), body, "e1"); !unauth(err) {
		t.Fatalf("old timestamp: %v", err)
	}
	if err := svc.ReceiveWebhook(ctx, "no-such-token", signed(secret, body, time.Now()), body, "e1"); !unauth(err) {
		t.Fatalf("unknown token: %v", err)
	}
	var stored int
	if err := env.Pools.System.QueryRow(ctx, `SELECT count(*) FROM integration.webhook_deliveries WHERE tenant_id = $1`, ten).Scan(&stored); err != nil || stored != 0 {
		t.Fatalf("a rejected webhook was stored: %d %v", stored, err)
	}
	var failures int
	if err := env.Super.QueryRow(ctx, `SELECT count(*) FROM audit.security_events WHERE event_type = 'webhook.signature_failed' AND tenant_id = $1`, ten).Scan(&failures); err != nil || failures != 4 {
		t.Fatalf("security events %d %v", failures, err)
	}

	// A genuine delivery, sent three times, is stored and processed once.
	for range 3 {
		if err := svc.ReceiveWebhook(ctx, token, signed(secret, body, time.Now()), body, "e1"); err != nil {
			t.Fatal(err)
		}
	}
	r := jobs.NewRunner(env.Pools.System, "wh")
	r.Register(integration.JobWebhookDispatch, svc.HandleWebhookDispatch)
	runJobs(t, r, jobs.QueueCritical)
	if calls != 1 {
		t.Fatalf("handler ran %d times", calls)
	}
	// The tenant comes from the endpoint record, not from a value in the payload.
	if gotTenant == nil || *gotTenant != ten {
		t.Fatalf("tenant %v", gotTenant)
	}
	if gotBody == "" {
		t.Fatal("payload lost")
	}
}

func TestWebhookEndpointsAreHiddenFromTenants(t *testing.T) {
	ten := newTenant(t)
	err := db.TenantTx(ctx, env.Pools.App, ten, uuid.Nil, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `SELECT * FROM integration.webhook_endpoints`)
		return err
	})
	if err == nil {
		t.Fatal("a tenant role can read webhook secrets")
	}
}

func TestCredentialsAreEncryptedAndNotReadableByTenantRole(t *testing.T) {
	svc := integration.New(env.Pools, keys)
	var provider uuid.UUID
	if err := env.Super.QueryRow(ctx, `SELECT id FROM integration.providers WHERE key = 'fake_ai'`).Scan(&provider); err != nil {
		t.Fatal(err)
	}
	const secret = "sk-live-very-secret-value"
	a := authctx.AdminPrincipal{AdminID: uuid.New()}
	if err := svc.SetCredential(ctx, a, provider, "api_key", secret); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Credential(ctx, provider, "api_key")
	if err != nil || got != secret {
		t.Fatalf("%q %v", got, err)
	}
	var stored []byte
	if err := env.Super.QueryRow(ctx, `SELECT secret_ciphertext FROM integration.platform_credentials WHERE provider_id = $1`, provider).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if contains(stored, secret) {
		t.Fatal("the credential is stored in clear text")
	}
	if _, err := env.Pools.App.Exec(ctx, `SELECT * FROM integration.platform_credentials`); err == nil {
		t.Fatal("the tenant role can read provider credentials")
	}
	views, err := svc.ListProviders(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, v := range views {
		found = found || (v.Key == "fake_ai" && v.HasCredential)
	}
	if !found {
		t.Fatal("the provider list should show that a credential exists")
	}
}

func contains(haystack []byte, needle string) bool {
	return len(needle) > 0 && string(haystack) != "" && indexOf(string(haystack), needle) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
