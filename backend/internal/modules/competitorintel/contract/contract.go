// Package contract is the only part of Competitor Intelligence that other modules may import.
package contract

import (
	"context"

	"github.com/google/uuid"
)

// Directory answers whether a company is one of a tenant's competitors, so Lead Generation can warn
// before treating a competitor as a prospect. The answer is private to the tenant.
type Directory interface {
	IsCompetitor(ctx context.Context, tenantID, companyID uuid.UUID) (bool, error)
}
