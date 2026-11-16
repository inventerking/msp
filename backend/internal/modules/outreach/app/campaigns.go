package app

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/modules/outreach/domain"
	"mspengine/internal/modules/outreach/repo"
	"mspengine/internal/platform/audit"
)

// CampaignInput is a new campaign, or the fields to change.
type CampaignInput struct {
	Name          *string
	SenderID      *uuid.UUID
	AudienceID    *uuid.UUID
	Timezone      *string
	Window        *domain.Window
	Purpose       *string
	PostalAddress *string
}

func validateCampaign(in CampaignInput) error {
	var errs []apperr.FieldError
	if in.Timezone != nil {
		if _, err := time.LoadLocation(*in.Timezone); err != nil || *in.Timezone == "" {
			errs = append(errs, apperr.FieldError{Field: "timezone", Code: "invalid_timezone"})
		}
	}
	if in.Window != nil {
		if err := in.Window.Validate(); err != nil {
			errs = append(errs, apperr.FieldError{Field: "sending_window", Code: "invalid_window"})
		}
	}
	if in.PostalAddress != nil && strings.TrimSpace(*in.PostalAddress) == "" {
		errs = append(errs, apperr.FieldError{Field: "postal_address", Code: "required"})
	}
	if in.Name != nil && strings.TrimSpace(*in.Name) == "" {
		errs = append(errs, apperr.FieldError{Field: "name", Code: "required"})
	}
	if len(errs) > 0 {
		return apperr.Validation(errs...)
	}
	return nil
}

// CreateCampaign adds a draft campaign.
func (s *Service) CreateCampaign(ctx context.Context, p authctx.Principal, in CampaignInput) (uuid.UUID, error) {
	if in.Name == nil || in.SenderID == nil || in.PostalAddress == nil {
		return uuid.Nil, apperr.Validation(apperr.FieldError{Field: "name", Code: "required"})
	}
	if in.Timezone == nil {
		in.Timezone = ptr("UTC")
	}
	if in.Window == nil {
		in.Window = &domain.Window{Days: []int{1, 2, 3, 4, 5}, Start: "09:00", End: "17:00"}
	}
	if in.Purpose == nil {
		in.Purpose = ptr("")
	}
	if err := validateCampaign(in); err != nil {
		return uuid.Nil, err
	}
	w, _ := json.Marshal(in.Window)
	var id uuid.UUID
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		if _, err := q.GetSender(ctx, repo.GetSenderParams{TenantID: p.TenantID, ID: *in.SenderID}); err != nil {
			return apperr.Validation(apperr.FieldError{Field: "sender_account_id", Code: "unknown_sender"})
		}
		if in.AudienceID != nil {
			if _, err := q.GetAudience(ctx, repo.GetAudienceParams{TenantID: p.TenantID, ID: *in.AudienceID}); err != nil {
				return apperr.Validation(apperr.FieldError{Field: "audience_id", Code: "unknown_audience"})
			}
		}
		var err error
		id, err = q.InsertCampaign(ctx, repo.InsertCampaignParams{TenantID: p.TenantID, Name: *in.Name, SenderAccountID: *in.SenderID, AudienceID: in.AudienceID,
			Timezone: *in.Timezone, SendingWindow: w, Purpose: *in.Purpose, PostalAddress: *in.PostalAddress, CreatedBy: uid(p)})
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "outreach.campaign_created", "campaign", &id, map[string]any{"name": *in.Name}))
	})
	return id, err
}

// Campaigns lists campaigns.
func (s *Service) Campaigns(ctx context.Context, p authctx.Principal) ([]repo.ListCampaignsRow, error) {
	var out []repo.ListCampaignsRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.ListCampaigns(ctx, p.TenantID)
		return err
	})
	return out, err
}

// CampaignView is a campaign with its steps, progress and approval state.
type CampaignView struct {
	repo.GetCampaignRow
	Steps       []repo.ListStepsRow
	Counts      []repo.EnrollmentCountsRow
	Totals      repo.CampaignTotalsRow
	ContentHash string
	// Approved is true when the approved content still matches the campaign.
	Approved bool
}

