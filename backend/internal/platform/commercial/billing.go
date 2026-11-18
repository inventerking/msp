package commercial

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/db"
	"mspengine/internal/kernel/events"
	"mspengine/internal/platform/audit"
	"mspengine/internal/platform/commercial/repo"
	"mspengine/internal/platform/integration"
)

// Payment providers' keys. Manual payments are bank transfers that an admin confirms.
const (
	ManualPayments = "manual_payments"
)

// Notifier sends in-app and email notifications inside a transaction.
type Notifier interface {
	Notify(ctx context.Context, tx pgx.Tx, tenantID, userID uuid.UUID, key string, data map[string]string, idempotencyKey string) error
}

// Billing are the services purchases and renewals use, set once at start-up.
type Billing struct {
	Integration *integration.Service
	Notify      Notifier
	// Owner returns a tenant's owner, who receives renewal and dunning notices.
	Owner func(ctx context.Context, tenantID uuid.UUID) (*uuid.UUID, error)
	// CardProvider is the provider used for card payments. Empty means none, so card is unavailable.
	CardProvider string
	BaseURL      string
	Now          func() time.Time
}

// SetBilling wires purchases and renewals.
func (s *Service) SetBilling(b Billing) {
	if b.Now == nil {
		b.Now = time.Now
	}
	s.billing = b
}

func (s *Service) now() time.Time {
	if s.billing.Now != nil {
		return s.billing.Now()
	}
	return time.Now()
}

// DunningPolicy says how unpaid renewals are chased (D-18). Days count from the first failure.
type DunningPolicy struct {
	RetryDays           []int `json:"retry_days"`
	SuspendAfterDays    int   `json:"suspend_after_days"`
	ExpireAfterDays     int   `json:"expire_after_days"`
	BankTransferDueDays int   `json:"bank_transfer_due_days"`
	ReviewTargetHours   int   `json:"review_target_hours"`
}

// Validate checks a policy makes sense: suspension before expiry, retries within the grace period.
func (p DunningPolicy) Validate() error {
	switch {
	case len(p.RetryDays) > 10:
		return apperr.Validation(apperr.FieldError{Field: "retry_days", Code: "too_many"})
	case p.SuspendAfterDays < 1 || p.ExpireAfterDays <= p.SuspendAfterDays || p.ExpireAfterDays > 365:
		return apperr.Validation(apperr.FieldError{Field: "expire_after_days", Code: "must_follow_suspension"})
	case p.BankTransferDueDays < 1 || p.BankTransferDueDays > 90:
		return apperr.Validation(apperr.FieldError{Field: "bank_transfer_due_days", Code: "out_of_range"})
	case p.ReviewTargetHours < 1 || p.ReviewTargetHours > 720:
		return apperr.Validation(apperr.FieldError{Field: "review_target_hours", Code: "out_of_range"})
	}
	last := 0
	for _, d := range p.RetryDays {
		if d <= last || d >= p.ExpireAfterDays {
			return apperr.Validation(apperr.FieldError{Field: "retry_days", Code: "must_increase_within_grace"})
		}
		last = d
	}
	return nil
}

var defaultPolicy = DunningPolicy{RetryDays: []int{1, 3, 7}, SuspendAfterDays: 14, ExpireAfterDays: 30, BankTransferDueDays: 14, ReviewTargetHours: 24}

func dunningPolicy(ctx context.Context, q *repo.Queries) DunningPolicy {
	p := defaultPolicy
	if raw, err := q.GetSetting(ctx, "dunning_policy"); err == nil {
		_ = json.Unmarshal(raw, &p)
	}
	return p
}

// Policy returns the dunning policy.
func (s *Service) Policy(ctx context.Context) (DunningPolicy, error) {
	var p DunningPolicy
	err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		p = dunningPolicy(ctx, repo.New(tx))
		return nil
	})
	return p, err
}

// Health is platform-wide billing counts for monitoring, with the review target to compare against.
type Health struct {
	repo.BillingHealthRow
	ReviewTargetHours int
}

// Health reads billing counts for monitoring. It names no tenant.
func (s *Service) Health(ctx context.Context) (Health, error) {
	var h Health
	err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		q := repo.New(tx)
		row, err := q.BillingHealth(ctx)
		h = Health{BillingHealthRow: row, ReviewTargetHours: dunningPolicy(ctx, q).ReviewTargetHours}
		return err
	})
	return h, err
}

// SetPolicy changes the dunning policy. It is audited.
func (s *Service) SetPolicy(ctx context.Context, p DunningPolicy) error {
	if err := p.Validate(); err != nil {
		return err
	}
	raw, _ := json.Marshal(p)
	admin := authctx.AdminFrom(ctx)
	return db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		q := repo.New(tx)
		before := dunningPolicy(ctx, q)
		if err := q.PutSetting(ctx, repo.PutSettingParams{Key: "dunning_policy", Value: raw, UpdatedBy: &admin.AdminID}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, adminEvent(ctx, nil, "billing.dunning_policy_changed", "setting", nil, before, p))
	})
}

// Quote is what a purchase will cost, frozen at submission.
type Quote struct {
	PlanKey       string `json:"plan_key"`
	PlanName      string `json:"plan_name"`
	ProductName   string `json:"product_name"`
	BillingPeriod string `json:"billing_period"`
	TrialDays     int32  `json:"trial_days"`
	Currency      string `json:"currency"`
	UnitAmount    int64  `json:"unit_amount_minor"`
	Quantity      int32  `json:"quantity"`
	Subtotal      int64  `json:"subtotal_minor"`
	Discount      int64  `json:"discount_minor"`
	Total         int64  `json:"total_minor"`
	Promotion     string `json:"promotion_code,omitempty"`
	// Tax is not calculated until a payment provider or merchant of record is chosen (D-04, D-11).
	Tax string `json:"tax"`
}

