package integration

import (
	"context"
	"fmt"
	"time"
)

// PaymentProvider takes payments. No provider or merchant of record is chosen yet (D-04), so this
// interface is what one must offer: a hosted checkout (card details never reach this platform),
// charging a method saved at checkout, refunds, and a list of its own records for reconciliation.
type PaymentProvider interface {
	Key() string
	CreateCheckout(ctx context.Context, r CheckoutRequest) (Checkout, error)
	ChargeSaved(ctx context.Context, r SavedCharge) (Charge, error)
	Refund(ctx context.Context, r RefundRequest) (RefundResult, error)
	ListPayments(ctx context.Context, since time.Time) ([]ProviderPayment, error)
	// ExpireCheckout closes an unpaid checkout, so a cancelled purchase can no longer be paid.
	ExpireCheckout(ctx context.Context, ref string) error
}

// CheckoutRequest starts a hosted payment page.
type CheckoutRequest struct {
	Amount         int64
	Currency       string
	Description    string
	IdempotencyKey string
	ReturnURL      string
	// SaveMethod asks the provider to keep the method for renewals.
	SaveMethod bool
}

// Checkout is where the customer pays. Its outcome arrives later as a PaymentEvent.
type Checkout struct {
	Ref string
	URL string
}

// SavedCharge charges a method saved at an earlier checkout, such as for a renewal.
type SavedCharge struct {
	MethodRef      string
	Amount         int64
	Currency       string
	Description    string
	IdempotencyKey string
}

// Charge is a charge's outcome: succeeded, failed or pending.
type Charge struct {
	Ref         string
	Status      string
	FailureCode string
}

// RefundRequest refunds part or all of a payment.
type RefundRequest struct {
	PaymentRef     string
	Amount         int64
	IdempotencyKey string
}

// RefundResult is a refund's outcome: succeeded, failed or pending.
type RefundResult struct {
	Ref    string
	Status string
}

// ProviderPayment is the provider's own record of a payment.
type ProviderPayment struct {
	Ref      string
	Amount   int64
	Refunded int64
	Currency string
	Status   string
	Created  time.Time
}

// PaymentEvent is a provider's report about a payment, after its adapter has checked and parsed it.
type PaymentEvent struct {
	EventID     string
	Type        string // payment.succeeded or payment.failed
	PaymentRef  string
	MethodRef   string
	FailureCode string
}

// RegisterPaymentProvider makes a provider available by key.
func (s *Service) RegisterPaymentProvider(p PaymentProvider) {
	if s.payments == nil {
		s.payments = map[string]PaymentProvider{}
	}
	s.payments[p.Key()] = p
}

// PaymentProvider returns a registered provider.
func (s *Service) PaymentProvider(key string) (PaymentProvider, error) {
	p, ok := s.payments[key]
	if !ok {
		return nil, fmt.Errorf("payment provider %q is not available", key)
	}
	return p, nil
}

// PaymentProviders lists the keys of registered providers.
func (s *Service) PaymentProviders() []string {
	out := make([]string, 0, len(s.payments))
	for k := range s.payments {
		out = append(out, k)
	}
	return out
}