// Campaign returns one campaign.
func (s *Service) Campaign(ctx context.Context, p authctx.Principal, id uuid.UUID) (CampaignView, error) {
	var v CampaignView
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		c, err := q.GetCampaign(ctx, repo.GetCampaignParams{TenantID: p.TenantID, ID: id})
		if err != nil {
			return notFound(err)
		}
		v.GetCampaignRow = c
		h, steps, err := currentHash(ctx, q, p.TenantID, c)
		if err != nil {
			return err
		}
		v.Steps, v.ContentHash = steps, hex.EncodeToString(h)
		if a, err := q.ApprovedCampaign(ctx, repo.ApprovedCampaignParams{TenantID: p.TenantID, SubjectID: id}); err == nil {
			v.Approved = bytes.Equal(a.ContentHash, h)
		}
		if v.Counts, err = q.EnrollmentCounts(ctx, repo.EnrollmentCountsParams{TenantID: p.TenantID, CampaignID: id}); err != nil {
			return err
		}
		v.Totals, err = q.CampaignTotals(ctx, repo.CampaignTotalsParams{TenantID: p.TenantID, CampaignID: id})
		return err
	})
	return v, err
}

// currentHash hashes exactly what an approver sees: sender, purpose, postal address and every step.
func currentHash(ctx context.Context, q *repo.Queries, tenant uuid.UUID, c repo.GetCampaignRow) ([]byte, []repo.ListStepsRow, error) {
	steps, err := q.ListSteps(ctx, repo.ListStepsParams{TenantID: tenant, CampaignID: c.ID})
	if err != nil {
		return nil, nil, err
	}
	sender, err := q.GetSender(ctx, repo.GetSenderParams{TenantID: tenant, ID: c.SenderAccountID})
	if err != nil {
		return nil, nil, err
	}
	a := domain.Approvable{Sender: sender.FromName + " <" + sender.FromEmail + ">", Purpose: c.Purpose, Postal: c.PostalAddress}
	for _, st := range steps {
		a.Steps = append(a.Steps, domain.Step{Position: int(st.Position), DelayMinutes: int(st.DelayMinutes), Subject: st.Subject, Body: st.Body, AIGenerated: st.AiGenerated})
	}
	return domain.Hash(a), steps, nil
}

// UpdateCampaign changes a campaign that has not started. Any change returns it to draft, so it
// must be approved again.
func (s *Service) UpdateCampaign(ctx context.Context, p authctx.Principal, id uuid.UUID, in CampaignInput) error {
	if err := validateCampaign(in); err != nil {
		return err
	}
	var w []byte
	if in.Window != nil {
		w, _ = json.Marshal(in.Window)
	}
	return s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		if in.AudienceID != nil {
			if _, err := q.GetAudience(ctx, repo.GetAudienceParams{TenantID: p.TenantID, ID: *in.AudienceID}); err != nil {
				return apperr.Validation(apperr.FieldError{Field: "audience_id", Code: "unknown_audience"})
			}
		}
		n, err := q.UpdateCampaign(ctx, repo.UpdateCampaignParams{TenantID: p.TenantID, ID: id, Name: in.Name, AudienceID: in.AudienceID, Timezone: in.Timezone,
			SendingWindow: w, Purpose: in.Purpose, PostalAddress: in.PostalAddress})
		if err != nil {
			return err
		}
		if n == 0 {
			return s.notEditable(ctx, q, p, id)
		}
		if err := q.ExpireApprovals(ctx, repo.ExpireApprovalsParams{TenantID: p.TenantID, SubjectID: id}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "outreach.campaign_updated", "campaign", &id, nil))
	})
}

func (s *Service) notEditable(ctx context.Context, q *repo.Queries, p authctx.Principal, id uuid.UUID) error {
	if _, err := q.GetCampaign(ctx, repo.GetCampaignParams{TenantID: p.TenantID, ID: id}); err != nil {
		return notFound(err)
	}
	return apperr.Conflict("campaign_started", "A campaign that has started cannot be changed")
}

// StepInput is one step of a sequence.
type StepInput struct {
	TemplateVersionID uuid.UUID
	DelayMinutes      int32
}

