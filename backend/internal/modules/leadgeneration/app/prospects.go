package app

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/jobs"
	crm "mspengine/internal/modules/crm/contract"
	"mspengine/internal/modules/leadgeneration/repo"
	"mspengine/internal/platform/audit"
	"mspengine/internal/platform/integration"
	"mspengine/internal/platform/shared"
)

// Attribute keys stored per prospect.
const (
	attrName       = "name"
	attrDomain     = "domain"
	attrCountry    = "country"
	attrIndustry   = "industry"
	attrEmployees  = "employees"
	attrTechnology = "technology"
)

// licenceTenantProvided marks data the tenant gave us itself.
const licenceTenantProvided = "tenant_provided"

// PromoteResult says what promotion did.
type PromoteResult struct {
	Created, Existing int
	ProspectIDs       []uuid.UUID
}

// Promote saves search results as prospects. Saving the same company twice returns the existing
// prospect, so promotion is safe to repeat.
func (s *Service) Promote(ctx context.Context, p authctx.Principal, searchID uuid.UUID, companyIDs []uuid.UUID, all bool) (PromoteResult, error) {
	var out PromoteResult
	var created []uuid.UUID
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		if _, err := q.GetSearchJob(ctx, repo.GetSearchJobParams{TenantID: p.TenantID, ID: searchID}); err != nil {
			return notFound(err)
		}
		if all {
			rows, err := q.ListSearchResults(ctx, repo.ListSearchResultsParams{TenantID: p.TenantID, SearchJobID: searchID})
			if err != nil {
				return err
			}
			companyIDs = companyIDs[:0]
			for _, r := range rows {
				companyIDs = append(companyIDs, r.CompanyID)
			}
		}
		if len(companyIDs) == 0 {
			return apperr.Validation(apperr.FieldError{Field: "company_ids", Code: "required"})
		}
		for _, cid := range companyIDs {
			r, err := q.GetSearchResultByCompany(ctx, repo.GetSearchResultByCompanyParams{TenantID: p.TenantID, SearchJobID: searchID, CompanyID: cid})
			if err != nil {
				return notFound(err)
			}
			var pv Preview
			if err := json.Unmarshal(r.Preview, &pv); err != nil {
				return err
			}
			id, isNew, err := s.insertProspect(ctx, q, tx, p, cid, pv.DisplayName, pv.Domain, r.SourceKey, ptr(searchID.String()))
			if err != nil {
				return err
			}
			if err := q.MarkResultPromoted(ctx, repo.MarkResultPromotedParams{TenantID: p.TenantID, ID: r.ID, ProspectID: &id}); err != nil {
				return err
			}
			out.ProspectIDs = append(out.ProspectIDs, id)
			if !isNew {
				out.Existing++
				continue
			}
			out.Created++
			created = append(created, id)
			if err := insertPreviewAttributes(ctx, q, p.TenantID, id, r.SourceKey, pv); err != nil {
				return err
			}
		}
		return audit.Record(ctx, tx, userAudit(p, "lead.results_promoted", "search_job", &searchID, map[string]any{"created": out.Created, "existing": out.Existing}))
	})
	if err != nil {
		return PromoteResult{}, err
	}
	s.scoreQuietly(ctx, p.TenantID, created)
	return out, nil
}

func ptr[T any](v T) *T { return &v }

func (s *Service) insertProspect(ctx context.Context, q *repo.Queries, tx pgx.Tx, p authctx.Principal, companyID uuid.UUID, name, domainName, source string, ref *string) (uuid.UUID, bool, error) {
	var d *string
	if domainName != "" {
		d = &domainName
	}
	id, err := q.InsertProspect(ctx, repo.InsertProspectParams{TenantID: p.TenantID, CompanyID: companyID, DisplayName: name, Domain: d, SourceKey: source, SourceRef: ref, CreatedBy: uid(p)})
	if errors.Is(err, pgx.ErrNoRows) {
		existing, err := q.FindProspectByCompany(ctx, repo.FindProspectByCompanyParams{TenantID: p.TenantID, CompanyID: companyID})
		return existing, false, err
	}
	if err != nil {
		return uuid.Nil, false, err
	}
	return id, true, emit(ctx, tx, p.TenantID, "prospect.created", "prospect", id, map[string]any{"prospect_id": id, "company_id": companyID, "source_key": source})
}

