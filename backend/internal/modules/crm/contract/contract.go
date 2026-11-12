// Package contract is the only part of CRM that other modules may import.
package contract

import (
	"context"

	"github.com/google/uuid"
)

// ProspectConversion is what Lead Generation hands to CRM when a prospect becomes a CRM company.
type ProspectConversion struct {
	TenantID    uuid.UUID
	ActorID     uuid.UUID
	ProspectID  uuid.UUID
	CompanyID   uuid.UUID // The canonical shared company.
	DisplayName string
	Domain      *string
	ContactIDs  []uuid.UUID
}

// Converter creates or finds the CRM company for a prospect. It is idempotent per prospect.
type Converter interface {
	ConvertProspect(ctx context.Context, in ProspectConversion) (crmCompanyID uuid.UUID, err error)
}

// CompanyInput identifies a customer for EnsureCompany. A canonical company ID is preferred; a
// module reference is used when there is none.
type CompanyInput struct {
	TenantID     uuid.UUID
	CompanyID    *uuid.UUID
	Name         string
	Domain       *string
	SourceModule string
	SourceRef    *uuid.UUID
	ActorID      *uuid.UUID
}

// CompanySummary is all another module may learn about a CRM company: never notes or deals.
type CompanySummary struct {
	ID        uuid.UUID
	CompanyID *uuid.UUID
	Name      string
	Domain    *string
	Lifecycle string
}

// Registry is the tenant's customer list. It works whether or not the tenant has the CRM module
// (D-02), so modules bought alone still have customers to refer to.
type Registry interface {
	EnsureCompany(ctx context.Context, in CompanyInput) (uuid.UUID, error)
	EnsureContact(ctx context.Context, tenantID, contactID uuid.UUID, crmCompanyID *uuid.UUID) (uuid.UUID, error)
	GetCompanySummary(ctx context.Context, tenantID, crmCompanyID uuid.UUID) (CompanySummary, error)
	SetLifecycle(ctx context.Context, tenantID, crmCompanyID uuid.UUID, lifecycle string) error
}

// DealSummary is what another module may learn about a deal.
type DealSummary struct {
	ID           uuid.UUID
	CRMCompanyID uuid.UUID
	Title        string
	Status       string
	StageKey     string
	Currency     string
}

// Deals lets other modules read deals and suggest stage changes. Suggestions move a deal forward
// only, and never close it: CRM decides.
type Deals interface {
	GetDealSummary(ctx context.Context, tenantID, dealID uuid.UUID) (DealSummary, error)
	RequestStage(ctx context.Context, tenantID, dealID uuid.UUID, stageKey, reason string) (moved bool, err error)
}
