// Package contract is the only part of Proposals that other modules may import.
package contract

import (
	"context"

	"github.com/google/uuid"
)

// IssuedSummary is what another module may know about an issued proposal version.
type IssuedSummary struct {
	VersionID       uuid.UUID
	ProposalID      uuid.UUID
	CRMCompanyID    uuid.UUID
	DealID          *uuid.UUID
	Currency        string
	SetupTotalMinor int64
	MRRTotalMinor   int64
	TermMonths      int32
	TCVMinor        int64
	Signed          bool
}

// Issued reads issued versions, for Customer Expansion's contract records.
type Issued interface {
	GetIssuedSummary(ctx context.Context, tenantID, versionID uuid.UUID) (IssuedSummary, error)
}
