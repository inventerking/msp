// Package contract is the only part of Lead Generation that other modules may import.
package contract

import (
	"context"

	"github.com/google/uuid"
)

// Lists gives other modules the people on a prospect list, for example as an Outreach audience.
type Lists interface {
	// ListContacts returns the contact IDs of every person on the list's prospects, or not found.
	ListContacts(ctx context.Context, tenantID, listID uuid.UUID) ([]uuid.UUID, error)
}
