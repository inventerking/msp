package app

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/jobs"
	"mspengine/internal/modules/salesintel/repo"
	"mspengine/internal/platform/audit"
)

// CostInput is a cost entry. Costs are the only facts a tenant types in, because the platform cannot
// know marketing spend.
type CostInput struct {
	Start, End time.Time
	Category   string
	Campaign   *uuid.UUID
	Amount     int64
	Currency   string
	Note       *string
}

var categories = map[string]bool{"advertising": true, "events": true, "content": true, "tools": true, "outreach": true, "staff": true, "other": true}

func (c CostInput) check() error {
	switch {
	case c.End.Before(c.Start):
		return invalid("period_end", "before_start")
	case c.End.Sub(c.Start) > 366*24*time.Hour:
		return invalid("period_end", "longer_than_a_year")
	case !categories[c.Category]:
		return invalid("category", "invalid_category")
	case c.Amount < 0:
		return invalid("amount_minor", "negative")
	case len(c.Currency) != 3 || strings.ToUpper(c.Currency) != c.Currency:
		return invalid("currency", "invalid_currency")
	case c.Note != nil && len(*c.Note) > 500:
		return invalid("note", "too_long")
	}
	return nil
}

// touch marks metrics as out of date after a change made here, such as a cost.
func (s *Service) touch(ctx context.Context, tx pgx.Tx, q *repo.Queries, tenant uuid.UUID) error {
	now := s.Now()
	if err := q.MarkDirty(ctx, repo.MarkDirtyParams{TenantID: tenant, FactsAsOf: &now}); err != nil {
		return err
	}
	_, _, err := jobs.Enqueue(ctx, tx, jobs.Spec{TenantID: &tenant, Kind: JobRollup, Queue: jobs.QueueAnalytics, MaxAttempts: 5, IdempotencyKey: "si-rollup:" + tenant.String() + ":" + uuid.NewString()})
	return err
}

// Costs lists cost entries.
func (s *Service) Costs(ctx context.Context, p authctx.Principal) ([]repo.ListCostsRow, error) {
	var out []repo.ListCostsRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.ListCosts(ctx, p.TenantID)
		return err
	})
	return out, err
}

// CreateCost records a cost. It is audited, because it changes CAC and ROI.
func (s *Service) CreateCost(ctx context.Context, p authctx.Principal, in CostInput) (repo.GetCostRow, error) {
	if err := in.check(); err != nil {
		return repo.GetCostRow{}, err
	}
	var out repo.GetCostRow
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		id, err := q.InsertCost(ctx, repo.InsertCostParams{TenantID: p.TenantID, PeriodStart: in.Start, PeriodEnd: in.End, Category: in.Category, CampaignRef: in.Campaign,
			AmountMinor: in.Amount, Currency: in.Currency, Note: in.Note, CreatedBy: uid(p)})
		if err != nil {
			return err
		}
		if out, err = q.GetCost(ctx, repo.GetCostParams{TenantID: p.TenantID, ID: id}); err != nil {
			return err
		}
		if err := audit.Record(ctx, tx, userAudit(p, "sales_intel.cost_created", "cost_entry", &id, map[string]any{"amount_minor": in.Amount, "currency": in.Currency, "category": in.Category})); err != nil {
			return err
		}
		return s.touch(ctx, tx, q, p.TenantID)
	})
	return out, err
}

// UpdateCost replaces a cost entry's fields.
func (s *Service) UpdateCost(ctx context.Context, p authctx.Principal, id uuid.UUID, in CostInput) (repo.GetCostRow, error) {
	if err := in.check(); err != nil {
		return repo.GetCostRow{}, err
	}
	var out repo.GetCostRow
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		before, err := q.GetCost(ctx, repo.GetCostParams{TenantID: p.TenantID, ID: id})
		if err != nil {
			return notFound(err)
		}
		if err := q.UpdateCost(ctx, repo.UpdateCostParams{TenantID: p.TenantID, ID: id, PeriodStart: in.Start, PeriodEnd: in.End, Category: in.Category,
			CampaignRef: in.Campaign, AmountMinor: in.Amount, Currency: in.Currency, Note: in.Note}); err != nil {
			return err
		}
		if out, err = q.GetCost(ctx, repo.GetCostParams{TenantID: p.TenantID, ID: id}); err != nil {
			return err
		}
		if err := audit.Record(ctx, tx, userAudit(p, "sales_intel.cost_changed", "cost_entry", &id, map[string]any{
			"before": map[string]any{"amount_minor": before.AmountMinor, "currency": before.Currency}, "after": map[string]any{"amount_minor": in.Amount, "currency": in.Currency}})); err != nil {
			return err
		}
		return s.touch(ctx, tx, q, p.TenantID)
	})
	return out, err
}

// DeleteCost removes a cost entry.
func (s *Service) DeleteCost(ctx context.Context, p authctx.Principal, id uuid.UUID) error {
	return s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		n, err := q.DeleteCost(ctx, repo.DeleteCostParams{TenantID: p.TenantID, ID: id})
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.NotFound()
		}
		if err := audit.Record(ctx, tx, userAudit(p, "sales_intel.cost_deleted", "cost_entry", &id, nil)); err != nil {
			return err
		}
		return s.touch(ctx, tx, q, p.TenantID)
	})
}
