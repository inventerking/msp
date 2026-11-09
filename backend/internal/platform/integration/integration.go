// Package integration owns providers, credentials and the webhook inbox. Business modules know
// capabilities, not vendors: only adapters know a vendor exists.
package integration

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/crypto"
	"mspengine/internal/kernel/db"
	"mspengine/internal/kernel/jobs"
	"mspengine/internal/platform/audit"
	"mspengine/internal/platform/integration/repo"
)

// Class is the common error classification every adapter maps its vendor's errors to.
type Class int

const (
	Retryable Class = iota
	Permanent
	RateLimited
	AuthFailed
	NotFound
)

// ProviderError is what adapters return.
type ProviderError struct {
	Class      Class
	RetryAfter time.Duration
	StatusCode int
	Err        error
}

func (e *ProviderError) Error() string { return e.Err.Error() }
func (e *ProviderError) Unwrap() error { return e.Err }

// ErrCircuitOpen means a provider is failing, so the call was not attempted.
var ErrCircuitOpen = errors.New("provider circuit open")

// ToJobError maps a provider failure to what a job should do next. A rate limit or an open circuit
// reschedules without using an attempt, because the job itself did nothing wrong.
func ToJobError(err error) error {
	if errors.Is(err, ErrCircuitOpen) {
		return jobs.Later(time.Minute, err)
	}
	var pe *ProviderError
	if errors.As(err, &pe) {
		switch pe.Class {
		case RateLimited:
			after := pe.RetryAfter
			if after <= 0 {
				after = time.Minute
			}
			return jobs.Later(after, err)
		case Permanent, NotFound, AuthFailed:
			return jobs.Permanent(err)
		}
	}
	return err
}

// breaker opens after repeated failures, and lets one trial through after a cooldown.
type breaker struct {
	failures int
	openedAt time.Time
	trial    bool
}

// Service implements provider management and calls.
type Service struct {
	pools     *db.Pools
	keys      crypto.Keys
	mu        sync.Mutex
	breakers  map[string]*breaker
	Threshold int
	Cooldown  time.Duration
	verifiers map[string]Verifier
	handlers  map[string]WebhookHandler
	companies []CompanyData
}

// ErrNoProvider means no enabled provider offers the capability.
var ErrNoProvider = errors.New("no enabled provider for this capability")

// RegisterCompanyData adds a company data adapter. The first one whose provider row is enabled is used.
func (s *Service) RegisterCompanyData(p CompanyData) { s.companies = append(s.companies, p) }

// CompanyData returns the company data adapter to use, with its provider ID for call logs.
func (s *Service) CompanyData(ctx context.Context) (CompanyData, uuid.UUID, error) {
	for _, c := range s.companies {
		var row repo.GetProviderByKeyRow
		err := pgx.BeginFunc(ctx, s.pools.App, func(tx pgx.Tx) error {
			var err error
			row, err = repo.New(tx).GetProviderByKey(ctx, c.Key())
			return err
		})
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, uuid.Nil, err
		}
		if row.Status == "enabled" && row.Kind == "company_data" {
			return c, row.ID, nil
		}
	}
	return nil, uuid.Nil, ErrNoProvider
}

// New creates the service.
func New(pools *db.Pools, keys crypto.Keys) *Service {
	return &Service{pools: pools, keys: keys, breakers: map[string]*breaker{}, Threshold: 5, Cooldown: 30 * time.Second, verifiers: map[string]Verifier{}, handlers: map[string]WebhookHandler{}}
}

func (s *Service) allow(provider string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	b := s.breakers[provider]
	if b == nil || b.failures < s.Threshold {
		return true
	}
	if time.Since(b.openedAt) < s.Cooldown || b.trial {
		return false
	}
	b.trial = true
	return true
}

func (s *Service) report(provider string, failed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b := s.breakers[provider]
	if b == nil {
		b = &breaker{}
		s.breakers[provider] = b
	}
	if !failed {
		*b = breaker{}
		return
	}
	b.failures++
	b.trial = false
	if b.failures >= s.Threshold {
		b.openedAt = time.Now()
	}
}

