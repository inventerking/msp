package app

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/events"
	"mspengine/internal/modules/customerexpansion/domain"
	"mspengine/internal/modules/customerexpansion/repo"
	"mspengine/internal/platform/audit"
)

// revenue is the part of a contract that decides MRR movements.
type revenue struct {
	Status    string
	MRR       int64
	Currency  string
	End       *time.Time
	AutoRenew *bool
}

// recordMovement publishes how a contract change moved MRR, comparing before with after inside the
// same transaction. Sales Intelligence counts MRR from these events only (D-03).
func recordMovement(ctx context.Context, q *repo.Queries, tx pgx.Tx, tenant, crmCompany, profile, contract uuid.UUID, deal *uuid.UUID, before *revenue, after revenue) error {
	was := before != nil && domain.CountsAsMRR(before.Status)
	is := domain.CountsAsMRR(after.Status)
	if !was && !is {
		return nil
	}
	other, err := q.OtherActiveMRR(ctx, repo.OtherActiveMRRParams{TenantID: tenant, CustomerProfileID: profile, ExcludeID: contract})
	if err != nil {
		return err
	}
	payload := map[string]any{"contract_id": contract, "crm_company_id": crmCompany, "currency": after.Currency, "source_deal_id": deal,
		"end_date": dateString(after.End), "auto_renew": after.AutoRenew}
	switch {
	case !was && is:
		had, err := q.HadRevenue(ctx, repo.HadRevenueParams{TenantID: tenant, CustomerProfileID: profile, ExcludeID: contract})
		if err != nil {
			return err
		}
		payload["movement"], payload["mrr_minor"], payload["mrr_delta_minor"] = domain.Activation(other, had, after.MRR), after.MRR, after.MRR
		return emit(ctx, tx, tenant, "contract.activated", "contract", contract, payload)
	case was && !is:
		payload["currency"] = before.Currency
		payload["movement"], payload["mrr_minor"], payload["mrr_delta_minor"] = domain.End(other, before.MRR), int64(0), -before.MRR
		return emit(ctx, tx, tenant, "contract.ended", "contract", contract, payload)
	default:
		datesChanged := dateString(before.End) != dateString(after.End) || boolOf(before.AutoRenew) != boolOf(after.AutoRenew)
		if before.MRR == after.MRR && !datesChanged {
			return nil
		}
		payload["movement"], payload["mrr_minor"], payload["mrr_delta_minor"] = domain.Change(before.MRR, after.MRR), after.MRR, after.MRR-before.MRR
		return emit(ctx, tx, tenant, "contract.changed", "contract", contract, payload)
	}
}

func dateString(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(time.DateOnly)
	return &s
}

func boolOf(b *bool) bool { return b != nil && *b }

// endFromTerm stores the end date a term implies, so a renewal can always be found by its date.
func endFromTerm(start time.Time, end *time.Time, term *int32) *time.Time {
	return domain.Contract{Start: start, End: end, TermMonths: term}.EffectiveEnd()
}

type dealEvent struct {
	DealID       uuid.UUID  `json:"deal_id"`
	CRMCompanyID uuid.UUID  `json:"crm_company_id"`
	CompanyID    *uuid.UUID `json:"company_id"`
	Currency     string     `json:"currency"`
	Value        *int64     `json:"contract_value_minor"`
	MRR          *int64     `json:"expected_mrr_minor"`
	ClosedOn     string     `json:"closed_on"`
	Owner        *uuid.UUID `json:"owner_user_id"`
}

type proposalEvent struct {
	VersionID    uuid.UUID  `json:"version_id"`
	CRMCompanyID uuid.UUID  `json:"crm_company_id"`
	DealID       *uuid.UUID `json:"deal_id"`
	Currency     string     `json:"currency"`
	MRR          int64      `json:"mrr_total_minor"`
	Setup        int64      `json:"setup_total_minor"`
	Term         *int32     `json:"term_months"`
}