// PurchaseInput is a tenant's purchase.
type PurchaseInput struct {
	PlanID         uuid.UUID
	Currency       string
	Quantity       int32
	PromotionCode  string
	PaymentMethod  string
	IdempotencyKey string
}

// PurchaseView is a purchase with its quote and payments.
type PurchaseView struct {
	ID              uuid.UUID
	PlanID          uuid.UUID
	PlanName        string
	Status          string
	Quote           Quote
	PaymentMethod   string
	InvoiceID       *uuid.UUID
	RejectionReason *string
	CheckoutURL     *string
	CreatedAt       time.Time
	ReviewedAt      *time.Time
	TenantID        uuid.UUID
}

// PurchasablePlans lists what a tenant can buy, by plan and currency.
func (s *Service) PurchasablePlans(ctx context.Context) ([]repo.PurchasablePlansRow, error) {
	var out []repo.PurchasablePlansRow
	err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		var err error
		out, err = repo.New(tx).PurchasablePlans(ctx)
		return err
	})
	return out, err
}

func invoiceNumber(n int64, at time.Time) string { return fmt.Sprintf("INV-%d-%06d", at.Year(), n) }

func periodEnd(start time.Time, period string) time.Time {
	if period == "annual" {
		return start.AddDate(1, 0, 0)
	}
	return start.AddDate(0, 1, 0) // Monthly; custom periods are billed monthly until defined.
}

func cardRef(p string) bool { return p != "" && p != ManualPayments }

// Submit records a purchase and, for card, starts a checkout. Charging happens at submission, and
// a rejected purchase is refunded (D-01). Submitting again with the same key returns the same one.
// It runs as the platform role, because tenant sessions may not change commercial state; the tenant
// comes from the signed-in principal only.
func (s *Service) Submit(ctx context.Context, p authctx.Principal, in PurchaseInput) (PurchaseView, error) {
	if in.Quantity <= 0 || in.Quantity > 10000 {
		return PurchaseView{}, apperr.Validation(apperr.FieldError{Field: "quantity", Code: "out_of_range"})
	}
	if in.PaymentMethod != "card" && in.PaymentMethod != "bank_transfer" {
		return PurchaseView{}, apperr.Validation(apperr.FieldError{Field: "payment_method", Code: "invalid"})
	}
	if in.PaymentMethod == "card" && s.billing.CardProvider == "" {
		return PurchaseView{}, apperr.New(http.StatusServiceUnavailable, "card_payments_unavailable", "Card payments are not available yet. Choose bank transfer")
	}
	var id uuid.UUID
	var paymentID *uuid.UUID
	var quote Quote
	err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		q := repo.New(tx)
		if existing, err := q.PurchaseByKey(ctx, repo.PurchaseByKeyParams{TenantID: p.TenantID, IdempotencyKey: in.IdempotencyKey}); err == nil {
			id = existing
			return nil
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		plan, err := q.PlanTerms(ctx, in.PlanID)
		if errors.Is(err, pgx.ErrNoRows) || err == nil && (plan.Status != "active" || plan.ProductStatus != "active") {
			return apperr.Validation(apperr.FieldError{Field: "plan_id", Code: "plan_not_available"})
		}
		if err != nil {
			return err
		}
		price, err := q.CurrentPrice(ctx, repo.CurrentPriceParams{PlanID: in.PlanID, Currency: in.Currency})
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.New(http.StatusUnprocessableEntity, "price_unavailable", "This plan has no price in that currency")
		}
		if err != nil {
			return err
		}
		quote = Quote{PlanKey: plan.Key, PlanName: plan.Name, ProductName: plan.ProductName, BillingPeriod: plan.BillingPeriod, TrialDays: plan.TrialDays,
			Currency: in.Currency, UnitAmount: price.AmountMinor, Quantity: in.Quantity, Subtotal: price.AmountMinor * int64(in.Quantity), Tax: "not_calculated"}
		var promo *uuid.UUID
		if code := strings.TrimSpace(in.PromotionCode); code != "" {
			pr, err := q.GetPromotionByCode(ctx, code)
			now := s.now()
			valid := err == nil && pr.Status == "active" && !pr.ValidFrom.After(now) && (pr.ValidTo == nil || pr.ValidTo.After(now)) &&
				(pr.MaxRedemptions == nil || pr.Redemptions < *pr.MaxRedemptions)
			if !valid {
				return apperr.Validation(apperr.FieldError{Field: "promotion_code", Code: "invalid_promotion"})
			}
			switch {
			case pr.Kind == "percent" && pr.PercentOff != nil:
				quote.Discount = int64(float64(quote.Subtotal) * *pr.PercentOff / 100)
			case pr.Kind == "fixed" && pr.AmountOffMinor != nil && pr.Currency != nil && *pr.Currency == in.Currency:
				quote.Discount = min(*pr.AmountOffMinor, quote.Subtotal)
			default:
				return apperr.Validation(apperr.FieldError{Field: "promotion_code", Code: "invalid_promotion"})
			}
			quote.Promotion = code
			promo = &pr.ID
		}
		quote.Total = quote.Subtotal - quote.Discount
		snap, _ := json.Marshal(quote)
		// Free purchases and plans with a trial need no payment now; a trial is charged at its end.
		method, status := in.PaymentMethod, "payment_pending"
		if quote.Total == 0 || plan.TrialDays > 0 {
			method, status = "none", "under_review"
		}
		id, err = q.InsertPurchase(ctx, repo.InsertPurchaseParams{TenantID: p.TenantID, PlanID: in.PlanID, PromotionID: promo, Quantity: in.Quantity,
			QuoteSnapshot: snap, Status: status, IdempotencyKey: in.IdempotencyKey, RequestedBy: p.UserID, Currency: &in.Currency, PaymentMethod: &method})
		if err != nil {
			return err
		}
		if promo != nil {
			if err := q.InsertRedemption(ctx, repo.InsertRedemptionParams{TenantID: p.TenantID, PromotionID: *promo, PurchaseRequestID: id}); err != nil {
				return err
			}
		}
		if method != "none" {
			policy := dunningPolicy(ctx, q)
			due := s.now()
			if method == "bank_transfer" {
				due = due.AddDate(0, 0, policy.BankTransferDueDays)
			}
			inv, err := s.issueInvoice(ctx, q, p.TenantID, nil, quote, due, nil, nil)
			if err != nil {
				return err
			}
			if err := q.SetPurchaseInvoice(ctx, repo.SetPurchaseInvoiceParams{ID: id, InvoiceID: &inv}); err != nil {
				return err
			}
			provider := ManualPayments
			if method == "card" {
				provider = s.billing.CardProvider
			}
			pid, err := q.InsertPayment(ctx, repo.InsertPaymentParams{TenantID: p.TenantID, InvoiceID: &inv, PurchaseRequestID: &id, Provider: provider,
				Method: method, AmountMinor: quote.Total, Currency: in.Currency, IdempotencyKey: ptr("purchase:" + id.String()), Purpose: "purchase"})
			if err != nil {
				return err
			}
			paymentID = &pid
		}
		if err := audit.Record(ctx, tx, audit.Event{TenantID: &p.TenantID, ActorType: "user", ActorID: &p.UserID, Action: "billing.purchase_submitted", ResourceType: "purchase_request",
			ResourceID: &id, After: map[string]any{"plan": quote.PlanKey, "total_minor": quote.Total, "currency": in.Currency, "payment_method": method}}); err != nil {
			return err
		}
		return events.Append(ctx, tx, events.Event{Type: "platform.purchase.submitted", TenantID: &p.TenantID, SourceModule: "platform", AggregateType: "purchase_request", AggregateID: id},
			map[string]any{"purchase_request_id": id, "plan_id": in.PlanID})
	})
	if err != nil {
		return PurchaseView{}, err
	}
	if paymentID != nil && in.PaymentMethod == "card" {
		if _, err := s.startCheckout(ctx, *paymentID, quote.ProductName+": "+quote.PlanName); err != nil {
			// The purchase stands; the tenant can try the payment again from its page.
			slog.WarnContext(ctx, "checkout could not start", "error", err)
		}
	}
	return s.Purchase(ctx, p.TenantID, id)
}

