package commercial

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/db"
	"mspengine/internal/kernel/jobs"
	"mspengine/internal/platform/audit"
	"mspengine/internal/platform/commercial/repo"
	"mspengine/internal/platform/integration"
)

// Job kinds for billing.
const (
	JobBillingTick = "billing.tick"
	JobReconcile   = "billing.reconcile"
)

// BillingJobs are the billing job handlers.
func (s *Service) BillingJobs() map[string]jobs.Handler {
	return map[string]jobs.Handler{JobBillingTick: s.HandleBillingTick, JobReconcile: s.HandleReconcile}
}

type priceSnap struct {
	Amount      int64  `json:"amount_minor"`
	Quantity    int32  `json:"quantity"`
	Currency    string `json:"currency"`
	Period      string `json:"billing_period"`
	PlanKey     string `json:"plan_key"`
	PlanName    string `json:"plan_name"`
	ProductName string `json:"product_name"`
}

func snapOf(raw []byte) priceSnap {
	p := priceSnap{Quantity: 1}
	_ = json.Unmarshal(raw, &p)
	if p.Quantity <= 0 {
		p.Quantity = 1
	}
	return p
}

// HandleBillingTick runs due renewals, trial ends, cancellations and dunning steps. Each
// subscription is handled under its own lock, so overlapping ticks do no double work.
func (s *Service) HandleBillingTick(ctx context.Context, _ jobs.Job) error {
	var due []uuid.UUID
	var overdue []repo.OverdueBankInvoicesRow
	if err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		q := repo.New(tx)
		var err error
		if due, err = q.DueSubscriptions(ctx); err != nil {
			return err
		}
		overdue, err = q.OverdueBankInvoices(ctx)
		return err
	}); err != nil {
		return err
	}
	for _, o := range overdue {
		if o.SubscriptionID != nil {
			if err := s.markOverdue(ctx, *o.SubscriptionID); err != nil {
				slog.WarnContext(ctx, "overdue invoice step failed", "error", err)
			}
		}
	}
	for _, id := range due {
		if err := s.process(ctx, id); err != nil {
			slog.WarnContext(ctx, "subscription billing step failed", "subscription_id", id, "error", err)
		}
	}
	return nil
}

// markOverdue starts dunning for a bank-transfer subscription whose renewal invoice is past due.
func (s *Service) markOverdue(ctx context.Context, id uuid.UUID) error {
	return db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		q := repo.New(tx)
		sub, err := q.GetSubscriptionBilling(ctx, id)
		if err != nil || sub.Status != "active" {
			return err
		}
		now := s.now()
		if err := q.SetSubscriptionBilling(ctx, repo.SetSubscriptionBillingParams{ID: id, Status: "past_due", CurrentPeriodStart: sub.CurrentPeriodStart,
			CurrentPeriodEnd: sub.CurrentPeriodEnd, PastDueSince: &now, DunningAttempts: 0}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, adminEvent(ctx, &sub.TenantID, "billing.subscription_past_due", "subscription", &id, nil, map[string]any{"reason": "invoice_overdue"}))
	})
}

// chargeJob is a saved-method charge to make after the transaction that recorded it commits.
type chargeJob struct {
	payment   uuid.UUID
	provider  string
	methodRef string
	amount    int64
	currency  string
	desc      string
}

