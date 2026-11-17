package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/csvx"
	"mspengine/internal/kernel/db"
	"mspengine/internal/kernel/events"
	crm "mspengine/internal/modules/crm/contract"
	"mspengine/internal/modules/customerexpansion/domain"
	"mspengine/internal/modules/customerexpansion/repo"
	"mspengine/internal/platform/audit"
	"mspengine/internal/platform/shared"
)

// Customer is a profile with what the list and detail screens show.
type Customer struct {
	repo.ListProfilesRow
	Company       crm.CompanySummary
	Unknown       int
	RenewalNext   *time.Time
	MixedCurrency bool
}

func (s *Service) activeServices(ctx context.Context, tenant uuid.UUID) ([]shared.TenantService, error) {
	all, err := s.Shared.TenantServices(ctx, tenant)
	var out []shared.TenantService
	for _, sv := range all {
		if sv.Status == "active" {
			out = append(out, sv)
		}
	}
	return out, err
}

// Customers lists the tenant's customer profiles, optionally by health status.
func (s *Service) Customers(ctx context.Context, p authctx.Principal, health *string, only *uuid.UUID) ([]Customer, error) {
	var rows []repo.ListProfilesRow
	if err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		rows, err = q.ListProfiles(ctx, repo.ListProfilesParams{TenantID: p.TenantID, Health: health, OnlyID: only})
		return err
	}); err != nil {
		return nil, err
	}
	services, err := s.activeServices(ctx, p.TenantID)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.CrmCompanyID)
	}
	names, err := s.Registry.CompanySummaries(ctx, p.TenantID, ids)
	if err != nil {
		return nil, err
	}
	out := make([]Customer, 0, len(rows))
	for _, r := range rows {
		c := Customer{ListProfilesRow: r, Company: names[r.CrmCompanyID], MixedCurrency: r.MrrCurrencies > 1}
		if c.Company.ID == uuid.Nil {
			continue // Deleted in CRM: the profile stays for history but is not listed.
		}
		c.Unknown = max(0, len(services)-int(r.CoveredActive+r.CoveredAbsent+r.CoveredDeclined))
		if r.RenewalNextDate != "" {
			if t, err := time.Parse(time.DateOnly, r.RenewalNextDate); err == nil {
				c.RenewalNext = &t
			}
		}
		out = append(out, c)
	}
	return out, nil
}

// Customer returns one profile.
func (s *Service) Customer(ctx context.Context, p authctx.Principal, id uuid.UUID) (Customer, error) {
	list, err := s.Customers(ctx, p, nil, &id)
	if err != nil {
		return Customer{}, err
	}
	if len(list) == 0 {
		return Customer{}, apperr.NotFound()
	}
	return list[0], nil
}

// NewCustomer names a customer by CRM company, or by name and domain through the registry, which
// works whether or not the tenant has CRM (D-02).
type NewCustomer struct {
	CRMCompanyID *uuid.UUID
	Name         string
	Domain       *string
}

// AddCustomer creates or finds a profile and marks the company as a customer in CRM.
func (s *Service) AddCustomer(ctx context.Context, p authctx.Principal, in NewCustomer) (uuid.UUID, error) {
	var crmID uuid.UUID
	switch {
	case in.CRMCompanyID != nil:
		if _, err := s.Registry.GetCompanySummary(ctx, p.TenantID, *in.CRMCompanyID); err != nil {
			return uuid.Nil, invalid("crm_company_id", "unknown_company")
		}
		crmID = *in.CRMCompanyID
	case strings.TrimSpace(in.Name) != "":
		var err error
		crmID, err = s.Registry.EnsureCompany(ctx, crm.CompanyInput{TenantID: p.TenantID, Name: strings.TrimSpace(in.Name), Domain: in.Domain, SourceModule: Module, ActorID: uid(p)})
		if err != nil {
			return uuid.Nil, err
		}
	default:
		return uuid.Nil, invalid("name", "required")
	}
	summary, err := s.Registry.GetCompanySummary(ctx, p.TenantID, crmID)
	if err != nil {
		return uuid.Nil, err
	}
	var id uuid.UUID
	err = s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		var err error
		id, err = q.EnsureProfile(ctx, repo.EnsureProfileParams{TenantID: p.TenantID, CrmCompanyID: crmID, CompanyID: summary.CompanyID})
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "expansion.customer_added", "customer_profile", &id, map[string]any{"crm_company_id": crmID}))
	})
	if err == nil {
		s.markCustomer(ctx, p.TenantID, crmID)
	}
	return id, err
}

