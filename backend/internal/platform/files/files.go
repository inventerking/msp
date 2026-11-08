// Package files stores tenant files. Objects stay in a private bucket, and bytes pass through the
// API, so the bucket is never exposed. A file cannot be used until a malware scan marks it clean.
package files

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/crypto"
	"mspengine/internal/kernel/db"
	"mspengine/internal/kernel/events"
	"mspengine/internal/kernel/jobs"
	"mspengine/internal/platform/audit"
	"mspengine/internal/platform/files/repo"
)

// MaxBytes is the largest upload accepted.
const MaxBytes = 20 << 20

// JobScan is the job kind that scans an uploaded file.
const JobScan = "files.scan"

// Purpose says what a file is for, and so which types it may be. Types are checked from the bytes,
// because a client's claim about its own file means nothing.
var purposes = map[string][]string{
	"attachment":        {"application/pdf", "image/png", "image/jpeg", "text/plain", "text/csv"},
	"csv_import":        {"text/plain", "text/csv"},
	"chatbot_knowledge": {"application/pdf", "text/plain"},
	"logo":              {"image/png", "image/jpeg"},
}

// Store holds object bytes.
type Store interface {
	Put(ctx context.Context, key string, body []byte, contentType string) error
	Get(ctx context.Context, key string) ([]byte, error)
}

// Scanner checks bytes for malware.
type Scanner interface {
	Scan(ctx context.Context, body []byte) (clean bool, err error)
}

// Service implements file storage.
type Service struct {
	pools   *db.Pools
	store   Store
	scanner Scanner
	bucket  string
}

// New creates the service.
func New(pools *db.Pools, store Store, scanner Scanner, bucket string) *Service {
	return &Service{pools: pools, store: store, scanner: scanner, bucket: bucket}
}

var unsafeName = func(r rune) bool { return unicode.IsControl(r) || strings.ContainsRune(`/\"<>:|?*`, r) }

