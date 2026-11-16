// Package httpapi serves Outreach's contract. The route guard has already authenticated, checked
// the entitlement and permission, and validated the request.
package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/httpx"
	"mspengine/internal/modules/outreach/app"
	"mspengine/internal/modules/outreach/domain"
	"mspengine/internal/modules/outreach/repo"
)

// Handlers implements the generated server interface.
type Handlers struct{ S *app.Service }

var _ ServerInterface = Handlers{}

func principal(r *http.Request) authctx.Principal { return authctx.PrincipalFrom(r.Context()) }

func fail(w http.ResponseWriter, r *http.Request, err error) { httpx.Error(w, r, err) }

func ok(w http.ResponseWriter, status int, v any) { httpx.JSON(w, status, v) }

func done(w http.ResponseWriter, r *http.Request, err error) {
	if err != nil {
		fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func i32(p *int) *int32 {
	if p == nil {
		return nil
	}
	v := int32(*p) //nolint:gosec // Bounded by the contract.
	return &v
}

func domainOut(d repo.GetDomainRow) Domain {
	return Domain{Id: d.ID, Domain: d.Domain, SpfStatus: d.SpfStatus, DkimStatus: d.DkimStatus, DmarcStatus: d.DmarcStatus,
		Authenticated: d.SpfStatus == "pass" && d.DkimStatus == "pass" && (d.DmarcStatus == "pass" || d.DmarcStatus == "none_policy"), LastCheckedAt: d.LastCheckedAt, CreatedAt: d.CreatedAt}
}

// ListSendingDomains handles GET /outreach/domains.
func (h Handlers) ListSendingDomains(w http.ResponseWriter, r *http.Request) {
	rows, err := h.S.Domains(r.Context(), principal(r))
	if err != nil {
		fail(w, r, err)
		return
	}
	out := DomainList{Data: []Domain{}}
	for _, d := range rows {
		out.Data = append(out.Data, domainOut(repo.GetDomainRow(d)))
	}
	ok(w, 200, out)
}

// AddSendingDomain handles POST /outreach/domains.
func (h Handlers) AddSendingDomain(w http.ResponseWriter, r *http.Request) {
	var b DomainInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	d, err := h.S.AddDomain(r.Context(), principal(r), b.Domain)
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, 201, domainOut(d))
}

// CheckSendingDomain handles POST /outreach/domains/{id}/check.
func (h Handlers) CheckSendingDomain(w http.ResponseWriter, r *http.Request, id Id) {
	d, err := h.S.CheckDomain(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, 200, domainOut(d))
}

func senderOut(s repo.GetSenderRow) Sender {
	auth := s.SpfStatus == "pass" && s.DkimStatus == "pass" && (s.DmarcStatus == "pass" || s.DmarcStatus == "none_policy")
	return Sender{Id: s.ID, SendingDomainId: s.SendingDomainID, FromEmail: s.FromEmail, FromName: s.FromName, Status: s.Status,
		DailyLimit: int(s.DailyLimit), ReputationState: s.ReputationState, DomainAuthenticated: &auth, CreatedAt: s.CreatedAt}
}

// ListSenders handles GET /outreach/senders.
func (h Handlers) ListSenders(w http.ResponseWriter, r *http.Request) {
	rows, err := h.S.Senders(r.Context(), principal(r))
	if err != nil {
		fail(w, r, err)
		return
	}
	out := SenderList{Data: []Sender{}}
	for _, s := range rows {
		out.Data = append(out.Data, Sender{Id: s.ID, SendingDomainId: s.SendingDomainID, FromEmail: s.FromEmail, FromName: s.FromName, Status: s.Status,
			DailyLimit: int(s.DailyLimit), ReputationState: s.ReputationState, CreatedAt: s.CreatedAt})
	}
	ok(w, 200, out)
}

// AddSender handles POST /outreach/senders.
func (h Handlers) AddSender(w http.ResponseWriter, r *http.Request) {
	var b SenderInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	var limit int32
	if b.DailyLimit != nil {
		limit = *i32(b.DailyLimit)
	}
	s, err := h.S.AddSender(r.Context(), principal(r), b.SendingDomainId, string(b.FromEmail), b.FromName, limit)
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, 201, senderOut(s))
}