// onContractSource keeps contracts in step with deals and signed proposals. A signed proposal is
// the better source for MRR than the deal's estimate; when they disagree the contract is flagged for
// a person to review, never silently resolved.
func (s *Service) onContractSource(ctx context.Context, tx pgx.Tx, e events.Event) error {
	if e.TenantID == nil {
		return nil
	}
	tenant := *e.TenantID
	q := repo.New(tx)
	switch e.Type {
	case "proposals.proposal.signed":
		var p proposalEvent
		if err := e.Decode(&p); err != nil {
			return err
		}
		if p.DealID == nil || !domain.ValidCurrency(p.Currency) {
			return nil // A contract follows a deal; a proposal without one waits for a person to add it.
		}
		return s.applyProposal(ctx, q, tx, tenant, p)
	case "crm.deal.reopened":
		var d dealEvent
		if err := e.Decode(&d); err != nil {
			return err
		}
		c, err := q.ContractByDealForUpdate(ctx, repo.ContractByDealForUpdateParams{TenantID: tenant, DealID: &d.DealID})
		if errors.Is(err, pgx.ErrNoRows) || err == nil && !domain.CountsAsMRR(c.Status) {
			return nil
		}
		if err != nil {
			return err
		}
		// The deal is no longer won, so the contract returns to draft for review.
		if err := q.SetContractStatus(ctx, repo.SetContractStatusParams{TenantID: tenant, ID: c.ID, Status: "draft", ReviewReason: ptr("deal_reopened")}); err != nil {
			return err
		}
		if err := q.ResolveAlertsForContract(ctx, repo.ResolveAlertsForContractParams{TenantID: tenant, ContractID: c.ID}); err != nil {
			return err
		}
		before := revenue{Status: c.Status, MRR: c.MrrMinor, Currency: c.Currency, End: c.EndDate, AutoRenew: c.AutoRenew}
		after := before
		after.Status = "draft"
		return recordMovement(ctx, q, tx, tenant, d.CRMCompanyID, c.CustomerProfileID, c.ID, &d.DealID, &before, after)
	default: // crm.deal.won
		var d dealEvent
		if err := e.Decode(&d); err != nil {
			return err
		}
		return s.activateFromDeal(ctx, q, tx, tenant, d, e.OccurredAt)
	}
}

func (s *Service) profileFor(ctx context.Context, q *repo.Queries, tenant, crmCompany uuid.UUID, company *uuid.UUID) (uuid.UUID, error) {
	if company == nil {
		summary, err := s.Registry.GetCompanySummary(ctx, tenant, crmCompany)
		if err != nil {
			return uuid.Nil, err
		}
		company = summary.CompanyID
	}
	return q.EnsureProfile(ctx, repo.EnsureProfileParams{TenantID: tenant, CrmCompanyID: crmCompany, CompanyID: company})
}

func (s *Service) activateFromDeal(ctx context.Context, q *repo.Queries, tx pgx.Tx, tenant uuid.UUID, d dealEvent, occurred time.Time) error {
	start, err := time.Parse(time.DateOnly, d.ClosedOn)
	if err != nil {
		start = occurred.UTC().Truncate(24 * time.Hour)
	}
	// The deal's monthly value is the contract MRR. A one-off value with no monthly value is setup.
	var mrr, setup int64
	if d.MRR != nil {
		mrr = *d.MRR
	} else if d.Value != nil {
		setup = *d.Value
	}
	c, err := q.ContractByDealForUpdate(ctx, repo.ContractByDealForUpdateParams{TenantID: tenant, DealID: &d.DealID})
	if errors.Is(err, pgx.ErrNoRows) {
		profile, err := s.profileFor(ctx, q, tenant, d.CRMCompanyID, d.CompanyID)
		if err != nil {
			return err
		}
		id, err := q.InsertContract(ctx, repo.InsertContractParams{TenantID: tenant, CustomerProfileID: profile, DealID: &d.DealID, Status: "active", StartDate: start,
			Currency: d.Currency, MrrMinor: mrr, SetupMinor: setup, Source: "won_deal", OwnerUserID: d.Owner})
		if err != nil {
			return err
		}
		return recordMovement(ctx, q, tx, tenant, d.CRMCompanyID, profile, id, &d.DealID, nil, revenue{Status: "active", MRR: mrr, Currency: d.Currency})
	}
	if err != nil {
		return err
	}
	if domain.CountsAsMRR(c.Status) {
		return nil // Already recorded: a replay, or a second win of a deal that was never reopened.
	}
	var review *string
	if c.ProposalVersionID != nil {
		// A signed proposal already set the commercial terms. They win over the deal's estimate.
		if d.MRR != nil && *d.MRR != c.MrrMinor {
			review = ptr("proposal_deal_mismatch")
		}
		mrr, setup, d.Currency = c.MrrMinor, c.SetupMinor, c.Currency
	}
	end := endFromTerm(start, nil, c.TermMonths)
	if err := q.ActivateContract(ctx, repo.ActivateContractParams{TenantID: tenant, ID: c.ID, StartDate: start, EndDate: end, Currency: d.Currency, MrrMinor: mrr,
		SetupMinor: setup, OwnerUserID: d.Owner, ReviewReason: review}); err != nil {
		return err
	}
	before := revenue{Status: c.Status, MRR: c.MrrMinor, Currency: c.Currency}
	return recordMovement(ctx, q, tx, tenant, d.CRMCompanyID, c.CustomerProfileID, c.ID, &d.DealID, &before, revenue{Status: "active", MRR: mrr, Currency: d.Currency, End: end, AutoRenew: c.AutoRenew})
}

