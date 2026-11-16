package app

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/modules/crm/contract"
	"mspengine/internal/modules/crm/repo"
	"mspengine/internal/platform/audit"
	"mspengine/internal/platform/shared"
)

// CompanyFilter narrows the company list.
type CompanyFilter struct {
	Lifecycle, Query *string
}

// Companies lists CRM companies, newest first.
func (s *Service) Companies(ctx context.Context, p authctx.Principal, f CompanyFilter, limit int32, before *uuid.UUID) ([]repo.ListCompaniesRow, error) {
	var out []repo.ListCompaniesRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.ListCompanies(ctx, repo.ListCompaniesParams{TenantID: p.TenantID, Lifecycle: f.Lifecycle, Q: f.Query, Before: before, Lim: limit})
		return err
	})
	return out, err
}

// CreateCompany adds a company. With a domain, it is linked to the shared company record; the name
// the user types stays the tenant's own.
func (s *Service) CreateCompany(ctx context.Context, p authctx.Principal, name string, domain *string, lifecycle string, owner *uuid.UUID) (repo.GetCompanyRow, error) {
	if lifecycle == "" {
		lifecycle = "prospect"
	}
	if !lifecycles[lifecycle] {
		return repo.GetCompanyRow{}, apperr.Validation(apperr.FieldError{Field: "lifecycle", Code: "invalid"})
	}
	if err := s.checkMember(ctx, p.TenantID, owner, "owner_user_id"); err != nil {
		return repo.GetCompanyRow{}, err
	}
	var canonical *uuid.UUID
	var norm *string
	if domain != nil && strings.TrimSpace(*domain) != "" {
		d, err := shared.NormalizeDomain(*domain)
		if err != nil {
			return repo.GetCompanyRow{}, apperr.Validation(apperr.FieldError{Field: "domain", Code: "invalid_company_domain"})
		}
		id, err := s.Shared.ResolveOrCreate(ctx, shared.Signal{Domain: d})
		if err != nil {
			return repo.GetCompanyRow{}, err
		}
		canonical, norm = &id, &d
	}
	var id uuid.UUID
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		if canonical != nil {
			if _, err := q.FindCompanyByCanonical(ctx, repo.FindCompanyByCanonicalParams{TenantID: p.TenantID, CompanyID: canonical}); err == nil {
				return apperr.Conflict("company_exists", "This company is already in your CRM")
			}
		}
		var err error
		id, err = s.ensureCompany(ctx, q, tx, contract.CompanyInput{TenantID: p.TenantID, CompanyID: canonical, Name: name, Domain: norm, ActorID: uid(p)})
		if err != nil {
			return err
		}
		if lifecycle != "prospect" || owner != nil {
			if _, err := q.UpdateCompany(ctx, repo.UpdateCompanyParams{TenantID: p.TenantID, ID: id, Lifecycle: &lifecycle, SetOwner: owner != nil, OwnerUserID: owner}); err != nil {
				return err
			}
		}
		return audit.Record(ctx, tx, userAudit(p, "crm.company_created", "crm_company", &id, map[string]any{"name": name}))
	})
	if err != nil {
		return repo.GetCompanyRow{}, err
	}
	return s.Company(ctx, p, id)
}

// Company returns one company.
func (s *Service) Company(ctx context.Context, p authctx.Principal, id uuid.UUID) (repo.GetCompanyRow, error) {
	var out repo.GetCompanyRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.GetCompany(ctx, repo.GetCompanyParams{TenantID: p.TenantID, ID: id})
		return notFound(err)
	})
	return out, err
}

// UpdateCompany changes a company's name, lifecycle or owner.
func (s *Service) UpdateCompany(ctx context.Context, p authctx.Principal, id uuid.UUID, name, lifecycle *string, setOwner bool, owner *uuid.UUID) (repo.GetCompanyRow, error) {
	if lifecycle != nil && !lifecycles[*lifecycle] {
		return repo.GetCompanyRow{}, apperr.Validation(apperr.FieldError{Field: "lifecycle", Code: "invalid"})
	}
	if err := s.checkMember(ctx, p.TenantID, owner, "owner_user_id"); err != nil {
		return repo.GetCompanyRow{}, err
	}
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		before, err := q.GetCompany(ctx, repo.GetCompanyParams{TenantID: p.TenantID, ID: id})
		if err != nil {
			return notFound(err)
		}
		if _, err := q.UpdateCompany(ctx, repo.UpdateCompanyParams{TenantID: p.TenantID, ID: id, Name: name, SetOwner: setOwner, OwnerUserID: owner}); err != nil {
			return err
		}
		if lifecycle != nil && *lifecycle != before.Lifecycle {
			if err := setLifecycle(ctx, q, tx, p.TenantID, id, *lifecycle); err != nil {
				return err
			}
		}
		return audit.Record(ctx, tx, userAudit(p, "crm.company_updated", "crm_company", &id, map[string]any{"name": name, "lifecycle": lifecycle, "owner_changed": setOwner}))
	})
	if err != nil {
		return repo.GetCompanyRow{}, err
	}
	return s.Company(ctx, p, id)
}

// DeleteCompany removes a company with no open deals.
func (s *Service) DeleteCompany(ctx context.Context, p authctx.Principal, id uuid.UUID) error {
	return s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		n, err := q.CompanyOpenDeals(ctx, repo.CompanyOpenDealsParams{TenantID: p.TenantID, CrmCompanyID: id})
		if err != nil {
			return err
		}
		if n > 0 {
			return apperr.Conflict("company_has_open_deals", "Close or move this company's open deals first")
		}
		rows, err := q.DeleteCompany(ctx, repo.DeleteCompanyParams{TenantID: p.TenantID, ID: id})
		if err != nil {
			return err
		}
		if rows == 0 {
			return apperr.NotFound()
		}
		return audit.Record(ctx, tx, userAudit(p, "crm.company_deleted", "crm_company", &id, nil))
	})
}

