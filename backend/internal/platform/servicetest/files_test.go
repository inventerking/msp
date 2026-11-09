package servicetest

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/jobs"
	"mspengine/internal/platform/files"
)

var pdfBytes = []byte("%PDF-1.7\nhello")

type scanner struct {
	clean bool
	err   error
}

func (s *scanner) Scan(context.Context, []byte) (bool, error) { return s.clean, s.err }

func fileSvc(s files.Scanner) (*files.Service, *files.MemoryStore) {
	store := &files.MemoryStore{}
	return files.New(env.Pools, store, s, "test-bucket"), store
}

func principal(t *testing.T) authctx.Principal {
	return authctx.Principal{TenantID: newTenant(t), UserID: uuid.New()}
}

func upload(t *testing.T, svc *files.Service, p authctx.Principal, purpose, name string, body []byte) uuid.UUID {
	t.Helper()
	id, err := svc.CreateUpload(ctx, p, purpose, name, int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Upload(ctx, p, id, body); err != nil {
		t.Fatal(err)
	}
	return id
}

func drainScans(t *testing.T, svc *files.Service) {
	t.Helper()
	r := jobs.NewRunner(env.Pools.System, "scan")
	r.BackoffBase, r.BackoffMax = time.Millisecond, time.Millisecond
	r.TenantLimit = 100
	r.Register(files.JobScan, svc.HandleScan)
	for range 20 {
		did, err := r.RunOne(ctx, jobs.QueueDefault)
		if err != nil {
			t.Fatal(err)
		}
		if !did {
			return
		}
	}
}

func errCode(err error) string {
	var e *apperr.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func TestFileIsUnusableUntilScannedClean(t *testing.T) {
	svc, _ := fileSvc(&scanner{clean: true})
	p := principal(t)
	id := upload(t, svc, p, "attachment", "report.pdf", pdfBytes)
	if _, err := svc.Download(ctx, p, id); errCode(err) != "file_not_ready" {
		t.Fatalf("got %v", err)
	}
	drainScans(t, svc)
	d, err := svc.Download(ctx, p, id)
	if err != nil || string(d.Body) != string(pdfBytes) || d.Name != "report.pdf" || d.ContentType != "application/pdf" {
		t.Fatalf("%v %+v", err, d)
	}
}

func TestInfectedFileIsQuarantined(t *testing.T) {
	svc, _ := fileSvc(&scanner{clean: false})
	p := principal(t)
	id := upload(t, svc, p, "attachment", "bad.pdf", pdfBytes)
	drainScans(t, svc)
	meta, err := svc.Meta(ctx, p, id)
	if err != nil || meta.Status != "quarantined" {
		t.Fatalf("%v %s", err, meta.Status)
	}
	if _, err := svc.Download(ctx, p, id); errCode(err) != "file_not_ready" {
		t.Fatalf("got %v", err)
	}
}

// If the scanner is down, files stay blocked. Failure must never mean clean.
func TestScannerOutageKeepsFileBlocked(t *testing.T) {
	for name, s := range map[string]files.Scanner{"failing": &scanner{err: errors.New("clamd down")}, "not configured": files.NoScanner{}} {
		t.Run(name, func(t *testing.T) {
			svc, _ := fileSvc(s)
			p := principal(t)
			id := upload(t, svc, p, "attachment", "x.pdf", pdfBytes)
			drainScans(t, svc)
			meta, err := svc.Meta(ctx, p, id)
			if err != nil || meta.Status != "scanning" {
				t.Fatalf("%v %s", err, meta.Status)
			}
		})
	}
}

func TestTypeComesFromTheBytes(t *testing.T) {
	svc, store := fileSvc(&scanner{clean: true})
	p := principal(t)
	for name, body := range map[string][]byte{
		"html named pdf":  []byte("<!DOCTYPE html><script>alert(1)</script>"),
		"executable":      {0x4d, 0x5a, 0x90, 0x00, 0x03, 0x00},
		"svg with script": []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`),
		"zip":             {0x50, 0x4b, 0x03, 0x04, 0x14, 0x00},
	} {
		t.Run(name, func(t *testing.T) {
			id, err := svc.CreateUpload(ctx, p, "logo", "innocent.png", int64(len(body)))
			if err != nil {
				t.Fatal(err)
			}
			if err := svc.Upload(ctx, p, id, body); errCode(err) != "file_type_not_allowed" {
				t.Fatalf("got %v", err)
			}
		})
	}
	if len(store.Keys()) != 0 {
		t.Fatal("a rejected file reached storage")
	}
}

func TestUploadLimits(t *testing.T) {
	svc, _ := fileSvc(&scanner{clean: true})
	p := principal(t)
	if _, err := svc.CreateUpload(ctx, p, "attachment", "big.pdf", files.MaxBytes+1); errCode(err) != "file_too_large" {
		t.Fatalf("got %v", err)
	}
	if _, err := svc.CreateUpload(ctx, p, "anything", "x.pdf", 10); errCode(err) != "validation_failed" {
		t.Fatalf("got %v", err)
	}
	id := upload(t, svc, p, "attachment", "x.pdf", pdfBytes)
	if err := svc.Upload(ctx, p, id, pdfBytes); errCode(err) != "invalid_transition" {
		t.Fatalf("a completed upload was overwritten: %v", err)
	}
}

func TestStorageKeyIgnoresTheFileName(t *testing.T) {
	svc, store := fileSvc(&scanner{clean: true})
	p := principal(t)
	upload(t, svc, p, "attachment", "../../../etc/passwd", pdfBytes)
	keys := store.Keys()
	if len(keys) != 1 || strings.Contains(keys[0], "..") || strings.Contains(keys[0], "passwd") || !strings.HasPrefix(keys[0], "tenants/"+p.TenantID.String()+"/") {
		t.Fatalf("keys %v", keys)
	}
	for in, want := range map[string]string{
		"../../../etc/passwd": "passwd", `..\..\windows\system32`: "system32", "a\r\nSet-Cookie: x.pdf": "a_Set-Cookie_ x.pdf", `quote".pdf`: "quote_.pdf", "": "file", "..": "file",
	} {
		if got := files.CleanName(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

func TestFilesAreTenantIsolated(t *testing.T) {
	svc, _ := fileSvc(&scanner{clean: true})
	a, b := principal(t), principal(t)
	id := upload(t, svc, a, "attachment", "private.pdf", pdfBytes)
	drainScans(t, svc)
	if _, err := svc.Download(ctx, b, id); errCode(err) != "not_found" {
		t.Fatalf("got %v", err)
	}
	if _, err := svc.Meta(ctx, b, id); errCode(err) != "not_found" {
		t.Fatalf("got %v", err)
	}
	if err := svc.Upload(ctx, b, id, pdfBytes); errCode(err) != "not_found" {
		t.Fatalf("got %v", err)
	}
	if _, err := svc.CreateGrant(ctx, b.TenantID, id, time.Hour, nil); errCode(err) != "not_found" {
		t.Fatalf("got %v", err)
	}
}

func TestDownloadGrants(t *testing.T) {
	svc, _ := fileSvc(&scanner{clean: true})
	p := principal(t)
	id := upload(t, svc, p, "attachment", "shared.pdf", pdfBytes)
	if _, err := svc.CreateGrant(ctx, p.TenantID, id, time.Hour, nil); errCode(err) != "file_not_ready" {
		t.Fatalf("an unscanned file was shared: %v", err)
	}
	drainScans(t, svc)
	limit := int32(2)
	token, err := svc.CreateGrant(ctx, p.TenantID, id, time.Hour, &limit)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := svc.RedeemGrant(ctx, token); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.RedeemGrant(ctx, token); errCode(err) != "not_found" {
		t.Fatalf("the download limit was exceeded: %v", err)
	}
	expired, err := svc.CreateGrant(ctx, p.TenantID, id, -time.Minute, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RedeemGrant(ctx, expired); errCode(err) != "not_found" {
		t.Fatalf("an expired link worked: %v", err)
	}
	for _, bad := range []string{"", "x", "a.b", token + "x", strings.Replace(token, ".", "x.", 1)} {
		if _, err := svc.RedeemGrant(ctx, bad); errCode(err) != "not_found" {
			t.Errorf("%q: %v", bad, err)
		}
	}
}
