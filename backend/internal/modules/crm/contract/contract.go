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

// Converter creates or finds the CRM company for a prospect. It must be idempotent per prospect.
// CRM provides it in Phase 4; until then no converter is registered.
type Converter interface {
	ConvertProspect(ctx context.Context, in ProspectConversion) (crmCompanyID uuid.UUID, err error)
}
