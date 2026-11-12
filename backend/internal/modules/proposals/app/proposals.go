package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/jobs"
	crm "mspengine/internal/modules/crm/contract"
	"mspengine/internal/modules/proposals/domain"
	"mspengine/internal/modules/proposals/repo"
	"mspengine/internal/platform/audit"
	"mspengine/internal/platform/integration/aigateway"
)

// Content is a version's text: its sections and the SLA and terms it includes.
type Content struct {
	Sections []Section  `json:"sections"`
	SLAID    *uuid.UUID `json:"sla_id,omitempty"`
	TermsID  *uuid.UUID `json:"terms_id,omitempty"`
}

// CreateInput starts a proposal.
type CreateInput struct {
	Title        string
	CRMCompanyID *uuid.UUID
	NewCompany   *crm.CompanyInput
	DealID       *uuid.UUID
	TemplateID   *uuid.UUID
	Currency     string
}

// Create starts a proposal with a first draft version. A new customer is added to the tenant's
// customer registry, which works without the CRM module (D-02).
func (s *Service) Create(ctx context.Context, p authctx.Principal, in CreateInput) (uuid.UUID, error) {
	var company uuid.UUID
	switch {
	case in.CRMCompanyID != nil:
		if _, err := s.Registry.GetCompanySummary(ctx, p.TenantID, *in.CRMCompanyID); err != nil {
			return uuid.Nil, apperr.Validation(apperr.FieldError{Field: "crm_company_id", Code: "unknown_company"})
		}
		company = *in.CRMCompanyID
	case in.NewCompany != nil:
		in.NewCompany.TenantID, in.NewCompany.SourceModule, in.NewCompany.ActorID = p.TenantID, Module, uid(p)
		ref := uuid.New()
		in.NewCompany.SourceRef = &ref
		id, err := s.Registry.EnsureCompany(ctx, *in.NewCompany)
		if err != nil {
			return uuid.Nil, err
		}
		company = id
	default:
		return uuid.Nil, apperr.Validation(apperr.FieldError{Field: "crm_company_id", Code: "required"})
	}
	if in.DealID != nil {
		d, err := s.Deals.GetDealSummary(ctx, p.TenantID, *in.DealID)
		if err != nil || d.CRMCompanyID != company {
			return uuid.Nil, apperr.Validation(apperr.FieldError{Field: "deal_id", Code: "unknown_deal"})
		}
		if in.Currency == "" {
			in.Currency = d.Currency
		}
	}
	if in.Currency == "" {
		in.Currency = "GBP"
	}
	var id uuid.UUID
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		content := Content{Sections: []Section{{Heading: "Overview", Body: ""}}}
		if in.TemplateID != nil {
			t, err := q.GetProposalTemplate(ctx, repo.GetProposalTemplateParams{TenantID: p.TenantID, ID: *in.TemplateID})
			if err != nil {
				return apperr.Validation(apperr.FieldError{Field: "template_id", Code: "unknown_template"})
			}
			if err := json.Unmarshal(t.Sections, &content.Sections); err != nil {
				return err
			}
			content.SLAID, content.TermsID = t.DefaultSlaID, t.DefaultTermsID
		}
		if _, err := q.LockNumbering(ctx, "proposal-number:"+p.TenantID.String()); err != nil {
			return err
		}
		prefix := fmt.Sprintf("P-%d-", time.Now().Year())
		n, err := q.CountProposalsThisYear(ctx, repo.CountProposalsThisYearParams{TenantID: p.TenantID, Prefix: prefix})
		if err != nil {
			return err
		}
		if id, err = q.InsertProposal(ctx, repo.InsertProposalParams{TenantID: p.TenantID, Number: fmt.Sprintf("%s%04d", prefix, n+1), Title: in.Title, CrmCompanyID: company, DealID: in.DealID, TemplateID: in.TemplateID, OwnerUserID: uid(p), CreatedBy: uid(p)}); err != nil {
			return err
		}
		v, err := q.InsertVersion(ctx, repo.InsertVersionParams{TenantID: p.TenantID, ProposalID: id, Content: mustJSON(content), Currency: in.Currency, TermMonths: 12, CreatedBy: uid(p)})
		if err != nil {
			return err
		}
		if err := q.SetCurrentVersion(ctx, repo.SetCurrentVersionParams{TenantID: p.TenantID, ID: id, VersionID: &v.ID, Status: "draft"}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "proposals.created", "proposal", &id, map[string]any{"title": in.Title, "crm_company_id": company}))
	})
	return id, err
}

