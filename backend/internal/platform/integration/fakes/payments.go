package fakes

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mspengine/internal/platform/integration"
)

// FakePaymentsKey is the fake provider's key.
const FakePaymentsKey = "fake_payments"

// FakePayments is a payment provider that moves no money. Its checkout page is part of the client
// app, its records are kept in integration.fake_payment_ledger, and its behaviour is chosen by the
// tester at checkout: pay, pay with a card that will decline later, or decline. It is registered only
// when fake providers are on.
type FakePayments struct {
	Pool    *pgxpool.Pool
	BaseURL string
}

// Key names the provider.
func (*FakePayments) Key() string { return FakePaymentsKey }

// CreateCheckout records an open checkout. Asking again with the same key returns the same one.
func (f *FakePayments) CreateCheckout(ctx context.Context, r integration.CheckoutRequest) (integration.Checkout, error) {
	ref := "fpay_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	err := f.Pool.QueryRow(ctx, `INSERT INTO integration.fake_payment_ledger (ref, kind, status, amount_minor, currency, idempotency_key, description)
		VALUES ($1, 'checkout', 'open', $2, $3, $4, $5)
		ON CONFLICT (idempotency_key) DO UPDATE SET updated_at = now() RETURNING ref`, ref, r.Amount, r.Currency, r.IdempotencyKey, r.Description).Scan(&ref)
	if err != nil {
		return integration.Checkout{}, err
	}
	return integration.Checkout{Ref: ref, URL: strings.TrimRight(f.BaseURL, "/") + "/fake-checkout/" + ref}, nil
}

// CheckoutInfo is what the fake checkout page shows.
type CheckoutInfo struct {
	Ref         string
	Amount      int64
	Currency    string
	Description string
	Status      string
}

// Info returns an open or finished checkout.
func (f *FakePayments) Info(ctx context.Context, ref string) (CheckoutInfo, error) {
	var c CheckoutInfo
	var desc *string
	err := f.Pool.QueryRow(ctx, `SELECT ref, amount_minor, currency, description, status FROM integration.fake_payment_ledger WHERE ref = $1 AND kind = 'checkout'`, ref).
		Scan(&c.Ref, &c.Amount, &c.Currency, &desc, &c.Status)
	if desc != nil {
		c.Description = *desc
	}
	return c, err
}

// Fake checkout outcomes.
const (
	OutcomePay            = "pay"
	OutcomePayThenDecline = "pay_card_declines_later"
	OutcomeDecline        = "decline"
)

// ErrCheckoutClosed means the checkout was already completed.
var ErrCheckoutClosed = errors.New("checkout already completed")

// Complete finishes a checkout as the tester chose, and returns the event a real provider would send.
func (f *FakePayments) Complete(ctx context.Context, ref, outcome string) (integration.PaymentEvent, error) {
	status, method, failure := "succeeded", "fpm_"+strings.TrimPrefix(ref, "fpay_"), ""
	switch outcome {
	case OutcomePay:
	case OutcomePayThenDecline:
		method += "_declines"
	case OutcomeDecline:
		status, method, failure = "failed", "", "card_declined"
	default:
		return integration.PaymentEvent{}, errors.New("unknown outcome")
	}
	var methodRef *string
	if method != "" {
		methodRef = &method
	}
	tag, err := f.Pool.Exec(ctx, `UPDATE integration.fake_payment_ledger SET status = $2, method_ref = $3 WHERE ref = $1 AND kind = 'checkout' AND status = 'open'`, ref, status, methodRef)
	if err != nil {
		return integration.PaymentEvent{}, err
	}
	if tag.RowsAffected() == 0 {
		return integration.PaymentEvent{}, ErrCheckoutClosed
	}
	typ := "payment.succeeded"
	if status == "failed" {
		typ = "payment.failed"
	}
	return integration.PaymentEvent{EventID: "fevt_" + strings.TrimPrefix(ref, "fpay_"), Type: typ, PaymentRef: ref, MethodRef: method, FailureCode: failure}, nil
}

// ChargeSaved charges a saved method. A method saved with the "declines later" outcome always fails.
func (f *FakePayments) ChargeSaved(ctx context.Context, r integration.SavedCharge) (integration.Charge, error) {
	if !strings.HasPrefix(r.MethodRef, "fpm_") {
		return integration.Charge{}, errors.New("unknown payment method")
	}
	status, failure := "succeeded", ""
	if strings.HasSuffix(r.MethodRef, "_declines") {
		status, failure = "failed", "card_declined"
	}
	ref := "fpay_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	var got, gotStatus string
	err := f.Pool.QueryRow(ctx, `INSERT INTO integration.fake_payment_ledger (ref, kind, status, amount_minor, currency, method_ref, idempotency_key, description)
		VALUES ($1, 'charge', $2, $3, $4, $5, $6, $7)
		ON CONFLICT (idempotency_key) DO UPDATE SET updated_at = now() RETURNING ref, status`, ref, status, r.Amount, r.Currency, r.MethodRef, r.IdempotencyKey, r.Description).Scan(&got, &gotStatus)
	if err != nil {
		return integration.Charge{}, err
	}
	if gotStatus == "failed" {
		failure = "card_declined"
	}
	return integration.Charge{Ref: got, Status: gotStatus, FailureCode: failure}, nil
}

// Refund records a refund against a succeeded payment.
func (f *FakePayments) Refund(ctx context.Context, r integration.RefundRequest) (integration.RefundResult, error) {
	tag, err := f.Pool.Exec(ctx, `UPDATE integration.fake_payment_ledger SET refunded_minor = refunded_minor + $2
		WHERE ref = $1 AND status = 'succeeded' AND refunded_minor + $2 <= amount_minor`, r.PaymentRef, r.Amount)
	if err != nil {
		return integration.RefundResult{}, err
	}
	if tag.RowsAffected() == 0 {
		return integration.RefundResult{Status: "failed"}, nil
	}
	return integration.RefundResult{Ref: "fref_" + strings.TrimPrefix(r.PaymentRef, "fpay_"), Status: "succeeded"}, nil
}

// ListPayments returns finished payments since a time.
func (f *FakePayments) ListPayments(ctx context.Context, since time.Time) ([]integration.ProviderPayment, error) {
	rows, err := f.Pool.Query(ctx, `SELECT ref, amount_minor, refunded_minor, currency, status, created_at FROM integration.fake_payment_ledger
		WHERE status <> 'open' AND created_at >= $1 ORDER BY created_at`, since)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (integration.ProviderPayment, error) {
		var p integration.ProviderPayment
		err := r.Scan(&p.Ref, &p.Amount, &p.Refunded, &p.Currency, &p.Status, &p.Created)
		return p, err
	})
}

// ExpireCheckout closes an open checkout. Closing one already finished changes nothing.
func (f *FakePayments) ExpireCheckout(ctx context.Context, ref string) error {
	_, err := f.Pool.Exec(ctx, `UPDATE integration.fake_payment_ledger SET status = 'failed' WHERE ref = $1 AND kind = 'checkout' AND status = 'open'`, ref)
	return err
}