func (s *Service) process(ctx context.Context, id uuid.UUID) error {
	var charge *chargeJob
	var noticeKey string
	var noticeData map[string]string
	var tenant uuid.UUID
	var grantRaw []byte
	var grantStart *time.Time
	err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		q := repo.New(tx)
		sub, err := q.GetSubscriptionBilling(ctx, id)
		if err != nil {
			return err
		}
		tenant = sub.TenantID
		now := s.now()
		policy := dunningPolicy(ctx, q)
		snap := snapOf(sub.PriceSnapshot)
		switch {
		case sub.CancelAt != nil && !sub.CancelAt.After(now) && (sub.Status == "active" || sub.Status == "trialing" || sub.Status == "past_due"):
			return s.endSubscription(ctx, q, tx, sub, "cancelled", "Cancelled at the end of the period")
		case (sub.Status == "active" || sub.Status == "trialing") && sub.CurrentPeriodEnd != nil && !sub.CurrentPeriodEnd.After(now):
			c, raw, start, err := s.renew(ctx, q, tx, sub, snap, policy)
			charge, grantRaw, grantStart = c, raw, start
			return err
		case sub.Status == "past_due" || sub.Status == "suspended":
			if sub.PastDueSince == nil {
				return nil
			}
			days := int(now.Sub(*sub.PastDueSince).Hours() / 24)
			if days >= policy.ExpireAfterDays {
				if inv, err := q.OpenInvoiceForSubscription(ctx, &id); err == nil {
					if _, err := q.SetInvoiceStatus(ctx, repo.SetInvoiceStatusParams{ID: inv.ID, Status: "uncollectible", FromStatuses: []string{"issued"}}); err != nil {
						return err
					}
				}
				return s.endSubscription(ctx, q, tx, sub, "expired", "Expired for non-payment")
			}
			if days >= policy.SuspendAfterDays && sub.Status == "past_due" {
				if err := q.SetSubscriptionBilling(ctx, repo.SetSubscriptionBillingParams{ID: id, Status: "suspended", CurrentPeriodStart: sub.CurrentPeriodStart,
					CurrentPeriodEnd: sub.CurrentPeriodEnd, PastDueSince: sub.PastDueSince, DunningAttempts: sub.DunningAttempts, NextRetryAt: sub.NextRetryAt}); err != nil {
					return err
				}
				if err := s.setGrants(ctx, q, tx, sub.TenantID, id, "suspended", []string{"active"}, "Suspended for non-payment"); err != nil {
					return err
				}
				noticeKey, noticeData = "billing.subscription_suspended", map[string]string{"plan": sub.PlanName}
				return audit.Record(ctx, tx, adminEvent(ctx, &sub.TenantID, "billing.subscription_suspended", "subscription", &id, nil, map[string]any{"days_past_due": days}))
			}
			// Retry a saved card on the policy's schedule.
			if sub.PaymentMethod != nil && *sub.PaymentMethod == "card" && sub.ProviderPaymentMethodRef != nil && sub.PaymentProvider != nil &&
				sub.NextRetryAt != nil && !sub.NextRetryAt.After(now) && int(sub.DunningAttempts) <= len(policy.RetryDays) {
				inv, err := q.OpenInvoiceForSubscription(ctx, &id)
				if err != nil {
					return err
				}
				key := fmt.Sprintf("retry:%s:%d", inv.ID, sub.DunningAttempts)
				pid, err := q.InsertPayment(ctx, repo.InsertPaymentParams{TenantID: sub.TenantID, InvoiceID: &inv.ID, Provider: *sub.PaymentProvider, Method: "card",
					AmountMinor: inv.TotalMinor, Currency: inv.Currency, IdempotencyKey: &key, Purpose: "renewal"})
				if err != nil {
					return err
				}
				// Pushed back now, so a slow provider is never retried twice in one tick.
				next := now.Add(24 * time.Hour)
				if err := q.SetSubscriptionBilling(ctx, repo.SetSubscriptionBillingParams{ID: id, Status: sub.Status, CurrentPeriodStart: sub.CurrentPeriodStart,
					CurrentPeriodEnd: sub.CurrentPeriodEnd, PastDueSince: sub.PastDueSince, DunningAttempts: sub.DunningAttempts, NextRetryAt: &next}); err != nil {
					return err
				}
				charge = &chargeJob{payment: pid, provider: *sub.PaymentProvider, methodRef: *sub.ProviderPaymentMethodRef, amount: inv.TotalMinor, currency: inv.Currency, desc: sub.PlanName}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if grantStart != nil {
		s.grantIncluded(ctx, tenant, id, grantRaw, *grantStart)
	}
	if noticeKey != "" {
		s.notifyOwner(ctx, tenant, noticeKey, noticeData, fmt.Sprintf("%s:%s:%s", noticeKey, id, s.now().UTC().Format(time.DateOnly)))
	}
	if charge != nil {
		return s.charge(ctx, *charge)
	}
	return nil
}

func (s *Service) notifyOwner(ctx context.Context, tenant uuid.UUID, key string, data map[string]string, idem string) {
	if s.billing.Notify == nil || s.billing.Owner == nil {
		return
	}
	owner, err := s.billing.Owner(ctx, tenant)
	if err != nil || owner == nil {
		return
	}
	s.notify(ctx, tenant, *owner, key, data, idem)
}

// renew issues the next period's invoice and moves the period on. A card on file is charged after
// commit; a bank transfer has until the policy's due date; anything else is past due at once.
func (s *Service) renew(ctx context.Context, q *repo.Queries, tx pgx.Tx, sub repo.GetSubscriptionBillingRow, snap priceSnap, policy DunningPolicy) (*chargeJob, []byte, *time.Time, error) {
	start := *sub.CurrentPeriodEnd
	end := periodEnd(start, sub.BillingPeriod)
	amount := snap.Amount * int64(snap.Quantity)
	plan, err := q.PlanTerms(ctx, sub.PlanID)
	if err != nil {
		return nil, nil, nil, err
	}
	method := ""
	if sub.PaymentMethod != nil {
		method = *sub.PaymentMethod
	}
	params := repo.SetSubscriptionBillingParams{ID: sub.ID, Status: "active", CurrentPeriodStart: &start, CurrentPeriodEnd: &end}
	if amount == 0 {
		if err := q.SetSubscriptionBilling(ctx, params); err != nil {
			return nil, nil, nil, err
		}
		return nil, plan.IncludedCredits, &start, audit.Record(ctx, tx, adminEvent(ctx, &sub.TenantID, "billing.subscription_renewed", "subscription", &sub.ID, nil, map[string]any{"period_start": start}))
	}
	quote := Quote{PlanName: snap.PlanName, ProductName: snap.ProductName, Currency: sub.Currency, UnitAmount: snap.Amount, Quantity: snap.Quantity, Subtotal: amount, Total: amount}
	due := s.now()
	if method == "bank_transfer" {
		due = due.AddDate(0, 0, policy.BankTransferDueDays)
	}
	inv, err := s.issueInvoice(ctx, q, sub.TenantID, &sub.ID, quote, due, &start, &end)
	if err != nil {
		return nil, nil, nil, err
	}
	var charge *chargeJob
	switch {
	case method == "card" && sub.ProviderPaymentMethodRef != nil && sub.PaymentProvider != nil:
		key := fmt.Sprintf("renewal:%s:%s", sub.ID, start.UTC().Format(time.RFC3339))
		pid, err := q.InsertPayment(ctx, repo.InsertPaymentParams{TenantID: sub.TenantID, InvoiceID: &inv, Provider: *sub.PaymentProvider, Method: "card",
			AmountMinor: amount, Currency: sub.Currency, IdempotencyKey: &key, Purpose: "renewal"})
		if err != nil {
			return nil, nil, nil, err
		}
		charge = &chargeJob{payment: pid, provider: *sub.PaymentProvider, methodRef: *sub.ProviderPaymentMethodRef, amount: amount, currency: sub.Currency, desc: snap.PlanName}
	case method == "bank_transfer":
		key := fmt.Sprintf("renewal:%s:%s", sub.ID, start.UTC().Format(time.RFC3339))
		if _, err := q.InsertPayment(ctx, repo.InsertPaymentParams{TenantID: sub.TenantID, InvoiceID: &inv, Provider: ManualPayments, Method: "bank_transfer",
			AmountMinor: amount, Currency: sub.Currency, IdempotencyKey: &key, Purpose: "renewal"}); err != nil {
			return nil, nil, nil, err
		}
	default:
		// No way to collect, such as the end of a trial without a card: the tenant pays the invoice.
		now := s.now()
		params.Status, params.PastDueSince = "past_due", &now
	}
	if err := q.SetSubscriptionBilling(ctx, params); err != nil {
		return nil, nil, nil, err
	}
	if err := audit.Record(ctx, tx, adminEvent(ctx, &sub.TenantID, "billing.renewal_invoiced", "subscription", &sub.ID, nil, map[string]any{"invoice_id": inv, "amount_minor": amount})); err != nil {
		return nil, nil, nil, err
	}
	// Included credits arrive once the period is paid, unless nothing is owed now.
	return charge, nil, nil, nil
}

// charge makes a saved-method charge and settles the payment with the result.
func (s *Service) charge(ctx context.Context, c chargeJob) error {
	provider, err := s.billing.Integration.PaymentProvider(c.provider)
	if err != nil {
		return err
	}
	res, err := provider.ChargeSaved(ctx, integration.SavedCharge{MethodRef: c.methodRef, Amount: c.amount, Currency: c.currency, Description: c.desc,
		IdempotencyKey: "payment:" + c.payment.String()})
	if err != nil {
		return err // The payment stays pending; reconciliation and the next retry pick it up.
	}
	var ref, failure *string
	if res.Ref != "" {
		ref = &res.Ref
	}
	if res.FailureCode != "" {
		failure = &res.FailureCode
	}
	switch res.Status {
	case "succeeded", "failed":
		return s.settlePayment(ctx, c.payment, res.Status, failure, nil, ref, nil)
	}
	if ref != nil {
		return db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
			return repo.New(tx).SetPaymentCheckout(ctx, repo.SetPaymentCheckoutParams{ID: c.payment, Ref: ref})
		})
	}
	return nil
}