// SetSteps replaces a draft campaign's sequence. Each step uses a fixed template version.
func (s *Service) SetSteps(ctx context.Context, p authctx.Principal, id uuid.UUID, steps []StepInput) error {
	if len(steps) == 0 || len(steps) > 10 {
		return apperr.Validation(apperr.FieldError{Field: "steps", Code: "between_1_and_10"})
	}
	return s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		// Touching the campaign moves it back to draft and fails if it has started.
		n, err := q.UpdateCampaign(ctx, repo.UpdateCampaignParams{TenantID: p.TenantID, ID: id})
		if err != nil {
			return err
		}
		if n == 0 {
			return s.notEditable(ctx, q, p, id)
		}
		if err := q.ExpireApprovals(ctx, repo.ExpireApprovalsParams{TenantID: p.TenantID, SubjectID: id}); err != nil {
			return err
		}
		if err := q.DeleteSteps(ctx, repo.DeleteStepsParams{TenantID: p.TenantID, CampaignID: id}); err != nil {
			return err
		}
		for i, st := range steps {
			if st.DelayMinutes < 0 || (i == 0 && st.DelayMinutes != 0) {
				return apperr.Validation(apperr.FieldError{Field: "steps", Code: "invalid_delay"})
			}
			if _, err := q.GetTemplateVersion(ctx, repo.GetTemplateVersionParams{TenantID: p.TenantID, ID: st.TemplateVersionID}); err != nil {
				return apperr.Validation(apperr.FieldError{Field: "steps", Code: "unknown_template_version"})
			}
			if err := q.InsertStep(ctx, repo.InsertStepParams{TenantID: p.TenantID, CampaignID: id, Position: int32(i + 1), DelayMinutes: st.DelayMinutes, TemplateVersionID: st.TemplateVersionID}); err != nil {
				return err
			}
		}
		return audit.Record(ctx, tx, userAudit(p, "outreach.steps_set", "campaign", &id, map[string]any{"steps": len(steps)}))
	})
}

// Preflight lists what stops a campaign from being sent, and summarises its audience.
type Preflight struct {
	Blockers []string
	Warnings []string
	Audience struct {
		Total, NoEmail, Suppressed, Blocked, Sendable int
	}
	ContentHash string
}

// Preflight checks a campaign before submission. It applies the same gates as sending.
func (s *Service) Preflight(ctx context.Context, p authctx.Principal, id uuid.UUID) (Preflight, error) {
	var out Preflight
	var members []uuid.UUID
	var rules map[string]string
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		c, err := q.GetCampaign(ctx, repo.GetCampaignParams{TenantID: p.TenantID, ID: id})
		if err != nil {
			return notFound(err)
		}
		h, steps, err := currentHash(ctx, q, p.TenantID, c)
		if err != nil {
			return err
		}
		out.ContentHash = hex.EncodeToString(h)
		if len(steps) == 0 {
			out.Blockers = append(out.Blockers, "no_steps")
		}
		for _, st := range steps {
			if st.AiGenerated {
				out.Warnings = append(out.Warnings, "ai_generated_content")
				break
			}
		}
		snd, err := q.GetSender(ctx, repo.GetSenderParams{TenantID: p.TenantID, ID: c.SenderAccountID})
		if err != nil {
			return err
		}
		if snd.Status != "active" {
			out.Blockers = append(out.Blockers, "sender_inactive")
		}
		if !authenticated(snd.SpfStatus, snd.DkimStatus, snd.DmarcStatus) {
			out.Blockers = append(out.Blockers, "domain_not_authenticated")
		}
		if strings.TrimSpace(c.PostalAddress) == "" {
			out.Blockers = append(out.Blockers, "no_postal_address")
		}
		if c.AudienceID == nil {
			out.Blockers = append(out.Blockers, "no_audience")
			return nil
		}
		if members, err = q.AudienceMembers(ctx, repo.AudienceMembersParams{TenantID: p.TenantID, AudienceID: *c.AudienceID}); err != nil {
			return err
		}
		rules, err = loadRules(ctx, q)
		return err
	})
	if err != nil || len(members) == 0 {
		if err == nil && !contains(out.Blockers, "no_audience") {
			out.Blockers = append(out.Blockers, "empty_audience")
		}
		return out, err
	}
	people, err := s.Shared.ContactsByID(ctx, p.TenantID, members)
	if err != nil {
		return out, err
	}
	out.Audience.Total = len(members)
	err = s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		for _, m := range members {
			c := people[m]
			if c.Email == nil || c.Erased {
				out.Audience.NoEmail++
				continue
			}
			sup, err := s.suppressed(ctx, q, p.TenantID, *c.Email)
			if err != nil {
				return err
			}
			if sup {
				out.Audience.Suppressed++
				continue
			}
			consent := false
			if rules[country(c.CountryCode)] == domain.ConsentRequired {
				if consent, err = s.Shared.HasConsent(ctx, p.TenantID, m, ConsentPurpose); err != nil {
					return err
				}
			}
			if !domain.JurisdictionAllows(country(c.CountryCode), rules, consent) {
				out.Audience.Blocked++
				continue
			}
			out.Audience.Sendable++
		}
		return nil
	})
	if err == nil && out.Audience.Sendable == 0 {
		out.Blockers = append(out.Blockers, "no_sendable_recipients")
	}
	return out, err
}

