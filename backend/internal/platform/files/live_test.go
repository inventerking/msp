package files

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// These tests use the real local services. Run with MSP_LIVE_INFRA=1 after `make up`.
func live(t *testing.T) {
	if os.Getenv("MSP_LIVE_INFRA") != "1" {
		t.Skip("set MSP_LIVE_INFRA=1 to test against the local Docker services")
	}
}

func TestSeaweedFSStore(t *testing.T) {
	live(t)
	ctx := context.Background()
	s := NewS3Store("http://127.0.0.1:58333", "dev_access_key", "dev_secret_key", "live-test")
	if err := s.EnsureBucket(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureBucket(ctx); err != nil {
		t.Fatal("ensuring an existing bucket should succeed:", err)
	}
	key := "tenants/" + uuid.NewString() + "/x"
	body := []byte("%PDF-1.7 live")
	if err := s.Put(ctx, key, body, "application/pdf"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, key)
	if err != nil || string(got) != string(body) {
		t.Fatalf("%v %q", err, got)
	}
	bad := NewS3Store("http://127.0.0.1:58333", "dev_access_key", "wrong-secret", "live-test")
	if _, err := bad.Get(ctx, key); err == nil {
		t.Fatal("wrong credentials could read an object")
	}
}

// The EICAR string is the standard harmless test file every scanner flags.
const eicar = `X5O!P%@AP[4\PZX54(P^)7CC)7}$EICAR-STANDARD-ANTIVIRUS-TEST-FILE!$H+H*`

func TestClamAV(t *testing.T) {
	live(t)
	if os.Getenv("MSP_LIVE_CLAMAV") != "1" {
		t.Skip("set MSP_LIVE_CLAMAV=1 once clamd has loaded its signatures")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	c := ClamAV{Addr: "127.0.0.1:53310"}
	clean, err := c.Scan(ctx, []byte("%PDF-1.7 a perfectly ordinary file"))
	if err != nil || !clean {
		t.Fatalf("clean file: %v %v", clean, err)
	}
	clean, err = c.Scan(ctx, []byte(eicar))
	if err != nil || clean {
		t.Fatalf("EICAR test file passed as clean: %v %v", clean, err)
	}
	big := []byte(strings.Repeat("a", 3<<20))
	if clean, err := c.Scan(ctx, big); err != nil || !clean {
		t.Fatalf("multi-chunk file: %v %v", clean, err)
	}
}
