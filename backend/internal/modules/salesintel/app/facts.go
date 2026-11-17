package app

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/events"
	"mspengine/internal/kernel/jobs"
	"mspengine/internal/modules/salesintel/repo"
)

// recorded marks the tenant's metrics as out of date and asks for a rollup. Events close together
// share one rollup.
func (s *Service) recorded(ctx context.Context, tx pgx.Tx, q *repo.Queries, tenant uuid.UUID, e events.Event) error {
	if err := q.MarkDirty(ctx, repo.MarkDirtyParams{TenantID: tenant, FactsAsOf: &e.OccurredAt}); err != nil {
		return err
	}
	now := s.Now()
	key := "si-rollup:" + tenant.String() + ":" + e.ID.String()
	runAt := now
	if s.RollupDelay > 0 {
		bucket := now.Truncate(s.RollupDelay)
		key = "si-rollup:" + tenant.String() + ":" + bucket.Format(time.RFC3339Nano)
		runAt = bucket.Add(s.RollupDelay)
	}
	_, _, err := jobs.Enqueue(ctx, tx, jobs.Spec{TenantID: &tenant, Kind: JobRollup, Queue: jobs.QueueAnalytics, MaxAttempts: 5, RunAt: runAt, IdempotencyKey: key})
	return err
}