// recover applies a paid renewal or overdue invoice: the subscription is active again, suspended
// modules return, and dunning stops. It returns the plan's included credits to grant after commit.
func (s *Service) recover(ctx context.Context, q *repo.Queries, tx pgx.Tx, sub repo.GetSubscriptionBillingRow, method *string) ([]byte, *time.Time, error) {
	if _, err := q.OpenInvoiceForSubscription(ctx, &sub.ID); err == nil {
		return nil, nil, nil // Another invoice is still open; dunning continues for it.
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, err
	}
	if sub.Status == "expired" || sub.Status == "cancelled" {
		return nil, nil, nil
	}
	if sub.Status == "suspended" {
		if err := s.setGrants(ctx, q, tx, sub.TenantID, sub.ID, "active", []string{"suspended"}, "Payment received"); err != nil {
			return nil, nil, err
		}
	}
	if err := q.SetSubscriptionBilling(ctx, repo.SetSubscriptionBillingParams{ID: sub.ID, Status: "active", CurrentPeriodStart: sub.CurrentPeriodStart,
		CurrentPeriodEnd: sub.CurrentPeriodEnd, DunningAttempts: 0, MethodRef: method}); err != nil {
		return nil, nil, err
	}
	plan, err := q.PlanTerms(ctx, sub.PlanID)
	if err != nil {
		return nil, nil, err
	}
	return plan.IncludedCredits, sub.CurrentPeriodStart, audit.Record(ctx, tx, adminEvent(ctx, &sub.TenantID, "billing.subscription_paid", "subscription", &sub.ID, nil, nil))
}

