// Package httpapi serves Proposals' contract. The route guard has already authenticated, checked the
// entitlement and permission, and validated the request. Public routes need only a share token.
package httpapi

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/httpx"
	crm "mspengine/internal/modules/crm/contract"
	"mspengine/internal/modules/proposals/app"
	"mspengine/internal/modules/proposals/domain"
	"mspengine/internal/modules/proposals/repo"
)

// Handlers implements the generated server interface.
type Handlers struct{ S *app.Service }

var _ ServerInterface = Handlers{}

func principal(r *http.Request) authctx.Principal { return authctx.PrincipalFrom(r.Context()) }

func fail(w http.ResponseWriter, r *http.Request, err error) { httpx.Error(w, r, err) }

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}

func ok(w http.ResponseWriter, v any)      { httpx.JSON(w, http.StatusOK, v) }
func created(w http.ResponseWriter, v any) { httpx.JSON(w, http.StatusCreated, v) }

func hash(s string) ([]byte, error) {
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil, apperr.Validation(apperr.FieldError{Field: "content_hash", Code: "invalid"})
	}
	return b, nil
}

func v32(v VersionNumber) int32 { return int32(v) } //nolint:gosec // The contract bounds this.

// Catalog and templates.

func catalogOut(c repo.GetCatalogItemRow) CatalogItem {
	return CatalogItem{Id: c.ID, Name: c.Name, Description: c.Description, PricingModel: PricingModel(c.PricingModel), Currency: c.Currency, UnitPriceMinor: c.UnitPriceMinor, SetupFeeMinor: c.SetupFeeMinor, Status: CatalogItemStatus(c.Status)}
}

// ListCatalog handles GET /proposals/catalog.
func (h Handlers) ListCatalog(w http.ResponseWriter, r *http.Request) {
	rows, err := h.S.Catalog(r.Context(), principal(r))
	if err != nil {
		fail(w, r, err)
		return
	}
	out := CatalogList{Data: []CatalogItem{}}
	for _, c := range rows {
		out.Data = append(out.Data, catalogOut(repo.GetCatalogItemRow(c)))
	}
	ok(w, out)
}

// CreateCatalogItem handles POST /proposals/catalog.
func (h Handlers) CreateCatalogItem(w http.ResponseWriter, r *http.Request) {
	var b CatalogInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	c, err := h.S.AddCatalogItem(r.Context(), principal(r), app.CatalogInput{Name: b.Name, Description: b.Description, PricingModel: string(b.PricingModel), Currency: b.Currency, UnitPrice: b.UnitPriceMinor, SetupFee: deref(b.SetupFeeMinor)})
	if err != nil {
		fail(w, r, err)
		return
	}
	created(w, catalogOut(c))
}

// UpdateCatalogItem handles PATCH /proposals/catalog/{id}.
func (h Handlers) UpdateCatalogItem(w http.ResponseWriter, r *http.Request, id Id) {
	var b CatalogPatch
	if !httpx.Decode(w, r, &b) {
		return
	}
	var status *string
	if b.Status != nil {
		s := string(*b.Status)
		status = &s
	}
	c, err := h.S.UpdateCatalogItem(r.Context(), principal(r), id, app.CatalogPatch{Name: b.Name, Description: b.Description, Status: status, UnitPrice: b.UnitPriceMinor, SetupFee: b.SetupFeeMinor})
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, catalogOut(c))
}

// ListTextTemplates handles GET /proposals/text-templates.
func (h Handlers) ListTextTemplates(w http.ResponseWriter, r *http.Request) {
	rows, err := h.S.TextTemplates(r.Context(), principal(r))
	if err != nil {
		fail(w, r, err)
		return
	}
	out := TextTemplateList{Data: []TextTemplate{}}
	for _, t := range rows {
		out.Data = append(out.Data, TextTemplate{Id: t.ID, Kind: TextTemplateKind(t.Kind), Name: t.Name, Body: t.Body, Version: int(t.Version)})
	}
	ok(w, out)
}

