package app

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/events"
	"mspengine/internal/kernel/jobs"
	crm "mspengine/internal/modules/crm/contract"
	"mspengine/internal/modules/customerexpansion/repo"
	"mspengine/internal/platform/audit"
)

// EvidenceView is one piece of evidence as shown.
type EvidenceView struct {
	Kind          string
	ReferenceType *string
	ReferenceID   *uuid.UUID
	Detail        map[string]any
}

// RecommendationView is a recommendation with its customer, service and evidence.
type RecommendationView struct {
	repo.ListRecommendationsRow
	CustomerName string
	ServiceName  *string
	AIWorded     bool
	Evidence     []EvidenceView
}

// RecommendationFilter narrows the list.
type RecommendationFilter struct {
	Status  *string
	Profile *uuid.UUID
}

// Recommendations lists recommendations, each with its evidence.
func (s *Service) Recommendations(ctx context.Context, p authctx.Principal, f RecommendationFilter) ([]RecommendationView, error) {
	var rows []repo.ListRecommendationsRow
	var ev []repo.EvidenceForRow
	if err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		if rows, err = q.ListRecommendations(ctx, repo.ListRecommendationsParams{TenantID: p.TenantID, Status: f.Status, CustomerProfileID: f.Profile}); err != nil {
			return err
		}
		ids := make([]uuid.UUID, len(rows))
		for i, r := range rows {
			ids[i] = r.ID
		}
		ev, err = q.EvidenceFor(ctx, repo.EvidenceForParams{TenantID: p.TenantID, Ids: ids})
		return err
	}); err != nil {
		return nil, err
	}
	return s.views(ctx, p.TenantID, rows, ev)
}

// Recommendation returns one recommendation.
func (s *Service) Recommendation(ctx context.Context, p authctx.Principal, id uuid.UUID) (RecommendationView, error) {
	var row repo.GetRecommendationRow
	var ev []repo.EvidenceForRow
	if err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		if row, err = q.GetRecommendation(ctx, repo.GetRecommendationParams{TenantID: p.TenantID, ID: id}); err != nil {
			return notFound(err)
		}
		ev, err = q.EvidenceFor(ctx, repo.EvidenceForParams{TenantID: p.TenantID, Ids: []uuid.UUID{id}})
		return err
	}); err != nil {
		return RecommendationView{}, err
	}
	out, err := s.views(ctx, p.TenantID, []repo.ListRecommendationsRow{{ID: row.ID, CustomerProfileID: row.CustomerProfileID, TenantServiceID: row.TenantServiceID,
		ContractID: row.ContractID, Kind: row.Kind, Rationale: row.Rationale, EstimatedMrrMinor: row.EstimatedMrrMinor, Currency: row.Currency,
		EvidenceClass: row.EvidenceClass, Status: row.Status, AiRequestID: row.AiRequestID, DecidedBy: row.DecidedBy, DecidedAt: row.DecidedAt,
		CreatedAt: row.CreatedAt, CrmCompanyID: row.CrmCompanyID}}, ev)
	if err != nil {
		return RecommendationView{}, err
	}
	return out[0], nil
}

func (s *Service) views(ctx context.Context, tenant uuid.UUID, rows []repo.ListRecommendationsRow, ev []repo.EvidenceForRow) ([]RecommendationView, error) {
	companies := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		companies = append(companies, r.CrmCompanyID)
	}
	names, err := s.Registry.CompanySummaries(ctx, tenant, companies)
	if err != nil {
		return nil, err
	}
	services, err := s.Shared.TenantServices(ctx, tenant)
	if err != nil {
		return nil, err
	}
	serviceName := map[uuid.UUID]string{}
	for _, sv := range services {
		serviceName[sv.ID] = sv.Name
	}
	byRec := map[uuid.UUID][]EvidenceView{}
	for _, e := range ev {
		var d map[string]any
		_ = json.Unmarshal(e.Detail, &d)
		byRec[e.RecommendationID] = append(byRec[e.RecommendationID], EvidenceView{Kind: e.Kind, ReferenceType: e.ReferenceType, ReferenceID: e.ReferenceID, Detail: d})
	}
	out := make([]RecommendationView, 0, len(rows))
	for _, r := range rows {
		v := RecommendationView{ListRecommendationsRow: r, CustomerName: names[r.CrmCompanyID].Name, AIWorded: r.AiRequestID != nil, Evidence: byRec[r.ID]}
		if v.Evidence == nil {
			v.Evidence = []EvidenceView{}
		}
		if r.TenantServiceID != nil {
			if n, ok := serviceName[*r.TenantServiceID]; ok {
				v.ServiceName = &n
			}
		}
		out = append(out, v)
	}
	return out, nil
}