// failRenewal records a failed renewal payment and schedules the next retry.
func (s *Service) failRenewal(ctx context.Context, q *repo.Queries, sub repo.GetSubscriptionBillingRow, amount int64) (string, map[string]string, *uuid.UUID, error) {
	policy := dunningPolicy(ctx, q)
	now := s.now()
	since := sub.PastDueSince
	if since == nil {
		since = &now
	}
	attempts := sub.DunningAttempts + 1
	var next *time.Time
	if int(attempts) <= len(policy.RetryDays) {
		t := since.AddDate(0, 0, policy.RetryDays[attempts-1])
		next = &t
	}
	status := sub.Status
	if status == "active" || status == "trialing" {
		status = "past_due"
	}
	if err := q.SetSubscriptionBilling(ctx, repo.SetSubscriptionBillingParams{ID: sub.ID, Status: status, CurrentPeriodStart: sub.CurrentPeriodStart,
		CurrentPeriodEnd: sub.CurrentPeriodEnd, PastDueSince: since, DunningAttempts: attempts, NextRetryAt: next}); err != nil {
		return "", nil, nil, err
	}
	retry := "the suspension date"
	if next != nil {
		retry = next.Format(time.DateOnly)
	}
	var owner *uuid.UUID
	if s.billing.Owner != nil {
		owner, _ = s.billing.Owner(ctx, sub.TenantID)
	}
	return "billing.payment_failed", map[string]string{"amount": money(amount, sub.Currency), "plan": sub.PlanName, "retry": retry}, owner, nil
}