// ProposalView is a proposal with its versions.
type ProposalView struct {
	repo.GetProposalRow
	Customer string
	Versions []repo.ListVersionsRow
}

// Get returns a proposal with its versions.
func (s *Service) Get(ctx context.Context, p authctx.Principal, id uuid.UUID) (ProposalView, error) {
	var out ProposalView
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		if out.GetProposalRow, err = q.GetProposal(ctx, repo.GetProposalParams{TenantID: p.TenantID, ID: id}); err != nil {
			return notFound(err)
		}
		out.Versions, err = q.ListVersions(ctx, repo.ListVersionsParams{TenantID: p.TenantID, ProposalID: id})
		return err
	})
	if err != nil {
		return out, err
	}
	if c, err := s.Registry.GetCompanySummary(ctx, p.TenantID, out.CrmCompanyID); err == nil {
		out.Customer = c.Name
	}
	return out, nil
}

// List lists proposals.
func (s *Service) List(ctx context.Context, p authctx.Principal, status *string, company *uuid.UUID) ([]repo.ListProposalsRow, error) {
	var out []repo.ListProposalsRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.ListProposals(ctx, repo.ListProposalsParams{TenantID: p.TenantID, Status: status, CrmCompanyID: company})
		return err
	})
	return out, err
}

// Delete removes a proposal that was never issued. Issued proposals are a record and stay.
func (s *Service) Delete(ctx context.Context, p authctx.Principal, id uuid.UUID) error {
	return s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		if _, err := q.GetProposal(ctx, repo.GetProposalParams{TenantID: p.TenantID, ID: id}); err != nil {
			return notFound(err)
		}
		n, err := q.DeleteProposal(ctx, repo.DeleteProposalParams{TenantID: p.TenantID, ID: id})
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.Conflict("proposal_issued", "An issued proposal cannot be deleted. Withdraw it with a new version instead")
		}
		return audit.Record(ctx, tx, userAudit(p, "proposals.deleted", "proposal", &id, nil))
	})
}

// VersionView is a version with its lines, totals and the hash of what it would issue.
type VersionView struct {
	repo.GetVersionRow
	Content       Content
	Lines         []repo.ListLinesRow
	ContentHash   []byte
	NeedsApproval bool
	Approved      bool
	AIAssisted    bool
}

func (s *Service) loadVersion(ctx context.Context, q *repo.Queries, tenant, proposal uuid.UUID, version int32) (repo.GetProposalRow, repo.GetVersionRow, error) {
	pr, err := q.GetProposal(ctx, repo.GetProposalParams{TenantID: tenant, ID: proposal})
	if err != nil {
		return pr, repo.GetVersionRow{}, notFound(err)
	}
	v, err := q.GetVersion(ctx, repo.GetVersionParams{TenantID: tenant, ProposalID: proposal, Version: version})
	return pr, v, notFound(err)
}

