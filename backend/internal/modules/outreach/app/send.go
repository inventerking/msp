package app

import (
	"context"
	"crypto/hmac"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/db"
	"mspengine/internal/kernel/jobs"
	"mspengine/internal/modules/outreach/domain"
	"mspengine/internal/modules/outreach/repo"
	"mspengine/internal/platform/audit"
	"mspengine/internal/platform/integration"
	"mspengine/internal/platform/shared"
)

// Jobs are Outreach's job handlers.
func (s *Service) Jobs() map[string]jobs.Handler {
	return map[string]jobs.Handler{JobDispatch: s.HandleDispatch, JobSend: s.HandleSend}
}

type sendPayload struct {
	EnrollmentID uuid.UUID `json:"enrollment_id"`
	Step         int32     `json:"step"`
}

// HandleDispatch finds due enrollments as the system role, which may read only active ones, then
// claims each in its tenant and queues one send job for it.
func (s *Service) HandleDispatch(ctx context.Context, _ jobs.Job) error {
	var due []repo.DueEnrollmentsRow
	if err := db.Tx(ctx, s.Pools.System, func(tx pgx.Tx) error {
		var err error
		due, err = repo.New(tx).DueEnrollments(ctx)
		return err
	}); err != nil {
		return err
	}
	for _, d := range due {
		err := s.systemTx(ctx, d.TenantID, func(q *repo.Queries, tx pgx.Tx) error {
			step, err := q.ClaimDue(ctx, repo.ClaimDueParams{TenantID: d.TenantID, ID: d.ID})
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			if err != nil {
				return err
			}
			_, _, err = jobs.Enqueue(ctx, tx, jobs.Spec{TenantID: &d.TenantID, Kind: JobSend, Queue: jobs.QueueEmailSend, MaxAttempts: 5,
				Payload: sendPayload{EnrollmentID: d.ID, Step: step}})
			return err
		})
		if err != nil {
			slog.WarnContext(ctx, "outreach dispatch failed", "error", err)
		}
	}
	return nil
}

// prepared is everything a send needs, gathered before the locked check.
type prepared struct {
	campaign repo.GetCampaignRow
	step     repo.ListStepsRow
	last     bool
	sender   repo.GetSenderRow
	contact  shared.ContactView
	consent  bool
	hash     []byte
	// steps are the steps after this one.
	steps []repo.ListStepsRow
}

func (p prepared) nextDelay() int32 {
	if len(p.steps) == 0 {
		return 0
	}
	return p.steps[0].DelayMinutes
}