func (s *Service) applyProposal(ctx context.Context, q *repo.Queries, tx pgx.Tx, tenant uuid.UUID, p proposalEvent) error {
	c, err := q.ContractByDealForUpdate(ctx, repo.ContractByDealForUpdateParams{TenantID: tenant, DealID: p.DealID})
	if errors.Is(err, pgx.ErrNoRows) {
		// Signed before the deal is won: keep the terms in a draft, which the win activates.
		profile, err := s.profileFor(ctx, q, tenant, p.CRMCompanyID, nil)
		if err != nil {
			return err
		}
		_, err = q.InsertContract(ctx, repo.InsertContractParams{TenantID: tenant, CustomerProfileID: profile, DealID: p.DealID, ProposalVersionID: &p.VersionID,
			Status: "draft", StartDate: s.today(), TermMonths: p.Term, Currency: p.Currency, MrrMinor: p.MRR, SetupMinor: p.Setup, Source: "signed_proposal"})
		return err
	}
	if err != nil {
		return err
	}
	var review *string
	if domain.CountsAsMRR(c.Status) && c.MrrMinor != p.MRR {
		review = ptr("proposal_deal_mismatch")
	}
	if err := q.ApplyProposal(ctx, repo.ApplyProposalParams{TenantID: tenant, ID: c.ID, ProposalVersionID: &p.VersionID, MrrMinor: p.MRR, SetupMinor: p.Setup,
		TermMonths: p.Term, Currency: p.Currency, ReviewReason: review}); err != nil {
		return err
	}
	before := revenue{Status: c.Status, MRR: c.MrrMinor, Currency: c.Currency, End: c.EndDate, AutoRenew: c.AutoRenew}
	after := before
	after.MRR, after.Currency = p.MRR, p.Currency
	return recordMovement(ctx, q, tx, tenant, p.CRMCompanyID, c.CustomerProfileID, c.ID, p.DealID, &before, after)
}

// ContractInput creates or changes a contract by hand.
type ContractInput struct {
	Status            *string
	Start             *time.Time
	End               *time.Time
	SetEnd            bool
	TermMonths        *int32
	SetTerm           bool
	AutoRenew         *bool
	NoticeDays        *int32
	SetNotice         bool
	Currency          *string
	MRR               *int64
	Setup             *int64
	ProposalVersionID *uuid.UUID
	DealID            *uuid.UUID
	ClearReview       bool
}

// transitions are the status changes a person may make. Draft is reached only when a won deal is
// reopened, so CRM and the contract agree.
var transitions = map[string]map[string]bool{
	"draft":    {"active": true, "cancelled": true},
	"active":   {"renewing": true, "ended": true, "cancelled": true},
	"renewing": {"active": true, "ended": true, "cancelled": true},
}