// SetHealth sets a status by hand, or clears it back to the computed one when status is nil.
func (s *Service) SetHealth(ctx context.Context, p authctx.Principal, id uuid.UUID, status *string) error {
	valid := map[string]bool{"unknown": true, "healthy": true, "watch": true, "at_risk": true}
	if status != nil && !valid[*status] {
		return invalid("health_status", "invalid_status")
	}
	return s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		pr, err := q.GetProfile(ctx, repo.GetProfileParams{TenantID: p.TenantID, ID: id})
		if err != nil {
			return notFound(err)
		}
		next, source := pr.HealthStatus, "computed"
		if status != nil {
			next, source = *status, "manual"
		}
		if err := q.SetHealth(ctx, repo.SetHealthParams{TenantID: p.TenantID, ID: id, HealthStatus: next, HealthSource: source}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "expansion.health_overridden", "customer_profile", &id, map[string]any{"from": pr.HealthStatus, "to": next, "source": source}))
	})
}

// CoverageLine is one service and what the customer has of it. Unknown when nothing is recorded.
type CoverageLine struct {
	Service  shared.TenantService
	Coverage string
	Quantity *int32
	MRR      *int64
	Currency *string
	Updated  *time.Time
}

// Coverage lists every active service with the customer's coverage of it.
func (s *Service) Coverage(ctx context.Context, p authctx.Principal, profile uuid.UUID) ([]CoverageLine, error) {
	all, err := s.Shared.TenantServices(ctx, p.TenantID)
	if err != nil {
		return nil, err
	}
	var rows []repo.ListCoverageRow
	if err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		if _, err := q.GetProfile(ctx, repo.GetProfileParams{TenantID: p.TenantID, ID: profile}); err != nil {
			return notFound(err)
		}
		var err error
		rows, err = q.ListCoverage(ctx, repo.ListCoverageParams{TenantID: p.TenantID, CustomerProfileID: profile})
		return err
	}); err != nil {
		return nil, err
	}
	byService := map[uuid.UUID]repo.ListCoverageRow{}
	for _, r := range rows {
		byService[r.TenantServiceID] = r
	}
	out := []CoverageLine{}
	for _, sv := range all {
		r, ok := byService[sv.ID]
		if !ok && sv.Status != "active" {
			continue // A retired service is shown only where coverage was recorded.
		}
		line := CoverageLine{Service: sv, Coverage: domain.Unknown}
		if ok {
			line.Coverage, line.Quantity, line.MRR, line.Currency, line.Updated = r.Coverage, r.Quantity, r.MrrMinor, r.Currency, &r.UpdatedAt
		}
		out = append(out, line)
	}
	return out, nil
}

// CoverageInput is one service's coverage to record.
type CoverageInput struct {
	ServiceID uuid.UUID
	Coverage  string
	Quantity  *int32
	MRR       *int64
	Currency  *string
}

var coverages = map[string]bool{domain.Active: true, domain.Absent: true, domain.Declined: true, domain.Unknown: true}