// HandleSend sends one step to one person. Every gate is checked again here, under a lock on the
// enrollment, so a stop, unsubscribe or pause that commits first always wins.
func (s *Service) HandleSend(ctx context.Context, j jobs.Job) error {
	var in sendPayload
	if err := j.Decode(&in); err != nil || j.TenantID == nil {
		return jobs.Permanent(errors.New("bad payload"))
	}
	tenant := *j.TenantID
	pr, done, err := s.prepare(ctx, tenant, in)
	if err != nil || done {
		return err
	}
	entitled := s.Commercial.RequireEntitled(ctx, tenant, Module) == nil
	if pr.contact.Email != nil {
		if r := strings.ToUpper(country(pr.contact.CountryCode)); r != "" {
			if pr.consent, err = s.Shared.HasConsent(ctx, tenant, pr.contact.ID, ConsentPurpose); err != nil {
				return err
			}
		}
	}

	var msgID uuid.UUID
	var rendered domain.Rendered
	send := false
	err = s.systemTx(ctx, tenant, func(q *repo.Queries, tx pgx.Tx) error {
		e, err := q.LockEnrollment(ctx, repo.LockEnrollmentParams{TenantID: tenant, ID: in.EnrollmentID})
		if err != nil {
			return notFound(err)
		}
		if e.CurrentStep != in.Step {
			return nil // Another job already handled this step.
		}
		c, err := q.GetCampaign(ctx, repo.GetCampaignParams{TenantID: tenant, ID: pr.campaign.ID})
		if err != nil {
			return err
		}
		f := domain.Facts{Entitled: entitled, CampaignRunning: c.Status == "running", EnrollmentActive: e.Status == "active", CurrentHash: pr.hash,
			HasEmail: pr.contact.Email != nil && !pr.contact.Erased, Country: country(pr.contact.CountryCode), HasConsent: pr.consent,
			SenderActive: pr.sender.Status == "active", DomainAuthenticated: authenticated(pr.sender.SpfStatus, pr.sender.DkimStatus, pr.sender.DmarcStatus),
			ReputationPaused: pr.sender.ReputationState == "paused", DailyLimit: int(pr.sender.DailyLimit)}
		if a, err := q.ApprovedCampaign(ctx, repo.ApprovedCampaignParams{TenantID: tenant, SubjectID: c.ID}); err == nil {
			f.ApprovedHash = a.ContentHash
		}
		if f.HasEmail {
			if f.Suppressed, err = q.IsSuppressed(ctx, repo.IsSuppressedParams{TenantID: tenant, EmailHash: s.Shared.EmailKey(tenant, *pr.contact.Email)}); err != nil {
				return err
			}
			if f.GloballySuppressed, err = q.IsGloballySuppressed(ctx, s.Shared.GlobalEmailKey(*pr.contact.Email)); err != nil {
				return err
			}
		}
		if f.Rules, err = loadRules(ctx, q); err != nil {
			return err
		}
		var w domain.Window
		_ = json.Unmarshal(c.SendingWindow, &w)
		loc, err := time.LoadLocation(c.Timezone)
		if err != nil {
			loc = time.UTC
		}
		var nextOpen time.Time
		f.WindowOpen, nextOpen = w.Open(time.Now(), loc)
		sent, err := q.SentToday(ctx, repo.SentTodayParams{TenantID: tenant, SenderAccountID: pr.sender.ID})
		if err != nil {
			return err
		}
		f.SentToday = int(sent)

		g := domain.Check(f)
		switch g.Result {
		case domain.Stop:
			return q.StopEnrollment(ctx, repo.StopEnrollmentParams{TenantID: tenant, ID: e.ID, StopReason: &g.Reason})
		case domain.Hold:
			// A paused or unapproved campaign is looked at again in an hour; resuming reschedules it.
			return q.HoldEnrollment(ctx, repo.HoldEnrollmentParams{TenantID: tenant, ID: e.ID, NextSendAt: ptr(time.Now().Add(time.Hour))})
		case domain.Wait:
			next := nextOpen
			if g.Reason == "daily_limit" || next.IsZero() {
				next = time.Now().Add(time.Hour)
			}
			return q.HoldEnrollment(ctx, repo.HoldEnrollmentParams{TenantID: tenant, ID: e.ID, NextSendAt: &next})
		}

		key := in.EnrollmentID.String() + ":" + itoa(in.Step)
		existing, err := q.FindMessageByKey(ctx, repo.FindMessageByKeyParams{TenantID: tenant, IdempotencyKey: key})
		switch {
		case err == nil && existing.Status == "sent":
			return s.advance(ctx, q, tenant, e.ID, in.Step, pr) // Sent before a crash: only move on.
		case err == nil:
			msgID = existing.ID
		case errors.Is(err, pgx.ErrNoRows):
			msgID = uuid.Must(uuid.NewV7())
		default:
			return err
		}
		token := s.unsubscribeToken(tenant, msgID)
		rendered = domain.Render(pr.step.Subject, pr.step.Body,
			domain.Recipient{FirstName: firstName(pr.contact), FullName: deref(pr.contact.FullName), Email: *pr.contact.Email},
			s.BaseURL+"/u/"+token, s.BaseURL+"/api/v1/public/outreach/unsubscribe/"+token, c.PostalAddress, pr.sender.FromName)
		if existing.ID == uuid.Nil {
			id, err := q.InsertMessage(ctx, repo.InsertMessageParams{ID: msgID, TenantID: tenant, EnrollmentID: e.ID, StepID: pr.step.ID, Subject: &rendered.Subject, Body: &rendered.Body, IdempotencyKey: key})
			if err != nil {
				return err
			}
			msgID = id
		}
		send = true
		return nil
	})
	if err != nil || !send {
		return err
	}

	sender, _, err := s.Integration.EmailSender(ctx, SenderProvider)
	if err != nil {
		return s.failSend(ctx, j, tenant, in, msgID, err)
	}
	providerID, err := sender.Send(ctx, integration.OutboundEmail{From: formatFrom(pr.sender.FromName, pr.sender.FromEmail), To: *pr.contact.Email,
		Subject: rendered.Subject, Body: rendered.Body, Headers: rendered.Headers, IdempotencyKey: msgID.String()})
	if err != nil {
		return s.failSend(ctx, j, tenant, in, msgID, integration.ToJobError(err))
	}
	return s.systemTx(ctx, tenant, func(q *repo.Queries, tx pgx.Tx) error {
		if err := q.MarkSent(ctx, repo.MarkSentParams{TenantID: tenant, ID: msgID, ProviderMessageID: &providerID}); err != nil {
			return err
		}
		if err := q.CountSend(ctx, repo.CountSendParams{TenantID: tenant, SenderAccountID: pr.sender.ID}); err != nil {
			return err
		}
		if err := q.CountStat(ctx, repo.CountStatParams{TenantID: tenant, CampaignID: pr.campaign.ID, Sent: 1}); err != nil {
			return err
		}
		if err := emit(ctx, tx, tenant, "message.sent", "message", msgID, map[string]any{"contact_id": pr.contact.ID, "campaign_id": pr.campaign.ID, "title": pr.campaign.Name}); err != nil {
			return err
		}
		return s.advance(ctx, q, tenant, in.EnrollmentID, in.Step, pr)
	})
}