func ptr[T any](v T) *T { return &v }

// issueInvoice creates an issued invoice with one line for the plan and one for any discount.
func (s *Service) issueInvoice(ctx context.Context, q *repo.Queries, tenant uuid.UUID, sub *uuid.UUID, quote Quote, due time.Time, from, to *time.Time) (uuid.UUID, error) {
	n, err := q.NextInvoiceNumber(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	id, err := q.InsertInvoice(ctx, repo.InsertInvoiceParams{TenantID: tenant, SubscriptionID: sub, Number: invoiceNumber(n, s.now()), Currency: quote.Currency,
		SubtotalMinor: quote.Total, DueAt: &due})
	if err != nil {
		return uuid.Nil, err
	}
	desc := quote.ProductName + ": " + quote.PlanName
	if err := q.InsertInvoiceLine(ctx, repo.InsertInvoiceLineParams{TenantID: tenant, InvoiceID: id, Description: desc, Quantity: quote.Quantity,
		UnitAmountMinor: quote.UnitAmount, AmountMinor: quote.Subtotal, PeriodStart: from, PeriodEnd: to}); err != nil {
		return uuid.Nil, err
	}
	if quote.Discount > 0 {
		if err := q.InsertInvoiceLine(ctx, repo.InsertInvoiceLineParams{TenantID: tenant, InvoiceID: id, Description: "Promotion " + quote.Promotion, Quantity: 1,
			UnitAmountMinor: -quote.Discount, AmountMinor: -quote.Discount}); err != nil {
			return uuid.Nil, err
		}
	}
	return id, nil
}

// startCheckout asks the card provider for a hosted checkout for a pending payment. It is called
// outside any transaction, so a slow provider never holds one open.
func (s *Service) startCheckout(ctx context.Context, paymentID uuid.UUID, description string) (string, error) {
	var pay repo.GetPaymentForUpdateRow
	if err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		var err error
		pay, err = repo.New(tx).GetPaymentForUpdate(ctx, paymentID)
		return err
	}); err != nil {
		return "", err
	}
	if pay.Status != "pending" || !cardRef(pay.Provider) {
		return "", apperr.Conflict("payment_not_pending", "This payment is no longer waiting")
	}
	if pay.CheckoutUrl != nil {
		return *pay.CheckoutUrl, nil
	}
	provider, err := s.billing.Integration.PaymentProvider(pay.Provider)
	if err != nil {
		return "", apperr.New(http.StatusServiceUnavailable, "card_payments_unavailable", "Card payments are not available")
	}
	co, err := provider.CreateCheckout(ctx, integration.CheckoutRequest{Amount: pay.AmountMinor, Currency: pay.Currency, Description: description,
		IdempotencyKey: "payment:" + paymentID.String(), ReturnURL: strings.TrimRight(s.billing.BaseURL, "/") + "/subscription", SaveMethod: true})
	if err != nil {
		return "", err
	}
	err = db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		return repo.New(tx).SetPaymentCheckout(ctx, repo.SetPaymentCheckoutParams{ID: paymentID, Ref: &co.Ref, CheckoutUrl: &co.URL})
	})
	return co.URL, err
}

func viewOf(row repo.GetPurchaseForUpdateRow, planName string) PurchaseView {
	var q Quote
	_ = json.Unmarshal(row.QuoteSnapshot, &q)
	method := ""
	if row.PaymentMethod != nil {
		method = *row.PaymentMethod
	}
	return PurchaseView{ID: row.ID, TenantID: row.TenantID, PlanID: row.PlanID, PlanName: planName, Status: row.Status, Quote: q, PaymentMethod: method,
		InvoiceID: row.InvoiceID, RejectionReason: row.RejectionReason, CreatedAt: row.CreatedAt}
}

