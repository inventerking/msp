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

	"mspengine/internal/app"
	"mspengine/internal/kernel/crypto"
	"mspengine/internal/platform/files"
	"mspengine/internal/platform/integration"
)

// Rotating the encryption key re-encrypts every stored secret under the new key, and the system
// keeps working throughout: here the harness still verifies a webhook whose secret was rewritten.
func TestEncryptionKeyRotation(t *testing.T) {
	adminClient(t, "super_admin") // Enrols two-step verification, so an admin secret exists.
	ep := adminClient(t, "integration_admin").must(201, http.MethodPost, "/api/v1/admin/integrations/webhook-endpoints", map[string]any{"provider_key": "fake_webhook"})

	rotated := crypto.Keys{HMAC: bytes.Repeat([]byte{7}, 32), Encryption: rotatedKey, EncryptionID: "env-2", Previous: map[string][]byte{"env-1": bytes.Repeat([]byte{9}, 32)}}
	g, err := app.Build(graph.Pools, rotated, app.Options{Mailer: &fakeMailer{}, Store: &files.MemoryStore{}, Scanner: files.AllowAllScanner{}, Bucket: "test", BaseURL: "http://app.test"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	ids, err := g.Identity.Reencrypt(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ints, err := g.Integration.Reencrypt(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ids == 0 || ints == 0 {
		t.Fatalf("nothing re-encrypted: %d two-step, %d integration", ids, ints)
	}
	for _, table := range []string{"identity.mfa_factors", "identity.platform_admin_mfa_factors", "integration.platform_credentials", "integration.webhook_endpoints"} {
		rows, err := appConn(t, "msp_owner", "dev_owner").Query(ctx, "SELECT secret_ciphertext, key_id FROM "+table+" WHERE secret_ciphertext IS NOT NULL")
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var c []byte
			var id *string
			if err := rows.Scan(&c, &id); err != nil {
				t.Fatal(err)
			}
			if crypto.KeyIDOf(c) != "env-2" || id == nil || *id != "env-2" {
				t.Fatalf("%s still has a value under %s (column %v)", table, crypto.KeyIDOf(c), id)
			}
		}
		rows.Close()
	}
	if again, _ := g.Identity.Reencrypt(ctx); again != 0 {
		t.Fatalf("a second run should change nothing, changed %d", again)
	}

	// The rewritten webhook secret still verifies a signed request.
	body := []byte(`{"id":"rotation"}`)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	req, _ := http.NewRequest(http.MethodPost, env.URL+"/api/v1/webhooks/email/"+ep["path_token"].(string), bytes.NewReader(body))
	req.Header.Set("X-Webhook-Timestamp", ts)
	req.Header.Set("X-Webhook-Signature", hex.EncodeToString(integration.SignHMAC(ep["secret"].(string), ts, body)))
	req.Header.Set("X-Webhook-Id", "rotation-1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("webhook after rotation: %d", resp.StatusCode)
	}
}

// Each erasure is logged outside the database before it runs, and can be replayed after a restore.
func TestErasureIsLoggedAndReplayable(t *testing.T) {
	c, tenant := crmTenant(t, "Erasure Log")
	person := c.must(201, http.MethodPost, "/api/v1/crm/contacts", map[string]any{"full_name": "Robin Restore", "email": "robin@erasure-log.example"})
	contact := person["contact_id"].(string)
	c.must(202, http.MethodPost, "/api/v1/privacy/contacts/"+contact+"/erase", nil)
	work(t, "default")

	keys, err := objects.List(context.Background(), "erasure-log/"+time.Now().UTC().Format("2006-01-02")+"/")
	if err != nil {
		t.Fatal(err)
	}
	var logged string
	for _, k := range keys {
		if strings.Contains(k, tenant+"_"+contact) {
			logged = k
		}
	}
	if logged == "" {
		t.Fatalf("erasure not logged: %v", keys)
	}
	raw, _ := objects.Get(context.Background(), logged)
	if strings.Contains(string(raw), "robin") || strings.Contains(string(raw), "Robin") {
		t.Fatal("the erasure log must hold identifiers only")
	}

	r, err := graph.Shared.ReplayErasures(context.Background(), time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if r.Logged == 0 || r.Queued == 0 {
		t.Fatalf("nothing replayed: %+v", r)
	}
	work(t, "default") // Erasing again is harmless.
	c.wantCode(404, "not_found", http.MethodGet, "/api/v1/crm/contacts/"+person["id"].(string), nil)
}
