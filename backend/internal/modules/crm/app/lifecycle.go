package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/db"
	"mspengine/internal/kernel/events"
	"mspengine/internal/kernel/jobs"
	"mspengine/internal/modules/crm/repo"
)

// Name identifies CRM to the privacy service.
func (s *Service) Name() string { return Module }

// EraseContact removes a person from CRM. Deals and their totals stay; the person's links go.
func (s *Service) EraseContact(ctx context.Context, tenantID, contactID uuid.UUID) error {
	return s.systemTx(ctx, tenantID, func(q *repo.Queries, _ pgx.Tx) error {
		ids, err := q.CrmContactIDsFor(ctx, repo.CrmContactIDsForParams{TenantID: tenantID, ContactID: contactID})
		if err != nil {
			return err
		}
		if err := q.ClearDealContact(ctx, repo.ClearDealContactParams{TenantID: tenantID, Ids: ids}); err != nil {
			return err
		}
		if err := q.EraseContactLinks(ctx, repo.EraseContactLinksParams{TenantID: tenantID, ContactID: contactID}); err != nil {
			return err
		}
		if err := q.EraseAttendee(ctx, repo.EraseAttendeeParams{TenantID: tenantID, Ids: ids}); err != nil {
			return err
		}
		return q.EraseActivityContact(ctx, repo.EraseActivityContactParams{TenantID: tenantID, Ids: ids})
	})
}

// ExportContact lists what CRM holds about a person, for an access request.
func (s *Service) ExportContact(ctx context.Context, tenantID, contactID uuid.UUID) (any, error) {
	var out []repo.ListActivitiesRow
	var links []uuid.UUID
	err := s.systemTx(ctx, tenantID, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		if links, err = q.CrmContactIDsFor(ctx, repo.CrmContactIDsForParams{TenantID: tenantID, ContactID: contactID}); err != nil {
			return err
		}
		for _, id := range links {
			rows, err := q.ListActivities(ctx, repo.ListActivitiesParams{TenantID: tenantID, ContactID: &id})
			if err != nil {
				return err
			}
			out = append(out, rows...)
		}
		return nil
	})
	return map[string]any{"crm_contact_ids": links, "activities": out}, err
}

// Consumers are the events CRM reacts to.
func (s *Service) Consumers() []events.Consumer {
	return []events.Consumer{
		{Name: "crm.company_merged", Types: []string{"shared.company.merged"}, Class: events.Lifecycle, Handler: s.onCompanyMerged},
		{Name: "crm.timeline", Class: events.Active, Module: Module, Handler: s.onModuleEvent, Types: []string{
			"proposals.version.issued", "proposals.proposal.viewed", "proposals.proposal.signed", "proposals.proposal.declined",
			"it_assessment.assessment.completed",
			"outreach.message.sent", "outreach.reply.received", "chatbot.lead.captured", "visitor_intelligence.company.sent",
		}},
	}
}

func (s *Service) onCompanyMerged(ctx context.Context, _ pgx.Tx, e events.Event) error {
	var p struct {
		Source uuid.UUID `json:"source_company_id"`
		Target uuid.UUID `json:"target_company_id"`
	}
	if err := e.Decode(&p); err != nil || p.Source == uuid.Nil || p.Target == uuid.Nil {
		return fmt.Errorf("bad merge payload: %w", err)
	}
	return db.Tx(ctx, s.Pools.System, func(tx pgx.Tx) error {
		_, err := repo.New(tx).RepointCompanies(ctx, repo.RepointCompaniesParams{Source: &p.Source, Target: &p.Target})
		return err
	})
}

// moduleEvent is the shared shape of the events CRM puts on its timeline.
type moduleEvent struct {
	CRMCompanyID *uuid.UUID `json:"crm_company_id"`
	DealID       *uuid.UUID `json:"deal_id"`
	Title        string     `json:"title"`
	Score        *int       `json:"score"`
	// ContactID is a shared contact, used by modules that know a person but not their CRM company.
	ContactID *uuid.UUID `json:"contact_id"`
}