// endSubscription cancels or expires a subscription and ends its modules' access.
func (s *Service) endSubscription(ctx context.Context, q *repo.Queries, tx pgx.Tx, sub repo.GetSubscriptionBillingRow, status, reason string) error {
	if err := q.SetSubscriptionBilling(ctx, repo.SetSubscriptionBillingParams{ID: sub.ID, Status: status, CurrentPeriodStart: sub.CurrentPeriodStart,
		CurrentPeriodEnd: sub.CurrentPeriodEnd, PastDueSince: sub.PastDueSince, DunningAttempts: sub.DunningAttempts}); err != nil {
		return err
	}
	if err := s.setGrants(ctx, q, tx, sub.TenantID, sub.ID, "expired", []string{"active", "suspended", "pending"}, reason); err != nil {
		return err
	}
	return audit.Record(ctx, tx, adminEvent(ctx, &sub.TenantID, "billing.subscription_"+status, "subscription", &sub.ID, nil, map[string]any{"reason": reason}))
}

// CancelSubscription stops renewal at the end of the paid period. Access continues until then.
func (s *Service) CancelSubscription(ctx context.Context, p authctx.Principal, id uuid.UUID) error {
	return db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		q := repo.New(tx)
		sub, err := q.GetSubscriptionBilling(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) || err == nil && sub.TenantID != p.TenantID {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		if sub.CancelAt != nil || (sub.Status != "active" && sub.Status != "trialing" && sub.Status != "past_due") {
			return apperr.Conflict("not_cancellable", "This subscription is not renewing")
		}
		at := s.now()
		if sub.CurrentPeriodEnd != nil {
			at = *sub.CurrentPeriodEnd
		}
		if _, err := tx.Exec(ctx, `UPDATE platform.subscriptions SET cancel_at = $2 WHERE id = $1`, id, at); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{TenantID: &p.TenantID, ActorType: "user", ActorID: &p.UserID, Action: "billing.subscription_cancel_requested",
			ResourceType: "subscription", ResourceID: &id, After: map[string]any{"cancel_at": at}})
	})
}

// HandleReconcile compares each card provider's records with ours for the last 35 days, and
// reports differences for an admin. It never changes a payment itself.
func (s *Service) HandleReconcile(ctx context.Context, _ jobs.Job) error {
	if s.billing.Integration == nil {
		return nil
	}
	for _, key := range s.billing.Integration.PaymentProviders() {
		if _, err := s.Reconcile(ctx, key); err != nil {
			slog.WarnContext(ctx, "reconciliation failed", "provider", key, "error", err)
		}
	}
	return nil
}