// snapshot builds what a customer would see, from stored lines and texts.
func (s *Service) snapshot(ctx context.Context, q *repo.Queries, tenant uuid.UUID, pr repo.GetProposalRow, v repo.GetVersionRow) (domain.Snapshot, []repo.ListLinesRow, Content, error) {
	var content Content
	if err := json.Unmarshal(v.Content, &content); err != nil {
		return domain.Snapshot{}, nil, content, err
	}
	lines, err := q.ListLines(ctx, repo.ListLinesParams{TenantID: tenant, VersionID: v.ID})
	if err != nil {
		return domain.Snapshot{}, nil, content, err
	}
	in := make([]domain.Line, len(lines))
	for i, l := range lines {
		in[i] = domain.Line{Name: l.Name, PricingModel: l.PricingModel, Quantity: int64(l.Quantity), UnitPriceMinor: l.UnitPriceMinor, SetupFeeMinor: l.SetupFeeMinor, DiscountBps: int64(l.DiscountBps)}
	}
	totals, _ := domain.Calculate(v.Currency, int64(v.TermMonths), in) // An empty draft has no totals yet.
	snap := domain.Snapshot{Title: pr.Title, Content: v.Content, Totals: totals, Version: v.Version}
	if c, err := s.Registry.GetCompanySummary(ctx, tenant, pr.CrmCompanyID); err == nil {
		snap.Customer = c.Name
	}
	if v.ValidUntil != nil {
		snap.Valid = v.ValidUntil.Format(time.DateOnly)
	}
	if content.SLAID != nil {
		if b, err := q.GetSLA(ctx, repo.GetSLAParams{TenantID: tenant, ID: *content.SLAID}); err == nil {
			snap.SLA = b
		}
	}
	if content.TermsID != nil {
		if b, err := q.GetTerms(ctx, repo.GetTermsParams{TenantID: tenant, ID: *content.TermsID}); err == nil {
			snap.Terms = b
		}
	}
	return snap, lines, content, nil
}

func needsApproval(v repo.GetVersionRow) bool {
	return v.TcvMinor >= ApprovalThresholdMinor || v.AiRequestID != nil
}

// Version returns a version, with the hash an approver or sender must confirm.
func (s *Service) Version(ctx context.Context, p authctx.Principal, proposal uuid.UUID, version int32) (VersionView, error) {
	var out VersionView
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		pr, v, err := s.loadVersion(ctx, q, p.TenantID, proposal, version)
		if err != nil {
			return err
		}
		out.GetVersionRow = v
		snap, lines, content, err := s.snapshot(ctx, q, p.TenantID, pr, v)
		if err != nil {
			return err
		}
		out.Lines, out.Content = lines, content
		if v.SnapshotHash != nil {
			out.ContentHash = v.SnapshotHash
		} else if _, out.ContentHash, err = domain.Hash(snap); err != nil {
			return err
		}
		out.NeedsApproval, out.AIAssisted = needsApproval(v), v.AiRequestID != nil
		if h, err := q.ApprovedHash(ctx, repo.ApprovedHashParams{TenantID: p.TenantID, VersionID: v.ID}); err == nil {
			out.Approved = bytes.Equal(h, out.ContentHash)
		}
		return nil
	})
	return out, err
}

// EditInput replaces a draft's content and lines.
type EditInput struct {
	Content    Content
	TermMonths int64
	Currency   string
	ValidUntil *time.Time
	Lines      []domain.Line
}

// Calculate prices lines without saving them.
func (s *Service) Calculate(in EditInput) (domain.Totals, error) {
	t, err := domain.Calculate(in.Currency, in.TermMonths, in.Lines)
	if err != nil {
		return t, apperr.Validation(apperr.FieldError{Field: "lines", Code: calcCode(err)})
	}
	return t, nil
}

func calcCode(err error) string {
	switch {
	case errors.Is(err, domain.ErrNoLines):
		return "required"
	case errors.Is(err, domain.ErrBadTerm):
		return "invalid_term"
	}
	return "invalid_line"
}