func insertPreviewAttributes(ctx context.Context, q *repo.Queries, tenant, prospect uuid.UUID, source string, pv Preview) error {
	add := func(attr string, v any) error {
		return q.InsertAttribute(ctx, repo.InsertAttributeParams{TenantID: tenant, ProspectID: prospect, Attribute: attr, Value: mustJSON(v), SourceKey: source, LicenceClass: pv.Licence})
	}
	for attr, v := range map[string]any{attrName: pv.DisplayName, attrDomain: pv.Domain, attrCountry: pv.Country, attrIndustry: pv.Industry} {
		if v.(string) != "" {
			if err := add(attr, v); err != nil {
				return err
			}
		}
	}
	if pv.Employees > 0 {
		if err := add(attrEmployees, pv.Employees); err != nil {
			return err
		}
	}
	if len(pv.Technology) > 0 {
		return add(attrTechnology, pv.Technology)
	}
	return nil
}

// CreateProspect adds a prospect the user typed in. The name they give stays tenant data: only the
// domain is used to find or create the shared company.
func (s *Service) CreateProspect(ctx context.Context, p authctx.Principal, domainName, displayName string) (repo.GetProspectRow, error) {
	norm, err := shared.NormalizeDomain(domainName)
	if err != nil {
		return repo.GetProspectRow{}, apperr.Validation(apperr.FieldError{Field: "domain", Code: "invalid_company_domain"})
	}
	companyID, err := s.Shared.ResolveOrCreate(ctx, shared.Signal{Domain: norm})
	if err != nil {
		return repo.GetProspectRow{}, err
	}
	if strings.TrimSpace(displayName) == "" {
		displayName = norm
	}
	var id uuid.UUID
	err = s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		var isNew bool
		var err error
		id, isNew, err = s.insertProspect(ctx, q, tx, p, companyID, displayName, norm, "manual", nil)
		if err != nil {
			return err
		}
		if !isNew {
			return apperr.Conflict("prospect_exists", "This company is already a prospect")
		}
		for attr, v := range map[string]string{attrName: displayName, attrDomain: norm} {
			if err := q.InsertAttribute(ctx, repo.InsertAttributeParams{TenantID: p.TenantID, ProspectID: id, Attribute: attr, Value: mustJSON(v), SourceKey: "manual", LicenceClass: licenceTenantProvided}); err != nil {
				return err
			}
		}
		return audit.Record(ctx, tx, userAudit(p, "lead.prospect_created", "prospect", &id, map[string]any{"domain": norm}))
	})
	if err != nil {
		return repo.GetProspectRow{}, err
	}
	s.scoreQuietly(ctx, p.TenantID, []uuid.UUID{id})
	return s.GetProspect(ctx, p, id)
}

// GetProspect returns one prospect.
func (s *Service) GetProspect(ctx context.Context, p authctx.Principal, id uuid.UUID) (repo.GetProspectRow, error) {
	var out repo.GetProspectRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.GetProspect(ctx, repo.GetProspectParams{TenantID: p.TenantID, ID: id})
		return notFound(err)
	})
	return out, err
}

// ProspectDetail is a prospect with its data, contacts and enrichment history.
type ProspectDetail struct {
	Prospect repo.GetProspectRow
	// IsCompetitor warns that the tenant tracks this company as a competitor.
	IsCompetitor bool
	Attributes   []repo.LatestAttributesRow
	Contacts     []repo.ListProspectContactsRow
	Enrichments  []repo.ListEnrichmentRunsRow
}

// Detail returns a prospect with everything known about it.
func (s *Service) Detail(ctx context.Context, p authctx.Principal, id uuid.UUID) (ProspectDetail, error) {
	var out ProspectDetail
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		if out.Prospect, err = q.GetProspect(ctx, repo.GetProspectParams{TenantID: p.TenantID, ID: id}); err != nil {
			return notFound(err)
		}
		if out.Attributes, err = q.LatestAttributes(ctx, repo.LatestAttributesParams{TenantID: p.TenantID, ProspectID: id}); err != nil {
			return err
		}
		if out.Contacts, err = q.ListProspectContacts(ctx, repo.ListProspectContactsParams{TenantID: p.TenantID, ProspectID: id}); err != nil {
			return err
		}
		out.Enrichments, err = q.ListEnrichmentRuns(ctx, repo.ListEnrichmentRunsParams{TenantID: p.TenantID, ProspectID: id})
		return err
	})
	if err == nil && s.Competitors != nil {
		out.IsCompetitor, err = s.Competitors.IsCompetitor(ctx, p.TenantID, out.Prospect.CompanyID)
	}
	return out, err
}