// ConsentPurpose is the consent a recipient needs where a jurisdiction requires it.
const ConsentPurpose = "marketing_email"

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func country(c *string) string {
	if c == nil {
		return ""
	}
	return strings.ToUpper(*c)
}

func loadRules(ctx context.Context, q *repo.Queries) (map[string]string, error) {
	rows, err := q.JurisdictionRules(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		out[strings.TrimSpace(r.CountryCode)] = r.Rule
	}
	return out, nil
}

// suppressed checks the tenant's list and the platform's global list. The global list is read only
// through a function that answers yes or no (D-09).
func (s *Service) suppressed(ctx context.Context, q *repo.Queries, tenant uuid.UUID, email string) (bool, error) {
	local, err := q.IsSuppressed(ctx, repo.IsSuppressedParams{TenantID: tenant, EmailHash: s.Shared.EmailKey(tenant, email)})
	if err != nil || local {
		return local, err
	}
	return q.IsGloballySuppressed(ctx, s.Shared.GlobalEmailKey(email))
}

// Submit asks for approval of the campaign's current content.
func (s *Service) Submit(ctx context.Context, p authctx.Principal, id uuid.UUID) (string, error) {
	pf, err := s.Preflight(ctx, p, id)
	if err != nil {
		return "", err
	}
	if len(pf.Blockers) > 0 {
		return "", apperr.Conflict("preflight_failed", "Fix these first: "+strings.Join(pf.Blockers, ", "))
	}
	h, _ := hex.DecodeString(pf.ContentHash)
	err = s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		n, err := q.SetCampaignStatus(ctx, repo.SetCampaignStatusParams{TenantID: p.TenantID, ID: id, Status: "pending_approval", FromStatuses: []string{"draft"}})
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.Conflict("not_draft", "Only a draft campaign can be submitted")
		}
		if err := q.ExpireApprovals(ctx, repo.ExpireApprovalsParams{TenantID: p.TenantID, SubjectID: id}); err != nil {
			return err
		}
		if _, err := q.InsertApproval(ctx, repo.InsertApprovalParams{TenantID: p.TenantID, SubjectID: id, ContentHash: h, RequestedBy: p.UserID}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "outreach.campaign_submitted", "campaign", &id, map[string]any{"content_hash": pf.ContentHash}))
	})
	return pf.ContentHash, err
}

// Decide approves or rejects a submitted campaign. An approval must confirm the hash of exactly
// what the approver reviewed; if anything changed since, it fails.
func (s *Service) Decide(ctx context.Context, p authctx.Principal, id uuid.UUID, approve bool, confirmedHash string, reason *string) error {
	return s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		c, err := q.GetCampaign(ctx, repo.GetCampaignParams{TenantID: p.TenantID, ID: id})
		if err != nil {
			return notFound(err)
		}
		pending, err := q.PendingApproval(ctx, repo.PendingApprovalParams{TenantID: p.TenantID, SubjectID: id})
		if err != nil || c.Status != "pending_approval" {
			return apperr.Conflict("not_pending", "This campaign is not waiting for approval")
		}
		status, next := "rejected", "draft"
		if approve {
			h, _, err := currentHash(ctx, q, p.TenantID, c)
			if err != nil {
				return err
			}
			if hex.EncodeToString(h) != confirmedHash || !bytes.Equal(h, pending.ContentHash) {
				return apperr.Conflict("content_changed", "The campaign changed after you reviewed it. Review it again")
			}
			status, next = "approved", "approved"
		}
		if err := q.DecideApproval(ctx, repo.DecideApprovalParams{TenantID: p.TenantID, ID: pending.ID, Status: status, DecidedBy: uid(p), Reason: reason}); err != nil {
			return err
		}
		if _, err := q.SetCampaignStatus(ctx, repo.SetCampaignStatusParams{TenantID: p.TenantID, ID: id, Status: next, FromStatuses: []string{"pending_approval"}}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "outreach.campaign_"+status, "campaign", &id, map[string]any{"content_hash": confirmedHash, "self_approved": pending.RequestedBy == p.UserID, "reason": reason}))
	})
}