// UpdateSender handles PATCH /outreach/senders/{id}.
func (h Handlers) UpdateSender(w http.ResponseWriter, r *http.Request, id Id) {
	var b SenderPatch
	if !httpx.Decode(w, r, &b) {
		return
	}
	var status *string
	if b.Status != nil {
		s := string(*b.Status)
		status = &s
	}
	s, err := h.S.UpdateSender(r.Context(), principal(r), id, b.FromName, status, i32(b.DailyLimit))
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, 200, senderOut(s))
}

// DeleteSender handles DELETE /outreach/senders/{id}.
func (h Handlers) DeleteSender(w http.ResponseWriter, r *http.Request, id Id) {
	done(w, r, h.S.DeleteSender(r.Context(), principal(r), id))
}

// ListOutreachTemplates handles GET /outreach/templates.
func (h Handlers) ListOutreachTemplates(w http.ResponseWriter, r *http.Request) {
	rows, err := h.S.Templates(r.Context(), principal(r))
	if err != nil {
		fail(w, r, err)
		return
	}
	out := TemplateList{Data: []Template{}}
	for _, t := range rows {
		var v *int
		if t.Version != nil {
			n := int(*t.Version)
			v = &n
		}
		out.Data = append(out.Data, Template{Id: t.ID, Name: t.Name, CurrentVersionId: t.CurrentVersionID, Version: v, Subject: t.Subject, Body: t.Body, AiGenerated: t.AiGenerated, UpdatedAt: t.UpdatedAt})
	}
	ok(w, 200, out)
}

// CreateOutreachTemplate handles POST /outreach/templates.
func (h Handlers) CreateOutreachTemplate(w http.ResponseWriter, r *http.Request) {
	var b TemplateInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	id, err := h.S.CreateTemplate(r.Context(), principal(r), b.Name, b.Subject, b.Body, b.AiRequestId)
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, 201, Ref{Id: id})
}

// DraftOutreachTemplate handles POST /outreach/templates/draft.
func (h Handlers) DraftOutreachTemplate(w http.ResponseWriter, r *http.Request) {
	var b DraftInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	audience := ""
	if b.Audience != nil {
		audience = *b.Audience
	}
	subject, body, req, err := h.S.Draft(r.Context(), principal(r), b.Purpose, audience)
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, 200, Draft{Subject: subject, Body: body, AiRequestId: req, AiGenerated: true})
}

// AddOutreachTemplateVersion handles POST /outreach/templates/{id}/versions.
func (h Handlers) AddOutreachTemplateVersion(w http.ResponseWriter, r *http.Request, id Id) {
	var b VersionInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	done(w, r, h.S.AddTemplateVersion(r.Context(), principal(r), id, b.Subject, b.Body))
}

// ListAudiences handles GET /outreach/audiences.
func (h Handlers) ListAudiences(w http.ResponseWriter, r *http.Request) {
	rows, err := h.S.Audiences(r.Context(), principal(r))
	if err != nil {
		fail(w, r, err)
		return
	}
	out := AudienceList{Data: []Audience{}}
	for _, a := range rows {
		out.Data = append(out.Data, Audience{Id: a.ID, Name: a.Name, Kind: a.Kind, Members: int(a.Members), CreatedAt: a.CreatedAt})
	}
	ok(w, 200, out)
}

// CreateAudience handles POST /outreach/audiences.
func (h Handlers) CreateAudience(w http.ResponseWriter, r *http.Request) {
	var b AudienceInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	id, err := h.S.CreateAudience(r.Context(), principal(r), b.Name)
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, 201, Ref{Id: id})
}

// AddAudienceMembers handles POST /outreach/audiences/{id}/members.
func (h Handlers) AddAudienceMembers(w http.ResponseWriter, r *http.Request, id Id) {
	var b MembersInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	var ids []uuid.UUID
	if b.ContactIds != nil {
		ids = *b.ContactIds
	}
	n, err := h.S.AddMembers(r.Context(), principal(r), id, ids, b.ListId)
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, 200, Count{Count: n})
}