func (s *Service) advance(ctx context.Context, q *repo.Queries, tenant, enrollment uuid.UUID, step int32, pr prepared) error {
	var next *time.Time
	if !pr.last {
		next = ptr(time.Now().Add(time.Duration(pr.nextDelay()) * time.Minute))
	}
	return q.AdvanceEnrollment(ctx, repo.AdvanceEnrollmentParams{TenantID: tenant, ID: enrollment, CurrentStep: step, NextSendAt: next})
}

// failSend retries a send the provider may accept later; on the last attempt it marks the message
// failed and stops the enrollment, so nothing waits forever.
func (s *Service) failSend(ctx context.Context, j jobs.Job, tenant uuid.UUID, in sendPayload, msg uuid.UUID, err error) error {
	if !j.Final(err) {
		return err
	}
	if e := s.systemTx(ctx, tenant, func(q *repo.Queries, _ pgx.Tx) error {
		if err := q.MarkMessageFailed(ctx, repo.MarkMessageFailedParams{TenantID: tenant, ID: msg}); err != nil {
			return err
		}
		return q.StopEnrollment(ctx, repo.StopEnrollmentParams{TenantID: tenant, ID: in.EnrollmentID, StopReason: ptr("send_failed")})
	}); e != nil {
		slog.WarnContext(ctx, "could not record failed send", "error", e)
	}
	return err
}

// prepare reads the campaign, step, sender and recipient. done means there is nothing to send.
func (s *Service) prepare(ctx context.Context, tenant uuid.UUID, in sendPayload) (prepared, bool, error) {
	var pr prepared
	var contactID uuid.UUID
	done := false
	err := s.systemTx(ctx, tenant, func(q *repo.Queries, _ pgx.Tx) error {
		e, err := q.GetEnrollment(ctx, repo.GetEnrollmentParams{TenantID: tenant, ID: in.EnrollmentID})
		if err != nil {
			return jobs.Permanent(err)
		}
		if e.Status != "active" || e.CurrentStep != in.Step {
			done = true
			return nil
		}
		contactID = e.ContactID
		if pr.campaign, err = q.GetCampaign(ctx, repo.GetCampaignParams{TenantID: tenant, ID: e.CampaignID}); err != nil {
			return err
		}
		h, steps, err := currentHash(ctx, q, tenant, pr.campaign)
		if err != nil {
			return err
		}
		pr.hash = h
		found := false
		for i, st := range steps {
			if st.Position == in.Step {
				pr.step, pr.last, found = st, i == len(steps)-1, true
				if !pr.last {
					pr.steps = steps[i+1:]
				}
			}
		}
		if !found {
			done = true
			return q.AdvanceEnrollment(ctx, repo.AdvanceEnrollmentParams{TenantID: tenant, ID: e.ID, CurrentStep: in.Step})
		}
		pr.sender, err = q.GetSender(ctx, repo.GetSenderParams{TenantID: tenant, ID: pr.campaign.SenderAccountID})
		return err
	})
	if err != nil || done {
		return pr, done, err
	}
	people, err := s.Shared.ContactsByID(ctx, tenant, []uuid.UUID{contactID})
	if err != nil {
		return pr, false, err
	}
	pr.contact = people[contactID]
	pr.contact.ID = contactID
	return pr, false, nil
}