// Contacts.

// ContactView is a CRM contact with the person's details from the tenant's contact records.
type ContactView struct {
	repo.GetContactRow
	Person shared.ContactView
}

func (s *Service) withPeople(ctx context.Context, tenant uuid.UUID, rows []repo.GetContactRow) ([]ContactView, error) {
	ids := make([]uuid.UUID, len(rows))
	for i, r := range rows {
		ids[i] = r.ContactID
	}
	people, err := s.Shared.ContactsByID(ctx, tenant, ids)
	if err != nil {
		return nil, err
	}
	out := make([]ContactView, len(rows))
	for i, r := range rows {
		out[i] = ContactView{GetContactRow: r, Person: people[r.ContactID]}
	}
	return out, nil
}

// Contacts lists contacts, optionally for one company.
func (s *Service) Contacts(ctx context.Context, p authctx.Principal, company *uuid.UUID) ([]ContactView, error) {
	var rows []repo.GetContactRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		list, err := q.ListContacts(ctx, repo.ListContactsParams{TenantID: p.TenantID, CrmCompanyID: company})
		for _, r := range list {
			rows = append(rows, repo.GetContactRow(r))
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.withPeople(ctx, p.TenantID, rows)
}

// ContactInput describes a new contact.
type ContactInput struct {
	Email, FullName, JobTitle string
	CountryCode               string
	CRMCompanyID              *uuid.UUID
	Primary                   bool
}

// CreateContact adds a person. They are created in the tenant's contact records, with "added by
// hand" as the source, then linked to CRM.
func (s *Service) CreateContact(ctx context.Context, p authctx.Principal, in ContactInput) (ContactView, error) {
	if strings.TrimSpace(in.Email) == "" && strings.TrimSpace(in.FullName) == "" {
		return ContactView{}, apperr.Validation(apperr.FieldError{Field: "full_name", Code: "required"})
	}
	var canonical *uuid.UUID
	if in.CRMCompanyID != nil {
		c, err := s.Company(ctx, p, *in.CRMCompanyID)
		if err != nil {
			return ContactView{}, apperr.Validation(apperr.FieldError{Field: "crm_company_id", Code: "unknown_company"})
		}
		canonical = c.CompanyID
	}
	contactID, err := s.Shared.ResolveOrCreateContact(ctx, p.TenantID, shared.ContactInput{Email: in.Email, FullName: in.FullName, JobTitle: in.JobTitle, CountryCode: in.CountryCode, CompanyID: canonical, SourceKey: "manual", Licence: "tenant_provided", CreatedBy: uid(p)})
	if err != nil {
		return ContactView{}, err
	}
	var id uuid.UUID
	err = s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		var err error
		if id, err = ensureContact(ctx, q, tx, p.TenantID, contactID, in.CRMCompanyID); err != nil {
			return err
		}
		if in.Primary {
			if _, err := q.UpdateContact(ctx, repo.UpdateContactParams{TenantID: p.TenantID, ID: id, IsPrimary: &in.Primary}); err != nil {
				return err
			}
		}
		return audit.Record(ctx, tx, userAudit(p, "crm.contact_created", "crm_contact", &id, nil))
	})
	if err != nil {
		return ContactView{}, err
	}
	return s.Contact(ctx, p, id)
}

// Contact returns one contact.
func (s *Service) Contact(ctx context.Context, p authctx.Principal, id uuid.UUID) (ContactView, error) {
	var row repo.GetContactRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		row, err = q.GetContact(ctx, repo.GetContactParams{TenantID: p.TenantID, ID: id})
		return notFound(err)
	})
	if err != nil {
		return ContactView{}, err
	}
	v, err := s.withPeople(ctx, p.TenantID, []repo.GetContactRow{row})
	if err != nil {
		return ContactView{}, err
	}
	return v[0], nil
}

// UpdateContact moves a contact to another company or marks them primary.
func (s *Service) UpdateContact(ctx context.Context, p authctx.Principal, id uuid.UUID, setCompany bool, company *uuid.UUID, primary *bool) (ContactView, error) {
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		if setCompany && company != nil {
			if _, err := q.GetCompany(ctx, repo.GetCompanyParams{TenantID: p.TenantID, ID: *company}); err != nil {
				return apperr.Validation(apperr.FieldError{Field: "crm_company_id", Code: "unknown_company"})
			}
		}
		n, err := q.UpdateContact(ctx, repo.UpdateContactParams{TenantID: p.TenantID, ID: id, SetCompany: setCompany, CrmCompanyID: company, IsPrimary: primary})
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.NotFound()
		}
		return audit.Record(ctx, tx, userAudit(p, "crm.contact_updated", "crm_contact", &id, nil))
	})
	if err != nil {
		return ContactView{}, err
	}
	return s.Contact(ctx, p, id)
}

// DeleteContact removes a contact from CRM. The person stays in the tenant's contact records.
func (s *Service) DeleteContact(ctx context.Context, p authctx.Principal, id uuid.UUID) error {
	return s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		n, err := q.DeleteContact(ctx, repo.DeleteContactParams{TenantID: p.TenantID, ID: id})
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.NotFound()
		}
		if err := q.ClearDealContact(ctx, repo.ClearDealContactParams{TenantID: p.TenantID, Ids: []uuid.UUID{id}}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "crm.contact_deleted", "crm_contact", &id, nil))
	})
}
