package integration

import (
	"context"
	"time"
)

// These are the provider-neutral capabilities. Business modules depend on these, never on a vendor.
// Real adapters arrive with the phases that need them, once D-05 selects each provider.

// CompanyQuery describes a company search.
type CompanyQuery struct {
	Countries   []string
	Industries  []string
	EmployeeMin int
	EmployeeMax int
	Technology  []string
	Limit       int
}

// CompanyRecord is one result. Licence says whether it may be shared across tenants.
type CompanyRecord struct {
	ProviderID string
	Name       string
	Domain     string
	Country    string
	Industry   string
	Employees  int
	Licence    string
}

// CompanyData finds and enriches companies. No results is not an error: a caller must be able to
// tell "no data" from "provider failed", because missing data is not negative evidence.
type CompanyData interface {
	Key() string
	SearchCompanies(ctx context.Context, q CompanyQuery) ([]CompanyRecord, error)
	EnrichCompany(ctx context.Context, domain string) (*CompanyRecord, error)
}

// OutboundEmail is one message to send.
type OutboundEmail struct {
	From, To, Subject, Body string
	Headers                 map[string]string
	// IdempotencyKey prevents a double send where the provider supports it.
	IdempotencyKey string
}

// EmailSender sends email and reports the provider's message ID.
type EmailSender interface {
	Key() string
	Send(ctx context.Context, m OutboundEmail) (providerMessageID string, err error)
}

// Slot is a time range.
type Slot struct{ Start, End time.Time }

// Calendar reads availability and books events.
type Calendar interface {
	Key() string
	Busy(ctx context.Context, connection string, from, to time.Time) ([]Slot, error)
	CreateEvent(ctx context.Context, connection, title string, s Slot, idempotencyKey string) (eventID string, err error)
	CancelEvent(ctx context.Context, connection, eventID string) error
}