// firstName is the stored first name, or the first word of the full name.
func firstName(c shared.ContactView) string {
	if n := strings.TrimSpace(deref(c.FirstName)); n != "" {
		return n
	}
	if f := strings.Fields(deref(c.FullName)); len(f) > 0 {
		return f[0]
	}
	return ""
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func itoa(n int32) string { return strconv.Itoa(int(n)) }

func formatFrom(name, email string) string {
	name = strings.NewReplacer("\"", "", "\r", "", "\n", "", "<", "", ">", "").Replace(name)
	if name == "" {
		return email
	}
	return "\"" + name + "\" <" + email + ">"
}

// Unsubscribe tokens carry the tenant and message, signed, so the public link needs no lookup
// across tenants and cannot be forged or pointed at someone else.

func (s *Service) unsubscribeToken(tenant, msg uuid.UUID) string {
	payload := append(tenant[:], msg[:]...)
	mac := s.Keys.HMACValue("outreach-unsubscribe", string(payload))[:16]
	return base64.RawURLEncoding.EncodeToString(append(payload, mac...))
}

func (s *Service) parseUnsubscribe(token string) (tenant, msg uuid.UUID, ok bool) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != 48 {
		return uuid.Nil, uuid.Nil, false
	}
	want := s.Keys.HMACValue("outreach-unsubscribe", string(raw[:32]))[:16]
	if !hmac.Equal(want, raw[32:]) {
		return uuid.Nil, uuid.Nil, false
	}
	copy(tenant[:], raw[:16])
	copy(msg[:], raw[16:32])
	return tenant, msg, true
}

// UnsubscribeInfo is what the public unsubscribe page may show: nothing about the person.
type UnsubscribeInfo struct {
	Sender string
	Done   bool
}

// UnsubscribeStatus checks a token and says who sent the email. It changes nothing.
func (s *Service) UnsubscribeStatus(ctx context.Context, token string) (UnsubscribeInfo, error) {
	tenant, msg, ok := s.parseUnsubscribe(token)
	if !ok {
		return UnsubscribeInfo{}, apperr.NotFound()
	}
	var out UnsubscribeInfo
	var contact uuid.UUID
	err := s.systemTx(ctx, tenant, func(q *repo.Queries, _ pgx.Tx) error {
		m, err := q.GetMessage(ctx, repo.GetMessageParams{TenantID: tenant, ID: msg})
		if err != nil {
			return notFound(err)
		}
		contact = m.ContactID
		if c, err := q.GetCampaign(ctx, repo.GetCampaignParams{TenantID: tenant, ID: m.CampaignID}); err == nil {
			if snd, err := q.GetSender(ctx, repo.GetSenderParams{TenantID: tenant, ID: c.SenderAccountID}); err == nil {
				out.Sender = snd.FromName
			}
		}
		return nil
	})
	if err != nil {
		return out, err
	}
	people, err := s.Shared.ContactsByID(ctx, tenant, []uuid.UUID{contact})
	if err != nil {
		return out, err
	}
	email := people[contact].Email
	out.Done = email == nil // Erased: nothing will be sent.
	if email != nil {
		err = s.systemTx(ctx, tenant, func(q *repo.Queries, _ pgx.Tx) error {
			out.Done, err = q.IsSuppressed(ctx, repo.IsSuppressedParams{TenantID: tenant, EmailHash: s.Shared.EmailKey(tenant, *email)})
			return err
		})
	}
	return out, err
}

