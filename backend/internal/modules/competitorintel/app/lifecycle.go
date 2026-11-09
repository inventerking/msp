package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"mspengine/internal/kernel/db"
	"mspengine/internal/kernel/events"
	"mspengine/internal/kernel/jobs"
	"mspengine/internal/modules/competitorintel/repo"
)

func containsCode(err error, code string) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == code
}

// Consumers are the events Competitor Intelligence reacts to.
func (s *Service) Consumers() []events.Consumer {
	return []events.Consumer{
		{Name: "competitor_intelligence.company_merged", Types: []string{"shared.company.merged"}, Class: events.Lifecycle, Handler: s.onCompanyMerged},
		{Name: "competitor_intelligence.entitlement_lost", Types: []string{"platform.entitlement.revoked", "platform.entitlement.suspended", "platform.entitlement.expired"}, Class: events.Lifecycle, Handler: s.onEntitlementLost},
	}
}

func (s *Service) onCompanyMerged(ctx context.Context, _ pgx.Tx, e events.Event) error {
	var p struct {
		Source uuid.UUID `json:"source_company_id"`
		Target uuid.UUID `json:"target_company_id"`
	}
	if err := e.Decode(&p); err != nil || p.Source == uuid.Nil || p.Target == uuid.Nil {
		return fmt.Errorf("bad merge payload: %w", err)
	}
	return db.Tx(ctx, s.Pools.System, func(tx pgx.Tx) error {
		q := repo.New(tx)
		if _, err := q.RepointCompetitors(ctx, repo.RepointCompetitorsParams{Source: p.Source, Target: p.Target}); err != nil {
			return err
		}
		_, err := q.RetireDuplicateCompetitors(ctx, p.Source)
		return err
	})
}

// onEntitlementLost stops open research. Scheduled monitoring stops too, because the schedule
// checks the entitlement before queuing anything.
func (s *Service) onEntitlementLost(ctx context.Context, tx pgx.Tx, e events.Event) error {
	var p struct {
		Module string `json:"module_key"`
	}
	if err := e.Decode(&p); err != nil {
		return err
	}
	if p.Module != Module || e.TenantID == nil {
		return nil
	}
	ids, err := repo.New(tx).CancelOpenRuns(ctx, *e.TenantID)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if id == nil {
			continue
		}
		if _, err := jobs.RequestCancel(ctx, tx, *id); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	}
	return nil
}

// Jobs are the module's job handlers.
func (s *Service) Jobs() map[string]jobs.Handler {
	return map[string]jobs.Handler{JobResearch: s.HandleResearch, JobReport: s.HandleReport, JobMonitorTick: s.HandleMonitorTick}
}