// CircuitOpen reports whether a provider is currently being skipped.
func (s *Service) CircuitOpen(provider string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	b := s.breakers[provider]
	return b != nil && b.failures >= s.Threshold && time.Since(b.openedAt) < s.Cooldown
}

// Call runs one provider operation with a timeout and a circuit breaker, and logs its metadata.
// Bodies are never logged, because they can hold personal data.
func (s *Service) Call(ctx context.Context, providerKey string, providerID uuid.UUID, tenantID *uuid.UUID, operation string, timeout time.Duration, fn func(context.Context) error) error {
	if !s.allow(providerKey) {
		s.log(ctx, providerID, tenantID, operation, "circuit_open", nil, 0)
		return ErrCircuitOpen
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	start := time.Now()
	err := fn(cctx)
	latency := time.Since(start)
	outcome := "success"
	var status *int32
	failed := false
	if err != nil {
		outcome, failed = "retryable_error", true
		var pe *ProviderError
		if errors.As(err, &pe) {
			if pe.StatusCode != 0 {
				code := int32(pe.StatusCode) //nolint:gosec // HTTP status codes fit.
				status = &code
			}
			switch pe.Class {
			case RateLimited:
				outcome, failed = "rate_limited", false // The provider is healthy, and only asks us to wait.
			case Permanent, NotFound, AuthFailed:
				outcome, failed = "permanent_error", false // A bad request says nothing about provider health.
			}
		}
	}
	s.report(providerKey, failed)
	s.log(ctx, providerID, tenantID, operation, outcome, status, latency)
	return err
}

func (s *Service) log(ctx context.Context, providerID uuid.UUID, tenantID *uuid.UUID, operation, outcome string, status *int32, latency time.Duration) {
	var corr *uuid.UUID
	if m := authctx.MetaFrom(ctx); m.RequestID != uuid.Nil {
		corr = &m.RequestID
	}
	err := repo.New(s.pools.System).InsertCallLog(context.WithoutCancel(ctx), repo.InsertCallLogParams{TenantID: tenantID, ProviderID: providerID, Operation: operation, Outcome: outcome, StatusCode: status, LatencyMs: int32(latency.Milliseconds()), CorrelationID: corr}) //nolint:gosec // Latency fits.
	if err != nil {
		slog.WarnContext(ctx, "record provider call failed", "error", err)
	}
}

// --- Credentials ---

func credAAD(providerID uuid.UUID, label string) []byte {
	return []byte(providerID.String() + "|" + label)
}

// SetCredential stores a provider secret encrypted. No API ever returns one.
func (s *Service) SetCredential(ctx context.Context, a authctx.AdminPrincipal, providerID uuid.UUID, label, secret string) error {
	sealed, err := s.keys.Encrypt([]byte(secret), credAAD(providerID, label))
	if err != nil {
		return err
	}
	return db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		err := repo.New(tx).UpsertCredential(ctx, repo.UpsertCredentialParams{ProviderID: providerID, Label: label, SecretCiphertext: sealed, KeyID: crypto.KeyID})
		var pgErr interface{ SQLState() string }
		if errors.As(err, &pgErr) && pgErr.SQLState() == "23503" {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{ActorType: "platform_admin", ActorID: &a.AdminID, Action: "provider.credential_set", ResourceType: "provider", ResourceID: &providerID, After: map[string]any{"label": label}})
	})
}

// Credential decrypts a provider secret for an adapter. Adapters must not log it or pass it to AI.
func (s *Service) Credential(ctx context.Context, providerID uuid.UUID, label string) (string, error) {
	row, err := repo.New(s.pools.Platform).GetCredential(ctx, repo.GetCredentialParams{ProviderID: providerID, Label: label})
	if err != nil {
		return "", err
	}
	plain, err := s.keys.Decrypt(row.SecretCiphertext, credAAD(providerID, label))
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// ProviderView is a provider for the admin console, with no secret material.
type ProviderView struct {
	ID            uuid.UUID
	Key, Kind     string
	Name, Status  string
	HasCredential bool
	Circuit       bool
	Calls24h      map[string]int64
}

// ListProviders returns providers with recent health.
func (s *Service) ListProviders(ctx context.Context) ([]ProviderView, error) {
	var out []ProviderView
	err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		q := repo.New(tx)
		rows, err := q.ListProviders(ctx)
		if err != nil {
			return err
		}
		health, err := q.ProviderHealth(ctx, time.Now().Add(-24*time.Hour))
		if err != nil {
			return err
		}
		for _, r := range rows {
			v := ProviderView{ID: r.ID, Key: r.Key, Kind: r.Kind, Name: r.Name, Status: r.Status, Circuit: s.CircuitOpen(r.Key), Calls24h: map[string]int64{}}
			n, err := q.HasCredential(ctx, r.ID)
			if err != nil {
				return err
			}
			v.HasCredential = n > 0
			for _, h := range health {
				if h.ProviderID == r.ID {
					v.Calls24h[h.Outcome] = h.Calls
				}
			}
			out = append(out, v)
		}
		return nil
	})
	return out, err
}