// SetCoverage replaces the customer's coverage. A service left out, or set to unknown, is unknown.
func (s *Service) SetCoverage(ctx context.Context, p authctx.Principal, profile uuid.UUID, items []CoverageInput) error {
	all, err := s.Shared.TenantServices(ctx, p.TenantID)
	if err != nil {
		return err
	}
	known := map[uuid.UUID]bool{}
	for _, sv := range all {
		known[sv.ID] = true
	}
	seen := map[uuid.UUID]bool{}
	for i, it := range items {
		field := fmt.Sprintf("items[%d]", i)
		switch {
		case !known[it.ServiceID]:
			return invalid(field+".tenant_service_id", "unknown_service")
		case seen[it.ServiceID]:
			return invalid(field+".tenant_service_id", "duplicate")
		case !coverages[it.Coverage]:
			return invalid(field+".coverage", "invalid_coverage")
		case (it.MRR == nil) != (it.Currency == nil):
			return invalid(field+".currency", "mrr_needs_currency")
		case it.MRR != nil && *it.MRR < 0:
			return invalid(field+".mrr_minor", "negative")
		case it.Currency != nil && !domain.ValidCurrency(*it.Currency):
			return invalid(field+".currency", "invalid_currency")
		case it.Quantity != nil && *it.Quantity < 0:
			return invalid(field+".quantity", "negative")
		case it.Coverage != domain.Active && it.MRR != nil:
			return invalid(field+".mrr_minor", "only_for_active")
		}
		seen[it.ServiceID] = true
	}
	return s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		if _, err := q.GetProfile(ctx, repo.GetProfileParams{TenantID: p.TenantID, ID: profile}); err != nil {
			return notFound(err)
		}
		keep := []uuid.UUID{}
		for _, it := range items {
			if it.Coverage == domain.Unknown {
				continue // Unknown is the absence of a record, so it is never stored as a claim.
			}
			keep = append(keep, it.ServiceID)
			if err := q.UpsertCoverage(ctx, repo.UpsertCoverageParams{TenantID: p.TenantID, CustomerProfileID: profile, TenantServiceID: it.ServiceID,
				Coverage: it.Coverage, Quantity: it.Quantity, MrrMinor: it.MRR, Currency: it.Currency, Source: "manual"}); err != nil {
				return err
			}
		}
		if err := q.DeleteCoverageExcept(ctx, repo.DeleteCoverageExceptParams{TenantID: p.TenantID, CustomerProfileID: profile, Keep: keep}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "expansion.coverage_changed", "customer_profile", &profile, map[string]any{"recorded": len(keep)}))
	})
}

// ImportResult says what an import did. Errors name lines, never cell contents.
type ImportResult struct {
	Created, Contracts int
	Errors             []string
}

// maxImportRows bounds one customer import.
const maxImportRows = 1000

// ImportCustomers reads a scanned CSV upload with name and domain columns, and optional mrr_minor,
// currency, start_date and end_date for a contract. Each row is its own step, so one bad row does
// not stop the rest.
func (s *Service) ImportCustomers(ctx context.Context, p authctx.Principal, fileID uuid.UUID) (ImportResult, error) {
	file, purpose, err := s.Files.ReadForTenant(ctx, p.TenantID, fileID)
	if err != nil {
		return ImportResult{}, err
	}
	if purpose != "csv_import" {
		return ImportResult{}, invalid("file_id", "not_an_import_file")
	}
	rows, err := csvx.Read(file.Body, []string{"name"}, maxImportRows)
	if err != nil {
		return ImportResult{}, apperr.Validation(apperr.FieldError{Field: "file_id", Code: "unreadable_csv"})
	}
	var res ImportResult
	for _, r := range rows {
		name := r.Fields["name"]
		if name == "" {
			res.Errors = append(res.Errors, fmt.Sprintf("line %d: name is empty", r.Line))
			continue
		}
		var domainName *string
		if d := r.Fields["domain"]; d != "" {
			domainName = &d
		}
		profile, err := s.AddCustomer(ctx, p, NewCustomer{Name: name, Domain: domainName})
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("line %d: %s", r.Line, reason(err)))
			continue
		}
		res.Created++
		if r.Fields["mrr_minor"] == "" {
			continue
		}
		in, msg := contractFromRow(r.Fields)
		if msg != "" {
			res.Errors = append(res.Errors, fmt.Sprintf("line %d: %s", r.Line, msg))
			continue
		}
		if _, err := s.CreateContract(ctx, p, profile, in); err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("line %d: %s", r.Line, reason(err)))
			continue
		}
		res.Contracts++
	}
	return res, nil
}