// Purchase returns one of a tenant's purchases, with its checkout link while payment is waiting.
func (s *Service) Purchase(ctx context.Context, tenant, id uuid.UUID) (PurchaseView, error) {
	var v PurchaseView
	err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		q := repo.New(tx)
		row, err := q.GetPurchaseForUpdate(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) || err == nil && row.TenantID != tenant {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		plan, err := q.GetPlan(ctx, row.PlanID)
		if err != nil {
			return err
		}
		v = viewOf(row, plan.Name)
		pays, err := q.PaymentsForPurchase(ctx, &id)
		for _, pay := range pays {
			if pay.Status == "pending" && pay.CheckoutUrl != nil {
				v.CheckoutURL = pay.CheckoutUrl
			}
		}
		return err
	})
	return v, err
}

// TenantPurchases lists a tenant's purchases, read under its own RLS.
func (s *Service) TenantPurchases(ctx context.Context, tenant uuid.UUID) ([]repo.ListTenantPurchasesRow, error) {
	var out []repo.ListTenantPurchasesRow
	err := db.TenantTx(ctx, s.pools.App, tenant, uuid.Nil, func(tx pgx.Tx) error {
		var err error
		out, err = repo.New(tx).ListTenantPurchases(ctx, tenant)
		return err
	})
	return out, err
}

// CancelPurchase withdraws a purchase that is still waiting for payment or review.
func (s *Service) CancelPurchase(ctx context.Context, p authctx.Principal, id uuid.UUID) error {
	var checkouts []checkoutRef
	err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		q := repo.New(tx)
		row, err := q.GetPurchaseForUpdate(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) || err == nil && row.TenantID != p.TenantID {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		if row.Status != "payment_pending" {
			return apperr.Conflict("purchase_not_cancellable", "Only a purchase waiting for payment can be cancelled. Ask for a refund instead")
		}
		if _, err := q.SetPurchaseStatus(ctx, repo.SetPurchaseStatusParams{ID: id, Status: "cancelled", FromStatuses: []string{"payment_pending"}}); err != nil {
			return err
		}
		if checkouts, err = s.voidUnpaid(ctx, q, row.InvoiceID, &id, "purchase_cancelled"); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{TenantID: &p.TenantID, ActorType: "user", ActorID: &p.UserID, Action: "billing.purchase_cancelled", ResourceType: "purchase_request", ResourceID: &id})
	})
	if err == nil {
		s.expireCheckouts(ctx, checkouts)
	}
	return err
}

// expireCheckouts closes provider checkouts for payments that will no longer be accepted. It runs
// after commit; a failure is logged, and a late payment would be reported by reconciliation.
func (s *Service) expireCheckouts(ctx context.Context, checkouts []checkoutRef) {
	for _, c := range checkouts {
		provider, err := s.billing.Integration.PaymentProvider(c.provider)
		if err == nil {
			err = provider.ExpireCheckout(ctx, c.ref)
		}
		if err != nil {
			slog.WarnContext(ctx, "checkout not expired", "provider", c.provider, "error", err)
		}
	}
}

type checkoutRef struct{ provider, ref string }

// voidUnpaid voids an unpaid invoice and fails its waiting payments. It returns the checkouts that
// must be closed at their providers.
func (s *Service) voidUnpaid(ctx context.Context, q *repo.Queries, invoice *uuid.UUID, purchase *uuid.UUID, code string) ([]checkoutRef, error) {
	if invoice != nil {
		if _, err := q.SetInvoiceStatus(ctx, repo.SetInvoiceStatusParams{ID: *invoice, Status: "void", FromStatuses: []string{"issued"}}); err != nil {
			return nil, err
		}
	}
	var out []checkoutRef
	if purchase != nil {
		pays, err := q.PaymentsForPurchase(ctx, purchase)
		if err != nil {
			return nil, err
		}
		for _, pay := range pays {
			if pay.Status != "pending" {
				continue
			}
			full, err := q.GetPaymentForUpdate(ctx, pay.ID)
			if err != nil {
				return nil, err
			}
			if _, err := q.SettlePayment(ctx, repo.SettlePaymentParams{ID: pay.ID, Status: "failed", FailureCode: &code}); err != nil {
				return nil, err
			}
			if cardRef(full.Provider) && full.ProviderPaymentRef != nil {
				out = append(out, checkoutRef{full.Provider, *full.ProviderPaymentRef})
			}
		}
	}
	return out, nil
}

// ApplyPaymentEvent records a provider's report. Events are applied once: a payment moves out of
// pending only once, so a repeated or late event has no second effect.
func (s *Service) ApplyPaymentEvent(ctx context.Context, provider string, ev integration.PaymentEvent) error {
	var id uuid.UUID
	if err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		var err error
		id, err = repo.New(tx).PaymentByRef(ctx, repo.PaymentByRefParams{Provider: provider, Ref: &ev.PaymentRef})
		return err
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			slog.WarnContext(ctx, "payment event for an unknown payment", "provider", provider)
			return nil // Reconciliation reports it.
		}
		return err
	}
	status := "failed"
	if ev.Type == "payment.succeeded" {
		status = "succeeded"
	}
	var method, failure *string
	if ev.MethodRef != "" {
		method = &ev.MethodRef
	}
	if ev.FailureCode != "" {
		failure = &ev.FailureCode
	}
	return s.settlePayment(ctx, id, status, failure, method, nil, nil)
}