// SetProviderStatus enables or disables a provider.
func (s *Service) SetProviderStatus(ctx context.Context, a authctx.AdminPrincipal, id uuid.UUID, status string) error {
	return db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		n, err := repo.New(tx).SetProviderStatus(ctx, repo.SetProviderStatusParams{ID: id, Status: status})
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.NotFound()
		}
		return audit.Record(ctx, tx, audit.Event{ActorType: "platform_admin", ActorID: &a.AdminID, Action: "provider.status_changed", ResourceType: "provider", ResourceID: &id, After: map[string]any{"status": status}})
	})
}

// --- Webhooks ---

// Verifier checks a provider's signature. Each real adapter supplies its vendor's scheme.
type Verifier func(secret string, header http.Header, body []byte, now time.Time) error

// WebhookHandler processes a verified delivery in a job. It returns the provider's event ID.
type WebhookHandler func(ctx context.Context, tenantID *uuid.UUID, payload []byte) error

// EventIDFunc extracts the provider's event ID, which is what makes a repeat harmless.
type EventIDFunc func(header http.Header, body []byte) string

// RegisterWebhook connects a provider to its signature check and its handler.
func (s *Service) RegisterWebhook(provider string, v Verifier, h WebhookHandler) {
	s.verifiers[provider] = v
	s.handlers[provider] = h
}

// VerifyHMAC is the default signature scheme: HMAC-SHA256 over "timestamp.body", with a limit on
// the timestamp's age, so that a captured request cannot be replayed later.
func VerifyHMAC(secret string, header http.Header, body []byte, now time.Time) error {
	ts := header.Get("X-Webhook-Timestamp")
	sig, err := hex.DecodeString(header.Get("X-Webhook-Signature"))
	if ts == "" || err != nil {
		return errors.New("missing signature")
	}
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return errors.New("bad timestamp")
	}
	if age := now.Sub(time.Unix(sec, 0)); age > 5*time.Minute || age < -5*time.Minute {
		return errors.New("timestamp out of range")
	}
	if !hmac.Equal(sig, SignHMAC(secret, ts, body)) {
		return errors.New("signature mismatch")
	}
	return nil
}

// SignHMAC computes the default signature.
func SignHMAC(secret, ts string, body []byte) []byte {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(ts))
	m.Write([]byte{'.'})
	m.Write(body)
	return m.Sum(nil)
}

// JobWebhookDispatch is the job kind that processes a stored webhook.
const JobWebhookDispatch = "integration.webhook_dispatch"

// MaxWebhookBytes is checked before any other work on a request.
const MaxWebhookBytes = 1 << 20

// CreateEndpoint creates a webhook endpoint, and returns its URL token and secret once.
func (s *Service) CreateEndpoint(ctx context.Context, a authctx.AdminPrincipal, tenantID *uuid.UUID, providerKey string) (uuid.UUID, string, string, error) {
	token, _, err := crypto.NewToken()
	if err != nil {
		return uuid.Nil, "", "", err
	}
	secret, _, err := crypto.NewToken()
	if err != nil {
		return uuid.Nil, "", "", err
	}
	sealed, err := s.keys.Encrypt([]byte(secret), []byte(token))
	if err != nil {
		return uuid.Nil, "", "", err
	}
	var id uuid.UUID
	err = db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		q := repo.New(tx)
		p, err := q.GetProviderByKey(ctx, providerKey)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.Validation(apperr.FieldError{Field: "provider_key", Code: "unknown_provider"})
		}
		if err != nil {
			return err
		}
		id, err = q.CreateWebhookEndpoint(ctx, repo.CreateWebhookEndpointParams{TenantID: tenantID, ProviderID: p.ID, PathToken: token, SecretCiphertext: sealed, KeyID: crypto.KeyID})
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{TenantID: tenantID, ActorType: "platform_admin", ActorID: &a.AdminID, Action: "webhook_endpoint.created", ResourceType: "webhook_endpoint", ResourceID: &id, After: map[string]any{"provider": providerKey}})
	})
	return id, token, secret, err
}