// Reconcile runs one comparison for one provider and returns the run's ID.
func (s *Service) Reconcile(ctx context.Context, key string) (uuid.UUID, error) {
	provider, err := s.billing.Integration.PaymentProvider(key)
	if err != nil {
		return uuid.Nil, err
	}
	end := s.now()
	start := end.AddDate(0, 0, -35)
	var run uuid.UUID
	if err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		var err error
		run, err = repo.New(tx).InsertReconciliationRun(ctx, repo.InsertReconciliationRunParams{Provider: key, WindowStart: start, WindowEnd: end})
		return err
	}); err != nil {
		return uuid.Nil, err
	}
	theirs, perr := provider.ListPayments(ctx, start)
	err = db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		q := repo.New(tx)
		if perr != nil {
			msg := perr.Error()
			return q.FinishReconciliationRun(ctx, repo.FinishReconciliationRunParams{ID: run, Status: "failed", Error: &msg})
		}
		ours, err := q.PaymentsSince(ctx, repo.PaymentsSinceParams{Provider: key, Since: start.AddDate(0, 0, -1)})
		if err != nil {
			return err
		}
		byRef := map[string]repo.PaymentsSinceRow{}
		for _, o := range ours {
			byRef[*o.ProviderPaymentRef] = o
		}
		issues := 0
		report := func(ref string, payment *uuid.UUID, kind string, detail map[string]any) error {
			raw, _ := json.Marshal(detail)
			n, err := q.InsertReconciliationIssue(ctx, repo.InsertReconciliationIssueParams{RunID: run, Provider: key, ProviderRef: ref, PaymentID: payment, Kind: kind, Detail: raw})
			issues += int(n)
			return err
		}
		seen := map[string]bool{}
		for _, t := range theirs {
			seen[t.Ref] = true
			o, ok := byRef[t.Ref]
			if !ok {
				if t.Status == "succeeded" {
					if err := report(t.Ref, nil, "missing_locally", map[string]any{"amount_minor": t.Amount, "currency": t.Currency}); err != nil {
						return err
					}
				}
				continue
			}
			id := o.ID
			ourStatus := o.Status
			if ourStatus == "refunded" {
				ourStatus = "succeeded"
			}
			switch {
			case t.Amount != o.AmountMinor || t.Currency != o.Currency:
				err = report(t.Ref, &id, "amount_mismatch", map[string]any{"ours": o.AmountMinor, "theirs": t.Amount, "currency": t.Currency})
			case t.Status != ourStatus && ourStatus != "pending":
				err = report(t.Ref, &id, "status_mismatch", map[string]any{"ours": o.Status, "theirs": t.Status})
			case t.Status == "succeeded" && ourStatus == "pending":
				err = report(t.Ref, &id, "status_mismatch", map[string]any{"ours": o.Status, "theirs": t.Status})
			case t.Refunded != o.RefundedAmountMinor:
				err = report(t.Ref, &id, "refund_mismatch", map[string]any{"ours": o.RefundedAmountMinor, "theirs": t.Refunded})
			}
			if err != nil {
				return err
			}
		}
		for _, o := range ours {
			if (o.Status == "succeeded" || o.Status == "refunded") && !seen[*o.ProviderPaymentRef] && !o.CreatedAt.Before(start) {
				id := o.ID
				if err := report(*o.ProviderPaymentRef, &id, "missing_at_provider", map[string]any{"amount_minor": o.AmountMinor, "currency": o.Currency}); err != nil {
					return err
				}
			}
		}
		return q.FinishReconciliationRun(ctx, repo.FinishReconciliationRunParams{ID: run, Status: "completed", Checked: int32(len(theirs)), Issues: int32(issues)}) //nolint:gosec // Bounded by the window.
	})
	return run, err
}

// ReconciliationView is recent runs and their issues.
type ReconciliationView struct {
	Runs   []repo.ListReconciliationRunsRow
	Issues []repo.ListReconciliationIssuesRow
}

// Reconciliation lists recent runs and issues.
func (s *Service) Reconciliation(ctx context.Context, status *string) (ReconciliationView, error) {
	var v ReconciliationView
	err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		q := repo.New(tx)
		var err error
		if v.Runs, err = q.ListReconciliationRuns(ctx); err != nil {
			return err
		}
		v.Issues, err = q.ListReconciliationIssues(ctx, status)
		return err
	})
	return v, err
}

// ResolveIssue closes an issue with a note on what was done.
func (s *Service) ResolveIssue(ctx context.Context, id uuid.UUID, note string) error {
	if note == "" || len(note) > 1000 {
		return apperr.Validation(apperr.FieldError{Field: "note", Code: "required"})
	}
	admin := authctx.AdminFrom(ctx)
	return db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		n, err := repo.New(tx).ResolveReconciliationIssue(ctx, repo.ResolveReconciliationIssueParams{ID: id, ResolvedBy: &admin.AdminID, Note: &note})
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.NotFound()
		}
		return audit.Record(ctx, tx, adminEvent(ctx, nil, "billing.reconciliation_issue_resolved", "reconciliation_issue", &id, nil, map[string]any{"note": note}))
	})
}