func (s *Service) onLead(ctx context.Context, tx pgx.Tx, e events.Event) error {
	if e.TenantID == nil {
		return nil
	}
	var p struct {
		Prospect   *uuid.UUID `json:"prospect_id"`
		Source     string     `json:"source_key"`
		CRMCompany *uuid.UUID `json:"crm_company_id"`
		Score      *int32     `json:"score"`
	}
	if err := e.Decode(&p); err != nil {
		return err
	}
	q := repo.New(tx)
	ref := p.Prospect
	source := p.Source
	switch e.Type {
	case "chatbot.lead.captured":
		ref, source = &e.AggregateID, "chatbot"
	case "visitor_intelligence.conversion.recorded":
		ref, source = &e.AggregateID, "website"
	case "lead_generation.prospect.created":
	default:
		// Later prospect events carry no source; it comes from the prospect's first event.
		if ref != nil {
			if src, err := q.LeadSource(ctx, repo.LeadSourceParams{TenantID: *e.TenantID, ProspectRef: ref}); err == nil {
				source = src
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
		}
	}
	if source == "" {
		source = "unknown"
	}
	n, err := q.InsertLeadFact(ctx, repo.InsertLeadFactParams{TenantID: *e.TenantID, SourceEventID: e.ID, EventType: e.Type, ProspectRef: ref, SourceKey: source,
		CrmCompanyRef: p.CRMCompany, Score: p.Score, OccurredAt: e.OccurredAt})
	if err != nil || n == 0 {
		return err // n == 0: a replay, already counted.
	}
	return s.recorded(ctx, tx, q, *e.TenantID, e)
}

func (s *Service) onOutreach(ctx context.Context, tx pgx.Tx, e events.Event) error {
	if e.TenantID == nil {
		return nil
	}
	var p struct {
		Campaign uuid.UUID  `json:"campaign_id"`
		Company  *uuid.UUID `json:"company_id"`
		Title    string     `json:"title"`
		Bounce   *string    `json:"bounce_type"`
	}
	if err := e.Decode(&p); err != nil {
		return err
	}
	if p.Campaign == uuid.Nil {
		return nil
	}
	q := repo.New(tx)
	n, err := q.InsertOutreachFact(ctx, repo.InsertOutreachFactParams{TenantID: *e.TenantID, SourceEventID: e.ID, EventType: e.Type, CampaignRef: p.Campaign,
		CompanyRef: p.Company, Subtype: p.Bounce, OccurredAt: e.OccurredAt})
	if err != nil || n == 0 {
		return err
	}
	// A campaign's name is a business label, kept apart from the facts so reports can show it.
	if title := strings.TrimSpace(p.Title); title != "" {
		if len(title) > 200 {
			title = title[:200]
		}
		if err := q.UpsertLabel(ctx, repo.UpsertLabelParams{TenantID: *e.TenantID, Kind: "campaign", Ref: p.Campaign, Label: title}); err != nil {
			return err
		}
	}
	return s.recorded(ctx, tx, q, *e.TenantID, e)
}

type dealPayload struct {
	Deal        *uuid.UUID `json:"deal_id"`
	Proposal    *uuid.UUID `json:"proposal_id"`
	Owner       *uuid.UUID `json:"owner_user_id"`
	CRMCompany  *uuid.UUID `json:"crm_company_id"`
	Company     *uuid.UUID `json:"company_id"`
	Pipeline    *uuid.UUID `json:"pipeline_id"`
	Stage       *string    `json:"stage_key"`
	Status      *string    `json:"status"`
	Source      *string    `json:"source_module"`
	Amount      *int64     `json:"contract_value_minor"`
	MRR         *int64     `json:"expected_mrr_minor"`
	ProposalMRR *int64     `json:"mrr_total_minor"`
	Currency    *string    `json:"currency"`
	Probability *int32     `json:"probability"`
	Close       *string    `json:"expected_close_date"`
}

func (s *Service) onDeal(ctx context.Context, tx pgx.Tx, e events.Event) error {
	if e.TenantID == nil {
		return nil
	}
	var p dealPayload
	if err := e.Decode(&p); err != nil {
		return err
	}
	if p.Deal == nil && p.Proposal == nil {
		return nil
	}
	if strings.HasPrefix(e.Type, "proposals.") {
		p.MRR, p.Amount, p.Stage, p.Status = p.ProposalMRR, nil, nil, nil
	}
	if strings.HasPrefix(e.Type, "crm.meeting.") {
		p.Stage, p.Status = nil, nil
	}
	var closeOn *time.Time
	if p.Close != nil {
		if t, err := time.Parse(time.DateOnly, *p.Close); err == nil {
			closeOn = &t
		}
	}
	q := repo.New(tx)
	n, err := q.InsertDealFact(ctx, repo.InsertDealFactParams{TenantID: *e.TenantID, SourceEventID: e.ID, EventType: e.Type, DealRef: p.Deal, ProposalRef: p.Proposal,
		OwnerUserRef: p.Owner, CrmCompanyRef: p.CRMCompany, CompanyRef: p.Company, PipelineRef: p.Pipeline, StageKey: p.Stage, Status: p.Status,
		SourceModule: p.Source, AmountMinor: p.Amount, MrrMinor: p.MRR, Currency: p.Currency, Probability: p.Probability, ExpectedClose: closeOn, OccurredAt: e.OccurredAt})
	if err != nil || n == 0 {
		return err
	}
	return s.recorded(ctx, tx, q, *e.TenantID, e)
}

func (s *Service) onRevenue(ctx context.Context, tx pgx.Tx, e events.Event) error {
	if e.TenantID == nil {
		return nil
	}
	var p struct {
		Contract  uuid.UUID  `json:"contract_id"`
		Customer  *uuid.UUID `json:"crm_company_id"`
		MRR       int64      `json:"mrr_minor"`
		Delta     int64      `json:"mrr_delta_minor"`
		Currency  string     `json:"currency"`
		Movement  string     `json:"movement"`
		End       *string    `json:"end_date"`
		AutoRenew *bool      `json:"auto_renew"`
	}
	if err := e.Decode(&p); err != nil {
		return err
	}
	reporting, err := s.Currency(ctx, *e.TenantID)
	if err != nil {
		return err
	}
	// Converted at the event date's rate (D-11). With no rate, the amount is kept in its own
	// currency and left out of reporting totals, flagged.
	var converted *int64
	var rate *float64
	if r, found, err := s.Rates(ctx, p.Currency, reporting, e.OccurredAt); err != nil {
		return err
	} else if found {
		v := int64(math.Round(float64(p.Delta) * r))
		converted, rate = &v, &r
	}
	var end *time.Time
	if p.End != nil {
		if t, err := time.Parse(time.DateOnly, *p.End); err == nil {
			end = &t
		}
	}
	if p.Movement == "" {
		p.Movement = "none"
	}
	q := repo.New(tx)
	n, err := q.InsertRevenueFact(ctx, repo.InsertRevenueFactParams{TenantID: *e.TenantID, SourceEventID: e.ID, Movement: p.Movement, ContractRef: p.Contract,
		CustomerRef: p.Customer, MrrDeltaMinor: p.Delta, ContractMrrMinor: p.MRR, Currency: p.Currency, ReportingMrrDeltaMinor: converted,
		ReportingCurrency: reporting, FxRate: rate, ContractEndDate: end, AutoRenew: p.AutoRenew, OccurredAt: e.OccurredAt})
	if err != nil || n == 0 {
		return err
	}
	return s.recorded(ctx, tx, q, *e.TenantID, e)
}