// settle moves a pending payment to succeeded or failed and applies the consequences, in one
// transaction: a paid purchase goes to review; a paid renewal restores the subscription; a failed
// renewal enters dunning.
func (s *Service) settlePayment(ctx context.Context, id uuid.UUID, status string, failure, method, ref *string, verifiedBy *uuid.UUID) error {
	var notifyUser *uuid.UUID
	var grantSub uuid.UUID
	var grantRaw []byte
	var grantStart *time.Time
	var notifyKey string
	var notifyData map[string]string
	var tenant uuid.UUID
	err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		q := repo.New(tx)
		pay, err := q.GetPaymentForUpdate(ctx, id)
		if err != nil {
			return err
		}
		tenant = pay.TenantID
		n, err := q.SettlePayment(ctx, repo.SettlePaymentParams{ID: id, Status: status, FailureCode: failure, MethodRef: method, Ref: ref, VerifiedBy: verifiedBy})
		if err != nil || n == 0 {
			return err // n == 0: already settled.
		}
		action := "billing.payment_" + status
		if err := audit.Record(ctx, tx, adminEvent(ctx, &pay.TenantID, action, "payment", &id, nil, map[string]any{"amount_minor": pay.AmountMinor, "currency": pay.Currency, "failure_code": failure})); err != nil {
			return err
		}
		if err := events.Append(ctx, tx, events.Event{Type: "platform.payment." + map[string]string{"succeeded": "verified", "failed": "failed"}[status], TenantID: &pay.TenantID,
			SourceModule: "platform", AggregateType: "payment", AggregateID: id}, map[string]any{"payment_id": id, "amount_minor": pay.AmountMinor, "currency": pay.Currency}); err != nil {
			return err
		}
		if status == "succeeded" && pay.InvoiceID != nil {
			if _, err := q.SetInvoiceStatus(ctx, repo.SetInvoiceStatusParams{ID: *pay.InvoiceID, Status: "paid", FromStatuses: []string{"issued"}}); err != nil {
				return err
			}
		}
		if pay.PurchaseRequestID != nil && pay.Purpose == "purchase" {
			if status == "succeeded" {
				if _, err := q.SetPurchaseStatus(ctx, repo.SetPurchaseStatusParams{ID: *pay.PurchaseRequestID, Status: "under_review", FromStatuses: []string{"payment_pending", "payment_verified"}}); err != nil {
					return err
				}
				return audit.Record(ctx, tx, adminEvent(ctx, &pay.TenantID, "billing.purchase_payment_verified", "purchase_request", pay.PurchaseRequestID, nil, nil))
			}
			return nil // A failed card payment leaves the purchase waiting; the tenant can pay again.
		}
		if pay.InvoiceID == nil {
			return nil
		}
		inv, err := q.GetInvoice(ctx, *pay.InvoiceID)
		if err != nil || inv.SubscriptionID == nil {
			return err
		}
		sub, err := q.GetSubscriptionBilling(ctx, *inv.SubscriptionID)
		if err != nil {
			return err
		}
		if status == "succeeded" {
			grantSub, grantRaw, grantStart = sub.ID, nil, nil
			grantRaw, grantStart, err = s.recover(ctx, q, tx, sub, method)
			return err
		}
		key, data, user, err := s.failRenewal(ctx, q, sub, pay.AmountMinor)
		notifyKey, notifyData, notifyUser = key, data, user
		return err
	})
	if err == nil && notifyUser != nil && s.billing.Notify != nil {
		s.notify(ctx, tenant, *notifyUser, notifyKey, notifyData, "payment:"+id.String())
	}
	if err == nil && grantStart != nil {
		s.grantIncluded(ctx, tenant, grantSub, grantRaw, *grantStart)
	}
	return err
}

func (s *Service) notify(ctx context.Context, tenant, user uuid.UUID, key string, data map[string]string, idem string) {
	if err := db.TenantTx(ctx, s.pools.App, tenant, uuid.Nil, func(tx pgx.Tx) error {
		return s.billing.Notify.Notify(ctx, tx, tenant, user, key, data, idem)
	}); err != nil {
		slog.WarnContext(ctx, "billing notification failed", "key", key, "error", err)
	}
}

func money(minor int64, currency string) string {
	return fmt.Sprintf("%s %d.%02d", currency, minor/100, abs(minor%100))
}

func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// MarkBankTransferPaid confirms a bank transfer arrived. Only an admin can, and it is audited.
func (s *Service) MarkBankTransferPaid(ctx context.Context, id uuid.UUID, reference string) error {
	admin := authctx.AdminFrom(ctx)
	var method string
	if err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		p, err := repo.New(tx).GetPaymentForUpdate(ctx, id)
		method = p.Method
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound()
		}
		return err
	}); err != nil {
		return err
	}
	if method != "bank_transfer" {
		return apperr.Conflict("not_a_bank_transfer", "Only bank transfers are confirmed by hand")
	}
	ref := strings.TrimSpace(reference)
	if ref == "" || len(ref) > 200 {
		return apperr.Validation(apperr.FieldError{Field: "reference", Code: "required"})
	}
	ref = "bank:" + ref
	return s.settlePayment(ctx, id, "succeeded", nil, nil, &ref, &admin.AdminID)
}