// Edit saves a draft. Totals are always computed here, never taken from the client. Editing an
// approved or pending version sends it back to draft, because the approval was for other content.
func (s *Service) Edit(ctx context.Context, p authctx.Principal, proposal uuid.UUID, version int32, in EditInput) (VersionView, error) {
	var totals domain.Totals
	if len(in.Lines) > 0 {
		var err error
		if totals, err = s.Calculate(in); err != nil {
			return VersionView{}, err
		}
	} else if in.TermMonths < 1 || in.TermMonths > domain.MaxTermMonths {
		return VersionView{}, apperr.Validation(apperr.FieldError{Field: "term_months", Code: "invalid_term"})
	}
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		_, v, err := s.loadVersion(ctx, q, p.TenantID, proposal, version)
		if err != nil {
			return err
		}
		switch v.Status {
		case "draft":
		case "pending_approval", "approved":
			if _, err := q.SetVersionStatus(ctx, repo.SetVersionStatusParams{TenantID: p.TenantID, ID: v.ID, Status: "draft", FromStatus: v.Status}); err != nil {
				return err
			}
		default:
			return apperr.Conflict("version_issued", "An issued version cannot change. Create a new version instead")
		}
		if err := checkTexts(ctx, q, p.TenantID, in.Content.SLAID, in.Content.TermsID); err != nil {
			return err
		}
		if _, err := q.UpdateDraftVersion(ctx, repo.UpdateDraftVersionParams{TenantID: p.TenantID, ID: v.ID, Content: mustJSON(in.Content), Currency: in.Currency,
			SetupTotalMinor: totals.SetupTotalMinor, MrrTotalMinor: totals.MRRTotalMinor, TermMonths: int32(in.TermMonths), TcvMinor: totals.TCVMinor, ValidUntil: in.ValidUntil, AiRequestID: v.AiRequestID}); err != nil { //nolint:gosec // Bounded by the calculation.
			return err
		}
		if err := q.DeleteLines(ctx, repo.DeleteLinesParams{TenantID: p.TenantID, VersionID: v.ID}); err != nil {
			return err
		}
		for i, l := range totals.Lines {
			var catalog *uuid.UUID
			if l.CatalogItemID != nil {
				id, err := uuid.Parse(*l.CatalogItemID)
				if err != nil {
					return apperr.Validation(apperr.FieldError{Field: "lines", Code: "unknown_catalog_item"})
				}
				if _, err := q.GetCatalogItem(ctx, repo.GetCatalogItemParams{TenantID: p.TenantID, ID: id}); err != nil {
					return apperr.Validation(apperr.FieldError{Field: "lines", Code: "unknown_catalog_item"})
				}
				catalog = &id
			}
			if err := q.InsertLine(ctx, repo.InsertLineParams{TenantID: p.TenantID, VersionID: v.ID, CatalogItemID: catalog, Position: int32(i + 1), Name: l.Name, PricingModel: l.PricingModel, //nolint:gosec // Bounded.
				Quantity: int32(l.Quantity), UnitPriceMinor: l.UnitPriceMinor, SetupFeeMinor: l.SetupFeeMinor, DiscountBps: int32(l.DiscountBps), RecurringTotalMinor: l.RecurringTotalMinor}); err != nil { //nolint:gosec // Bounded.
				return err
			}
		}
		return nil
	})
	if err != nil {
		return VersionView{}, err
	}
	return s.Version(ctx, p, proposal, version)
}