// AcceptResult says what accepting did.
type AcceptResult struct {
	OpportunityID uuid.UUID
	DealID        *uuid.UUID
	// DealPending is set when a deal was asked for but not yet created; it is retried.
	DealPending bool
	// DealSkipped names why no deal was asked for, such as crm_not_active.
	DealSkipped *string
}

// Accept accepts a recommendation and tracks it as an opportunity. Repeating it returns the same
// opportunity, so a retried request never creates a second deal.
func (s *Service) Accept(ctx context.Context, p authctx.Principal, id uuid.UUID, createDeal bool, dealTitle *string) (AcceptResult, error) {
	var res AcceptResult
	var rec repo.GetRecommendationRow
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		var err error
		rec, err = q.GetRecommendation(ctx, repo.GetRecommendationParams{TenantID: p.TenantID, ID: id})
		if err != nil {
			return notFound(err)
		}
		switch rec.Status {
		case "open":
			if _, err := q.DecideRecommendation(ctx, repo.DecideRecommendationParams{TenantID: p.TenantID, ID: id, Status: "accepted", DecidedBy: uid(p)}); err != nil {
				return err
			}
			if err := emit(ctx, tx, p.TenantID, "recommendation.accepted", "recommendation", id, map[string]any{"recommendation_id": id, "kind": rec.Kind}); err != nil {
				return err
			}
			if err := audit.Record(ctx, tx, userAudit(p, "expansion.recommendation_accepted", "recommendation", &id, map[string]any{"kind": rec.Kind, "create_deal": createDeal})); err != nil {
				return err
			}
		case "accepted", "converted":
		default:
			return apperr.Conflict("recommendation_closed", "This recommendation was "+rec.Status)
		}
		o, err := q.InsertOpportunity(ctx, repo.InsertOpportunityParams{TenantID: p.TenantID, RecommendationID: id, EstimatedMrrMinor: rec.EstimatedMrrMinor, Currency: rec.Currency})
		res.OpportunityID, res.DealID = o.ID, o.DealID
		return err
	})
	if err != nil || res.DealID != nil || !createDeal {
		return res, err
	}
	if rec.Kind == "confirm_coverage" {
		res.DealSkipped = ptr("nothing_to_sell_yet")
		return res, nil
	}
	if s.Deals == nil || !s.crmActive(ctx, p.TenantID) {
		res.DealSkipped = ptr("crm_not_active")
		return res, nil
	}
	deal, err := s.openDeal(ctx, p.TenantID, uid(p), res.OpportunityID, rec, dealTitle)
	if err != nil {
		slog.WarnContext(ctx, "open deal failed; retrying later", "error", err)
		res.DealPending = true
		retry := s.systemTx(ctx, p.TenantID, func(_ *repo.Queries, tx pgx.Tx) error {
			_, _, err := jobs.Enqueue(ctx, tx, jobs.Spec{TenantID: &p.TenantID, Kind: JobOpenDeal, Queue: jobs.QueueDefault, MaxAttempts: 10,
				Payload: map[string]any{"opportunity_id": res.OpportunityID, "actor_id": uid(p), "title": dealTitle}, RunAt: s.Now().Add(time.Minute),
				IdempotencyKey: "expansion-open-deal:" + res.OpportunityID.String()})
			return err
		})
		return res, retry
	}
	res.DealID = &deal
	return res, nil
}