// ProspectFilter narrows the prospect list.
type ProspectFilter struct {
	Status   *string
	MinScore *int32
	ListID   *uuid.UUID
	Query    *string
}

// ListProspects returns prospects, newest first.
func (s *Service) ListProspects(ctx context.Context, p authctx.Principal, f ProspectFilter, limit int32, before *uuid.UUID) ([]repo.ListProspectsRow, error) {
	var out []repo.ListProspectsRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.ListProspects(ctx, repo.ListProspectsParams{TenantID: p.TenantID, Status: f.Status, MinScore: f.MinScore, ListID: f.ListID, Q: f.Query, Before: before, Lim: limit})
		return err
	})
	return out, err
}

// UpdateProspect changes status or owner.
func (s *Service) UpdateProspect(ctx context.Context, p authctx.Principal, id uuid.UUID, status *string, setOwner bool, owner *uuid.UUID) (repo.GetProspectRow, error) {
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		before, err := q.GetProspect(ctx, repo.GetProspectParams{TenantID: p.TenantID, ID: id})
		if err != nil {
			return notFound(err)
		}
		if before.Status == "converted" && status != nil {
			return apperr.Conflict("prospect_converted", "A converted prospect's status cannot change")
		}
		if setOwner && owner != nil {
			if _, err := s.Identity.Access(ctx, *owner, p.TenantID); err != nil {
				return apperr.Validation(apperr.FieldError{Field: "owner_user_id", Code: "not_a_member"})
			}
		}
		if _, err := q.UpdateProspect(ctx, repo.UpdateProspectParams{TenantID: p.TenantID, ID: id, Status: status, SetOwner: setOwner, OwnerUserID: owner}); err != nil {
			return err
		}
		if status != nil && *status != before.Status && (*status == "qualified" || *status == "disqualified") {
			if err := emit(ctx, tx, p.TenantID, "prospect."+*status, "prospect", id, map[string]any{"prospect_id": id}); err != nil {
				return err
			}
		}
		return audit.Record(ctx, tx, userAudit(p, "lead.prospect_updated", "prospect", &id, map[string]any{"status": status, "owner_changed": setOwner}))
	})
	if err != nil {
		return repo.GetProspectRow{}, err
	}
	return s.GetProspect(ctx, p, id)
}

// DeleteProspect removes a prospect.
func (s *Service) DeleteProspect(ctx context.Context, p authctx.Principal, id uuid.UUID) error {
	return s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		n, err := q.DeleteProspect(ctx, repo.DeleteProspectParams{TenantID: p.TenantID, ID: id})
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.NotFound()
		}
		return audit.Record(ctx, tx, userAudit(p, "lead.prospect_deleted", "prospect", &id, nil))
	})
}

type enrichPayload struct {
	RunID         uuid.UUID `json:"run_id"`
	ReservationID uuid.UUID `json:"reservation_id"`
}

// Enrich queues company or contact enrichment. Each kind reserves its credits first.
func (s *Service) Enrich(ctx context.Context, p authctx.Principal, id uuid.UUID, kinds []string) ([]repo.ListEnrichmentRunsRow, error) {
	if _, err := s.GetProspect(ctx, p, id); err != nil {
		return nil, err
	}
	provider, _, err := s.Integration.CompanyData(ctx)
	if errors.Is(err, integration.ErrNoProvider) {
		return nil, apperr.Conflict("provider_unavailable", "No company data provider is connected")
	}
	if err != nil {
		return nil, err
	}
	var reservations []uuid.UUID
	for range kinds {
		r, err := s.Commercial.Reserve(ctx, p.TenantID, creditMeter, CreditsPerEnrichment, Module, 30*time.Minute)
		if err != nil {
			for _, x := range reservations {
				s.release(ctx, p.TenantID, x)
			}
			return nil, err
		}
		reservations = append(reservations, r)
	}
	err = s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		for i, kind := range kinds {
			runID, err := q.InsertEnrichmentRun(ctx, repo.InsertEnrichmentRunParams{TenantID: p.TenantID, ProspectID: id, Kind: kind, ProviderKey: provider.Key()})
			if err != nil {
				return err
			}
			jobID, _, err := jobs.Enqueue(ctx, tx, jobs.Spec{TenantID: &p.TenantID, Kind: JobEnrich, Queue: jobs.QueueProviderIO, MaxAttempts: 3, Payload: enrichPayload{RunID: runID, ReservationID: reservations[i]}, IdempotencyKey: "enrich:" + runID.String()})
			if err != nil {
				return err
			}
			if err := q.SetEnrichmentJob(ctx, repo.SetEnrichmentJobParams{TenantID: p.TenantID, ID: runID, JobID: &jobID}); err != nil {
				return err
			}
		}
		return audit.Record(ctx, tx, userAudit(p, "lead.enrichment_requested", "prospect", &id, map[string]any{"kinds": kinds}))
	})
	if err != nil {
		for _, x := range reservations {
			s.release(ctx, p.TenantID, x)
		}
		return nil, err
	}
	d, err := s.Detail(ctx, p, id)
	return d.Enrichments[:min(len(kinds), len(d.Enrichments))], err
}