// NewVersion copies the latest version into a new draft. This is how an issued proposal is corrected.
func (s *Service) NewVersion(ctx context.Context, p authctx.Principal, proposal uuid.UUID) (VersionView, error) {
	var version int32
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		pr, err := q.GetProposal(ctx, repo.GetProposalParams{TenantID: p.TenantID, ID: proposal})
		if err != nil {
			return notFound(err)
		}
		versions, err := q.ListVersions(ctx, repo.ListVersionsParams{TenantID: p.TenantID, ProposalID: proposal})
		if err != nil || len(versions) == 0 {
			return err
		}
		for _, v := range versions {
			if v.Status == "draft" || v.Status == "pending_approval" || v.Status == "approved" {
				return apperr.Conflict("draft_exists", "This proposal already has an unsent version. Edit that one")
			}
		}
		latest, err := q.GetVersion(ctx, repo.GetVersionParams{TenantID: p.TenantID, ProposalID: proposal, Version: versions[0].Version})
		if err != nil {
			return err
		}
		nv, err := q.InsertVersion(ctx, repo.InsertVersionParams{TenantID: p.TenantID, ProposalID: proposal, Content: latest.Content, Currency: latest.Currency, SetupTotalMinor: latest.SetupTotalMinor,
			MrrTotalMinor: latest.MrrTotalMinor, TermMonths: latest.TermMonths, TcvMinor: latest.TcvMinor, ValidUntil: nil, CreatedBy: uid(p)})
		if err != nil {
			return err
		}
		lines, err := q.ListLines(ctx, repo.ListLinesParams{TenantID: p.TenantID, VersionID: latest.ID})
		if err != nil {
			return err
		}
		for _, l := range lines {
			if err := q.InsertLine(ctx, repo.InsertLineParams{TenantID: p.TenantID, VersionID: nv.ID, CatalogItemID: l.CatalogItemID, Position: l.Position, Name: l.Name, PricingModel: l.PricingModel, Quantity: l.Quantity,
				UnitPriceMinor: l.UnitPriceMinor, SetupFeeMinor: l.SetupFeeMinor, DiscountBps: l.DiscountBps, RecurringTotalMinor: l.RecurringTotalMinor}); err != nil {
				return err
			}
		}
		version = nv.Version
		if err := q.SetCurrentVersion(ctx, repo.SetCurrentVersionParams{TenantID: p.TenantID, ID: proposal, VersionID: &nv.ID, Status: pr.Status}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "proposals.version_created", "proposal", &proposal, map[string]any{"version": nv.Version}))
	})
	if err != nil {
		return VersionView{}, err
	}
	return s.Version(ctx, p, proposal, version)
}

// Submit asks for approval of exactly the content shown, by its hash.
func (s *Service) Submit(ctx context.Context, p authctx.Principal, proposal uuid.UUID, version int32) (VersionView, error) {
	view, err := s.Version(ctx, p, proposal, version)
	if err != nil {
		return view, err
	}
	if view.Status != "draft" {
		return view, apperr.Conflict("not_a_draft", "Only a draft can be sent for approval")
	}
	if len(view.Lines) == 0 {
		return view, apperr.Validation(apperr.FieldError{Field: "lines", Code: "required"})
	}
	err = s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		if n, err := q.SetVersionStatus(ctx, repo.SetVersionStatusParams{TenantID: p.TenantID, ID: view.ID, Status: "pending_approval", FromStatus: "draft"}); err != nil || n == 0 {
			if err == nil {
				err = apperr.Conflict("not_a_draft", "Only a draft can be sent for approval")
			}
			return err
		}
		if _, err := q.InsertApproval(ctx, repo.InsertApprovalParams{TenantID: p.TenantID, VersionID: view.ID, ContentHash: view.ContentHash, RequestedBy: p.UserID}); err != nil {
			return err
		}
		if err := q.SetProposalStatus(ctx, repo.SetProposalStatusParams{TenantID: p.TenantID, ID: proposal, Status: "in_review"}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "proposals.approval_requested", "proposal", &proposal, map[string]any{"version": version}))
	})
	if err != nil {
		return VersionView{}, err
	}
	return s.Version(ctx, p, proposal, version)
}

// Decide approves or rejects a pending version. The approver confirms the hash they reviewed, so an
// edit made after review cannot slip through.
func (s *Service) Decide(ctx context.Context, p authctx.Principal, proposal uuid.UUID, version int32, approve bool, hash []byte, reason *string) (VersionView, error) {
	view, err := s.Version(ctx, p, proposal, version)
	if err != nil {
		return view, err
	}
	if view.Status != "pending_approval" {
		return view, apperr.Conflict("not_pending", "This version is not waiting for approval")
	}
	if !bytes.Equal(hash, view.ContentHash) {
		return view, apperr.Conflict("content_changed", "The proposal changed after you opened it. Review it again")
	}
	status, to, action := "approved", "approved", "proposals.approved"
	if !approve {
		status, to, action = "rejected", "draft", "proposals.rejected"
	}
	err = s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		a, err := q.PendingApproval(ctx, repo.PendingApprovalParams{TenantID: p.TenantID, VersionID: view.ID})
		if err != nil {
			return notFound(err)
		}
		if err := q.DecideApproval(ctx, repo.DecideApprovalParams{TenantID: p.TenantID, ID: a.ID, Status: status, DecidedBy: &p.UserID, Reason: reason}); err != nil {
			return err
		}
		if _, err := q.SetVersionStatus(ctx, repo.SetVersionStatusParams{TenantID: p.TenantID, ID: view.ID, Status: to, FromStatus: "pending_approval"}); err != nil {
			return err
		}
		if err := q.SetProposalStatus(ctx, repo.SetProposalStatusParams{TenantID: p.TenantID, ID: proposal, Status: "draft"}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, action, "proposal", &proposal, map[string]any{"version": version, "reason": reason}))
	})
	if err != nil {
		return VersionView{}, err
	}
	return s.Version(ctx, p, proposal, version)
}