// Unsubscribe suppresses the recipient for the whole tenant and stops their sequences, at once and
// without an account or the tenant's entitlement. Repeating it is harmless.
func (s *Service) Unsubscribe(ctx context.Context, token string) error {
	tenant, msg, ok := s.parseUnsubscribe(token)
	if !ok {
		return apperr.NotFound()
	}
	var contact uuid.UUID
	if err := s.systemTx(ctx, tenant, func(q *repo.Queries, _ pgx.Tx) error {
		m, err := q.GetMessage(ctx, repo.GetMessageParams{TenantID: tenant, ID: msg})
		contact = m.ContactID
		return notFound(err)
	}); err != nil {
		return err
	}
	people, err := s.Shared.ContactsByID(ctx, tenant, []uuid.UUID{contact})
	if err != nil {
		return err
	}
	return s.systemTx(ctx, tenant, func(q *repo.Queries, tx pgx.Tx) error {
		return s.optOut(ctx, q, tx, tenant, contact, people[contact].Email, "unsubscribe", "link", &msg)
	})
}

// optOut stops the contact's sequences and adds them to the tenant's suppression list.
func (s *Service) optOut(ctx context.Context, q *repo.Queries, tx pgx.Tx, tenant, contact uuid.UUID, email *string, reason, source string, msg *uuid.UUID) error {
	stopped, err := q.StopContactEnrollments(ctx, repo.StopContactEnrollmentsParams{TenantID: tenant, ContactID: contact, StopReason: &reason})
	if err != nil {
		return err
	}
	if email != nil {
		if err := q.Suppress(ctx, repo.SuppressParams{TenantID: tenant, EmailHash: s.Shared.EmailKey(tenant, *email), Reason: reason, Source: source}); err != nil {
			return err
		}
	}
	if reason == "unsubscribe" && len(stopped) > 0 && msg != nil {
		if m, err := q.GetMessage(ctx, repo.GetMessageParams{TenantID: tenant, ID: *msg}); err == nil {
			if err := q.CountStat(ctx, repo.CountStatParams{TenantID: tenant, CampaignID: m.CampaignID, Unsubscribed: 1}); err != nil {
				return err
			}
		}
	}
	return audit.Record(ctx, tx, audit.Event{TenantID: &tenant, ActorType: "system", Action: "outreach.opt_out", ResourceType: "contact", ResourceID: &contact,
		After: map[string]any{"reason": reason, "source": source, "stopped": len(stopped)}})
}

// emailEvent is the sink provider's webhook body. Real providers get their own adapters (D-05).
type emailEvent struct {
	EventID   string `json:"event_id"`
	Type      string `json:"type"` // delivered, soft_bounce, hard_bounce, complaint, reply
	MessageID string `json:"message_id"`
	Snippet   string `json:"snippet"`
}