func (s *Service) openDeal(ctx context.Context, tenant uuid.UUID, actor *uuid.UUID, opportunity uuid.UUID, rec repo.GetRecommendationRow, title *string) (uuid.UUID, error) {
	name := "Expansion opportunity"
	if title != nil && *title != "" {
		name = *title
	} else if rec.Kind == "renewal" {
		name = "Contract renewal"
	} else if rec.TenantServiceID != nil {
		if services, err := s.Shared.TenantServices(ctx, tenant); err == nil {
			for _, sv := range services {
				if sv.ID == *rec.TenantServiceID {
					name = sv.Name
				}
			}
		}
	}
	// The estimate's currency, else the one the customer pays in now, else the reporting currency.
	var currency string
	if rec.Currency != nil {
		currency = *rec.Currency
	} else {
		_ = s.systemTx(ctx, tenant, func(q *repo.Queries, _ pgx.Tx) error {
			c, err := q.CustomerCurrency(ctx, repo.CustomerCurrencyParams{TenantID: tenant, CustomerProfileID: rec.CustomerProfileID})
			currency = c
			return err
		})
		if currency == "" {
			t, err := s.reportingCurrency(ctx, tenant)
			if err != nil {
				return uuid.Nil, err
			}
			currency = t
		}
	}
	deal, err := s.Deals.OpenDeal(ctx, crm.NewDeal{TenantID: tenant, ActorID: actor, CRMCompanyID: rec.CrmCompanyID, Title: name, Currency: currency,
		ExpectedMRR: rec.EstimatedMrrMinor, SourceModule: Module, SourceRef: opportunity})
	if err != nil {
		return uuid.Nil, err
	}
	err = s.systemTx(ctx, tenant, func(q *repo.Queries, tx pgx.Tx) error {
		if err := q.LinkOpportunityDeal(ctx, repo.LinkOpportunityDealParams{TenantID: tenant, ID: opportunity, DealID: &deal}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, systemAudit(tenant, "expansion.opportunity_deal_opened", "revenue_opportunity", &opportunity, map[string]any{"deal_id": deal}))
	})
	return deal, err
}

// crmActive reports whether the tenant has CRM, which deals need. Without it, accepted
// recommendations are tracked as opportunities only.
func (s *Service) crmActive(ctx context.Context, tenant uuid.UUID) bool {
	return s.Commercial != nil && s.Commercial.RequireEntitled(ctx, tenant, "crm") == nil
}

// reportingCurrency is the tenant's reporting currency, used when an opportunity has no estimate.
func (s *Service) reportingCurrency(ctx context.Context, tenant uuid.UUID) (string, error) {
	if s.Currency == nil {
		return "", errors.New("no reporting currency source")
	}
	return s.Currency(ctx, tenant)
}

// HandleOpenDeal retries opening a deal for an accepted recommendation.
func (s *Service) HandleOpenDeal(ctx context.Context, j jobs.Job) error {
	if j.TenantID == nil {
		return nil
	}
	tenant := *j.TenantID
	var in struct {
		Opportunity uuid.UUID  `json:"opportunity_id"`
		Actor       *uuid.UUID `json:"actor_id"`
		Title       *string    `json:"title"`
	}
	if err := j.Decode(&in); err != nil {
		return jobs.Permanent(err)
	}
	var o repo.GetOpportunityRow
	var rec repo.GetRecommendationRow
	if err := s.systemTx(ctx, tenant, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		if o, err = q.GetOpportunity(ctx, repo.GetOpportunityParams{TenantID: tenant, ID: in.Opportunity}); err != nil {
			return err
		}
		if _, err := q.CountDealAttempt(ctx, repo.CountDealAttemptParams{TenantID: tenant, ID: in.Opportunity}); err != nil {
			return err
		}
		rec, err = q.GetRecommendation(ctx, repo.GetRecommendationParams{TenantID: tenant, ID: o.RecommendationID})
		return err
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	if o.DealID != nil || o.Status != "open" {
		return nil
	}
	if !s.crmActive(ctx, tenant) {
		return nil // CRM was lost meanwhile: the opportunity stays tracked without a deal.
	}
	_, err := s.openDeal(ctx, tenant, in.Actor, o.ID, rec, in.Title)
	return err
}

// Dismiss closes a recommendation. Analysis does not suggest it again for 90 days.
func (s *Service) Dismiss(ctx context.Context, p authctx.Principal, id uuid.UUID) error {
	return s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		rec, err := q.GetRecommendation(ctx, repo.GetRecommendationParams{TenantID: p.TenantID, ID: id})
		if err != nil {
			return notFound(err)
		}
		if rec.Status == "dismissed" {
			return nil
		}
		n, err := q.DecideRecommendation(ctx, repo.DecideRecommendationParams{TenantID: p.TenantID, ID: id, Status: "dismissed", DecidedBy: uid(p)})
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.Conflict("recommendation_closed", "This recommendation was "+rec.Status)
		}
		if err := emit(ctx, tx, p.TenantID, "recommendation.dismissed", "recommendation", id, map[string]any{"recommendation_id": id, "kind": rec.Kind}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "expansion.recommendation_dismissed", "recommendation", &id, map[string]any{"kind": rec.Kind}))
	})
}

