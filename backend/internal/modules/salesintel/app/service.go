// Package app is Sales Intelligence's application service. It projects other modules' events into
// facts, computes metrics and forecasts from them, and serves reports. It never writes to another
// module and reads no other module's tables: it imports no other module at all.
package app

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/db"
	"mspengine/internal/kernel/events"
	"mspengine/internal/kernel/jobs"
	"mspengine/internal/modules/salesintel/repo"
	"mspengine/internal/platform/audit"
	"mspengine/internal/platform/files"
	"mspengine/internal/platform/integration/aigateway"
)

// Module is the entitlement key.
const Module = "sales_intelligence"

// Job kinds.
const (
	JobRollup   = "analytics.metric_rollup"
	JobSweep    = "analytics.rollup_sweep"
	JobForecast = "analytics.forecast"
	JobExport   = "analytics.report_export"
)

// TaskCommentary is the AI task that writes commentary on metrics. It never produces numbers.
const TaskCommentary = "sales_intelligence.commentary"

// RateFunc gives the exchange rate from base to quote on a date, from approved reference data.
type RateFunc func(ctx context.Context, base, quote string, on time.Time) (rate float64, found bool, err error)

// Deps are the services Sales Intelligence uses.
type Deps struct {
	Pools *db.Pools
	// ModuleStatus reads a module's entitlement status. It is a function, not the commercial
	// service, so this module has no path to change billing or entitlements.
	ModuleStatus func(ctx context.Context, tenantID uuid.UUID, moduleKey string) (string, error)
	Rates        RateFunc
	Currency     func(ctx context.Context, tenantID uuid.UUID) (string, error)
	AI           *aigateway.Gateway
	Files        *files.Service
	// RollupDelay gathers events arriving close together into one rollup. Zero rolls up per event.
	RollupDelay time.Duration
	Now         func() time.Time
}

// Service implements Sales Intelligence.
type Service struct{ Deps }

// DefaultRollupDelay is the gathering window for rollups.
const DefaultRollupDelay = 5 * time.Second

// New creates the service.
func New(d Deps) *Service {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.AI != nil {
		d.AI.RegisterTask(aigateway.Task{Key: TaskCommentary, Module: Module, ReserveCredits: 1, Timeout: 60 * time.Second})
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

func invalid(field, code string) error {
	return apperr.Validation(apperr.FieldError{Field: field, Code: code})
}

func userAudit(p authctx.Principal, action, resource string, id *uuid.UUID, after any) audit.Event {
	return audit.Event{TenantID: &p.TenantID, ActorType: "user", ActorID: &p.UserID, Action: action, ResourceType: resource, ResourceID: id, After: after}
}

func emit(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, typ, aggregate string, id uuid.UUID, payload any) error {
	return events.Append(ctx, tx, events.Event{Type: "sales_intelligence." + typ, TenantID: &tenantID, SourceModule: Module, AggregateType: aggregate, AggregateID: id}, payload)
}

func uid(p authctx.Principal) *uuid.UUID {
	if p.UserID == uuid.Nil {
		return nil
	}
	id := p.UserID
	return &id
}

func ptr[T any](v T) *T { return &v }

// Jobs are Sales Intelligence's job handlers.
func (s *Service) Jobs() map[string]jobs.Handler {
	return map[string]jobs.Handler{JobRollup: s.HandleRollup, JobSweep: s.HandleSweep, JobForecast: s.HandleForecast, JobExport: s.HandleExport}
}

// Consumers project events into facts. All are passive (ADR-014): they run for every tenant, so a
// tenant that buys the module later sees its history.
func (s *Service) Consumers() []events.Consumer {
	return []events.Consumer{
		{Name: "sales_intelligence.lead_facts", Class: events.Passive, Handler: s.onLead, Types: []string{
			"lead_generation.prospect.created", "lead_generation.prospect.qualified", "lead_generation.prospect.disqualified", "lead_generation.prospect.converted",
			"chatbot.lead.captured", "visitor_intelligence.conversion.recorded"}},
		{Name: "sales_intelligence.outreach_facts", Class: events.Passive, Handler: s.onOutreach, Types: []string{
			"outreach.message.sent", "outreach.message.bounced", "outreach.reply.received", "outreach.unsubscribe.recorded"}},
		{Name: "sales_intelligence.deal_facts", Class: events.Passive, Handler: s.onDeal, Types: []string{
			"crm.deal.created", "crm.deal.updated", "crm.deal.stage_changed", "crm.deal.won", "crm.deal.lost", "crm.deal.reopened",
			"crm.meeting.scheduled", "crm.meeting.completed", "proposals.version.issued", "proposals.proposal.signed", "proposals.proposal.declined"}},
		{Name: "sales_intelligence.revenue_facts", Class: events.Passive, Handler: s.onRevenue, Types: []string{
			"customer_expansion.contract.activated", "customer_expansion.contract.changed", "customer_expansion.contract.ended"}},
	}
}
