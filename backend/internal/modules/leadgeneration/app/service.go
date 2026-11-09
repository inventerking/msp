// Package app is Lead Generation's application service: searches, prospects, scoring, enrichment,
// lists, imports and exports. Handlers and jobs call it; it owns every write to its schema.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/db"
	"mspengine/internal/kernel/events"
	ci "mspengine/internal/modules/competitorintel/contract"
	crm "mspengine/internal/modules/crm/contract"
	"mspengine/internal/modules/leadgeneration/repo"
	"mspengine/internal/platform/audit"
	"mspengine/internal/platform/commercial"
	"mspengine/internal/platform/files"
	"mspengine/internal/platform/identity"
	"mspengine/internal/platform/integration"
	"mspengine/internal/platform/integration/aigateway"
	"mspengine/internal/platform/shared"
)

// Module is the entitlement key.
const Module = "lead_generation"

// Job kinds.
const (
	JobSearch  = "lead.search"
	JobEnrich  = "lead.enrich"
	JobExplain = "lead.explain"
	JobImport  = "lead.import"
	JobExport  = "lead.export"
)

// Credit costs, in lead credits. They are internal metering, not provider prices.
const (
	CreditsPerResult     = 1
	CreditsPerEnrichment = 1
	creditMeter          = "lead_credits"
)

// TaskExplain is the AI task that explains a score.
const TaskExplain = "lead_generation.explain_score"

// Deps are the platform services the module uses.
type Deps struct {
	Pools       *db.Pools
	Identity    *identity.Service
	Commercial  *commercial.Service
	Shared      *shared.Service
	Integration *integration.Service
	Files       *files.Service
	AI          *aigateway.Gateway
	// CRM converts prospects. It is nil until the CRM module is built.
	CRM crm.Converter
	// Competitors says whether a company is one of the tenant's competitors. It may be nil.
	Competitors ci.Directory
}

// Service implements Lead Generation.
type Service struct{ Deps }

// New creates the service and registers its AI task.
func New(d Deps) *Service {
	if d.AI != nil {
		d.AI.RegisterTask(aigateway.Task{Key: TaskExplain, Module: Module, ReserveCredits: 1, Timeout: 30 * time.Second})
	}
	return &Service{Deps: d}
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
	return events.Append(ctx, tx, events.Event{Type: "lead_generation." + typ, TenantID: &tenantID, SourceModule: Module, AggregateType: aggregate, AggregateID: id}, payload)
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err) // Only ever called with plain maps and structs.
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

var errCRMUnavailable = apperr.New(http.StatusConflict, "crm_unavailable", "The CRM module is not available yet, so prospects cannot be converted")
