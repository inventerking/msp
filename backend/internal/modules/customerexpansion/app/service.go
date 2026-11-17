// Package app is Customer Expansion's application service. It records contracts as the source of
// record for recurring revenue (D-03), tracks coverage and renewals, and suggests expansion with the
// evidence for every suggestion.
package app

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/db"
	"mspengine/internal/kernel/events"
	"mspengine/internal/kernel/jobs"
	crm "mspengine/internal/modules/crm/contract"
	"mspengine/internal/modules/customerexpansion/repo"
	"mspengine/internal/platform/audit"
	"mspengine/internal/platform/commercial"
	"mspengine/internal/platform/files"
	"mspengine/internal/platform/integration/aigateway"
	"mspengine/internal/platform/notification"
	"mspengine/internal/platform/shared"
)

// Module is the entitlement key.
const Module = "customer_expansion"

// Job kinds.
const (
	JobRenewalScan = "expansion.renewal_scan"
	JobWord        = "expansion.word_recommendation"
	JobOpenDeal    = "expansion.open_deal"
)

// TaskWord is the AI task that rewords a rule-based rationale. It never sets numbers.
const TaskWord = "customer_expansion.word_recommendation"

// Deps are the services Customer Expansion uses. Only Pools and Registry are needed to record
// contracts, which happens for every tenant (D-03).
type Deps struct {
	Pools      *db.Pools
	Registry   crm.Registry
	Deals      crm.Deals
	Shared     *shared.Service
	Commercial *commercial.Service
	Files      *files.Service
	AI         *aigateway.Gateway
	Notify     *notification.Service
	// Currency gives a tenant's reporting currency.
	Currency func(ctx context.Context, tenantID uuid.UUID) (string, error)
	// Now is the clock, replaceable in tests.
	Now func() time.Time
}

// Service implements Customer Expansion.
type Service struct{ Deps }

// New creates the service.
func New(d Deps) *Service {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.AI != nil {
		d.AI.RegisterTask(aigateway.Task{Key: TaskWord, Module: Module, ReserveCredits: 1, Timeout: 30 * time.Second})
	}
	return &Service{Deps: d}
}

func (s *Service) today() time.Time {
	n := s.Now().UTC()
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
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

func systemAudit(tenant uuid.UUID, action, resource string, id *uuid.UUID, after any) audit.Event {
	return audit.Event{TenantID: &tenant, ActorType: "system", Action: action, ResourceType: resource, ResourceID: id, After: after}
}

func emit(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, typ, aggregate string, id uuid.UUID, payload any) error {
	return events.Append(ctx, tx, events.Event{Type: "customer_expansion." + typ, TenantID: &tenantID, SourceModule: Module, AggregateType: aggregate, AggregateID: id}, payload)
}

func uid(p authctx.Principal) *uuid.UUID {
	if p.UserID == uuid.Nil {
		return nil
	}
	id := p.UserID
	return &id
}

func ptr[T any](v T) *T { return &v }

func pgUnique(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "23505"
}

func invalid(field, code string) error {
	return apperr.Validation(apperr.FieldError{Field: field, Code: code})
}

// Jobs are Customer Expansion's job handlers.
func (s *Service) Jobs() map[string]jobs.Handler {
	return map[string]jobs.Handler{JobRenewalScan: s.HandleRenewalScan, JobWord: s.HandleWord, JobOpenDeal: s.HandleOpenDeal}
}

// Consumers record contracts and follow deals and assessments. They are passive (ADR-014): they
// run whether or not the tenant has the module, so the records exist if it is bought later.
func (s *Service) Consumers() []events.Consumer {
	return []events.Consumer{
		{Name: "customer_expansion.contract_recorder", Types: []string{"crm.deal.won", "crm.deal.reopened", "proposals.proposal.signed"}, Class: events.Passive, Handler: s.onContractSource},
		{Name: "customer_expansion.opportunities", Types: []string{"crm.deal.won", "crm.deal.lost", "crm.deal.reopened"}, Class: events.Passive, Handler: s.onOpportunityDeal},
		{Name: "customer_expansion.assessments", Types: []string{"it_assessment.assessment.completed"}, Class: events.Passive, Handler: s.onAssessment},
		{Name: "customer_expansion.lifecycle", Types: []string{"crm.company.lifecycle_changed"}, Class: events.Passive, Handler: s.onLifecycle},
		{Name: "customer_expansion.company_merged", Types: []string{"shared.company.merged"}, Class: events.Lifecycle, Handler: s.onCompanyMerged},
	}
}