func contractFromRow(f map[string]string) (ContractInput, string) {
	mrr, err := strconv.ParseInt(f["mrr_minor"], 10, 64)
	if err != nil {
		return ContractInput{}, "mrr_minor is not a whole number"
	}
	cur := strings.ToUpper(f["currency"])
	start := time.Now().UTC().Truncate(24 * time.Hour)
	if v := f["start_date"]; v != "" {
		if start, err = time.Parse(time.DateOnly, v); err != nil {
			return ContractInput{}, "start_date is not YYYY-MM-DD"
		}
	}
	in := ContractInput{Start: &start, Currency: &cur, MRR: &mrr}
	if v := f["end_date"]; v != "" {
		end, err := time.Parse(time.DateOnly, v)
		if err != nil {
			return ContractInput{}, "end_date is not YYYY-MM-DD"
		}
		in.End = &end
	}
	return in, ""
}

func reason(err error) string {
	var ae *apperr.Error
	if errors.As(err, &ae) {
		if len(ae.Fields) > 0 {
			return ae.Fields[0].Field + " " + ae.Fields[0].Code
		}
		return ae.Code
	}
	return "could not be saved"
}

// onAssessment keeps a completed assessment's risk as evidence for an existing customer. Prospects
// have no profile, so their assessments are not kept here.
func (s *Service) onAssessment(ctx context.Context, tx pgx.Tx, e events.Event) error {
	if e.TenantID == nil {
		return nil
	}
	var p struct {
		AssessmentID uuid.UUID  `json:"assessment_id"`
		CRMCompanyID *uuid.UUID `json:"crm_company_id"`
		Score        *int       `json:"score"`
		Risk         *string    `json:"risk_level"`
	}
	if err := e.Decode(&p); err != nil {
		return err
	}
	if p.CRMCompanyID == nil || p.Risk == nil {
		return nil
	}
	q := repo.New(tx)
	profile, err := q.ProfileByCompany(ctx, repo.ProfileByCompanyParams{TenantID: *e.TenantID, CrmCompanyID: *p.CRMCompanyID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	polarity := map[string]string{"high": "negative", "critical": "negative", "medium": "neutral", "low": "positive"}[*p.Risk]
	if polarity == "" {
		polarity = "unknown"
	}
	value, _ := json.Marshal(map[string]any{"risk_level": *p.Risk, "score": p.Score})
	evidence, _ := json.Marshal(map[string]any{"assessment_id": p.AssessmentID})
	return q.InsertIndicator(ctx, repo.InsertIndicatorParams{TenantID: *e.TenantID, CustomerProfileID: profile, IndicatorKey: "assessment_risk", Value: value,
		Polarity: polarity, Evidence: evidence, AsOf: e.OccurredAt})
}

// onLifecycle gives a company that becomes a customer in CRM a profile here.
func (s *Service) onLifecycle(ctx context.Context, tx pgx.Tx, e events.Event) error {
	if e.TenantID == nil {
		return nil
	}
	var p struct {
		CRMCompanyID uuid.UUID `json:"crm_company_id"`
		To           string    `json:"to"`
	}
	if err := e.Decode(&p); err != nil || p.To != "customer" {
		return err
	}
	_, err := s.profileFor(ctx, repo.New(tx), *e.TenantID, p.CRMCompanyID, nil)
	return err
}

// onCompanyMerged repoints profiles to the surviving canonical company, across tenants.
func (s *Service) onCompanyMerged(ctx context.Context, _ pgx.Tx, e events.Event) error {
	var p struct {
		Source uuid.UUID `json:"source_company_id"`
		Target uuid.UUID `json:"target_company_id"`
	}
	if err := e.Decode(&p); err != nil || p.Source == uuid.Nil || p.Target == uuid.Nil {
		return fmt.Errorf("bad merge payload: %w", err)
	}
	return db.Tx(ctx, s.Pools.System, func(tx pgx.Tx) error {
		_, err := repo.New(tx).RepointProfiles(ctx, repo.RepointProfilesParams{Source: &p.Source, Target: &p.Target})
		return err
	})
}
