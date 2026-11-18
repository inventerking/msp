package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/api/adminapi"
	"mspengine/internal/api/tenantapi"
	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/platform/commercial"
	"mspengine/internal/platform/identity"
	"mspengine/internal/platform/integration/fakes"
	"mspengine/internal/platform/trust"
	trustrepo "mspengine/internal/platform/trust/repo"
)

// Legal documents, abuse reports, purchases, invoices and the fake checkout (Phase 7).

type (
	identityAcceptance = identity.Acceptance
	trustReport        = trustrepo.ListAbuseReportsRow
)

func legalOut(id uuid.UUID, kind string, version int32, title, body string, requires, reviewed bool, published *time.Time) tenantapi.LegalDocument {
	out := tenantapi.LegalDocument{Id: id, Kind: tenantapi.LegalDocumentKind(kind), Version: int(version), Title: title, Body: body, RequiresAcceptance: requires, CounselReviewed: reviewed}
	if published != nil {
		out.PublishedAt = *published
	}
	return out
}

func (h tenantHandlers) ListLegalDocuments(w http.ResponseWriter, r *http.Request) {
	docs, err := h.Trust.Published(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := tenantapi.LegalDocumentList{Data: []tenantapi.LegalDocument{}}
	for _, d := range docs {
		out.Data = append(out.Data, legalOut(d.ID, d.Kind, d.Version, d.Title, d.Body, d.RequiresAcceptance, d.CounselReviewed, d.PublishedAt))
	}
	writeJSON(w, http.StatusOK, out)
}

// pendingLegal is what the user still has to accept: published documents that require it, at their
// current version. A DPA is accepted for the workspace by its owner; the rest by each person.
func (h tenantHandlers) pendingLegal(r *http.Request) ([]tenantapi.LegalDocument, error) {
	p := authctx.PrincipalFrom(r.Context())
	docs, err := h.Trust.Published(r.Context())
	if err != nil {
		return nil, err
	}
	accepted, err := h.identity.Acceptances(r.Context(), p.UserID)
	if err != nil {
		return nil, err
	}
	out := []tenantapi.LegalDocument{}
	for _, d := range docs {
		if !d.RequiresAcceptance {
			continue
		}
		perWorkspace := d.Kind == "dpa"
		if perWorkspace && (p.TenantID == uuid.Nil || p.RoleKey != "owner") {
			continue
		}
		done := slices.ContainsFunc(accepted, func(a identityAcceptance) bool {
			return a.DocumentID == d.ID && (!perWorkspace || a.WorkspaceID != nil && *a.WorkspaceID == p.TenantID)
		})
		if !done {
			out = append(out, legalOut(d.ID, d.Kind, d.Version, d.Title, d.Body, d.RequiresAcceptance, d.CounselReviewed, d.PublishedAt))
		}
	}
	return out, nil
}

func (h tenantHandlers) GetPendingLegal(w http.ResponseWriter, r *http.Request) {
	out, err := h.pendingLegal(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, tenantapi.PendingLegal{Data: out})
}

func (h tenantHandlers) AcceptLegal(w http.ResponseWriter, r *http.Request) {
	var b tenantapi.AcceptLegal
	if !decode(w, r, &b) {
		return
	}
	p := authctx.PrincipalFrom(r.Context())
	pending, err := h.pendingLegal(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	// Only documents that are current and due can be accepted, so a stale page records nothing wrong.
	var personal, workspace []uuid.UUID
	for _, id := range b.DocumentIds {
		i := slices.IndexFunc(pending, func(d tenantapi.LegalDocument) bool { return d.Id == id })
		if i < 0 {
			writeError(w, r, apperr.Validation(apperr.FieldError{Field: "document_ids", Code: "not_due"}))
			return
		}
		if pending[i].Kind == tenantapi.LegalDocumentKindDpa {
			workspace = append(workspace, id)
		} else {
			personal = append(personal, id)
		}
	}
	if len(personal) > 0 {
		if err := h.identity.Accept(r.Context(), p, personal, nil); err != nil {
			writeError(w, r, err)
			return
		}
	}
	if len(workspace) > 0 {
		if err := h.identity.Accept(r.Context(), p, workspace, &p.TenantID); err != nil {
			writeError(w, r, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h tenantHandlers) ReportAbuse(w http.ResponseWriter, r *http.Request) {
	var b tenantapi.AbuseReportInput
	if !decode(w, r, &b) {
		return
	}
	// A tighter limit than other public routes: reports are reviewed by people.
	key := "abuse:" + string(authctx.MetaFrom(r.Context()).IPHash)
	if ok, wait := h.Limiter.Allow(r.Context(), key, 5, time.Hour); !ok {
		writeError(w, r, apperr.RateLimited(wait))
		return
	}
	in := trust.ReportInput{Category: string(b.Category), Details: b.Details}
	if b.SubjectUrl != nil {
		in.SubjectURL = *b.SubjectUrl
	}
	if b.ReporterEmail != nil {
		in.ReporterEmail = *b.ReporterEmail
	}
	if _, err := h.Trust.Report(r.Context(), in); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, tenantapi.Accepted{Status: "received"})
}

func (h tenantHandlers) GetFakeCheckout(w http.ResponseWriter, r *http.Request, ref string) {
	if h.FakeCheckout == nil {
		writeError(w, r, apperr.NotFound())
		return
	}
	c, err := h.FakeCheckout.Info(r.Context(), ref)
	if errors.Is(err, pgx.ErrNoRows) {
		err = apperr.NotFound()
	}
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, fakeOut(c, h.Config.AppBaseURL))
}

func fakeOut(c fakes.CheckoutInfo, base string) tenantapi.FakeCheckout {
	ret := base + "/subscription"
	return tenantapi.FakeCheckout{Ref: c.Ref, AmountMinor: c.Amount, Currency: c.Currency, Description: c.Description, Status: tenantapi.FakeCheckoutStatus(c.Status), ReturnUrl: &ret}
}

// CompleteFakeCheckout finishes a test checkout and applies the result, as a provider's webhook would.
func (h tenantHandlers) CompleteFakeCheckout(w http.ResponseWriter, r *http.Request, ref string) {
	if h.FakeCheckout == nil {
		writeError(w, r, apperr.NotFound())
		return
	}
	var b tenantapi.FakeCheckoutOutcome
	if !decode(w, r, &b) {
		return
	}
	ev, err := h.FakeCheckout.Complete(r.Context(), ref, string(b.Outcome))
	switch {
	case errors.Is(err, fakes.ErrCheckoutClosed):
		writeError(w, r, apperr.Conflict("checkout_closed", "This checkout is already finished"))
		return
	case err != nil:
		writeError(w, r, err)
		return
	}
	if err := h.commercial.ApplyPaymentEvent(r.Context(), fakes.FakePaymentsKey, ev); err != nil {
		writeError(w, r, err)
		return
	}
	h.GetFakeCheckout(w, r, ref)
}

func (h tenantHandlers) ListPlanOffers(w http.ResponseWriter, r *http.Request) {
	rows, err := h.commercial.PurchasablePlans(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := tenantapi.PlanOfferList{Data: []tenantapi.PlanOffer{}, CardAvailable: h.commercial.CardAvailable(),
		TaxNote: "Prices exclude tax. Tax is not calculated until a payment provider is chosen."}
	for _, p := range rows {
		o := tenantapi.PlanOffer{PlanId: p.ID, PlanKey: p.Key, PlanName: p.Name, ProductKey: p.ProductKey, ProductName: p.ProductName, ProductDescription: p.ProductDescription,
			BillingPeriod: tenantapi.PlanOfferBillingPeriod(p.BillingPeriod), TrialDays: int(p.TrialDays), Currency: p.Currency, AmountMinor: p.AmountMinor,
			UsageLimits: map[string]any{}, IncludedCredits: map[string]any{}}
		_ = json.Unmarshal(p.UsageLimits, &o.UsageLimits)
		_ = json.Unmarshal(p.IncludedCredits, &o.IncludedCredits)
		out.Data = append(out.Data, o)
	}
	writeJSON(w, http.StatusOK, out)
}

func quoteOut(q commercial.Quote) tenantapi.Quote {
	out := tenantapi.Quote{PlanName: q.PlanName, ProductName: q.ProductName, BillingPeriod: q.BillingPeriod, TrialDays: int(q.TrialDays), Currency: q.Currency,
		UnitAmountMinor: q.UnitAmount, Quantity: int(q.Quantity), SubtotalMinor: q.Subtotal, DiscountMinor: q.Discount, TotalMinor: q.Total, Tax: q.Tax}
	if q.Promotion != "" {
		out.PromotionCode = &q.Promotion
	}
	return out
}

func (h tenantHandlers) purchaseOut(r *http.Request, v commercial.PurchaseView) tenantapi.Purchase {
	out := tenantapi.Purchase{Id: v.ID, PlanId: v.PlanID, PlanName: v.PlanName, Status: tenantapi.PurchaseStatus(v.Status), Quote: quoteOut(v.Quote),
		PaymentMethod: v.PaymentMethod, InvoiceId: v.InvoiceID, RejectionReason: v.RejectionReason, CheckoutUrl: v.CheckoutURL, CreatedAt: v.CreatedAt, ReviewedAt: v.ReviewedAt}
	if v.PaymentMethod == "bank_transfer" && v.Status == "payment_pending" {
		if text, err := h.commercial.BankInstructions(r.Context()); err == nil {
			out.BankInstructions = &text
		}
	}
	return out
}

func (h tenantHandlers) SubmitPurchase(w http.ResponseWriter, r *http.Request, params tenantapi.SubmitPurchaseParams) {
	var b tenantapi.PurchaseInput
	if !decode(w, r, &b) {
		return
	}
	in := commercial.PurchaseInput{PlanID: b.PlanId, Currency: b.Currency, Quantity: int32(b.Quantity), PaymentMethod: string(b.PaymentMethod), IdempotencyKey: params.IdempotencyKey} //nolint:gosec // Bounded by the contract.
	if b.PromotionCode != nil {
		in.PromotionCode = *b.PromotionCode
	}
	v, err := h.commercial.Submit(r.Context(), authctx.PrincipalFrom(r.Context()), in)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, h.purchaseOut(r, v))
}

func (h tenantHandlers) ListPurchases(w http.ResponseWriter, r *http.Request) {
	p := authctx.PrincipalFrom(r.Context())
	rows, err := h.commercial.TenantPurchases(r.Context(), p.TenantID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := tenantapi.PurchaseList{Data: []tenantapi.Purchase{}}
	for _, row := range rows {
		var q commercial.Quote
		_ = json.Unmarshal(row.QuoteSnapshot, &q)
		v := commercial.PurchaseView{ID: row.ID, PlanID: row.PlanID, PlanName: row.PlanName, Status: row.Status, Quote: q, InvoiceID: row.InvoiceID,
			RejectionReason: row.RejectionReason, CreatedAt: row.CreatedAt, ReviewedAt: row.ReviewedAt}
		if row.PaymentMethod != nil {
			v.PaymentMethod = *row.PaymentMethod
		}
		out.Data = append(out.Data, h.purchaseOut(r, v))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h tenantHandlers) GetPurchase(w http.ResponseWriter, r *http.Request, id tenantapi.Id) {
	v, err := h.commercial.Purchase(r.Context(), authctx.PrincipalFrom(r.Context()).TenantID, id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, h.purchaseOut(r, v))
}

func (h tenantHandlers) CancelPurchase(w http.ResponseWriter, r *http.Request, id tenantapi.Id) {
	if err := h.commercial.CancelPurchase(r.Context(), authctx.PrincipalFrom(r.Context()), id); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h tenantHandlers) ListInvoices(w http.ResponseWriter, r *http.Request) {
	rows, err := h.commercial.TenantInvoices(r.Context(), authctx.PrincipalFrom(r.Context()).TenantID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := tenantapi.InvoiceList{Data: []tenantapi.Invoice{}}
	for _, i := range rows {
		out.Data = append(out.Data, tenantapi.Invoice{Id: i.ID, SubscriptionId: i.SubscriptionID, Number: i.Number, Status: tenantapi.InvoiceStatus(i.Status), Currency: i.Currency,
			SubtotalMinor: i.SubtotalMinor, TaxMinor: i.TaxMinor, TotalMinor: i.TotalMinor, IssuedAt: i.IssuedAt, DueAt: i.DueAt, PaidAt: i.PaidAt})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h tenantHandlers) GetInvoice(w http.ResponseWriter, r *http.Request, id tenantapi.Id) {
	v, err := h.commercial.TenantInvoice(r.Context(), authctx.PrincipalFrom(r.Context()).TenantID, id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := tenantapi.InvoiceDetail{Invoice: tenantapi.Invoice{Id: v.ID, SubscriptionId: v.SubscriptionID, Number: v.Number, Status: tenantapi.InvoiceStatus(v.Status), Currency: v.Currency,
		SubtotalMinor: v.SubtotalMinor, TaxMinor: v.TaxMinor, TotalMinor: v.TotalMinor, IssuedAt: v.IssuedAt, DueAt: v.DueAt, PaidAt: v.PaidAt},
		Lines: []tenantapi.InvoiceLine{}, Payments: []tenantapi.InvoicePayment{}}
	for _, l := range v.Lines {
		out.Lines = append(out.Lines, tenantapi.InvoiceLine{Description: l.Description, Quantity: int(l.Quantity), UnitAmountMinor: l.UnitAmountMinor, AmountMinor: l.AmountMinor,
			PeriodStart: l.PeriodStart, PeriodEnd: l.PeriodEnd})
	}
	bank := false
	for _, p := range v.Payments {
		out.Payments = append(out.Payments, tenantapi.InvoicePayment{Id: p.ID, Method: p.Method, Status: p.Status, AmountMinor: p.AmountMinor, Currency: p.Currency,
			RefundStatus: p.RefundStatus, FailureCode: p.FailureCode, CreatedAt: p.CreatedAt})
		bank = bank || p.Method == "bank_transfer" && p.Status == "pending"
	}
	if bank && v.Status == "issued" {
		if text, err := h.commercial.BankInstructions(r.Context()); err == nil {
			out.BankInstructions = &text
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (h tenantHandlers) PayInvoice(w http.ResponseWriter, r *http.Request, id tenantapi.Id) {
	url, err := h.commercial.PayInvoice(r.Context(), authctx.PrincipalFrom(r.Context()), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, tenantapi.CheckoutLink{CheckoutUrl: url})
}

func (h tenantHandlers) CancelSubscription(w http.ResponseWriter, r *http.Request, id tenantapi.Id) {
	if err := h.commercial.CancelSubscription(r.Context(), authctx.PrincipalFrom(r.Context()), id); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Admin: purchases, payments, refunds, dunning, reconciliation, legal documents and abuse reports.

func (h adminHandlers) names(r *http.Request, ids []uuid.UUID) map[uuid.UUID]string {
	names, err := h.identity.TenantNames(r.Context(), ids)
	if err != nil {
		return map[uuid.UUID]string{}
	}
	return names
}

func (h adminHandlers) ListPurchaseQueue(w http.ResponseWriter, r *http.Request, params adminapi.ListPurchaseQueueParams) {
	rows, err := h.commercial.ReviewQueue(r.Context(), params.Status)
	if err != nil {
		writeError(w, r, err)
		return
	}
	policy, err := h.commercial.Policy(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.TenantID)
	}
	names := h.names(r, ids)
	out := adminapi.AdminPurchaseList{Data: []adminapi.AdminPurchase{}, ReviewTargetHours: policy.ReviewTargetHours}
	for _, row := range rows {
		q := map[string]any{}
		_ = json.Unmarshal(row.QuoteSnapshot, &q)
		since := row.CreatedAt
		if row.SubmittedAt != nil {
			since = *row.SubmittedAt
		}
		waiting := time.Since(since).Hours()
		if row.ReviewedAt != nil {
			waiting = row.ReviewedAt.Sub(since).Hours()
		}
		over := row.Status == "under_review" && waiting > float64(policy.ReviewTargetHours)
		out.Data = append(out.Data, adminapi.AdminPurchase{Id: row.ID, TenantId: row.TenantID, TenantName: names[row.TenantID], PlanName: row.PlanName, Status: row.Status,
			Quantity: int(row.Quantity), Quote: q, PaymentMethod: row.PaymentMethod, InvoiceId: row.InvoiceID, RejectionReason: row.RejectionReason, WaitingHours: waiting,
			OverTarget: &over, CreatedAt: row.CreatedAt, SubmittedAt: row.SubmittedAt, ReviewedAt: row.ReviewedAt})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h adminHandlers) ApprovePurchase(w http.ResponseWriter, r *http.Request, id adminapi.Id) {
	var b adminapi.Reason
	if !decode(w, r, &b) {
		return
	}
	sub, err := h.commercial.ApprovePurchase(r.Context(), id, b.Reason)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, adminapi.SubscriptionRef{SubscriptionId: sub})
}

func (h adminHandlers) RejectPurchase(w http.ResponseWriter, r *http.Request, id adminapi.Id) {
	var b adminapi.Reason
	if !decode(w, r, &b) {
		return
	}
	if err := h.commercial.RejectPurchase(r.Context(), id, b.Reason); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h adminHandlers) ListBankTransfers(w http.ResponseWriter, r *http.Request) {
	rows, err := h.commercial.PendingBankTransfers(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.TenantID)
	}
	names := h.names(r, ids)
	out := adminapi.BankTransferList{Data: []adminapi.BankTransfer{}}
	for _, row := range rows {
		out.Data = append(out.Data, adminapi.BankTransfer{PaymentId: row.ID, TenantId: row.TenantID, TenantName: names[row.TenantID], InvoiceId: row.InvoiceID,
			InvoiceNumber: row.InvoiceNumber, AmountMinor: row.AmountMinor, Currency: row.Currency, Purpose: row.Purpose, CreatedAt: row.CreatedAt})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h adminHandlers) MarkPaymentPaid(w http.ResponseWriter, r *http.Request, id adminapi.Id) {
	var b adminapi.PaymentReference
	if !decode(w, r, &b) {
		return
	}
	if err := h.commercial.MarkBankTransferPaid(r.Context(), id, b.Reference); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h adminHandlers) ListRefunds(w http.ResponseWriter, r *http.Request) {
	rows, err := h.commercial.RefundsToMake(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.TenantID)
	}
	names := h.names(r, ids)
	out := adminapi.RefundList{Data: []adminapi.Refund{}}
	for _, row := range rows {
		status := ""
		if row.RefundStatus != nil {
			status = *row.RefundStatus
		}
		out.Data = append(out.Data, adminapi.Refund{PaymentId: row.ID, TenantId: row.TenantID, TenantName: names[row.TenantID], AmountMinor: row.AmountMinor,
			Currency: row.Currency, Method: row.Method, RefundStatus: status, CreatedAt: row.CreatedAt})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h adminHandlers) RetryRefund(w http.ResponseWriter, r *http.Request, id adminapi.Id) {
	if err := h.commercial.RetryRefund(r.Context(), id); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h adminHandlers) MarkRefunded(w http.ResponseWriter, r *http.Request, id adminapi.Id) {
	var b adminapi.Note
	if !decode(w, r, &b) {
		return
	}
	if err := h.commercial.MarkRefunded(r.Context(), id, b.Note); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func policyOut(p commercial.DunningPolicy) adminapi.DunningPolicy {
	days := p.RetryDays
	if days == nil {
		days = []int{}
	}
	return adminapi.DunningPolicy{RetryDays: days, SuspendAfterDays: p.SuspendAfterDays, ExpireAfterDays: p.ExpireAfterDays, BankTransferDueDays: p.BankTransferDueDays,
		ReviewTargetHours: p.ReviewTargetHours}
}

func (h adminHandlers) GetDunningPolicy(w http.ResponseWriter, r *http.Request) {
	p, err := h.commercial.Policy(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, policyOut(p))
}

func (h adminHandlers) PutDunningPolicy(w http.ResponseWriter, r *http.Request) {
	var b adminapi.DunningPolicy
	if !decode(w, r, &b) {
		return
	}
	p := commercial.DunningPolicy{RetryDays: b.RetryDays, SuspendAfterDays: b.SuspendAfterDays, ExpireAfterDays: b.ExpireAfterDays, BankTransferDueDays: b.BankTransferDueDays,
		ReviewTargetHours: b.ReviewTargetHours}
	if err := h.commercial.SetPolicy(r.Context(), p); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, policyOut(p))
}

func (h adminHandlers) GetBankInstructions(w http.ResponseWriter, r *http.Request) {
	text, err := h.commercial.BankInstructions(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, adminapi.BankInstructions{Text: text})
}

func (h adminHandlers) PutBankInstructions(w http.ResponseWriter, r *http.Request) {
	var b adminapi.BankInstructions
	if !decode(w, r, &b) {
		return
	}
	if err := h.commercial.SetBankInstructions(r.Context(), b.Text); err != nil {
		writeError(w, r, err)
		return
	}
	h.GetBankInstructions(w, r)
}

func (h adminHandlers) reconciliation(w http.ResponseWriter, r *http.Request, status *string) {
	v, err := h.commercial.Reconciliation(r.Context(), status)
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := adminapi.Reconciliation{Runs: []adminapi.ReconciliationRun{}, Issues: []adminapi.ReconciliationIssue{}}
	for _, run := range v.Runs {
		out.Runs = append(out.Runs, adminapi.ReconciliationRun{Id: run.ID, Provider: run.Provider, WindowStart: run.WindowStart, WindowEnd: run.WindowEnd, Checked: int(run.Checked),
			Issues: int(run.Issues), Status: run.Status, Error: run.Error, CreatedAt: run.CreatedAt})
	}
	for _, i := range v.Issues {
		d := map[string]any{}
		_ = json.Unmarshal(i.Detail, &d)
		out.Issues = append(out.Issues, adminapi.ReconciliationIssue{Id: i.ID, RunId: i.RunID, Provider: i.Provider, ProviderRef: i.ProviderRef, PaymentId: i.PaymentID,
			Kind: i.Kind, Detail: d, Status: i.Status, Note: i.Note, ResolvedAt: i.ResolvedAt, CreatedAt: i.CreatedAt})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h adminHandlers) GetReconciliation(w http.ResponseWriter, r *http.Request, params adminapi.GetReconciliationParams) {
	h.reconciliation(w, r, params.Status)
}

func (h adminHandlers) RunReconciliation(w http.ResponseWriter, r *http.Request) {
	for _, key := range h.Integrate.PaymentProviders() {
		if _, err := h.commercial.Reconcile(r.Context(), key); err != nil {
			writeError(w, r, err)
			return
		}
	}
	h.reconciliation(w, r, nil)
}

func (h adminHandlers) ResolveReconciliationIssue(w http.ResponseWriter, r *http.Request, id adminapi.Id) {
	var b adminapi.Note
	if !decode(w, r, &b) {
		return
	}
	if err := h.commercial.ResolveIssue(r.Context(), id, b.Note); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h adminHandlers) legalDoc(w http.ResponseWriter, r *http.Request, id uuid.UUID, status int) {
	d, err := h.Trust.Document(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	counts, err := h.identity.AcceptanceCounts(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, status, adminapi.AdminLegalDocument{Id: d.ID, Kind: d.Kind, Version: int(d.Version), Title: d.Title, Body: &d.Body, Status: adminapi.AdminLegalDocumentStatus(d.Status),
		RequiresAcceptance: d.RequiresAcceptance, CounselReviewed: d.CounselReviewed, AcceptedCount: counts[d.ID], PublishedAt: d.PublishedAt, CreatedAt: d.CreatedAt})
}

func (h adminHandlers) ListAdminLegalDocuments(w http.ResponseWriter, r *http.Request) {
	docs, err := h.Trust.Documents(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	counts, err := h.identity.AcceptanceCounts(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := adminapi.AdminLegalDocumentList{Data: []adminapi.AdminLegalDocument{}}
	for _, d := range docs {
		out.Data = append(out.Data, adminapi.AdminLegalDocument{Id: d.ID, Kind: d.Kind, Version: int(d.Version), Title: d.Title, Status: adminapi.AdminLegalDocumentStatus(d.Status),
			RequiresAcceptance: d.RequiresAcceptance, CounselReviewed: d.CounselReviewed, AcceptedCount: counts[d.ID], PublishedAt: d.PublishedAt, CreatedAt: d.CreatedAt})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h adminHandlers) CreateLegalDraft(w http.ResponseWriter, r *http.Request) {
	var b adminapi.LegalDraft
	if !decode(w, r, &b) {
		return
	}
	id, err := h.Trust.CreateDraft(r.Context(), trust.DocumentInput{Kind: string(b.Kind), Title: b.Title, Body: b.Body, RequiresAcceptance: b.RequiresAcceptance})
	if err != nil {
		writeError(w, r, err)
		return
	}
	h.legalDoc(w, r, id, http.StatusCreated)
}

func (h adminHandlers) GetAdminLegalDocument(w http.ResponseWriter, r *http.Request, id adminapi.Id) {
	h.legalDoc(w, r, id, http.StatusOK)
}

func (h adminHandlers) UpdateLegalDraft(w http.ResponseWriter, r *http.Request, id adminapi.Id) {
	var b adminapi.LegalDraft
	if !decode(w, r, &b) {
		return
	}
	if err := h.Trust.UpdateDraft(r.Context(), id, trust.DocumentInput{Kind: string(b.Kind), Title: b.Title, Body: b.Body, RequiresAcceptance: b.RequiresAcceptance}); err != nil {
		writeError(w, r, err)
		return
	}
	h.legalDoc(w, r, id, http.StatusOK)
}

func (h adminHandlers) PublishLegalDocument(w http.ResponseWriter, r *http.Request, id adminapi.Id) {
	var b adminapi.PublishLegal
	if !decode(w, r, &b) {
		return
	}
	if err := h.Trust.Publish(r.Context(), id, b.CounselReviewed); err != nil {
		writeError(w, r, err)
		return
	}
	h.legalDoc(w, r, id, http.StatusOK)
}

func abuseOut(a trustReport) adminapi.AbuseReport {
	return adminapi.AbuseReport{Id: a.ID, Category: a.Category, SubjectUrl: a.SubjectUrl, Details: a.Details, ReporterEmail: a.ReporterEmail, TargetTenantId: a.TargetTenantID,
		Status: adminapi.AbuseReportStatus(a.Status), Decision: a.Decision, ReviewedAt: a.ReviewedAt, CreatedAt: a.CreatedAt}
}

func (h adminHandlers) ListAbuseReports(w http.ResponseWriter, r *http.Request, params adminapi.ListAbuseReportsParams) {
	rows, err := h.Trust.Reports(r.Context(), params.Status)
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := adminapi.AbuseReportList{Data: []adminapi.AbuseReport{}}
	for _, row := range rows {
		out.Data = append(out.Data, abuseOut(row))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h adminHandlers) ReviewAbuseReport(w http.ResponseWriter, r *http.Request, id adminapi.Id) {
	var b adminapi.AbuseReview
	if !decode(w, r, &b) {
		return
	}
	if err := h.Trust.Review(r.Context(), id, trust.ReviewInput{Status: string(b.Status), Decision: b.Decision, Tenant: b.TargetTenantId}); err != nil {
		writeError(w, r, err)
		return
	}
	rows, err := h.Trust.Reports(r.Context(), nil)
	if err != nil {
		writeError(w, r, err)
		return
	}
	for _, row := range rows {
		if row.ID == id {
			writeJSON(w, http.StatusOK, abuseOut(row))
			return
		}
	}
	writeError(w, r, apperr.NotFound())
}