// ReceiveWebhook verifies a request, stores it and queues processing. The tenant comes only from
// the endpoint record, never from the payload. It answers quickly, so a slow module cannot make the
// provider retry. Store first and process later also means a crash loses nothing.
func (s *Service) ReceiveWebhook(ctx context.Context, pathToken string, header http.Header, body []byte, eventID string) error {
	unauthorized := apperr.New(http.StatusUnauthorized, "signature_invalid", "Signature check failed")
	q := repo.New(s.pools.System)
	ep, err := q.GetWebhookEndpoint(ctx, pathToken)
	if errors.Is(err, pgx.ErrNoRows) {
		return unauthorized // The same answer as a bad signature, so tokens cannot be probed.
	}
	if err != nil {
		return err
	}
	secret, err := s.keys.Decrypt(ep.SecretCiphertext, []byte(pathToken))
	if err != nil {
		return err
	}
	verify, ok := s.verifiers[ep.ProviderKey]
	if !ok {
		verify = VerifyHMAC
	}
	if err := verify(string(secret), header, body, time.Now()); err != nil {
		if serr := audit.Security(ctx, s.pools.App, "webhook.signature_failed", "warning", ep.TenantID, map[string]any{"provider": ep.ProviderKey}); serr != nil {
			slog.ErrorContext(ctx, "record security event failed", "error", serr)
		}
		return unauthorized
	}
	sum := sha256.Sum256(body)
	return pgx.BeginFunc(ctx, s.pools.System, func(tx pgx.Tx) error {
		tq := repo.New(tx)
		var evt *string
		if eventID != "" {
			evt = &eventID
		}
		id, err := tq.InsertWebhookDelivery(ctx, repo.InsertWebhookDeliveryParams{TenantID: ep.TenantID, EndpointID: ep.ID, ProviderEventID: evt, PayloadHash: sum[:], Payload: body})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // A repeat of an event already stored. Providers expect success here.
		}
		if err != nil {
			return err
		}
		_, _, err = jobs.Enqueue(ctx, tx, jobs.Spec{TenantID: ep.TenantID, Kind: JobWebhookDispatch, Queue: jobs.QueueCritical, Payload: map[string]any{"delivery_id": id}, IdempotencyKey: "webhook:" + id.String()})
		return err
	})
}

// HandleWebhookDispatch runs the provider's handler for a stored delivery.
func (s *Service) HandleWebhookDispatch(ctx context.Context, j jobs.Job) error {
	var p struct {
		DeliveryID uuid.UUID `json:"delivery_id"`
	}
	if err := j.Decode(&p); err != nil {
		return err
	}
	q := repo.New(s.pools.System)
	d, err := q.GetWebhookDelivery(ctx, p.DeliveryID)
	if err != nil {
		return err
	}
	h, ok := s.handlers[d.ProviderKey]
	if !ok {
		return jobs.Permanent(fmt.Errorf("no webhook handler for %s", d.ProviderKey))
	}
	if err := h(ctx, d.TenantID, d.Payload); err != nil {
		msg := err.Error()
		if serr := q.SetWebhookDeliveryStatus(ctx, repo.SetWebhookDeliveryStatusParams{ID: d.ID, Status: "failed", LastError: &msg}); serr != nil {
			slog.ErrorContext(ctx, "record webhook failure failed", "error", serr)
		}
		return ToJobError(err)
	}
	return q.SetWebhookDeliveryStatus(ctx, repo.SetWebhookDeliveryStatusParams{ID: d.ID, Status: "dispatched"})
}