// Approvals lists campaigns waiting for approval.
func (s *Service) Approvals(ctx context.Context, p authctx.Principal) ([]repo.ListPendingApprovalsRow, error) {
	var out []repo.ListPendingApprovalsRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.ListPendingApprovals(ctx, p.TenantID)
		return err
	})
	return out, err
}

// Start enrols the audience and starts sending. The send job checks every gate again for each
// message, so enrolling someone does not mean they will be sent to.
func (s *Service) Start(ctx context.Context, p authctx.Principal, id uuid.UUID) (int, error) {
	enrolled := 0
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		c, err := q.GetCampaign(ctx, repo.GetCampaignParams{TenantID: p.TenantID, ID: id})
		if err != nil {
			return notFound(err)
		}
		if c.Status != "approved" || c.AudienceID == nil {
			return apperr.Conflict("not_approved", "Only an approved campaign can be started")
		}
		h, _, err := currentHash(ctx, q, p.TenantID, c)
		if err != nil {
			return err
		}
		a, err := q.ApprovedCampaign(ctx, repo.ApprovedCampaignParams{TenantID: p.TenantID, SubjectID: id})
		if err != nil || !bytes.Equal(a.ContentHash, h) {
			return apperr.Conflict("approval_mismatch", "The approved content no longer matches the campaign")
		}
		members, err := q.AudienceMembers(ctx, repo.AudienceMembersParams{TenantID: p.TenantID, AudienceID: *c.AudienceID})
		if err != nil {
			return err
		}
		now := time.Now()
		for _, m := range members {
			if err := q.Enroll(ctx, repo.EnrollParams{TenantID: p.TenantID, CampaignID: id, ContactID: m, NextSendAt: &now}); err != nil {
				return err
			}
			enrolled++
		}
		if n, err := q.SetCampaignStatus(ctx, repo.SetCampaignStatusParams{TenantID: p.TenantID, ID: id, Status: "running", FromStatuses: []string{"approved"}}); err != nil || n == 0 {
			return firstErr(err, apperr.Conflict("not_approved", "Only an approved campaign can be started"))
		}
		return audit.Record(ctx, tx, userAudit(p, "outreach.campaign_started", "campaign", &id, map[string]any{"enrolled": enrolled}))
	})
	return enrolled, err
}

func firstErr(a, b error) error {
	if a != nil {
		return a
	}
	return b
}

// SetRunState pauses, resumes or cancels a campaign.
func (s *Service) SetRunState(ctx context.Context, p authctx.Principal, id uuid.UUID, action string) error {
	from, to := map[string][]string{"pause": {"running"}, "resume": {"paused"}, "cancel": {"draft", "pending_approval", "approved", "running", "paused"}}[action],
		map[string]string{"pause": "paused", "resume": "running", "cancel": "cancelled"}[action]
	if from == nil {
		return apperr.Validation(apperr.FieldError{Field: "action", Code: "invalid"})
	}
	return s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		n, err := q.SetCampaignStatus(ctx, repo.SetCampaignStatusParams{TenantID: p.TenantID, ID: id, Status: to, FromStatuses: from})
		if err != nil {
			return err
		}
		if n == 0 {
			if _, err := q.GetCampaign(ctx, repo.GetCampaignParams{TenantID: p.TenantID, ID: id}); err != nil {
				return notFound(err)
			}
			return apperr.Conflict("invalid_state", "The campaign cannot be "+to+" from its current state")
		}
		switch action {
		case "pause":
			err = q.PauseCampaignEnrollments(ctx, repo.PauseCampaignEnrollmentsParams{TenantID: p.TenantID, CampaignID: id})
		case "resume":
			err = q.ResumeCampaignEnrollments(ctx, repo.ResumeCampaignEnrollmentsParams{TenantID: p.TenantID, CampaignID: id})
		case "cancel":
			if err = q.StopCampaignEnrollments(ctx, repo.StopCampaignEnrollmentsParams{TenantID: p.TenantID, CampaignID: id}); err == nil {
				err = q.ExpireApprovals(ctx, repo.ExpireApprovalsParams{TenantID: p.TenantID, SubjectID: id})
			}
		}
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "outreach.campaign_"+to, "campaign", &id, nil))
	})
}
