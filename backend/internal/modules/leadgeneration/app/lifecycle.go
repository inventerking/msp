package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/db"
	"mspengine/internal/kernel/events"
	"mspengine/internal/kernel/jobs"
	"mspengine/internal/modules/leadgeneration/repo"
)

// Name identifies the module to the privacy service.
func (s *Service) Name() string { return Module }

// EraseContact removes the contact's links to prospects. The contact itself is erased by the
// privacy service.
func (s *Service) EraseContact(ctx context.Context, tenantID, contactID uuid.UUID) error {
	return s.systemTx(ctx, tenantID, func(q *repo.Queries, _ pgx.Tx) error {
		return q.DeleteContactLinks(ctx, repo.DeleteContactLinksParams{TenantID: tenantID, ContactID: contactID})
	})
}

// ExportContact lists what Lead Generation holds about a contact, for an access request.
func (s *Service) ExportContact(ctx context.Context, tenantID, contactID uuid.UUID) (any, error) {
	var out []repo.ContactLinksRow
	err := s.systemTx(ctx, tenantID, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.ContactLinks(ctx, repo.ContactLinksParams{TenantID: tenantID, ContactID: contactID})
		return err
	})
	return map[string]any{"prospect_links": out}, err
}

// Consumers are the events Lead Generation reacts to.
func (s *Service) Consumers() []events.Consumer {
	return []events.Consumer{
		{Name: "lead_generation.company_merged", Types: []string{"shared.company.merged"}, Class: events.Lifecycle, Handler: s.onCompanyMerged},
		{Name: "lead_generation.entitlement_lost", Types: []string{"platform.entitlement.revoked", "platform.entitlement.suspended", "platform.entitlement.expired"}, Class: events.Lifecycle, Handler: s.onEntitlementLost},
	}
}

// onCompanyMerged repoints every tenant's prospects to the surviving company. A merge is
// platform-wide, so this runs as the system role, whose policy allows only these updates. Where a
// tenant already had a prospect for the survivor, the duplicate is retired.
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
		if _, err := q.RepointProspects(ctx, repo.RepointProspectsParams{Source: p.Source, Target: p.Target}); err != nil {
			return err
		}
		if _, err := q.RetireDuplicateProspects(ctx, p.Source); err != nil {
			return err
		}
		_, err := q.RepointSearchResults(ctx, repo.RepointSearchResultsParams{Source: p.Source, Target: p.Target})
		return err
	})
}

// onEntitlementLost stops the tenant's open searches and enrichments when the module is lost.
// Their reserved credits are released by the jobs, or by the reservation sweep.
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
	q := repo.New(tx)
	searchJobs, err := q.CancelOpenSearches(ctx, *e.TenantID)
	if err != nil {
		return err
	}
	enrichJobs, err := q.CancelOpenEnrichments(ctx, *e.TenantID)
	if err != nil {
		return err
	}
	for _, id := range append(searchJobs, enrichJobs...) {
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
	return map[string]jobs.Handler{JobSearch: s.HandleSearch, JobEnrich: s.HandleEnrich, JobExplain: s.HandleExplain, JobImport: s.HandleImport, JobExport: s.HandleExport}
}
