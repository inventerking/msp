// Package app is Customer Expansion's application service. In Phase 4 it records contracts only
// (D-03), so revenue reporting has one source of record whether or not the module is bought.
package app

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/db"
	"mspengine/internal/kernel/events"
	crm "mspengine/internal/modules/crm/contract"
	"mspengine/internal/modules/customerexpansion/repo"
)

// Module is the entitlement key.
const Module = "customer_expansion"

// Service records contracts.
type Service struct {
	pools    *db.Pools
	registry crm.Registry
}

// New creates the service.
func New(pools *db.Pools, registry crm.Registry) *Service {
	return &Service{pools: pools, registry: registry}
}

// Consumers record contracts from won deals. They are passive: they run whether or not the tenant
// has Customer Expansion, so the record exists if the module is bought later (D-03).
func (s *Service) Consumers() []events.Consumer {
	return []events.Consumer{
		{Name: "customer_expansion.contract_recorder", Types: []string{"crm.deal.won", "crm.deal.reopened"}, Class: events.Passive, Handler: s.onDeal},
	}
}

type dealEvent struct {
	DealID        uuid.UUID `json:"deal_id"`
	CRMCompanyID  uuid.UUID `json:"crm_company_id"`
	Currency      string    `json:"currency"`
	ContractValue *int64    `json:"contract_value_minor"`
	MRR           *int64    `json:"expected_mrr_minor"`
	ClosedOn      string    `json:"closed_on"`
}

func (s *Service) onDeal(ctx context.Context, tx pgx.Tx, e events.Event) error {
	if e.TenantID == nil {
		return nil
	}
	var p dealEvent
	if err := e.Decode(&p); err != nil {
		return err
	}
	q := repo.New(tx)
	if e.Type == "crm.deal.reopened" {
		// A reopened deal is no longer a contract. Winning it again records a fresh one.
		_, err := q.EndContractForDeal(ctx, repo.EndContractForDealParams{TenantID: *e.TenantID, DealID: &p.DealID})
		return err
	}
	summary, err := s.registry.GetCompanySummary(ctx, *e.TenantID, p.CRMCompanyID)
	if err != nil {
		return err
	}
	profile, err := q.EnsureProfile(ctx, repo.EnsureProfileParams{TenantID: *e.TenantID, CrmCompanyID: p.CRMCompanyID, CompanyID: summary.CompanyID})
	if err != nil {
		return err
	}
	start, err := time.Parse(time.DateOnly, p.ClosedOn)
	if err != nil {
		start = e.OccurredAt
	}
	// The deal's monthly value is the contract MRR. A one-off contract value with no monthly value
	// is recorded as setup.
	var mrr, setup int64
	if p.MRR != nil {
		mrr = *p.MRR
	} else if p.ContractValue != nil {
		setup = *p.ContractValue
	}
	_, err = q.InsertContractFromDeal(ctx, repo.InsertContractFromDealParams{TenantID: *e.TenantID, CustomerProfileID: profile, DealID: &p.DealID, StartDate: start, Currency: p.Currency, MrrMinor: mrr, SetupMinor: setup})
	return err
}