// ApprovePurchase starts the subscription and activates its modules, in one transaction.
// Approving an approved purchase returns its subscription.
func (s *Service) ApprovePurchase(ctx context.Context, id uuid.UUID, reason string) (uuid.UUID, error) {
	var sub uuid.UUID
	var tenant, user uuid.UUID
	var plan repo.PlanTermsRow
	var start time.Time
	approved := false
	err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		q := repo.New(tx)
		pr, err := q.GetPurchaseForUpdate(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		tenant, user = pr.TenantID, pr.RequestedBy
		if pr.Status == "approved" {
			sub, err = q.SubscriptionForPurchase(ctx, &id)
			return err
		}
		if pr.Status != "under_review" {
			return apperr.Conflict("purchase_not_reviewable", "Only a purchase under review can be approved")
		}
		var quote Quote
		_ = json.Unmarshal(pr.QuoteSnapshot, &quote)
		if plan, err = q.PlanTerms(ctx, pr.PlanID); err != nil {
			return err
		}
		start = s.now()
		end := periodEnd(start, plan.BillingPeriod)
		var trialEnd *time.Time
		if plan.TrialDays > 0 {
			t := start.AddDate(0, 0, int(plan.TrialDays))
			trialEnd, end = &t, t
		}
		method := "none"
		if pr.PaymentMethod != nil {
			method = *pr.PaymentMethod
		}
		var provider, methodRef *string
		pays, err := q.PaymentsForPurchase(ctx, &id)
		if err != nil {
			return err
		}
		for _, pay := range pays {
			if pay.Status == "succeeded" {
				full, err := q.GetPaymentForUpdate(ctx, pay.ID)
				if err != nil {
					return err
				}
				provider, methodRef = &full.Provider, full.ProviderMethodRef
			}
		}
		snap, _ := json.Marshal(map[string]any{"amount_minor": quote.UnitAmount, "quantity": quote.Quantity, "currency": quote.Currency,
			"billing_period": plan.BillingPeriod, "plan_key": plan.Key, "plan_name": plan.Name, "product_name": plan.ProductName})
		sub, err = q.CreateSubscriptionFull(ctx, repo.CreateSubscriptionFullParams{TenantID: tenant, PlanID: pr.PlanID, PurchaseRequestID: &id, PriceSnapshot: snap,
			Currency: quote.Currency, CurrentPeriodStart: &start, CurrentPeriodEnd: &end, TrialEndsAt: trialEnd, PaymentMethod: &method,
			PaymentProvider: provider, ProviderPaymentMethodRef: methodRef})
		if err != nil {
			return err
		}
		mods, err := q.PlanModuleIDs(ctx, pr.PlanID)
		if err != nil {
			return err
		}
		for _, m := range mods {
			if _, err := q.CreateGrant(ctx, repo.CreateGrantParams{TenantID: tenant, ModuleID: m, SubscriptionID: &sub, Source: "subscription", Status: "pending"}); err != nil {
				return err
			}
		}
		to := "active"
		if trialEnd != nil {
			to = "trialing"
		}
		if _, err := q.SetSubscriptionStatus(ctx, repo.SetSubscriptionStatusParams{ID: sub, Status: to, FromStatus: "pending_activation"}); err != nil {
			return err
		}
		if err := s.setGrants(ctx, q, tx, tenant, sub, "active", []string{"pending"}, reason); err != nil {
			return err
		}
		if _, err := q.DecidePurchase(ctx, repo.DecidePurchaseParams{ID: id, Status: "approved", ReviewedBy: adminID(ctx), FromStatuses: []string{"under_review"}}); err != nil {
			return err
		}
		if err := audit.Record(ctx, tx, adminEvent(ctx, &tenant, "billing.purchase_approved", "purchase_request", &id, nil, map[string]any{"reason": reason, "subscription_id": sub})); err != nil {
			return err
		}
		approved = true
		return events.Append(ctx, tx, events.Event{Type: "platform.purchase.approved", TenantID: &tenant, SourceModule: "platform", AggregateType: "purchase_request", AggregateID: id},
			map[string]any{"purchase_request_id": id})
	})
	if err != nil || !approved {
		return sub, err
	}
	s.hub.Publish(ctx, tenant, EntitlementsChanged)
	s.grantIncluded(ctx, tenant, sub, plan.IncludedCredits, start)
	if s.billing.Notify != nil {
		s.notify(ctx, tenant, user, "billing.purchase_approved", map[string]string{"plan": plan.Name}, "purchase-approved:"+id.String())
	}
	return sub, nil
}

func adminID(ctx context.Context) *uuid.UUID {
	_, id := actorOf(ctx)
	return id
}

// setGrants moves a subscription's grants to a status and recomputes access for each module.
func (s *Service) setGrants(ctx context.Context, q *repo.Queries, tx pgx.Tx, tenant, sub uuid.UUID, to string, from []string, reason string) error {
	grants, err := q.ListSubscriptionGrants(ctx, repo.ListSubscriptionGrantsParams{TenantID: tenant, SubscriptionID: &sub})
	if err != nil {
		return err
	}
	now := s.now()
	for _, g := range grants {
		ok := false
		for _, f := range from {
			ok = ok || g.Status == f
		}
		if !ok {
			continue
		}
		var starts, ends *time.Time
		if to == "active" {
			starts = &now
		}
		if to == "expired" {
			ends = &now
		}
		if err := q.SetGrantStatus(ctx, repo.SetGrantStatusParams{ID: g.ID, Status: to, Reason: &reason, ActivatedBy: adminID(ctx), StartsAt: starts, EndsAt: ends}); err != nil {
			return err
		}
		res, err := s.recompute(ctx, q, tenant, g.ModuleID, &g.ID, reason)
		if err != nil {
			return err
		}
		key, err := q.GetModuleKey(ctx, g.ModuleID)
		if err != nil {
			return err
		}
		if err := entitlementEvent(ctx, tx, tenant, g.ModuleID, key, res); err != nil {
			return err
		}
	}
	return nil
}

// grantIncluded adds a plan's included credits for a period. The key makes it once per period.
func (s *Service) grantIncluded(ctx context.Context, tenant, sub uuid.UUID, raw []byte, periodStart time.Time) {
	credits := map[string]int64{}
	_ = json.Unmarshal(raw, &credits)
	for meter, amount := range credits {
		if amount <= 0 {
			continue
		}
		key := fmt.Sprintf("sub:%s:%s:%s", sub, meter, periodStart.UTC().Format(time.RFC3339))
		if err := s.Grant(ctx, tenant, meter, amount, key, nil, "Included with the plan"); err != nil {
			slog.WarnContext(ctx, "included credits not granted", "meter", meter, "error", err)
		}
	}
}