// Recipient receives a share link.
type Recipient struct {
	Name, Email string
}

// Issue freezes a version and sends it. The caller confirms the hash of what they reviewed. Issuing
// an issued version again returns it unchanged. The PDF and emails follow as a job, so a delivery
// failure never undoes the issue.
func (s *Service) Issue(ctx context.Context, p authctx.Principal, proposal uuid.UUID, version int32, recipients []Recipient, message string, hash []byte) (VersionView, error) {
	view, err := s.Version(ctx, p, proposal, version)
	if err != nil {
		return view, err
	}
	if view.Status == "issued" {
		return view, nil
	}
	if view.Status != "draft" && view.Status != "approved" {
		return view, apperr.Conflict("cannot_issue", "This version cannot be issued")
	}
	if !bytes.Equal(hash, view.ContentHash) {
		return view, apperr.Conflict("content_changed", "The proposal changed after you opened it. Review it again")
	}
	if len(view.Lines) == 0 {
		return view, apperr.Validation(apperr.FieldError{Field: "lines", Code: "required"})
	}
	if view.ValidUntil == nil || !view.ValidUntil.After(time.Now()) {
		return view, apperr.Validation(apperr.FieldError{Field: "valid_until", Code: "must_be_future"})
	}
	if view.NeedsApproval && !view.Approved {
		return view, apperr.New(http.StatusConflict, "approval_required", "This proposal needs approval before it is sent")
	}
	err = s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		pr, v, err := s.loadVersion(ctx, q, p.TenantID, proposal, version)
		if err != nil {
			return err
		}
		snap, _, _, err := s.snapshot(ctx, q, p.TenantID, pr, v)
		if err != nil {
			return err
		}
		raw, h, err := domain.Hash(snap)
		if err != nil {
			return err
		}
		if !bytes.Equal(h, hash) {
			return apperr.Conflict("content_changed", "The proposal changed after you opened it. Review it again")
		}
		n, err := q.IssueVersion(ctx, repo.IssueVersionParams{TenantID: p.TenantID, ID: v.ID, CommercialSnapshot: raw, SnapshotHash: h})
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.Conflict("cannot_issue", "This version cannot be issued")
		}
		if err := q.SupersedeOthers(ctx, repo.SupersedeOthersParams{TenantID: p.TenantID, ProposalID: proposal, Keep: v.ID}); err != nil {
			return err
		}
		if err := q.SetCurrentVersion(ctx, repo.SetCurrentVersionParams{TenantID: p.TenantID, ID: proposal, VersionID: &v.ID, Status: "issued"}); err != nil {
			return err
		}
		expires := v.ValidUntil.Add(24 * time.Hour)
		for _, r := range recipients {
			id := uuid.New()
			if _, err := q.InsertShareLink(ctx, repo.InsertShareLinkParams{ID: id, TenantID: p.TenantID, VersionID: v.ID, RecipientEmail: strings.ToLower(strings.TrimSpace(r.Email)), TokenHash: tokenHash(s.linkToken(id)), ExpiresAt: expires}); err != nil {
				return err
			}
		}
		if _, _, err := jobs.Enqueue(ctx, tx, jobs.Spec{TenantID: &p.TenantID, Kind: JobDeliver, Queue: jobs.QueueDefault, MaxAttempts: 5,
			Payload: deliverPayload{VersionID: v.ID, Recipients: recipients, Message: message}, IdempotencyKey: "deliver:" + v.ID.String()}); err != nil {
			return err
		}
		if err := emit(ctx, tx, p.TenantID, "version.issued", "proposal", proposal, map[string]any{"proposal_id": proposal, "version_id": v.ID, "crm_company_id": pr.CrmCompanyID,
			"deal_id": pr.DealID, "title": pr.Title, "currency": v.Currency, "mrr_total_minor": v.MrrTotalMinor, "tcv_minor": v.TcvMinor}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "proposals.issued", "proposal", &proposal, map[string]any{"version": version, "recipients": len(recipients), "tcv_minor": v.TcvMinor}))
	})
	if err != nil {
		return VersionView{}, err
	}
	return s.Version(ctx, p, proposal, version)
}