// ContractView is a contract as shown.
type ContractView = repo.ListContractsRow

// Contracts lists a customer's contracts.
func (s *Service) Contracts(ctx context.Context, p authctx.Principal, profile uuid.UUID) ([]ContractView, error) {
	var out []ContractView
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		if _, err := q.GetProfile(ctx, repo.GetProfileParams{TenantID: p.TenantID, ID: profile}); err != nil {
			return notFound(err)
		}
		var err error
		out, err = q.ListContracts(ctx, repo.ListContractsParams{TenantID: p.TenantID, CustomerProfileID: profile})
		return err
	})
	return out, err
}

// CreateContract records a contract by hand. A linked deal must be the same customer's.
func (s *Service) CreateContract(ctx context.Context, p authctx.Principal, profile uuid.UUID, in ContractInput) (uuid.UUID, error) {
	if in.Start == nil || in.Currency == nil || in.MRR == nil {
		return uuid.Nil, invalid("start_date", "required")
	}
	setup := int64(0)
	if in.Setup != nil {
		setup = *in.Setup
	}
	if f, c := domain.ValidateContract(*in.Start, in.End, in.TermMonths, in.NoticeDays, *in.Currency, *in.MRR, setup); f != "" {
		return uuid.Nil, invalid(f, c)
	}
	status := "active"
	if in.Status != nil {
		status = *in.Status
	}
	if status != "active" && status != "draft" {
		return uuid.Nil, invalid("status", "invalid_status")
	}
	var crmCompany uuid.UUID
	var id uuid.UUID
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		pr, err := q.GetProfile(ctx, repo.GetProfileParams{TenantID: p.TenantID, ID: profile})
		if err != nil {
			return notFound(err)
		}
		crmCompany = pr.CrmCompanyID
		if in.DealID != nil {
			if err := s.checkDeal(ctx, p.TenantID, *in.DealID, pr.CrmCompanyID); err != nil {
				return err
			}
		}
		end := endFromTerm(*in.Start, in.End, in.TermMonths)
		id, err = q.InsertContract(ctx, repo.InsertContractParams{TenantID: p.TenantID, CustomerProfileID: profile, DealID: in.DealID, ProposalVersionID: in.ProposalVersionID,
			Status: status, StartDate: *in.Start, EndDate: end, TermMonths: in.TermMonths, AutoRenew: in.AutoRenew, NoticeDays: in.NoticeDays,
			Currency: *in.Currency, MrrMinor: *in.MRR, SetupMinor: setup, Source: "manual", OwnerUserID: uid(p)})
		if pgUnique(err) {
			return apperr.Conflict("deal_has_contract", "That deal already has a contract")
		}
		if err != nil {
			return err
		}
		if err := recordMovement(ctx, q, tx, p.TenantID, pr.CrmCompanyID, profile, id, in.DealID, nil, revenue{Status: status, MRR: *in.MRR, Currency: *in.Currency, End: end, AutoRenew: in.AutoRenew}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "expansion.contract_created", "contract", &id, map[string]any{"mrr_minor": *in.MRR, "currency": *in.Currency, "status": status}))
	})
	if err == nil && status == "active" {
		s.markCustomer(ctx, p.TenantID, crmCompany)
	}
	return id, err
}

func (s *Service) checkDeal(ctx context.Context, tenant, deal, crmCompany uuid.UUID) error {
	if s.Deals == nil {
		return invalid("deal_id", "unknown_deal")
	}
	d, err := s.Deals.GetDealSummary(ctx, tenant, deal)
	if err != nil || d.CRMCompanyID != crmCompany {
		return invalid("deal_id", "unknown_deal")
	}
	return nil
}

// markCustomer sets the CRM lifecycle to customer. It is a separate step, so a failure is logged
// and the contract still stands.
func (s *Service) markCustomer(ctx context.Context, tenant, crmCompany uuid.UUID) {
	summary, err := s.Registry.GetCompanySummary(ctx, tenant, crmCompany)
	if err == nil && summary.Lifecycle == "customer" {
		return
	}
	if err == nil {
		err = s.Registry.SetLifecycle(ctx, tenant, crmCompany, "customer")
	}
	if err != nil {
		slog.WarnContext(ctx, "set lifecycle failed", "error", err)
	}
}