// OpportunityView is an opportunity with its customer's name.
type OpportunityView struct {
	repo.ListOpportunitiesRow
	CustomerName string
}

// Opportunities lists tracked opportunities.
func (s *Service) Opportunities(ctx context.Context, p authctx.Principal, status *string) ([]OpportunityView, error) {
	var rows []repo.ListOpportunitiesRow
	if err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		rows, err = q.ListOpportunities(ctx, repo.ListOpportunitiesParams{TenantID: p.TenantID, Status: status})
		return err
	}); err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.CrmCompanyID)
	}
	names, err := s.Registry.CompanySummaries(ctx, p.TenantID, ids)
	if err != nil {
		return nil, err
	}
	out := make([]OpportunityView, 0, len(rows))
	for _, r := range rows {
		out = append(out, OpportunityView{ListOpportunitiesRow: r, CustomerName: names[r.CrmCompanyID].Name})
	}
	return out, nil
}

// OpportunitySummary totals opportunities by status and currency, estimated and realised apart.
func (s *Service) OpportunitySummary(ctx context.Context, p authctx.Principal) ([]repo.OpportunitySummaryRow, error) {
	var out []repo.OpportunitySummaryRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.OpportunitySummary(ctx, p.TenantID)
		return err
	})
	return out, err
}

// onOpportunityDeal follows an opportunity's deal. When it is won, the deal's MRR is realised.
func (s *Service) onOpportunityDeal(ctx context.Context, tx pgx.Tx, e events.Event) error {
	if e.TenantID == nil {
		return nil
	}
	tenant := *e.TenantID
	var d dealEvent
	if err := e.Decode(&d); err != nil {
		return err
	}
	q := repo.New(tx)
	o, err := q.OpportunityByDealForUpdate(ctx, repo.OpportunityByDealForUpdateParams{TenantID: tenant, DealID: &d.DealID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	switch e.Type {
	case "crm.deal.won":
		if o.Status == "won" {
			return nil
		}
		cur := d.Currency
		if err := q.CloseOpportunity(ctx, repo.CloseOpportunityParams{TenantID: tenant, ID: o.ID, Status: "won", RealisedMrrMinor: d.MRR, Currency: &cur}); err != nil {
			return err
		}
		if err := q.SetRecommendationStatus(ctx, repo.SetRecommendationStatusParams{TenantID: tenant, ID: o.RecommendationID, Status: "converted"}); err != nil {
			return err
		}
		return emit(ctx, tx, tenant, "opportunity.won", "revenue_opportunity", o.ID, map[string]any{"opportunity_id": o.ID, "deal_id": d.DealID, "realised_mrr_minor": d.MRR, "currency": cur})
	case "crm.deal.lost":
		if o.Status == "lost" {
			return nil
		}
		if err := q.CloseOpportunity(ctx, repo.CloseOpportunityParams{TenantID: tenant, ID: o.ID, Status: "lost"}); err != nil {
			return err
		}
		return emit(ctx, tx, tenant, "opportunity.lost", "revenue_opportunity", o.ID, map[string]any{"opportunity_id": o.ID, "deal_id": d.DealID})
	default: // reopened
		if o.Status != "won" && o.Status != "lost" {
			return nil
		}
		if err := q.ReopenOpportunity(ctx, repo.ReopenOpportunityParams{TenantID: tenant, ID: o.ID}); err != nil {
			return err
		}
		return q.SetRecommendationStatus(ctx, repo.SetRecommendationStatusParams{TenantID: tenant, ID: o.RecommendationID, Status: "accepted"})
	}
}