// HandleEmailWebhook records a delivery event. Bounces and complaints suppress the address; a
// complaint also adds it to the platform's global list (D-09). A reply stops the sequence before it
// is recorded, so no further step goes out.
func (s *Service) HandleEmailWebhook(ctx context.Context, endpointTenant *uuid.UUID, body []byte) error {
	var ev emailEvent
	if err := json.Unmarshal(body, &ev); err != nil || ev.EventID == "" || ev.MessageID == "" {
		return errors.New("bad email event")
	}
	var found repo.FindProviderMessageRow
	if err := db.Tx(ctx, s.Pools.System, func(tx pgx.Tx) error {
		var err error
		found, err = repo.New(tx).FindProviderMessage(ctx, &ev.MessageID)
		return err
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // Not ours, or erased.
		}
		return err
	}
	tenant := found.TenantID
	if endpointTenant != nil && *endpointTenant != tenant {
		return nil // A tenant's own endpoint cannot report on another tenant's messages.
	}
	var email *string
	var contact, campaign uuid.UUID
	if err := s.systemTx(ctx, tenant, func(q *repo.Queries, _ pgx.Tx) error {
		m, err := q.GetMessage(ctx, repo.GetMessageParams{TenantID: tenant, ID: found.ID})
		contact, campaign = m.ContactID, m.CampaignID
		return err
	}); err != nil {
		return err
	}
	if people, err := s.Shared.ContactsByID(ctx, tenant, []uuid.UUID{contact}); err == nil {
		email = people[contact].Email
	} else {
		return err
	}
	err := s.systemTx(ctx, tenant, func(q *repo.Queries, tx pgx.Tx) error {
		if ev.Type == "reply" {
			// Stop first, so no later step goes out, then record. A replayed reply records nothing.
			if _, err := q.StopContactEnrollments(ctx, repo.StopContactEnrollmentsParams{TenantID: tenant, ContactID: contact, StopReason: ptr("reply")}); err != nil {
				return err
			}
			snippet := []rune(ev.Snippet)
			if len(snippet) > 500 {
				snippet = snippet[:500]
			}
			n, err := q.InsertReply(ctx, repo.InsertReplyParams{TenantID: tenant, EnrollmentID: &found.EnrollmentID, MessageID: &found.ID, ProviderMessageID: ev.EventID, Snippet: ptr(string(snippet))})
			if err != nil || n == 0 {
				return err
			}
			if err := q.CountStat(ctx, repo.CountStatParams{TenantID: tenant, CampaignID: campaign, Replied: 1}); err != nil {
				return err
			}
			return emit(ctx, tx, tenant, "reply.received", "message", found.ID, map[string]any{"contact_id": contact, "campaign_id": campaign})
		}
		if !deliveryTypes[ev.Type] {
			return nil
		}
		detail, _ := json.Marshal(map[string]string{"type": ev.Type})
		n, err := q.InsertDeliveryEvent(ctx, repo.InsertDeliveryEventParams{TenantID: tenant, MessageID: &found.ID, ProviderKey: SenderProvider, ProviderEventID: ev.EventID, EventType: ev.Type, Detail: detail})
		if err != nil || n == 0 {
			return err // n == 0: a replay, already handled.
		}
		switch ev.Type {
		case "delivered":
			return q.CountStat(ctx, repo.CountStatParams{TenantID: tenant, CampaignID: campaign, Delivered: 1})
		case "hard_bounce", "complaint":
			if err := q.CountStat(ctx, repo.CountStatParams{TenantID: tenant, CampaignID: campaign, Bounced: b2i(ev.Type == "hard_bounce")}); err != nil {
				return err
			}
			return s.optOut(ctx, q, tx, tenant, contact, email, ev.Type, "provider", &found.ID)
		}
		return nil
	})
	if err != nil || ev.Type != "complaint" || email == nil {
		return err
	}
	return db.Tx(ctx, s.Pools.System, func(tx pgx.Tx) error {
		return repo.New(tx).InsertGlobalSuppression(ctx, repo.InsertGlobalSuppressionParams{EmailHmac: s.Shared.GlobalEmailKey(*email), Reason: "complaint"})
	})
}

var deliveryTypes = map[string]bool{"accepted": true, "delivered": true, "deferred": true, "soft_bounce": true, "hard_bounce": true, "complaint": true}

func b2i(b bool) int32 {
	if b {
		return 1
	}
	return 0
}

// Name identifies Outreach to the erasure job.
func (s *Service) Name() string { return Module }

// EraseContact stops the person's sequences and removes the personal text of messages and
// replies. The suppression entry stays: it holds only a keyed hash, and keeps them opted out.
func (s *Service) EraseContact(ctx context.Context, tenantID, contactID uuid.UUID) error {
	return s.systemTx(ctx, tenantID, func(q *repo.Queries, _ pgx.Tx) error {
		if _, err := q.StopContactEnrollments(ctx, repo.StopContactEnrollmentsParams{TenantID: tenantID, ContactID: contactID, StopReason: ptr("erased")}); err != nil {
			return err
		}
		if err := q.RedactContactMessages(ctx, repo.RedactContactMessagesParams{TenantID: tenantID, ContactID: contactID}); err != nil {
			return err
		}
		return q.RedactContactReplies(ctx, repo.RedactContactRepliesParams{TenantID: tenantID, ContactID: contactID})
	})
}

// ExportContact returns what Outreach holds about the person.
func (s *Service) ExportContact(ctx context.Context, tenantID, contactID uuid.UUID) (any, error) {
	var out []repo.ListMessagesRow
	err := s.systemTx(ctx, tenantID, func(q *repo.Queries, _ pgx.Tx) error {
		all, err := q.ListMessages(ctx, repo.ListMessagesParams{TenantID: tenantID})
		for _, m := range all {
			if m.ContactID == contactID {
				out = append(out, m)
			}
		}
		return err
	})
	return map[string]any{"messages": out}, err
}