// CreateTextTemplate handles POST /proposals/text-templates.
func (h Handlers) CreateTextTemplate(w http.ResponseWriter, r *http.Request) {
	var b TextTemplateInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	id, err := h.S.AddTextTemplate(r.Context(), principal(r), string(b.Kind), b.Name, b.Body)
	if err != nil {
		fail(w, r, err)
		return
	}
	created(w, Ref{Id: id})
}

// UpdateTextTemplate handles PATCH /proposals/text-templates/{kind}/{id}.
func (h Handlers) UpdateTextTemplate(w http.ResponseWriter, r *http.Request, kind UpdateTextTemplateParamsKind, id Id) {
	var b TextTemplatePatch
	if !httpx.Decode(w, r, &b) {
		return
	}
	if err := h.S.UpdateTextTemplate(r.Context(), principal(r), string(kind), id, b.Name, b.Body); err != nil {
		fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func sections(in []Section) []app.Section {
	out := make([]app.Section, len(in))
	for i, s := range in {
		out[i] = app.Section{Heading: s.Heading, Body: s.Body}
	}
	return out
}

func sectionsOut(in []app.Section) []Section {
	out := make([]Section, len(in))
	for i, s := range in {
		out[i] = Section{Heading: s.Heading, Body: s.Body}
	}
	return out
}

// ListTemplates handles GET /proposals/templates.
func (h Handlers) ListTemplates(w http.ResponseWriter, r *http.Request) {
	rows, err := h.S.Templates(r.Context(), principal(r))
	if err != nil {
		fail(w, r, err)
		return
	}
	out := TemplateList{Data: []Template{}}
	for _, t := range rows {
		var secs []app.Section
		_ = json.Unmarshal(t.Sections, &secs)
		out.Data = append(out.Data, Template{Id: t.ID, Name: t.Name, Sections: sectionsOut(secs), DefaultSlaId: t.DefaultSlaID, DefaultTermsId: t.DefaultTermsID})
	}
	ok(w, out)
}

// CreateTemplate handles POST /proposals/templates.
func (h Handlers) CreateTemplate(w http.ResponseWriter, r *http.Request) {
	var b TemplateInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	id, err := h.S.AddTemplate(r.Context(), principal(r), b.Name, sections(b.Sections), b.DefaultSlaId, b.DefaultTermsId)
	if err != nil {
		fail(w, r, err)
		return
	}
	created(w, Ref{Id: id})
}

// UpdateTemplate handles PATCH /proposals/templates/{id}.
func (h Handlers) UpdateTemplate(w http.ResponseWriter, r *http.Request, id Id) {
	var b TemplatePatch
	keys, good := httpx.DecodePatch(w, r, &b)
	if !good {
		return
	}
	var secs []app.Section
	if b.Sections != nil {
		secs = sections(*b.Sections)
	}
	if err := h.S.UpdateTemplate(r.Context(), principal(r), id, b.Name, secs, keys["default_sla_id"], b.DefaultSlaId, keys["default_terms_id"], b.DefaultTermsId); err != nil {
		fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Proposals.

func (h Handlers) proposal(w http.ResponseWriter, r *http.Request, id uuid.UUID, status int) {
	p, err := h.S.Get(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	out := Proposal{Id: p.ID, Number: p.Number, Title: p.Title, CrmCompanyId: p.CrmCompanyID, Customer: &p.Customer, DealId: p.DealID, Status: ProposalStatus(p.Status), Versions: []VersionSummary{}, UpdatedAt: p.UpdatedAt}
	for _, v := range p.Versions {
		vs := VersionSummary{Version: int(v.Version), Status: v.Status, Currency: v.Currency, MrrTotalMinor: v.MrrTotalMinor, SetupTotalMinor: v.SetupTotalMinor, TcvMinor: v.TcvMinor, TermMonths: int(v.TermMonths), IssuedAt: v.IssuedAt, CreatedAt: v.CreatedAt}
		if v.ValidUntil != nil {
			vs.ValidUntil = &openapi_types.Date{Time: *v.ValidUntil}
		}
		if p.CurrentVersionID != nil && *p.CurrentVersionID == v.ID {
			n := int(v.Version)
			out.CurrentVersion = &n
		}
		out.Versions = append(out.Versions, vs)
	}
	httpx.JSON(w, status, out)
}

// ListProposals handles GET /proposals/proposals.
func (h Handlers) ListProposals(w http.ResponseWriter, r *http.Request, params ListProposalsParams) {
	var status *string
	if params.Status != nil {
		s := string(*params.Status)
		status = &s
	}
	rows, err := h.S.List(r.Context(), principal(r), status, params.CrmCompanyId)
	if err != nil {
		fail(w, r, err)
		return
	}
	out := ProposalList{Data: []ProposalListItem{}}
	for _, p := range rows {
		x := ProposalListItem{Id: p.ID, Number: p.Number, Title: p.Title, CrmCompanyId: p.CrmCompanyID, DealId: p.DealID, Status: ProposalStatus(p.Status), Currency: p.Currency, MrrTotalMinor: p.MrrTotalMinor, TcvMinor: p.TcvMinor, UpdatedAt: p.UpdatedAt}
		if p.Version != nil {
			n := int(*p.Version)
			x.Version = &n
		}
		out.Data = append(out.Data, x)
	}
	ok(w, out)
}

// CreateProposal handles POST /proposals/proposals.
func (h Handlers) CreateProposal(w http.ResponseWriter, r *http.Request) {
	var b CreateProposal
	if !httpx.Decode(w, r, &b) {
		return
	}
	in := app.CreateInput{Title: b.Title, CRMCompanyID: b.CrmCompanyId, DealID: b.DealId, TemplateID: b.TemplateId, Currency: deref(b.Currency)}
	if b.NewCompany != nil {
		in.NewCompany = &crm.CompanyInput{Name: b.NewCompany.Name, Domain: b.NewCompany.Domain}
	}
	id, err := h.S.Create(r.Context(), principal(r), in)
	if err != nil {
		fail(w, r, err)
		return
	}
	h.proposal(w, r, id, http.StatusCreated)
}

// GetProposal handles GET /proposals/proposals/{id}.
func (h Handlers) GetProposal(w http.ResponseWriter, r *http.Request, id Id) {
	h.proposal(w, r, id, http.StatusOK)
}

// DeleteProposal handles DELETE /proposals/proposals/{id}.
func (h Handlers) DeleteProposal(w http.ResponseWriter, r *http.Request, id Id) {
	if err := h.S.Delete(r.Context(), principal(r), id); err != nil {
		fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetProposalActivity handles GET /proposals/proposals/{id}/activity.
func (h Handlers) GetProposalActivity(w http.ResponseWriter, r *http.Request, id Id) {
	a, err := h.S.ActivityFor(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	type view struct {
		ViewedAt       time.Time `json:"viewed_at"`
		RecipientEmail string    `json:"recipient_email"`
	}
	type sig struct {
		EventType  string    `json:"event_type"`
		OccurredAt time.Time `json:"occurred_at"`
	}
	type link struct {
		ID             uuid.UUID  `json:"id"`
		RecipientEmail string     `json:"recipient_email"`
		ExpiresAt      time.Time  `json:"expires_at"`
		RevokedAt      *time.Time `json:"revoked_at,omitempty"`
		Views          int32      `json:"views"`
	}
	out := struct {
		Views      []view `json:"views"`
		Signatures []sig  `json:"signatures"`
		Links      []link `json:"links"`
	}{[]view{}, []sig{}, []link{}}
	for _, v := range a.Views {
		out.Views = append(out.Views, view{v.ViewedAt, v.RecipientEmail})
	}
	for _, s := range a.Signatures {
		out.Signatures = append(out.Signatures, sig{s.EventType, s.OccurredAt})
	}
	for _, l := range a.Links {
		out.Links = append(out.Links, link{l.ID, l.RecipientEmail, l.ExpiresAt, l.RevokedAt, l.Views})
	}
	ok(w, out)
}

func versionOut(v app.VersionView) Version {
	out := Version{Id: v.ID, Version: int(v.Version), Status: VersionStatus(v.Status), Sections: sectionsOut(v.Content.Sections), SlaId: v.Content.SLAID, TermsId: v.Content.TermsID,
		Currency: v.Currency, TermMonths: int(v.TermMonths), Lines: []LineTotal{}, SetupTotalMinor: v.SetupTotalMinor, MrrTotalMinor: v.MrrTotalMinor, TcvMinor: v.TcvMinor,
		TaxNote: domain.TaxNote, ContentHash: hex.EncodeToString(v.ContentHash), NeedsApproval: v.NeedsApproval, Approved: v.Approved, AiAssisted: v.AIAssisted, HasPdf: v.PdfFileID != nil, IssuedAt: v.IssuedAt}
	if v.ValidUntil != nil {
		out.ValidUntil = &openapi_types.Date{Time: *v.ValidUntil}
	}
	for _, l := range v.Lines {
		out.Lines = append(out.Lines, LineTotal{CatalogItemId: l.CatalogItemID, Name: l.Name, PricingModel: l.PricingModel, Quantity: int(l.Quantity), UnitPriceMinor: l.UnitPriceMinor,
			SetupFeeMinor: l.SetupFeeMinor, DiscountBps: int(l.DiscountBps), RecurringTotalMinor: l.RecurringTotalMinor})
	}
	return out
}

func editInput(b EditVersion) app.EditInput {
	in := app.EditInput{Content: app.Content{Sections: sections(b.Sections), SLAID: b.SlaId, TermsID: b.TermsId}, TermMonths: int64(b.TermMonths), Currency: b.Currency}
	if b.ValidUntil != nil {
		in.ValidUntil = &b.ValidUntil.Time
	}
	for _, l := range b.Lines {
		line := domain.Line{Name: l.Name, PricingModel: string(l.PricingModel), Quantity: int64(l.Quantity), UnitPriceMinor: l.UnitPriceMinor, SetupFeeMinor: deref(l.SetupFeeMinor), DiscountBps: int64(deref(l.DiscountBps))}
		if l.CatalogItemId != nil {
			s := l.CatalogItemId.String()
			line.CatalogItemID = &s
		}
		in.Lines = append(in.Lines, line)
	}
	return in
}

// CreateVersion handles POST /proposals/proposals/{id}/versions.
func (h Handlers) CreateVersion(w http.ResponseWriter, r *http.Request, id Id) {
	v, err := h.S.NewVersion(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	created(w, versionOut(v))
}

// GetVersion handles GET /proposals/proposals/{id}/versions/{version}.
func (h Handlers) GetVersion(w http.ResponseWriter, r *http.Request, id Id, version VersionNumber) {
	v, err := h.S.Version(r.Context(), principal(r), id, v32(version))
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, versionOut(v))
}

// EditVersion handles PATCH /proposals/proposals/{id}/versions/{version}.
func (h Handlers) EditVersion(w http.ResponseWriter, r *http.Request, id Id, version VersionNumber) {
	var b EditVersion
	if !httpx.Decode(w, r, &b) {
		return
	}
	v, err := h.S.Edit(r.Context(), principal(r), id, v32(version), editInput(b))
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, versionOut(v))
}

// CalculateVersion handles POST /proposals/proposals/{id}/versions/{version}/calculate.
func (h Handlers) CalculateVersion(w http.ResponseWriter, r *http.Request, _ Id, _ VersionNumber) {
	var b EditVersion
	if !httpx.Decode(w, r, &b) {
		return
	}
	t, err := h.S.Calculate(editInput(b))
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, t)
}

// AiDraft handles POST /proposals/proposals/{id}/versions/{version}/ai-draft.
func (h Handlers) AiDraft(w http.ResponseWriter, r *http.Request, id Id, version VersionNumber) {
	var b AIDraftInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	s, _, err := h.S.AIDraft(r.Context(), principal(r), id, v32(version), b.Section, deref(b.Notes))
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, AIDraft{Heading: s.Heading, Body: s.Body, AiGenerated: true})
}

// SubmitVersion handles POST .../submit.
func (h Handlers) SubmitVersion(w http.ResponseWriter, r *http.Request, id Id, version VersionNumber) {
	v, err := h.S.Submit(r.Context(), principal(r), id, v32(version))
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, versionOut(v))
}

func (h Handlers) decide(w http.ResponseWriter, r *http.Request, id Id, version VersionNumber, approve bool) {
	var b Decision
	if !httpx.Decode(w, r, &b) {
		return
	}
	hsh, err := hash(b.ContentHash)
	if err != nil {
		fail(w, r, err)
		return
	}
	v, err := h.S.Decide(r.Context(), principal(r), id, v32(version), approve, hsh, b.Reason)
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, versionOut(v))
}

// ApproveVersion handles POST .../approve.
func (h Handlers) ApproveVersion(w http.ResponseWriter, r *http.Request, id Id, version VersionNumber) {
	h.decide(w, r, id, version, true)
}

// RejectVersion handles POST .../reject.
func (h Handlers) RejectVersion(w http.ResponseWriter, r *http.Request, id Id, version VersionNumber) {
	h.decide(w, r, id, version, false)
}

func recipients(in []Recipient) []app.Recipient {
	out := make([]app.Recipient, len(in))
	for i, x := range in {
		out[i] = app.Recipient{Name: x.Name, Email: string(x.Email)}
	}
	return out
}

// IssueVersion handles POST .../issue.
func (h Handlers) IssueVersion(w http.ResponseWriter, r *http.Request, id Id, version VersionNumber, _ IssueVersionParams) {
	var b IssueInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	hsh, err := hash(b.ContentHash)
	if err != nil {
		fail(w, r, err)
		return
	}
	v, err := h.S.Issue(r.Context(), principal(r), id, v32(version), recipients(b.Recipients), deref(b.Message), hsh)
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, versionOut(v))
}

// RequestSignature handles POST .../signature-requests.
func (h Handlers) RequestSignature(w http.ResponseWriter, r *http.Request, id Id, version VersionNumber, params RequestSignatureParams) {
	var b SignatureInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	s, err := h.S.RequestSignature(r.Context(), principal(r), id, v32(version), recipients(b.Recipients), params.IdempotencyKey)
	if err != nil {
		fail(w, r, err)
		return
	}
	created(w, SignatureRequest{Id: s.ID, Status: s.Status, ProviderKey: s.ProviderKey, EnvelopeId: s.ProviderEnvelopeID})
}

// RevokeShareLink handles POST /proposals/share-links/{id}/revoke.
func (h Handlers) RevokeShareLink(w http.ResponseWriter, r *http.Request, id Id) {
	if err := h.S.RevokeLink(r.Context(), principal(r), id); err != nil {
		fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ViewPublicProposal handles GET /public/proposals/{token}. It shows only the frozen snapshot.
func (h Handlers) ViewPublicProposal(w http.ResponseWriter, r *http.Request, token Token) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	v, err := h.S.Public(r.Context(), token)
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, map[string]any{"title": v.Title, "customer": v.Customer, "version": v.Version, "valid_until": v.Valid, "sections": sectionsOut(v.Sections), "totals": v.Totals,
		"sla": v.SLA, "terms": v.Terms, "superseded": v.Superseded, "expired": v.Expired, "has_pdf": v.HasPDF})
}

// DownloadPublicProposal handles GET /public/proposals/{token}/pdf.
func (h Handlers) DownloadPublicProposal(w http.ResponseWriter, r *http.Request, token Token) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	d, err := h.S.PublicPDF(r.Context(), token)
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.Attachment(w, d.Name, d.ContentType, d.Body)
}