// AIDraft suggests text for one section. The suggestion is not saved into the proposal; the
// version is marked as AI-assisted, so it needs a person's approval before it can be issued.
func (s *Service) AIDraft(ctx context.Context, p authctx.Principal, proposal uuid.UUID, version int32, section, notes string) (Section, uuid.UUID, error) {
	view, err := s.Version(ctx, p, proposal, version)
	if err != nil {
		return Section{}, uuid.Nil, err
	}
	if view.Status != "draft" {
		return Section{}, uuid.Nil, apperr.Conflict("not_a_draft", "Only a draft can be edited")
	}
	pr, err := s.Get(ctx, p, proposal)
	if err != nil {
		return Section{}, uuid.Nil, err
	}
	var services []string
	for _, l := range view.Lines {
		services = append(services, l.Name)
	}
	res, err := s.AI.Run(ctx, aigateway.Request{TenantID: p.TenantID, Task: TaskDraft, IdempotencyKey: uuid.NewString(),
		Input: aigateway.Input{Trusted: map[string]string{"section": section, "customer": pr.Customer, "services": strings.Join(services, ", ")}, Untrusted: map[string]string{"notes": notes}}})
	if err != nil {
		return Section{}, uuid.Nil, err
	}
	var out Section
	if err := json.Unmarshal(res.Output, &out); err != nil {
		return Section{}, uuid.Nil, err
	}
	err = s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		if err := q.MarkAIAssisted(ctx, repo.MarkAIAssistedParams{TenantID: p.TenantID, ID: view.ID, AiRequestID: &res.RequestID}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "proposals.ai_draft_requested", "proposal", &proposal, map[string]any{"version": version, "section": section}))
	})
	return out, res.RequestID, err
}

// Activity is what happened to an issued proposal.
type Activity struct {
	Views      []repo.ListViewsRow
	Signatures []repo.ListSignatureEventsRow
	Links      []repo.ListShareLinksRow
}

// ActivityFor returns views, signature events and links.
func (s *Service) ActivityFor(ctx context.Context, p authctx.Principal, proposal uuid.UUID) (Activity, error) {
	var out Activity
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		if _, err := q.GetProposal(ctx, repo.GetProposalParams{TenantID: p.TenantID, ID: proposal}); err != nil {
			return notFound(err)
		}
		var err error
		if out.Views, err = q.ListViews(ctx, repo.ListViewsParams{TenantID: p.TenantID, ProposalID: proposal}); err != nil {
			return err
		}
		if out.Signatures, err = q.ListSignatureEvents(ctx, repo.ListSignatureEventsParams{TenantID: p.TenantID, ProposalID: proposal}); err != nil {
			return err
		}
		out.Links, err = q.ListShareLinks(ctx, repo.ListShareLinksParams{TenantID: p.TenantID, ProposalID: proposal})
		return err
	})
	return out, err
}

// RevokeLink stops a share link working.
func (s *Service) RevokeLink(ctx context.Context, p authctx.Principal, id uuid.UUID) error {
	return s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		n, err := q.RevokeShareLink(ctx, repo.RevokeShareLinkParams{TenantID: p.TenantID, ID: id})
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.NotFound()
		}
		return audit.Record(ctx, tx, userAudit(p, "proposals.link_revoked", "share_link", &id, nil))
	})
}
