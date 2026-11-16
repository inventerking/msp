package app

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/modules/outreach/repo"
	"mspengine/internal/platform/audit"
	"mspengine/internal/platform/integration/aigateway"
	"mspengine/internal/platform/shared"
)

// AddDomain adds a sending domain and checks its records.
func (s *Service) AddDomain(ctx context.Context, p authctx.Principal, raw string) (repo.GetDomainRow, error) {
	d, err := shared.NormalizeDomain(raw)
	if err != nil {
		return repo.GetDomainRow{}, apperr.Validation(apperr.FieldError{Field: "domain", Code: "invalid_company_domain"})
	}
	var id uuid.UUID
	err = s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		var err error
		if id, err = q.InsertDomain(ctx, repo.InsertDomainParams{TenantID: p.TenantID, Domain: d}); err != nil {
			if strings.Contains(err.Error(), "23505") {
				return apperr.Conflict("domain_exists", "This domain is already added")
			}
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "outreach.domain_added", "sending_domain", &id, map[string]any{"domain": d}))
	})
	if err != nil {
		return repo.GetDomainRow{}, err
	}
	return s.CheckDomain(ctx, p, id)
}

// CheckDomain refreshes a domain's SPF, DKIM and DMARC results.
func (s *Service) CheckDomain(ctx context.Context, p authctx.Principal, id uuid.UUID) (repo.GetDomainRow, error) {
	var out repo.GetDomainRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		d, err := q.GetDomain(ctx, repo.GetDomainParams{TenantID: p.TenantID, ID: id})
		if err != nil {
			return notFound(err)
		}
		spf, dkim, dmarc := s.Checker.Check(ctx, d.Domain)
		if err := q.SetDomainChecks(ctx, repo.SetDomainChecksParams{TenantID: p.TenantID, ID: id, SpfStatus: spf, DkimStatus: dkim, DmarcStatus: dmarc}); err != nil {
			return err
		}
		out, err = q.GetDomain(ctx, repo.GetDomainParams{TenantID: p.TenantID, ID: id})
		return err
	})
	return out, err
}

// Domains lists sending domains.
func (s *Service) Domains(ctx context.Context, p authctx.Principal) ([]repo.ListDomainsRow, error) {
	var out []repo.ListDomainsRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.ListDomains(ctx, p.TenantID)
		return err
	})
	return out, err
}

func authenticated(spf, dkim, dmarc string) bool {
	return spf == "pass" && dkim == "pass" && (dmarc == "pass" || dmarc == "none_policy")
}

// AddSender adds a from-address on a sending domain. New senders start with a low daily limit.
func (s *Service) AddSender(ctx context.Context, p authctx.Principal, domainID uuid.UUID, email, name string, limit int32) (repo.GetSenderRow, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if limit <= 0 {
		limit = 50
	}
	var dom repo.GetDomainRow
	if err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		dom, err = q.GetDomain(ctx, repo.GetDomainParams{TenantID: p.TenantID, ID: domainID})
		return err
	}); err != nil {
		return repo.GetSenderRow{}, apperr.Validation(apperr.FieldError{Field: "sending_domain_id", Code: "unknown_domain"})
	}
	if !strings.HasSuffix(email, "@"+dom.Domain) {
		return repo.GetSenderRow{}, apperr.Validation(apperr.FieldError{Field: "from_email", Code: "not_on_domain"})
	}
	conn, err := s.Integration.Connect(ctx, p.TenantID, uid(p), SenderProvider, email, &email)
	if err != nil {
		return repo.GetSenderRow{}, err
	}
	var id uuid.UUID
	err = s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		var err error
		if id, err = q.InsertSender(ctx, repo.InsertSenderParams{TenantID: p.TenantID, SendingDomainID: domainID, ConnectionID: conn, FromEmail: email, FromName: name, DailyLimit: limit}); err != nil {
			if strings.Contains(err.Error(), "23505") {
				return apperr.Conflict("sender_exists", "This sender is already added")
			}
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "outreach.sender_added", "sender_account", &id, map[string]any{"from_email": email, "daily_limit": limit}))
	})
	if err != nil {
		return repo.GetSenderRow{}, err
	}
	return s.Sender(ctx, p, id)
}

// Sender returns one sender.
func (s *Service) Sender(ctx context.Context, p authctx.Principal, id uuid.UUID) (repo.GetSenderRow, error) {
	var out repo.GetSenderRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.GetSender(ctx, repo.GetSenderParams{TenantID: p.TenantID, ID: id})
		return notFound(err)
	})
	return out, err
}

// Senders lists senders.
func (s *Service) Senders(ctx context.Context, p authctx.Principal) ([]repo.ListSendersRow, error) {
	var out []repo.ListSendersRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.ListSenders(ctx, p.TenantID)
		return err
	})
	return out, err
}

// UpdateSender pauses, resumes or renames a sender, or changes its daily limit.
func (s *Service) UpdateSender(ctx context.Context, p authctx.Principal, id uuid.UUID, name, status *string, limit *int32) (repo.GetSenderRow, error) {
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		n, err := q.UpdateSender(ctx, repo.UpdateSenderParams{TenantID: p.TenantID, ID: id, FromName: name, Status: status, DailyLimit: limit})
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.NotFound()
		}
		return audit.Record(ctx, tx, userAudit(p, "outreach.sender_updated", "sender_account", &id, map[string]any{"status": status, "daily_limit": limit}))
	})
	if err != nil {
		return repo.GetSenderRow{}, err
	}
	return s.Sender(ctx, p, id)
}