func window(raw []byte) Window {
	var d domain.Window
	_ = json.Unmarshal(raw, &d)
	if d.Days == nil {
		d.Days = []int{}
	}
	return Window{Days: d.Days, Start: d.Start, End: d.End}
}

func toDomainWindow(w *Window) *domain.Window {
	if w == nil {
		return nil
	}
	return &domain.Window{Days: w.Days, Start: w.Start, End: w.End}
}

// ListCampaigns handles GET /outreach/campaigns.
func (h Handlers) ListCampaigns(w http.ResponseWriter, r *http.Request) {
	rows, err := h.S.Campaigns(r.Context(), principal(r))
	if err != nil {
		fail(w, r, err)
		return
	}
	out := CampaignList{Data: []Campaign{}}
	for _, c := range rows {
		out.Data = append(out.Data, Campaign{Id: c.ID, Name: c.Name, SenderAccountId: c.SenderAccountID, AudienceId: c.AudienceID, Status: CampaignStatus(c.Status),
			Timezone: c.Timezone, SendingWindow: window(c.SendingWindow), Purpose: c.Purpose, PostalAddress: c.PostalAddress, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt})
	}
	ok(w, 200, out)
}

// CreateCampaign handles POST /outreach/campaigns.
func (h Handlers) CreateCampaign(w http.ResponseWriter, r *http.Request) {
	var b CampaignInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	id, err := h.S.CreateCampaign(r.Context(), principal(r), app.CampaignInput{Name: &b.Name, SenderID: &b.SenderAccountId, AudienceID: b.AudienceId,
		Timezone: b.Timezone, Window: toDomainWindow(b.SendingWindow), Purpose: b.Purpose, PostalAddress: &b.PostalAddress})
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, 201, Ref{Id: id})
}