// timeline says how each event appears, and which stage it suggests.
var timeline = map[string]struct {
	kind, summary, stage string
}{
	"proposals.version.issued":           {"proposal", "Proposal sent", "proposal_sent"},
	"proposals.proposal.viewed":          {"proposal", "Proposal viewed", ""},
	"proposals.proposal.signed":          {"proposal", "Proposal signed. Review the deal and mark it won", "negotiation"},
	"proposals.proposal.declined":        {"proposal", "Proposal declined", ""},
	"it_assessment.assessment.completed": {"assessment", "IT assessment completed", "assessment_completed"},
	"outreach.message.sent":              {"email_sent", "Outreach email sent", "contacted"},
	"outreach.reply.received":            {"email_replied", "Reply received to outreach", ""},
	"chatbot.lead.captured":              {"chat", "Lead captured by chatbot", ""},
	"visitor_intelligence.company.sent":  {"visit", "Website visits from this company", ""},
}

// onModuleEvent records another module's event on the timeline, once, and applies its stage
// suggestion forward only. A signed proposal does not win the deal: a person does that.
func (s *Service) onModuleEvent(ctx context.Context, tx pgx.Tx, e events.Event) error {
	if e.TenantID == nil {
		return nil
	}
	var p moduleEvent
	if err := e.Decode(&p); err != nil {
		return err
	}
	t := timeline[e.Type]
	q := repo.New(tx)
	var crmContact *uuid.UUID
	if p.ContactID != nil {
		c, err := q.FindContactCompany(ctx, repo.FindContactCompanyParams{TenantID: *e.TenantID, ContactID: *p.ContactID})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // The person is not in CRM, so there is no timeline to add to.
		}
		if err != nil {
			return err
		}
		crmContact = &c.ID
		if p.CRMCompanyID == nil {
			p.CRMCompanyID = c.CrmCompanyID
		}
	}
	summary := t.summary
	if p.Title != "" {
		summary += ": " + p.Title
	}
	if p.Score != nil {
		summary += fmt.Sprintf(" (score %d)", *p.Score)
	}
	id := e.ID
	if _, err := q.InsertActivity(ctx, repo.InsertActivityParams{TenantID: *e.TenantID, ActivityType: t.kind, CrmCompanyID: p.CRMCompanyID, ContactID: crmContact, DealID: p.DealID, Summary: summary, OccurredAt: &e.OccurredAt, SourceModule: &e.SourceModule, SourceEventID: &id}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // Already recorded: a replayed event has no second effect.
		}
		return err
	}
	if t.stage == "" {
		return nil
	}
	var deals []uuid.UUID
	if p.DealID != nil {
		deals = []uuid.UUID{*p.DealID}
	} else if p.CRMCompanyID != nil {
		var err error
		if deals, err = q.OpenDealsForCompany(ctx, repo.OpenDealsForCompanyParams{TenantID: *e.TenantID, CrmCompanyID: *p.CRMCompanyID}); err != nil {
			return err
		}
	}
	for _, d := range deals {
		if _, err := requestStage(ctx, q, tx, *e.TenantID, d, t.stage, e.Type); err != nil {
			return err
		}
	}
	return nil
}

// HandleReminders fires due task reminders as notifications. It finds them as the system role,
// then claims and sends each under its tenant, so each fires once.
func (s *Service) HandleReminders(ctx context.Context, _ jobs.Job) error {
	var due []repo.DueRemindersRow
	if err := db.Tx(ctx, s.Pools.System, func(tx pgx.Tx) error {
		var err error
		due, err = repo.New(tx).DueReminders(ctx)
		return err
	}); err != nil {
		return err
	}
	for _, r := range due {
		err := s.systemTx(ctx, r.TenantID, func(q *repo.Queries, tx pgx.Tx) error {
			row, err := q.ClaimReminder(ctx, repo.ClaimReminderParams{TenantID: r.TenantID, ID: r.ID})
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			if err != nil || row.Status != "open" || row.DeletedAt != nil {
				return err
			}
			return s.Notify.Notify(ctx, tx, r.TenantID, row.UserID, "crm.task_reminder", map[string]string{"title": row.Title}, "reminder:"+r.ID.String())
		})
		if err != nil {
			slog.WarnContext(ctx, "reminder failed", "error", err)
		}
	}
	return nil
}

// Jobs are CRM's job handlers.
func (s *Service) Jobs() map[string]jobs.Handler {
	return map[string]jobs.Handler{JobReminders: s.HandleReminders}
}