// UpdateContract changes a contract. MRR before and after are audited, and the movement is published.
func (s *Service) UpdateContract(ctx context.Context, p authctx.Principal, id uuid.UUID, in ContractInput) error {
	activated := false
	var crmCompany uuid.UUID
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		c, err := q.GetContractForUpdate(ctx, repo.GetContractForUpdateParams{TenantID: p.TenantID, ID: id})
		if err != nil {
			return notFound(err)
		}
		crmCompany = c.CrmCompanyID
		next := repo.UpdateContractParams{TenantID: p.TenantID, ID: id, Status: c.Status, StartDate: c.StartDate, EndDate: c.EndDate, TermMonths: c.TermMonths,
			AutoRenew: c.AutoRenew, NoticeDays: c.NoticeDays, Currency: c.Currency, MrrMinor: c.MrrMinor, SetupMinor: c.SetupMinor, ReviewReason: c.ReviewReason}
		if in.Status != nil && *in.Status != c.Status {
			if !transitions[c.Status][*in.Status] {
				return apperr.Conflict("invalid_transition", "A "+c.Status+" contract cannot become "+*in.Status)
			}
			next.Status = *in.Status
		}
		if in.Start != nil {
			next.StartDate = *in.Start
		}
		if in.SetEnd {
			next.EndDate = in.End
		}
		if in.SetTerm {
			next.TermMonths = in.TermMonths
		}
		if in.AutoRenew != nil {
			next.AutoRenew = in.AutoRenew
		}
		if in.SetNotice {
			next.NoticeDays = in.NoticeDays
		}
		if in.Currency != nil {
			if domain.CountsAsMRR(c.Status) && *in.Currency != c.Currency {
				return invalid("currency", "end_and_replace_to_change_currency")
			}
			next.Currency = *in.Currency
		}
		if in.MRR != nil {
			next.MrrMinor = *in.MRR
		}
		if in.Setup != nil {
			next.SetupMinor = *in.Setup
		}
		if in.ClearReview {
			next.ReviewReason = nil
		}
		if (next.Status == "ended" || next.Status == "cancelled") && next.EndDate == nil {
			next.EndDate = ptr(s.today())
		}
		if !in.SetEnd && in.SetTerm {
			next.EndDate = endFromTerm(next.StartDate, nil, next.TermMonths)
		}
		if f, code := domain.ValidateContract(next.StartDate, next.EndDate, next.TermMonths, next.NoticeDays, next.Currency, next.MrrMinor, next.SetupMinor); f != "" {
			return invalid(f, code)
		}
		if err := q.UpdateContract(ctx, next); err != nil {
			return err
		}
		if !domain.CountsAsMRR(next.Status) || dateString(c.EndDate) != dateString(next.EndDate) {
			if err := q.ResolveAlertsForContract(ctx, repo.ResolveAlertsForContractParams{TenantID: p.TenantID, ContractID: id}); err != nil {
				return err
			}
		}
		activated = !domain.CountsAsMRR(c.Status) && domain.CountsAsMRR(next.Status)
		before := revenue{Status: c.Status, MRR: c.MrrMinor, Currency: c.Currency, End: c.EndDate, AutoRenew: c.AutoRenew}
		after := revenue{Status: next.Status, MRR: next.MrrMinor, Currency: next.Currency, End: next.EndDate, AutoRenew: next.AutoRenew}
		if err := recordMovement(ctx, q, tx, p.TenantID, c.CrmCompanyID, c.CustomerProfileID, id, c.DealID, &before, after); err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "expansion.contract_changed", "contract", &id, map[string]any{
			"before": map[string]any{"status": c.Status, "mrr_minor": c.MrrMinor, "end_date": dateString(c.EndDate)},
			"after":  map[string]any{"status": next.Status, "mrr_minor": next.MrrMinor, "end_date": dateString(next.EndDate)}}))
	})
	if err == nil && activated {
		s.markCustomer(ctx, p.TenantID, crmCompany)
	}
	return err
}