// GetCampaign handles GET /outreach/campaigns/{id}.
func (h Handlers) GetCampaign(w http.ResponseWriter, r *http.Request, id Id) {
	v, err := h.S.Campaign(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	out := CampaignDetail{Id: v.ID, Name: v.Name, SenderAccountId: v.SenderAccountID, AudienceId: v.AudienceID, Status: CampaignDetailStatus(v.Status),
		Timezone: v.Timezone, SendingWindow: window(v.SendingWindow), Purpose: v.Purpose, PostalAddress: v.PostalAddress, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt,
		Steps: []Step{}, Enrollments: []StepCount{}, ContentHash: v.ContentHash, Approved: v.Approved,
		Totals: Totals{Sent: int(v.Totals.Sent), Delivered: int(v.Totals.Delivered), Bounced: int(v.Totals.Bounced), Replied: int(v.Totals.Replied), Unsubscribed: int(v.Totals.Unsubscribed)}}
	for _, s := range v.Steps {
		out.Steps = append(out.Steps, Step{Id: s.ID, Position: int(s.Position), DelayMinutes: int(s.DelayMinutes), TemplateVersionId: s.TemplateVersionID, Subject: s.Subject, Body: s.Body, AiGenerated: s.AiGenerated})
	}
	for _, c := range v.Counts {
		out.Enrollments = append(out.Enrollments, StepCount{Status: c.Status, StopReason: c.StopReason, Count: int(c.N)})
	}
	ok(w, 200, out)
}

// UpdateCampaign handles PATCH /outreach/campaigns/{id}.
func (h Handlers) UpdateCampaign(w http.ResponseWriter, r *http.Request, id Id) {
	var b CampaignPatch
	if !httpx.Decode(w, r, &b) {
		return
	}
	done(w, r, h.S.UpdateCampaign(r.Context(), principal(r), id, app.CampaignInput{Name: b.Name, AudienceID: b.AudienceId, Timezone: b.Timezone,
		Window: toDomainWindow(b.SendingWindow), Purpose: b.Purpose, PostalAddress: b.PostalAddress}))
}

// SetCampaignSteps handles PUT /outreach/campaigns/{id}/steps.
func (h Handlers) SetCampaignSteps(w http.ResponseWriter, r *http.Request, id Id) {
	var b StepsInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	steps := make([]app.StepInput, len(b.Steps))
	for i, s := range b.Steps {
		steps[i] = app.StepInput{TemplateVersionID: s.TemplateVersionId, DelayMinutes: int32(s.DelayMinutes)} //nolint:gosec // Bounded by the contract.
	}
	done(w, r, h.S.SetSteps(r.Context(), principal(r), id, steps))
}

// PreflightCampaign handles GET /outreach/campaigns/{id}/preflight.
func (h Handlers) PreflightCampaign(w http.ResponseWriter, r *http.Request, id Id) {
	p, err := h.S.Preflight(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	out := Preflight{Blockers: nonNil(p.Blockers), Warnings: nonNil(p.Warnings), ContentHash: p.ContentHash}
	out.Audience.Total, out.Audience.NoEmail, out.Audience.Suppressed, out.Audience.Blocked, out.Audience.Sendable =
		p.Audience.Total, p.Audience.NoEmail, p.Audience.Suppressed, p.Audience.Blocked, p.Audience.Sendable
	ok(w, 200, out)
}

func nonNil(xs []string) []string {
	if xs == nil {
		return []string{}
	}
	return xs
}

// SubmitCampaign handles POST /outreach/campaigns/{id}/submit.
func (h Handlers) SubmitCampaign(w http.ResponseWriter, r *http.Request, id Id) {
	hash, err := h.S.Submit(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, 200, HashOut{ContentHash: hash})
}

// ApproveCampaign handles POST /outreach/campaigns/{id}/approve.
func (h Handlers) ApproveCampaign(w http.ResponseWriter, r *http.Request, id Id) {
	var b ApproveInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	done(w, r, h.S.Decide(r.Context(), principal(r), id, true, b.ContentHash, nil))
}

// RejectCampaign handles POST /outreach/campaigns/{id}/reject.
func (h Handlers) RejectCampaign(w http.ResponseWriter, r *http.Request, id Id) {
	var b RejectInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	done(w, r, h.S.Decide(r.Context(), principal(r), id, false, "", &b.Reason))
}

// StartCampaign handles POST /outreach/campaigns/{id}/start.
func (h Handlers) StartCampaign(w http.ResponseWriter, r *http.Request, id Id) {
	n, err := h.S.Start(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, 200, Count{Count: n})
}

// PauseCampaign handles POST /outreach/campaigns/{id}/pause.
func (h Handlers) PauseCampaign(w http.ResponseWriter, r *http.Request, id Id) {
	done(w, r, h.S.SetRunState(r.Context(), principal(r), id, "pause"))
}

// ResumeCampaign handles POST /outreach/campaigns/{id}/resume.
func (h Handlers) ResumeCampaign(w http.ResponseWriter, r *http.Request, id Id) {
	done(w, r, h.S.SetRunState(r.Context(), principal(r), id, "resume"))
}

// CancelCampaign handles POST /outreach/campaigns/{id}/cancel.
func (h Handlers) CancelCampaign(w http.ResponseWriter, r *http.Request, id Id) {
	done(w, r, h.S.SetRunState(r.Context(), principal(r), id, "cancel"))
}

// ListOutreachApprovals handles GET /outreach/approvals.
func (h Handlers) ListOutreachApprovals(w http.ResponseWriter, r *http.Request) {
	rows, err := h.S.Approvals(r.Context(), principal(r))
	if err != nil {
		fail(w, r, err)
		return
	}
	out := ApprovalList{Data: []Approval{}}
	for _, a := range rows {
		out.Data = append(out.Data, Approval{Id: a.ID, CampaignId: a.SubjectID, CampaignName: a.Name, RequestedBy: a.RequestedBy, CreatedAt: a.CreatedAt})
	}
	ok(w, 200, out)
}

// ListOutreachMessages handles GET /outreach/messages.
func (h Handlers) ListOutreachMessages(w http.ResponseWriter, r *http.Request, params ListOutreachMessagesParams) {
	rows, err := h.S.Messages(r.Context(), principal(r), params.CampaignId)
	if err != nil {
		fail(w, r, err)
		return
	}
	out := MessageList{Data: []Message{}}
	for _, m := range rows {
		out.Data = append(out.Data, Message{Id: m.ID, Subject: m.Subject, Status: m.Status, SentAt: m.SentAt, ContactId: m.ContactID, CampaignId: m.CampaignID, CreatedAt: m.CreatedAt})
	}
	ok(w, 200, out)
}

// GetOutreachMessage handles GET /outreach/messages/{id}.
func (h Handlers) GetOutreachMessage(w http.ResponseWriter, r *http.Request, id Id) {
	m, err := h.S.Message(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, 200, Message{Id: m.ID, Subject: m.Subject, Body: m.Body, Status: m.Status, SentAt: m.SentAt, ContactId: m.ContactID, CampaignId: m.CampaignID, CreatedAt: m.CreatedAt})
}

// ListReplies handles GET /outreach/replies.
func (h Handlers) ListReplies(w http.ResponseWriter, r *http.Request) {
	rows, err := h.S.Replies(r.Context(), principal(r))
	if err != nil {
		fail(w, r, err)
		return
	}
	out := ReplyList{Data: []Reply{}}
	for _, x := range rows {
		out.Data = append(out.Data, Reply{Id: x.ID, MessageId: x.MessageID, Classification: x.Classification, Snippet: x.Snippet, ReceivedAt: x.ReceivedAt, ContactId: x.ContactID, CampaignId: x.CampaignID})
	}
	ok(w, 200, out)
}

// ClassifyReply handles POST /outreach/replies/{id}/classify.
func (h Handlers) ClassifyReply(w http.ResponseWriter, r *http.Request, id Id) {
	var b ClassifyInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	done(w, r, h.S.ClassifyReply(r.Context(), principal(r), id, string(b.Classification)))
}

// ListSuppressions handles GET /outreach/suppressions.
func (h Handlers) ListSuppressions(w http.ResponseWriter, r *http.Request) {
	rows, err := h.S.Suppressions(r.Context(), principal(r))
	if err != nil {
		fail(w, r, err)
		return
	}
	out := SuppressionList{Data: []Suppression{}}
	for _, s := range rows {
		out.Data = append(out.Data, Suppression{Id: s.ID, Reason: s.Reason, Source: s.Source, CreatedAt: s.CreatedAt})
	}
	ok(w, 200, out)
}

// AddSuppression handles POST /outreach/suppressions.
func (h Handlers) AddSuppression(w http.ResponseWriter, r *http.Request) {
	var b SuppressInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	done(w, r, h.S.Suppress(r.Context(), principal(r), string(b.Email)))
}

// RemoveSuppression handles DELETE /outreach/suppressions/{id}.
func (h Handlers) RemoveSuppression(w http.ResponseWriter, r *http.Request, id Id) {
	done(w, r, h.S.Unsuppress(r.Context(), principal(r), id))
}

// GetUnsubscribe handles GET /public/outreach/unsubscribe/{token}. It never unsubscribes, so link
// scanners that follow links cannot opt someone out.
func (h Handlers) GetUnsubscribe(w http.ResponseWriter, r *http.Request, token Token) {
	s, err := h.S.UnsubscribeStatus(r.Context(), token)
	if err != nil {
		fail(w, r, err)
		return
	}
	w.Header().Set("Referrer-Policy", "no-referrer")
	ok(w, 200, UnsubscribeStatus{Sender: s.Sender, Done: s.Done})
}

// Unsubscribe handles POST /public/outreach/unsubscribe/{token}: the page's button and the
// one-click unsubscribe of RFC 8058, whose form body carries nothing we need.
func (h Handlers) Unsubscribe(w http.ResponseWriter, r *http.Request, token Token) {
	done(w, r, h.S.Unsubscribe(r.Context(), token))
}