// CleanName keeps a file name for display only. It is never used in a storage path.
func CleanName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, `\`, "/"))
	name = strings.Join(strings.FieldsFunc(name, unsafeName), "_")
	if len(name) > 200 {
		name = name[:200]
	}
	if name == "" || name == "." || name == ".." {
		return "file"
	}
	return name
}

// CreateUpload records an intended upload. The storage key is generated, never taken from input.
func (s *Service) CreateUpload(ctx context.Context, p authctx.Principal, purpose, name string, size int64) (uuid.UUID, error) {
	if _, ok := purposes[purpose]; !ok {
		return uuid.Nil, apperr.Validation(apperr.FieldError{Field: "purpose", Code: "unknown_purpose"})
	}
	if size <= 0 || size > MaxBytes {
		return uuid.Nil, apperr.New(http.StatusRequestEntityTooLarge, "file_too_large", "That file is too large")
	}
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, err
	}
	clean := CleanName(name)
	err = db.TenantTx(ctx, s.pools.App, p.TenantID, p.UserID, func(tx pgx.Tx) error {
		_, err := repo.New(tx).CreateObject(ctx, repo.CreateObjectParams{ID: id, TenantID: &p.TenantID, Bucket: s.bucket, ObjectKey: "tenants/" + p.TenantID.String() + "/" + id.String(), ContentType: "application/octet-stream", OriginalName: &clean, OwnerModule: purpose, SizeBytes: &size, UploadedBy: &p.UserID})
		return err
	})
	return id, err
}

// detect finds the real type from the bytes. CSV has no signature, so it reads as plain text, and
// it is accepted only when the declared purpose allows text.
func detect(body []byte) string {
	t := http.DetectContentType(body)
	t, _, _ = strings.Cut(t, ";")
	return strings.TrimSpace(t)
}

// Upload stores the bytes, and queues a scan. The file stays unusable until the scan passes.
func (s *Service) Upload(ctx context.Context, p authctx.Principal, id uuid.UUID, body []byte) error {
	var obj repo.GetObjectRow
	err := db.TenantTx(ctx, s.pools.App, p.TenantID, p.UserID, func(tx pgx.Tx) error {
		var err error
		obj, err = repo.New(tx).GetObject(ctx, repo.GetObjectParams{TenantID: &p.TenantID, ID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound()
		}
		return err
	})
	if err != nil {
		return err
	}
	if obj.Status != "pending_upload" {
		return apperr.Conflict("invalid_transition", "This upload has already been completed")
	}
	if int64(len(body)) > MaxBytes {
		return apperr.New(http.StatusRequestEntityTooLarge, "file_too_large", "That file is too large")
	}
	if len(body) == 0 {
		return apperr.Validation(apperr.FieldError{Field: "body", Code: "empty_file"})
	}
	actual := detect(body)
	allowed := purposes[obj.OwnerModule]
	if !slices.Contains(allowed, actual) {
		return apperr.New(http.StatusUnprocessableEntity, "file_type_not_allowed", "That type of file is not allowed here")
	}
	if err := s.store.Put(ctx, obj.ObjectKey, body, actual); err != nil {
		return fmt.Errorf("store object: %w", err)
	}
	sum := sha256.Sum256(body)
	size := int64(len(body))
	return db.TenantTx(ctx, s.pools.App, p.TenantID, p.UserID, func(tx pgx.Tx) error {
		q := repo.New(tx)
		n, err := q.MarkUploaded(ctx, repo.MarkUploadedParams{TenantID: &p.TenantID, ID: id, Sha256: sum[:], SizeBytes: &size, ContentType: actual})
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.Conflict("invalid_transition", "This upload has already been completed")
		}
		if _, _, err := jobs.Enqueue(ctx, tx, jobs.Spec{TenantID: &p.TenantID, Kind: JobScan, Payload: map[string]any{"object_id": id}, IdempotencyKey: "scan:" + id.String(), MaxAttempts: 10}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{TenantID: &p.TenantID, ActorType: "user", ActorID: &p.UserID, Action: "file.uploaded", ResourceType: "file", ResourceID: &id, After: map[string]any{"size": size, "type": actual}})
	})
}

// HandleScan is the scan job. If the scanner is down the job retries, and the file stays unusable.
func (s *Service) HandleScan(ctx context.Context, j jobs.Job) error {
	var p struct {
		ObjectID uuid.UUID `json:"object_id"`
	}
	if err := j.Decode(&p); err != nil {
		return err
	}
	if j.TenantID == nil {
		return jobs.Permanent(errors.New("scan job has no tenant"))
	}
	tenantID := *j.TenantID
	var obj repo.GetObjectRow
	err := db.TenantTx(ctx, s.pools.App, tenantID, uuid.Nil, func(tx pgx.Tx) error {
		var err error
		obj, err = repo.New(tx).GetObject(ctx, repo.GetObjectParams{TenantID: &tenantID, ID: p.ObjectID})
		return err
	})
	if err != nil {
		return err
	}
	body, err := s.store.Get(ctx, obj.ObjectKey)
	if err != nil {
		return fmt.Errorf("read object for scan: %w", err)
	}
	clean, err := s.scanner.Scan(ctx, body)
	if err != nil {
		return fmt.Errorf("scan: %w", err)
	}
	status, eventType := "clean", "files.object.ready"
	if !clean {
		status, eventType = "quarantined", "files.object.quarantined"
	}
	return db.TenantTx(ctx, s.pools.App, tenantID, uuid.Nil, func(tx pgx.Tx) error {
		q := repo.New(tx)
		n, err := q.SetObjectStatus(ctx, repo.SetObjectStatusParams{TenantID: &tenantID, ID: p.ObjectID, Status: status})
		if err != nil {
			return err
		}
		if n == 0 {
			return nil // Already decided by an earlier attempt.
		}
		if !clean {
			if err := audit.Record(ctx, tx, audit.Event{TenantID: &tenantID, ActorType: "system", Action: "file.quarantined", ResourceType: "file", ResourceID: &p.ObjectID}); err != nil {
				return err
			}
		}
		return events.Append(ctx, tx, events.Event{Type: eventType, TenantID: &tenantID, SourceModule: "files", AggregateType: "file", AggregateID: p.ObjectID}, map[string]any{"object_id": p.ObjectID, "owner_module": obj.OwnerModule})
	})
}

// Meta returns a file's details, under tenant RLS.
func (s *Service) Meta(ctx context.Context, p authctx.Principal, id uuid.UUID) (repo.GetObjectRow, error) {
	var obj repo.GetObjectRow
	err := db.TenantTx(ctx, s.pools.App, p.TenantID, p.UserID, func(tx pgx.Tx) error {
		var err error
		obj, err = repo.New(tx).GetObject(ctx, repo.GetObjectParams{TenantID: &p.TenantID, ID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound()
		}
		return err
	})
	return obj, err
}

// Download is a file ready to serve.
type Download struct {
	Name, ContentType string
	Body              []byte
}

func notReady() *apperr.Error {
	return apperr.Conflict("file_not_ready", "This file is not available yet")
}

// Download returns a clean file for the caller's tenant.
func (s *Service) Download(ctx context.Context, p authctx.Principal, id uuid.UUID) (Download, error) {
	obj, err := s.Meta(ctx, p, id)
	if err != nil {
		return Download{}, err
	}
	return s.read(ctx, obj)
}

func (s *Service) read(ctx context.Context, obj repo.GetObjectRow) (Download, error) {
	if obj.Status != "clean" {
		return Download{}, notReady()
	}
	body, err := s.store.Get(ctx, obj.ObjectKey)
	if err != nil {
		return Download{}, fmt.Errorf("read object: %w", err)
	}
	name := "file"
	if obj.OriginalName != nil {
		name = *obj.OriginalName
	}
	return Download{Name: name, ContentType: obj.ContentType, Body: body}, nil
}

// CreateGrant makes a time-limited link token for sharing outside a session. The token carries the
// tenant ID, so redemption runs under that tenant's RLS. Only its hash is stored.
func (s *Service) CreateGrant(ctx context.Context, tenantID, objectID uuid.UUID, ttl time.Duration, maxDownloads *int32) (string, error) {
	secret, _, err := crypto.NewToken()
	if err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(tenantID[:]) + "." + secret
	err = db.TenantTx(ctx, s.pools.App, tenantID, uuid.Nil, func(tx pgx.Tx) error {
		q := repo.New(tx)
		obj, err := q.GetObject(ctx, repo.GetObjectParams{TenantID: &tenantID, ID: objectID})
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		if obj.Status != "clean" {
			return notReady()
		}
		policy, err := q.GetPolicyID(ctx, "signed_link")
		if err != nil {
			return err
		}
		return q.CreateGrant(ctx, repo.CreateGrantParams{TenantID: &tenantID, ObjectID: objectID, PolicyID: policy, TokenHash: crypto.HashToken(token), ExpiresAt: time.Now().Add(ttl), MaxDownloads: maxDownloads})
	})
	return token, err
}

// RedeemGrant serves a file from a link token. Every failure looks the same, to prevent probing.
func (s *Service) RedeemGrant(ctx context.Context, token string) (Download, error) {
	head, _, ok := strings.Cut(token, ".")
	b, err := base64.RawURLEncoding.DecodeString(head)
	if !ok || err != nil {
		return Download{}, apperr.NotFound()
	}
	tenantID, err := uuid.FromBytes(b)
	if err != nil {
		return Download{}, apperr.NotFound()
	}
	var obj repo.GetObjectRow
	err = db.TenantTx(ctx, s.pools.App, tenantID, uuid.Nil, func(tx pgx.Tx) error {
		q := repo.New(tx)
		id, err := q.UseGrant(ctx, repo.UseGrantParams{TenantID: &tenantID, TokenHash: crypto.HashToken(token)})
		if err != nil {
			return err
		}
		obj, err = q.GetObject(ctx, repo.GetObjectParams{TenantID: &tenantID, ID: id})
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Download{}, apperr.NotFound()
	}
	if err != nil {
		return Download{}, err
	}
	return s.read(ctx, obj)
}

// MemoryStore keeps objects in memory, for tests.
type MemoryStore struct {
	mu   sync.Mutex
	data map[string][]byte
}

// Put stores bytes.
func (m *MemoryStore) Put(_ context.Context, key string, body []byte, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.data == nil {
		m.data = map[string][]byte{}
	}
	m.data[key] = bytes.Clone(body)
	return nil
}

// Get returns bytes.
func (m *MemoryStore) Get(_ context.Context, key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.data[key]
	if !ok {
		return nil, errors.New("no such object")
	}
	return b, nil
}

// Keys lists stored keys.
func (m *MemoryStore) Keys() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for k := range m.data {
		out = append(out, k)
	}
	return out
}

