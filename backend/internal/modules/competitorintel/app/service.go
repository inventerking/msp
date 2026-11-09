// Package app is Competitor Intelligence's application service. It owns every write to its schema.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/db"
	"mspengine/internal/kernel/egress"
	"mspengine/internal/kernel/events"
	"mspengine/internal/modules/competitorintel/repo"
	"mspengine/internal/platform/audit"
	"mspengine/internal/platform/commercial"
	"mspengine/internal/platform/files"
	"mspengine/internal/platform/identity"
	"mspengine/internal/platform/integration"
	"mspengine/internal/platform/integration/aigateway"
	"mspengine/internal/platform/pdf"
	"mspengine/internal/platform/shared"
)

// Module is the entitlement key.
const Module = "competitor_intelligence"

// Job kinds.
const (
	JobResearch    = "competitor.research"
	JobReport      = "competitor.report"
	JobMonitorTick = "competitor.monitor_tick"
)

// AI tasks.
const (
	TaskExtract = "competitor_intelligence.extract"
	TaskReport  = "competitor_intelligence.report"
)

// Deps are the platform services the module uses.
type Deps struct {
	Pools       *db.Pools
	Identity    *identity.Service
	Commercial  *commercial.Service
	Shared      *shared.Service
	Integration *integration.Service
	Files       *files.Service
	AI          *aigateway.Gateway
	// PDF renders reports. Without it, reports are produced without a PDF.
	PDF pdf.Renderer
	// Fetcher reads public pages. It is the fake one unless live research is configured.
	Fetcher Fetcher
	// FetchDelay spaces requests to one host during a run.
	FetchDelay time.Duration
}

// Service implements Competitor Intelligence.
type Service struct {
	Deps
	guard *egress.Client
}

// New creates the service and registers its AI tasks.
func New(d Deps) *Service {
	if d.Fetcher == nil {
		d.Fetcher = NewFakeFetcher()
	}
	if d.AI != nil {
		d.AI.RegisterTask(aigateway.Task{Key: TaskExtract, Module: Module, ReserveCredits: 2, Timeout: 60 * time.Second})
		d.AI.RegisterTask(aigateway.Task{Key: TaskReport, Module: Module, ReserveCredits: 5, Timeout: 120 * time.Second})
	}
	return &Service{Deps: d, guard: egress.New()}
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
	return events.Append(ctx, tx, events.Event{Type: "competitor_intelligence." + typ, TenantID: &tenantID, SourceModule: Module, AggregateType: aggregate, AggregateID: id}, payload)
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

func ptr[T any](v T) *T { return &v }