// HandleEnrich runs one enrichment. No data is a result, not a failure, and costs nothing.
func (s *Service) HandleEnrich(ctx context.Context, j jobs.Job) (err error) {
	var pl enrichPayload
	defer func() {
		if err != nil && j.Final(err) && j.TenantID != nil && pl.RunID != uuid.Nil {
			tenant := *j.TenantID
			bg := context.WithoutCancel(ctx)
			if ferr := s.systemTx(bg, tenant, func(q *repo.Queries, _ pgx.Tx) error {
				return q.FailEnrichmentIfOpen(bg, repo.FailEnrichmentIfOpenParams{TenantID: tenant, ID: pl.RunID})
			}); ferr != nil {
				slog.WarnContext(bg, "record failed enrichment", "error", ferr)
			}
			s.release(bg, tenant, pl.ReservationID)
		}
	}()
	if err := j.Decode(&pl); err != nil {
		return err
	}
	if j.TenantID == nil {
		return jobs.Permanent(errors.New("enrich job without tenant"))
	}
	tenant := *j.TenantID
	var run repo.GetEnrichmentRunRow
	var prospect repo.GetProspectRow
	err = s.systemTx(ctx, tenant, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		if run, err = q.GetEnrichmentRun(ctx, repo.GetEnrichmentRunParams{TenantID: tenant, ID: pl.RunID}); err != nil {
			return err
		}
		prospect, err = q.GetProspect(ctx, repo.GetProspectParams{TenantID: tenant, ID: run.ProspectID})
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		s.release(ctx, tenant, pl.ReservationID)
		return nil // The prospect was deleted.
	}
	if err != nil {
		return err
	}
	if run.Status != "queued" && run.Status != "running" {
		s.release(ctx, tenant, pl.ReservationID)
		return nil
	}
	finish := func(status string, code *string, used int64, apply func(q *repo.Queries, tx pgx.Tx) error) error {
		err := s.systemTx(ctx, tenant, func(q *repo.Queries, tx pgx.Tx) error {
			if apply != nil {
				if err := apply(q, tx); err != nil {
					return err
				}
			}
			if err := q.FinishEnrichmentRun(ctx, repo.FinishEnrichmentRunParams{TenantID: tenant, ID: run.ID, Status: status, CreditsUsed: used, ErrorCode: code}); err != nil {
				return err
			}
			return emit(ctx, tx, tenant, "prospect.enriched", "prospect", run.ProspectID, map[string]any{"prospect_id": run.ProspectID, "kind": run.Kind, "status": status})
		})
		if err != nil {
			return err
		}
		if used > 0 {
			if err := s.Commercial.Commit(ctx, tenant, pl.ReservationID, used); err != nil {
				slog.ErrorContext(ctx, "commit enrichment credits failed", "error", err)
			}
		} else {
			s.release(ctx, tenant, pl.ReservationID)
		}
		return nil
	}
	if prospect.Domain == nil {
		return finish("no_data", ptr("no_domain"), 0, nil)
	}
	provider, providerID, err := s.Integration.CompanyData(ctx)
	if err != nil {
		if ferr := finish("failed", ptr("provider_unavailable"), 0, nil); ferr != nil {
			return ferr
		}
		return jobs.Permanent(err)
	}
	var company *integration.CompanyRecord
	var contacts []integration.ContactRecord
	err = s.Integration.Call(ctx, provider.Key(), providerID, &tenant, "enrich_"+run.Kind, 30*time.Second, func(cctx context.Context) error {
		var err error
		if run.Kind == "company" {
			company, err = provider.EnrichCompany(cctx, *prospect.Domain)
		} else {
			contacts, err = provider.FindContacts(cctx, *prospect.Domain)
		}
		return err
	})
	if err != nil {
		if j.Attempt >= 3 {
			if ferr := finish("failed", ptr("provider_failed"), 0, nil); ferr != nil {
				return ferr
			}
			return jobs.Permanent(err)
		}
		return integration.ToJobError(err)
	}
	if run.Kind == "company" {
		if company == nil {
			return finish("no_data", nil, 0, nil)
		}
		pv := Preview{DisplayName: company.Name, Domain: company.Domain, Country: company.Country, Industry: company.Industry, Employees: company.Employees, Technology: company.Technology, Licence: company.Licence}
		if err := finish("succeeded", nil, CreditsPerEnrichment, func(q *repo.Queries, _ pgx.Tx) error {
			if err := insertPreviewAttributes(ctx, q, tenant, run.ProspectID, provider.Key(), pv); err != nil {
				return err
			}
			if prospect.SourceKey == "manual" && company.Name != "" && prospect.DisplayName == *prospect.Domain {
				return q.SetProspectName(ctx, repo.SetProspectNameParams{TenantID: tenant, ID: run.ProspectID, DisplayName: company.Name})
			}
			return nil
		}); err != nil {
			return err
		}
		s.scoreQuietly(ctx, tenant, []uuid.UUID{run.ProspectID})
		return nil
	}
	if len(contacts) == 0 {
		return finish("no_data", nil, 0, nil)
	}
	ids := make([]uuid.UUID, len(contacts))
	for i, c := range contacts {
		id, err := s.Shared.ResolveOrCreateContact(ctx, tenant, shared.ContactInput{Email: c.Email, FullName: c.FullName, JobTitle: c.JobTitle, CompanyID: &prospect.CompanyID, SourceKey: provider.Key(), SourceRef: "enrichment:" + run.ID.String(), Licence: c.Licence})
		if err != nil {
			return err
		}
		ids[i] = id
	}
	return finish("succeeded", nil, CreditsPerEnrichment, func(q *repo.Queries, _ pgx.Tx) error {
		for i, c := range contacts {
			dm, conf := c.DecisionMaker, c.Confidence
			if err := q.InsertProspectContact(ctx, repo.InsertProspectContactParams{TenantID: tenant, ProspectID: run.ProspectID, ContactID: ids[i], RoleTitle: optional(c.JobTitle), IsDecisionMaker: &dm, Confidence: &conf, SourceKey: provider.Key()}); err != nil {
				return err
			}
		}
		return nil
	})
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Contacts for conversion and the eraser.
func (s *Service) contactIDs(ctx context.Context, q *repo.Queries, tenant, prospect uuid.UUID) ([]uuid.UUID, error) {
	rows, err := q.ListProspectContacts(ctx, repo.ListProspectContactsParams{TenantID: tenant, ProspectID: prospect})
	if err != nil {
		return nil, err
	}
	out := make([]uuid.UUID, len(rows))
	for i, r := range rows {
		out[i] = r.ContactID
	}
	return out, nil
}

// Convert hands a prospect to CRM. Converting twice returns the first result.
func (s *Service) Convert(ctx context.Context, p authctx.Principal, id uuid.UUID) (repo.GetProspectRow, error) {
	var in crm.ProspectConversion
	pr, err := s.GetProspect(ctx, p, id)
	if err != nil {
		return pr, err
	}
	if pr.Status == "converted" && pr.CrmCompanyID != nil {
		return pr, nil
	}
	if s.CRM == nil {
		return repo.GetProspectRow{}, errCRMUnavailable
	}
	if err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		ids, err := s.contactIDs(ctx, q, p.TenantID, id)
		in = crm.ProspectConversion{TenantID: p.TenantID, ActorID: p.UserID, ProspectID: id, CompanyID: pr.CompanyID, DisplayName: pr.DisplayName, Domain: pr.Domain, ContactIDs: ids}
		return err
	}); err != nil {
		return repo.GetProspectRow{}, err
	}
	crmID, err := s.CRM.ConvertProspect(ctx, in)
	if err != nil {
		return repo.GetProspectRow{}, err
	}
	err = s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		if err := q.MarkConverted(ctx, repo.MarkConvertedParams{TenantID: p.TenantID, ID: id, CrmCompanyID: &crmID}); err != nil {
			return err
		}
		if err := emit(ctx, tx, p.TenantID, "prospect.converted", "prospect", id, map[string]any{"prospect_id": id, "crm_company_id": crmID}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "lead.prospect_converted", "prospect", &id, map[string]any{"crm_company_id": crmID}))
	})
	if err != nil {
		return repo.GetProspectRow{}, err
	}
	return s.GetProspect(ctx, p, id)
}