// RejectPurchase declines a purchase. A card payment is refunded at once through its provider; a
// bank transfer is marked for an admin to refund by hand (D-01).
func (s *Service) RejectPurchase(ctx context.Context, id uuid.UUID, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" || len(reason) > 1000 {
		return apperr.Validation(apperr.FieldError{Field: "reason", Code: "required"})
	}
	var tenant, user uuid.UUID
	var planName string
	var refunds []uuid.UUID
	var checkouts []checkoutRef
	err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		q := repo.New(tx)
		pr, err := q.GetPurchaseForUpdate(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		tenant, user = pr.TenantID, pr.RequestedBy
		if plan, err := q.GetPlan(ctx, pr.PlanID); err == nil {
			planName = plan.Name
		}
		n, err := q.DecidePurchase(ctx, repo.DecidePurchaseParams{ID: id, Status: "rejected", ReviewedBy: adminID(ctx), RejectionReason: &reason,
			FromStatuses: []string{"payment_pending", "payment_verified", "under_review"}})
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.Conflict("purchase_not_reviewable", "This purchase was already decided")
		}
		pays, err := q.PaymentsForPurchase(ctx, &id)
		if err != nil {
			return err
		}
		for _, pay := range pays {
			if pay.Status == "succeeded" {
				if err := q.RecordRefund(ctx, repo.RecordRefundParams{ID: pay.ID, RefundStatus: ptr("pending")}); err != nil {
					return err
				}
				refunds = append(refunds, pay.ID)
			}
		}
		if checkouts, err = s.voidUnpaid(ctx, q, pr.InvoiceID, &id, "purchase_rejected"); err != nil {
			return err
		}
		if err := audit.Record(ctx, tx, adminEvent(ctx, &tenant, "billing.purchase_rejected", "purchase_request", &id, nil, map[string]any{"reason": reason, "refunds": len(refunds)})); err != nil {
			return err
		}
		return events.Append(ctx, tx, events.Event{Type: "platform.purchase.rejected", TenantID: &tenant, SourceModule: "platform", AggregateType: "purchase_request", AggregateID: id},
			map[string]any{"purchase_request_id": id})
	})
	if err != nil {
		return err
	}
	s.expireCheckouts(ctx, checkouts)
	refundText := "Nothing was charged."
	for _, pid := range refunds {
		if s.RetryRefund(ctx, pid) == nil {
			refundText = "Your payment has been refunded."
		} else {
			refundText = "Your payment will be refunded."
		}
	}
	if s.billing.Notify != nil {
		s.notify(ctx, tenant, user, "billing.purchase_rejected", map[string]string{"plan": planName, "refund": refundText}, "purchase-rejected:"+id.String())
	}
	return nil
}

// RetryRefund refunds a payment through its provider. Bank transfers are refunded by hand, and an
// admin records that with MarkRefunded.
func (s *Service) RetryRefund(ctx context.Context, id uuid.UUID) error {
	var pay repo.GetPaymentForUpdateRow
	if err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		var err error
		pay, err = repo.New(tx).GetPaymentForUpdate(ctx, id)
		return err
	}); err != nil {
		return err
	}
	if pay.RefundStatus == nil || *pay.RefundStatus == "succeeded" {
		return nil
	}
	if !cardRef(pay.Provider) || pay.ProviderPaymentRef == nil {
		return apperr.Conflict("manual_refund", "Refund this payment by hand, then record it")
	}
	provider, err := s.billing.Integration.PaymentProvider(pay.Provider)
	if err != nil {
		return err
	}
	res, err := provider.Refund(ctx, integration.RefundRequest{PaymentRef: *pay.ProviderPaymentRef, Amount: pay.AmountMinor - pay.RefundedAmountMinor, IdempotencyKey: "refund:" + id.String()})
	status := "failed"
	if err == nil && res.Status == "succeeded" {
		status = "succeeded"
	}
	var ref *string
	if res.Ref != "" {
		ref = &res.Ref
	}
	if rerr := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		q := repo.New(tx)
		if err := q.RecordRefund(ctx, repo.RecordRefundParams{ID: id, RefundStatus: &status, RefundRef: ref}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, adminEvent(ctx, &pay.TenantID, "billing.refund_"+status, "payment", &id, nil, map[string]any{"amount_minor": pay.AmountMinor}))
	}); rerr != nil {
		return rerr
	}
	if status != "succeeded" {
		return apperr.New(http.StatusBadGateway, "refund_failed", "The provider did not refund the payment")
	}
	return nil
}

// MarkRefunded records a refund made by hand, such as a bank transfer returned.
func (s *Service) MarkRefunded(ctx context.Context, id uuid.UUID, note string) error {
	note = strings.TrimSpace(note)
	if note == "" || len(note) > 500 {
		return apperr.Validation(apperr.FieldError{Field: "note", Code: "required"})
	}
	return db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		q := repo.New(tx)
		pay, err := q.GetPaymentForUpdate(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		if pay.RefundStatus == nil || *pay.RefundStatus == "succeeded" {
			return apperr.Conflict("no_refund_due", "No refund is due on this payment")
		}
		ref := "manual:" + note
		if err := q.RecordRefund(ctx, repo.RecordRefundParams{ID: id, RefundStatus: ptr("succeeded"), RefundRef: &ref}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, adminEvent(ctx, &pay.TenantID, "billing.refund_recorded", "payment", &id, nil, map[string]any{"note": note}))
	})
}

// ReviewQueue lists purchases for admins, oldest first.
func (s *Service) ReviewQueue(ctx context.Context, status *string) ([]repo.ReviewQueueRow, error) {
	var out []repo.ReviewQueueRow
	err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		var err error
		out, err = repo.New(tx).ReviewQueue(ctx, status)
		return err
	})
	return out, err
}

