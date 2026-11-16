// Package fakes holds providers that send nothing outside the system. They run in tests and in
// non-production environments, until D-05 selects real providers.
package fakes

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"mspengine/internal/kernel/mailer"
	"mspengine/internal/platform/integration"
)

// CompanyData is a fake company data provider.
type CompanyData struct{ Records []integration.CompanyRecord }

// Key names the provider.
func (*CompanyData) Key() string { return "fake_company_data" }

// SearchCompanies filters the fake records.
func (f *CompanyData) SearchCompanies(_ context.Context, q integration.CompanyQuery) ([]integration.CompanyRecord, error) {
	var out []integration.CompanyRecord
	for _, r := range f.Records {
		if len(q.Countries) > 0 && !contains(q.Countries, r.Country) {
			continue
		}
		if len(q.Industries) > 0 && !contains(q.Industries, r.Industry) {
			continue
		}
		if (q.EmployeeMin > 0 && r.Employees < q.EmployeeMin) || (q.EmployeeMax > 0 && r.Employees > q.EmployeeMax) {
			continue
		}
		out = append(out, r)
	}
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, nil
}

// EnrichCompany finds a record by domain, or returns nil for no data.
func (f *CompanyData) EnrichCompany(_ context.Context, domain string) (*integration.CompanyRecord, error) {
	for i := range f.Records {
		if f.Records[i].Domain == domain {
			return &f.Records[i], nil
		}
	}
	return nil, nil
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// Email records what would have been sent. Repeating an idempotency key sends nothing new.
type Email struct {
	mu   sync.Mutex
	Sent []integration.OutboundEmail
	seen map[string]string
}

// Key names the provider.
func (*Email) Key() string { return "fake_email" }

// Send records a message.
func (f *Email) Send(_ context.Context, m integration.OutboundEmail) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.seen == nil {
		f.seen = map[string]string{}
	}
	if id, dup := f.seen[m.IdempotencyKey]; dup && m.IdempotencyKey != "" {
		return id, nil
	}
	f.Sent = append(f.Sent, m)
	id := "fake-" + m.IdempotencyKey
	f.seen[m.IdempotencyKey] = id
	return id, nil
}

// Calendar is a fake calendar with no double-booking.
type Calendar struct {
	mu     sync.Mutex
	events map[string]integration.Slot
	keys   map[string]string
}

// Key names the provider.
func (*Calendar) Key() string { return "fake_calendar" }

// Busy lists booked slots in a range, in order.
func (f *Calendar) Busy(_ context.Context, _ string, from, to time.Time) ([]integration.Slot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []integration.Slot
	for _, s := range f.events {
		if s.Start.Before(to) && s.End.After(from) {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out, nil
}

// CreateEvent books a slot, or fails if it overlaps another booking.
func (f *Calendar) CreateEvent(_ context.Context, _, _ string, s integration.Slot, key string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.events == nil {
		f.events, f.keys = map[string]integration.Slot{}, map[string]string{}
	}
	if id, ok := f.keys[key]; ok {
		return id, nil
	}
	for _, e := range f.events {
		if s.Start.Before(e.End) && s.End.After(e.Start) {
			return "", &integration.ProviderError{Class: integration.Permanent, Err: errors.New("slot unavailable")}
		}
	}
	id := "evt-" + key
	f.events[id] = s
	f.keys[key] = id
	return id, nil
}

// CancelEvent removes a booking.
func (f *Calendar) CancelEvent(_ context.Context, _, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.events, id)
	return nil
}

// SinkEmail sends through the platform mailer, which is the Mailpit sink outside production, so
// outreach can be seen end to end while reaching no real inbox.
type SinkEmail struct {
	Mailer interface {
		Send(ctx context.Context, m mailer.Message) error
	}
}

// Key names the provider.
func (*SinkEmail) Key() string { return "sink_email" }

// Send delivers to the sink and returns a message ID derived from the idempotency key.
func (s *SinkEmail) Send(ctx context.Context, m integration.OutboundEmail) (string, error) {
	headers := map[string]string{"Message-ID": "<" + m.IdempotencyKey + "@sink.local>"}
	for k, v := range m.Headers {
		headers[k] = v
	}
	if err := s.Mailer.Send(ctx, mailer.Message{To: m.To, From: m.From, Subject: m.Subject, Body: m.Body, Headers: headers}); err != nil {
		return "", err
	}
	return "sink-" + m.IdempotencyKey, nil
}