// DeleteSender removes a sender. Campaigns using it stop at their next send.
func (s *Service) DeleteSender(ctx context.Context, p authctx.Principal, id uuid.UUID) error {
	return s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		n, err := q.DeleteSender(ctx, repo.DeleteSenderParams{TenantID: p.TenantID, ID: id})
		if err == nil && n == 0 {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "outreach.sender_removed", "sender_account", &id, nil))
	})
}

// Templates.

// Templates lists templates with their current versions.
func (s *Service) Templates(ctx context.Context, p authctx.Principal) ([]repo.ListTemplatesRow, error) {
	var out []repo.ListTemplatesRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.ListTemplates(ctx, p.TenantID)
		return err
	})
	return out, err
}

// CreateTemplate adds a template with its first version.
func (s *Service) CreateTemplate(ctx context.Context, p authctx.Principal, name, subject, body string, aiRequest *uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		var err error
		if id, err = q.InsertTemplate(ctx, repo.InsertTemplateParams{TenantID: p.TenantID, Name: name, CreatedBy: uid(p)}); err != nil {
			return err
		}
		if err := addVersion(ctx, q, p.TenantID, id, subject, body, aiRequest); err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "outreach.template_created", "template", &id, map[string]any{"name": name, "ai_generated": aiRequest != nil}))
	})
	return id, err
}

func addVersion(ctx context.Context, q *repo.Queries, tenant, template uuid.UUID, subject, body string, aiRequest *uuid.UUID) error {
	v, err := q.InsertTemplateVersion(ctx, repo.InsertTemplateVersionParams{TenantID: tenant, TemplateID: template, Subject: subject, Body: body, AiGenerated: aiRequest != nil, AiRequestID: aiRequest})
	if err != nil {
		return err
	}
	return q.SetCurrentTemplateVersion(ctx, repo.SetCurrentTemplateVersionParams{TenantID: tenant, ID: template, VersionID: &v.ID})
}

// AddTemplateVersion adds a new version. Versions never change, so approved campaigns keep theirs.
func (s *Service) AddTemplateVersion(ctx context.Context, p authctx.Principal, template uuid.UUID, subject, body string) error {
	return s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		if _, err := q.GetTemplate(ctx, repo.GetTemplateParams{TenantID: p.TenantID, ID: template}); err != nil {
			return notFound(err)
		}
		return addVersion(ctx, q, p.TenantID, template, subject, body, nil)
	})
}

// Draft asks AI for a template draft. It is labelled as AI-generated wherever it is used, and a
// campaign using it still needs a person's approval.
func (s *Service) Draft(ctx context.Context, p authctx.Principal, purpose, audience string) (subject, body string, request uuid.UUID, err error) {
	res, err := s.AI.Run(ctx, aigateway.Request{TenantID: p.TenantID, Task: TaskDraft, IdempotencyKey: uuid.NewString(),
		Input: aigateway.Input{Trusted: map[string]string{"purpose": purpose}, Untrusted: map[string]string{"audience": audience}}})
	if err != nil {
		return "", "", uuid.Nil, err
	}
	var out struct {
		Subject string `json:"subject"`
		Body    string `json:"body"`
	}
	if err := json.Unmarshal(res.Output, &out); err != nil {
		return "", "", uuid.Nil, err
	}
	return out.Subject, out.Body, res.RequestID, nil
}

// Audiences.

// Audiences lists audiences.
func (s *Service) Audiences(ctx context.Context, p authctx.Principal) ([]repo.ListAudiencesRow, error) {
	var out []repo.ListAudiencesRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.ListAudiences(ctx, p.TenantID)
		return err
	})
	return out, err
}

// CreateAudience adds a static audience.
func (s *Service) CreateAudience(ctx context.Context, p authctx.Principal, name string) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		id, err = q.InsertAudience(ctx, repo.InsertAudienceParams{TenantID: p.TenantID, Name: name, CreatedBy: uid(p)})
		return err
	})
	return id, err
}

// AddMembers adds people to an audience, directly or from a Lead Generation list.
func (s *Service) AddMembers(ctx context.Context, p authctx.Principal, audience uuid.UUID, contacts []uuid.UUID, listID *uuid.UUID) (int, error) {
	if listID != nil {
		if s.Lists == nil {
			return 0, apperr.Conflict("lists_unavailable", "Lead Generation lists are not available")
		}
		ids, err := s.Lists.ListContacts(ctx, p.TenantID, *listID)
		if err != nil {
			return 0, apperr.Validation(apperr.FieldError{Field: "list_id", Code: "unknown_list"})
		}
		contacts = append(contacts, ids...)
	}
	people, err := s.Shared.ContactsByID(ctx, p.TenantID, contacts)
	if err != nil {
		return 0, err
	}
	added := 0
	err = s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		if _, err := q.GetAudience(ctx, repo.GetAudienceParams{TenantID: p.TenantID, ID: audience}); err != nil {
			return notFound(err)
		}
		for _, c := range contacts {
			if _, ok := people[c]; !ok {
				return apperr.Validation(apperr.FieldError{Field: "contact_ids", Code: "unknown_contact"})
			}
			if err := q.AddAudienceMember(ctx, repo.AddAudienceMemberParams{TenantID: p.TenantID, AudienceID: audience, ContactID: c}); err != nil {
				return err
			}
			added++
		}
		return nil
	})
	return added, err
}
