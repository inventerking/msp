// Package app is Proposals' application service. Prices come only from the domain calculation, and
// an issued version is frozen by the database.
package app

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/crypto"
	"mspengine/internal/kernel/db"
	"mspengine/internal/kernel/events"
	"mspengine/internal/kernel/mailer"
	crm "mspengine/internal/modules/crm/contract"
	"mspengine/internal/modules/proposals/repo"
	"mspengine/internal/platform/audit"
	"mspengine/internal/platform/files"
	"mspengine/internal/platform/integration"
	"mspengine/internal/platform/integration/aigateway"
	"mspengine/internal/platform/pdf"
)

// Module is the entitlement key.
const Module = "proposals"

// Job kinds.
const (
	JobDeliver = "proposal.deliver"
)

// TaskDraft is the AI task that drafts a section of text.
const TaskDraft = "proposals.draft_section"

// ApprovalThresholdMinor is the total contract value at or above which issuing needs approval.
// It is a proposed default, in minor units of the proposal's currency, until tenants can set it.
const ApprovalThresholdMinor = 1_000_000

// Deps are the services Proposals uses.
type Deps struct {
	Pools       *db.Pools
	Keys        crypto.Keys
	Registry    crm.Registry
	Deals       crm.Deals
	Files       *files.Service
	PDF         pdf.Renderer
	AI          *aigateway.Gateway
	Mailer      mailer.Mailer
	Integration *integration.Service
	// BaseURL is the client app's address, for links in emails.
	BaseURL string
	// Signer sends documents for e-signature. The fake one sends nothing.
	Signer Signer
}

// Service implements Proposals.
type Service struct{ Deps }

// New creates the service and registers its AI task and e-signature webhook.
func New(d Deps) *Service {
	if d.Signer == nil {
		d.Signer = &FakeSigner{}
	}
	s := &Service{Deps: d}
	if d.AI != nil {
		d.AI.RegisterTask(aigateway.Task{Key: TaskDraft, Module: Module, ReserveCredits: 1, Timeout: 60 * time.Second})
	}
	if d.Integration != nil {
		d.Integration.RegisterWebhook(d.Signer.Key(), integration.VerifyHMAC, s.HandleSignatureWebhook)
	}
	return s
}

func (s *Service) tx(ctx context.Context, p authctx.Principal, fn func(q *repo.Queries, tx pgx.Tx) error) error {
	return db.TenantTx(ctx, s.Pools.App, p.TenantID, p.UserID, func(tx pgx.Tx) error { return fn(repo.New(tx), tx) })
}

func (s *Service) systemTx(ctx context.Context, tenantID uuid.UUID, fn func(q *repo.Queries, tx pgx.Tx) error) error {
	return db.TenantTx(ctx, s.Pools.App, tenantID, uuid.Nil, func(tx pgx.Tx) error { return fn(repo.New(tx), tx) })
}

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.NotFound()
	}
	return err
}

func userAudit(p authctx.Principal, action, resource string, id *uuid.UUID, after any) audit.Event {
	return audit.Event{TenantID: &p.TenantID, ActorType: "user", ActorID: &p.UserID, Action: action, ResourceType: resource, ResourceID: id, After: after}
}

func emit(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, typ, aggregate string, id uuid.UUID, payload any) error {
	return events.Append(ctx, tx, events.Event{Type: "proposals." + typ, TenantID: &tenantID, SourceModule: Module, AggregateType: aggregate, AggregateID: id}, payload)
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err) // Only plain maps and structs.
	}
	return b
}

func uid(p authctx.Principal) *uuid.UUID {
	if p.UserID == uuid.Nil {
		return nil
	}
	id := p.UserID
	return &id
}

// linkToken derives a share link's token from its ID with a server key. Only its hash is stored, and
// the delivery job can derive it again, so a retried email carries the same working link.
func (s *Service) linkToken(linkID uuid.UUID) string {
	return base64.RawURLEncoding.EncodeToString(s.Keys.HMACValue("proposal-link", linkID.String()))
}

func tokenHash(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }
