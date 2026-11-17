// Package app is CRM's application service. Only CRM changes CRM records; other modules ask through
// its contract.
package app

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/db"
	"mspengine/internal/kernel/events"
	"mspengine/internal/modules/crm/contract"
	"mspengine/internal/modules/crm/repo"
	"mspengine/internal/platform/audit"
	"mspengine/internal/platform/files"
	"mspengine/internal/platform/identity"
	"mspengine/internal/platform/notification"
	"mspengine/internal/platform/shared"
)

// Module is the entitlement key.
const Module = "crm"

// Job kinds.
const JobReminders = "crm.reminders"

// Deps are the platform services CRM uses.
type Deps struct {
	Pools    *db.Pools
	Identity *identity.Service
	Shared   *shared.Service
	Files    *files.Service
	Notify   *notification.Service
}

// Service implements CRM.
type Service struct{ Deps }

// New creates the service.
func New(d Deps) *Service { return &Service{Deps: d} }

var (
	_ contract.Registry  = (*Service)(nil)
	_ contract.Deals     = (*Service)(nil)
	_ contract.Converter = (*Service)(nil)
)

func (s *Service) tx(ctx context.Context, p authctx.Principal, fn func(q *repo.Queries, tx pgx.Tx) error) error {
	return db.TenantTx(ctx, s.Pools.App, p.TenantID, p.UserID, func(tx pgx.Tx) error { return fn(repo.New(tx), tx) })
}

func (s *Service) systemTx(ctx context.Context, tenantID uuid.UUID, fn func(q *repo.Queries, tx pgx.Tx) error) error {
	return db.TenantTx(ctx, s.Pools.App, tenantID, uuid.Nil, func(tx pgx.Tx) error { return fn(repo.New(tx), tx) })
}

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.NotFound()
	}
	return err
}

func pgCode(err error, code string) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == code
}

func userAudit(p authctx.Principal, action, resource string, id *uuid.UUID, after any) audit.Event {
	return audit.Event{TenantID: &p.TenantID, ActorType: "user", ActorID: &p.UserID, Action: action, ResourceType: resource, ResourceID: id, After: after}
}

func emit(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, typ, aggregate string, id uuid.UUID, payload any) error {
	return events.Append(ctx, tx, events.Event{Type: "crm." + typ, TenantID: &tenantID, SourceModule: Module, AggregateType: aggregate, AggregateID: id}, payload)
}

// emitDeal publishes a deal event carrying the deal's current facts (see DealFacts), merged under
// the event's own fields. Read inside the same transaction, so it matches what was committed.
func emitDeal(ctx context.Context, q *repo.Queries, tx pgx.Tx, tenantID uuid.UUID, typ string, id uuid.UUID, own map[string]any) error {
	f, err := q.DealFacts(ctx, repo.DealFactsParams{TenantID: tenantID, ID: id})
	if err != nil {
		return err
	}
	var closeOn *string
	if f.ExpectedCloseDate != nil {
		v := f.ExpectedCloseDate.Format(time.DateOnly)
		closeOn = &v
	}
	payload := map[string]any{"deal_id": f.ID, "crm_company_id": f.CrmCompanyID, "company_id": f.CompanyID, "pipeline_id": f.PipelineID,
		"stage_key": f.StageKey, "status": f.Status, "owner_user_id": f.OwnerUserID, "currency": f.Currency,
		"contract_value_minor": f.ContractValueMinor, "expected_mrr_minor": f.ExpectedMrrMinor, "probability": f.Probability,
		"expected_close_date": closeOn, "source_module": f.SourceModule, "source_ref": f.SourceRef, "row_version": f.RowVersion}
	for k, v := range own {
		payload[k] = v
	}
	return emit(ctx, tx, tenantID, typ, "deal", id, payload)
}

func uid(p authctx.Principal) *uuid.UUID {
	if p.UserID == uuid.Nil {
		return nil
	}
	id := p.UserID
	return &id
}

func ptr[T any](v T) *T { return &v }

// checkMember confirms a user is an active member of the tenant, for owners and assignees.
func (s *Service) checkMember(ctx context.Context, tenant uuid.UUID, user *uuid.UUID, field string) error {
	if user == nil {
		return nil
	}
	if _, err := s.Identity.Access(ctx, *user, tenant); err != nil {
		return apperr.Validation(apperr.FieldError{Field: field, Code: "not_a_member"})
	}
	return nil
}

var lifecycles = map[string]bool{"prospect": true, "customer": true, "former_customer": true, "partner": true, "other": true}

// Registry.

// EnsureCompany finds or creates the tenant's CRM company. It needs no CRM entitlement (D-02).
func (s *Service) EnsureCompany(ctx context.Context, in contract.CompanyInput) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.systemTx(ctx, in.TenantID, func(q *repo.Queries, tx pgx.Tx) error {
		var err error
		id, err = s.ensureCompany(ctx, q, tx, in)
		return err
	})
	return id, err
}

func (s *Service) ensureCompany(ctx context.Context, q *repo.Queries, tx pgx.Tx, in contract.CompanyInput) (uuid.UUID, error) {
	if in.CompanyID != nil {
		if id, err := q.FindCompanyByCanonical(ctx, repo.FindCompanyByCanonicalParams{TenantID: in.TenantID, CompanyID: in.CompanyID}); err == nil {
			return id, nil
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, err
		}
	} else if in.SourceRef != nil && in.SourceModule != "" {
		if id, err := q.FindCompanyBySource(ctx, repo.FindCompanyBySourceParams{TenantID: in.TenantID, SourceModule: &in.SourceModule, SourceRef: in.SourceRef}); err == nil {
			return id, nil
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, err
		}
	}
	name := strings.TrimSpace(in.Name)
	if name == "" && in.Domain != nil {
		name = *in.Domain
	}
	if name == "" {
		return uuid.Nil, apperr.Validation(apperr.FieldError{Field: "name", Code: "required"})
	}
	var src *string
	if in.SourceModule != "" {
		src = &in.SourceModule
	}
	id, err := q.InsertCompany(ctx, repo.InsertCompanyParams{TenantID: in.TenantID, CompanyID: in.CompanyID, Name: name, Domain: in.Domain, Lifecycle: "prospect", SourceModule: src, SourceRef: in.SourceRef, CreatedBy: in.ActorID})
	if pgCode(err, "23505") && in.CompanyID != nil {
		// A concurrent caller created it first.
		return q.FindCompanyByCanonical(ctx, repo.FindCompanyByCanonicalParams{TenantID: in.TenantID, CompanyID: in.CompanyID})
	}
	if err != nil {
		return uuid.Nil, err
	}
	return id, emit(ctx, tx, in.TenantID, "company.created", "company", id, map[string]any{"crm_company_id": id, "company_id": in.CompanyID, "source_module": in.SourceModule})
}

// EnsureContact links a tenant contact to CRM, once.
func (s *Service) EnsureContact(ctx context.Context, tenantID, contactID uuid.UUID, crmCompanyID *uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.systemTx(ctx, tenantID, func(q *repo.Queries, tx pgx.Tx) error {
		var err error
		id, err = ensureContact(ctx, q, tx, tenantID, contactID, crmCompanyID)
		return err
	})
	return id, err
}

func ensureContact(ctx context.Context, q *repo.Queries, tx pgx.Tx, tenantID, contactID uuid.UUID, crmCompanyID *uuid.UUID) (uuid.UUID, error) {
	id, err := q.FindContact(ctx, repo.FindContactParams{TenantID: tenantID, ContactID: contactID})
	if err == nil || !errors.Is(err, pgx.ErrNoRows) {
		return id, err
	}
	id, err = q.InsertContact(ctx, repo.InsertContactParams{TenantID: tenantID, ContactID: contactID, CrmCompanyID: crmCompanyID})
	if err != nil {
		if pgCode(err, "23503") {
			return uuid.Nil, apperr.Validation(apperr.FieldError{Field: "contact_id", Code: "unknown_contact"})
		}
		return uuid.Nil, err
	}
	return id, emit(ctx, tx, tenantID, "contact.created", "contact", id, map[string]any{"crm_contact_id": id, "contact_id": contactID})
}

// GetCompanySummary returns a company's name and domain, never its notes or deals.
func (s *Service) GetCompanySummary(ctx context.Context, tenantID, crmCompanyID uuid.UUID) (contract.CompanySummary, error) {
	var out contract.CompanySummary
	err := s.systemTx(ctx, tenantID, func(q *repo.Queries, _ pgx.Tx) error {
		c, err := q.GetCompany(ctx, repo.GetCompanyParams{TenantID: tenantID, ID: crmCompanyID})
		if err != nil {
			return notFound(err)
		}
		out = contract.CompanySummary{ID: c.ID, CompanyID: c.CompanyID, Name: c.Name, Domain: c.Domain, Lifecycle: c.Lifecycle}
		return nil
	})
	return out, err
}

// CompanySummaries looks up several of the tenant's companies at once.
func (s *Service) CompanySummaries(ctx context.Context, tenantID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]contract.CompanySummary, error) {
	out := map[uuid.UUID]contract.CompanySummary{}
	if len(ids) == 0 {
		return out, nil
	}
	err := s.systemTx(ctx, tenantID, func(q *repo.Queries, _ pgx.Tx) error {
		rows, err := q.CompaniesByIDs(ctx, repo.CompaniesByIDsParams{TenantID: tenantID, Ids: ids})
		for _, c := range rows {
			out[c.ID] = contract.CompanySummary{ID: c.ID, CompanyID: c.CompanyID, Name: c.Name, Domain: c.Domain, Lifecycle: c.Lifecycle}
		}
		return err
	})
	return out, err
}

// SetLifecycle changes a company's lifecycle, such as to customer when a contract starts.
func (s *Service) SetLifecycle(ctx context.Context, tenantID, crmCompanyID uuid.UUID, lifecycle string) error {
	if !lifecycles[lifecycle] {
		return apperr.Validation(apperr.FieldError{Field: "lifecycle", Code: "invalid"})
	}
	return s.systemTx(ctx, tenantID, func(q *repo.Queries, tx pgx.Tx) error {
		return setLifecycle(ctx, q, tx, tenantID, crmCompanyID, lifecycle)
	})
}

func setLifecycle(ctx context.Context, q *repo.Queries, tx pgx.Tx, tenantID, id uuid.UUID, lifecycle string) error {
	c, err := q.GetCompany(ctx, repo.GetCompanyParams{TenantID: tenantID, ID: id})
	if err != nil {
		return notFound(err)
	}
	if c.Lifecycle == lifecycle {
		return nil
	}
	if _, err := q.UpdateCompany(ctx, repo.UpdateCompanyParams{TenantID: tenantID, ID: id, Lifecycle: &lifecycle}); err != nil {
		return err
	}
	return emit(ctx, tx, tenantID, "company.lifecycle_changed", "company", id, map[string]any{"crm_company_id": id, "from": c.Lifecycle, "to": lifecycle})
}

// ConvertProspect turns a Lead Generation prospect into a CRM company with its contacts. Converting
// the same prospect twice returns the first company.
func (s *Service) ConvertProspect(ctx context.Context, in contract.ProspectConversion) (uuid.UUID, error) {
	var out uuid.UUID
	err := s.systemTx(ctx, in.TenantID, func(q *repo.Queries, tx pgx.Tx) error {
		if id, err := q.GetConversion(ctx, repo.GetConversionParams{TenantID: in.TenantID, ProspectID: in.ProspectID}); err == nil {
			out = id
			return nil
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		company := in.CompanyID
		id, err := s.ensureCompany(ctx, q, tx, contract.CompanyInput{TenantID: in.TenantID, CompanyID: &company, Name: in.DisplayName, Domain: in.Domain, SourceModule: "lead_generation", SourceRef: &in.ProspectID, ActorID: &in.ActorID})
		if err != nil {
			return err
		}
		for _, c := range in.ContactIDs {
			if _, err := ensureContact(ctx, q, tx, in.TenantID, c, &id); err != nil {
				return err
			}
		}
		if _, err := q.InsertConversion(ctx, repo.InsertConversionParams{TenantID: in.TenantID, ProspectID: in.ProspectID, CrmCompanyID: id, ConvertedBy: &in.ActorID}); err != nil {
			return err
		}
		if _, err := q.InsertActivity(ctx, repo.InsertActivityParams{TenantID: in.TenantID, ActivityType: "system", CrmCompanyID: &id, ActorUserID: &in.ActorID, Summary: "Converted from a Lead Generation prospect", SourceModule: ptr("lead_generation")}); err != nil {
			return err
		}
		out = id
		return nil
	})
	return out, err
}

var errConflict = apperr.New(http.StatusConflict, "stale_version", "Someone else changed this deal. Reload to see the latest version")