// OldestReviewAge is how long the oldest purchase has waited for review, for monitoring.
func (s *Service) OldestReviewAge(ctx context.Context) (time.Duration, error) {
	var secs float64
	err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		var err error
		secs, err = repo.New(tx).OldestUnderReview(ctx)
		return err
	})
	return time.Duration(secs * float64(time.Second)), err
}

// PendingBankTransfers and RefundsToMake are admin work lists.
func (s *Service) PendingBankTransfers(ctx context.Context) ([]repo.PendingBankTransfersRow, error) {
	var out []repo.PendingBankTransfersRow
	err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		var err error
		out, err = repo.New(tx).PendingBankTransfers(ctx)
		return err
	})
	return out, err
}

// RefundsToMake lists refunds that are due or failed.
func (s *Service) RefundsToMake(ctx context.Context) ([]repo.RefundsToMakeRow, error) {
	var out []repo.RefundsToMakeRow
	err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		var err error
		out, err = repo.New(tx).RefundsToMake(ctx)
		return err
	})
	return out, err
}

// InvoiceView is an invoice with its lines and payments.
type InvoiceView struct {
	repo.GetInvoiceRow
	Lines    []repo.InvoiceLinesRow
	Payments []repo.PaymentsForInvoiceRow
}

// TenantInvoices lists a tenant's invoices, under its own RLS.
func (s *Service) TenantInvoices(ctx context.Context, tenant uuid.UUID) ([]repo.ListTenantInvoicesRow, error) {
	var out []repo.ListTenantInvoicesRow
	err := db.TenantTx(ctx, s.pools.App, tenant, uuid.Nil, func(tx pgx.Tx) error {
		var err error
		out, err = repo.New(tx).ListTenantInvoices(ctx, tenant)
		return err
	})
	return out, err
}

// TenantInvoice returns one invoice, under the tenant's RLS, so another tenant's is not found.
func (s *Service) TenantInvoice(ctx context.Context, tenant, id uuid.UUID) (InvoiceView, error) {
	var v InvoiceView
	err := db.TenantTx(ctx, s.pools.App, tenant, uuid.Nil, func(tx pgx.Tx) error {
		q := repo.New(tx)
		var err error
		if v.GetInvoiceRow, err = q.GetInvoice(ctx, id); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return apperr.NotFound()
			}
			return err
		}
		if v.Lines, err = q.InvoiceLines(ctx, id); err != nil {
			return err
		}
		v.Payments, err = q.PaymentsForInvoice(ctx, &id)
		return err
	})
	return v, err
}

// PayInvoice starts a card checkout for an issued invoice, such as a renewal in dunning. It returns
// the checkout address.
func (s *Service) PayInvoice(ctx context.Context, p authctx.Principal, id uuid.UUID) (string, error) {
	if s.billing.CardProvider == "" {
		return "", apperr.New(http.StatusServiceUnavailable, "card_payments_unavailable", "Card payments are not available yet")
	}
	inv, err := s.TenantInvoice(ctx, p.TenantID, id)
	if err != nil {
		return "", err
	}
	if inv.Status != "issued" {
		return "", apperr.Conflict("invoice_not_open", "This invoice is not open")
	}
	var pid uuid.UUID
	err = db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		q := repo.New(tx)
		// One open card payment per invoice per day, so repeated clicks reuse the same checkout.
		key := fmt.Sprintf("invoice:%s:%s", id, s.now().UTC().Format(time.DateOnly))
		purpose := "renewal"
		if inv.SubscriptionID == nil {
			purpose = "purchase"
		}
		var err error
		pid, err = q.InsertPayment(ctx, repo.InsertPaymentParams{TenantID: p.TenantID, InvoiceID: &id, Provider: s.billing.CardProvider, Method: "card",
			AmountMinor: inv.TotalMinor, Currency: inv.Currency, IdempotencyKey: &key, Purpose: purpose})
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{TenantID: &p.TenantID, ActorType: "user", ActorID: &p.UserID, Action: "billing.invoice_payment_started", ResourceType: "invoice", ResourceID: &id})
	})
	if err != nil {
		return "", err
	}
	return s.startCheckout(ctx, pid, "Invoice "+inv.Number)
}

// SeatLimit is the most members the tenant's active plans allow. Zero means no limit.
func (s *Service) SeatLimit(ctx context.Context, tenant uuid.UUID) (int, error) {
	var n int32
	err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		var err error
		n, err = repo.New(tx).ActiveSeatLimit(ctx, tenant)
		return err
	})
	return int(n), err
}

// defaultBankInstructions is shown until the platform operator enters real bank details.
const defaultBankInstructions = "Bank details have not been set up yet. Contact the platform operator before paying."

// BankInstructions returns what tenants paying by bank transfer are told.
func (s *Service) BankInstructions(ctx context.Context) (string, error) {
	text := defaultBankInstructions
	err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		raw, err := repo.New(tx).GetSetting(ctx, "bank_transfer_instructions")
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		var v struct {
			Text string `json:"text"`
		}
		if json.Unmarshal(raw, &v) == nil && strings.TrimSpace(v.Text) != "" {
			text = v.Text
		}
		return nil
	})
	return text, err
}

// SetBankInstructions changes them. It is audited.
func (s *Service) SetBankInstructions(ctx context.Context, text string) error {
	text = strings.TrimSpace(text)
	if len(text) > 2000 {
		return apperr.Validation(apperr.FieldError{Field: "text", Code: "too_long"})
	}
	raw, _ := json.Marshal(map[string]string{"text": text})
	admin := authctx.AdminFrom(ctx)
	return db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		if err := repo.New(tx).PutSetting(ctx, repo.PutSettingParams{Key: "bank_transfer_instructions", Value: raw, UpdatedBy: &admin.AdminID}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, adminEvent(ctx, nil, "billing.bank_instructions_changed", "setting", nil, nil, nil))
	})
}

// CardAvailable reports whether card payments can be taken.
func (s *Service) CardAvailable() bool { return s.billing.CardProvider != "" }
