package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/jobs"
	"mspengine/internal/modules/leadgeneration/repo"
	"mspengine/internal/platform/audit"
	"mspengine/internal/platform/integration"
	"mspengine/internal/platform/shared"
)

// Criteria is what a search looks for.
type Criteria struct {
	Countries    []string `json:"countries,omitempty"`
	Industries   []string `json:"industries,omitempty"`
	EmployeeMin  int      `json:"employee_min,omitempty"`
	EmployeeMax  int      `json:"employee_max,omitempty"`
	Technologies []string `json:"technologies,omitempty"`
}

// Preview is what a search result shows before it is saved. It may hold tenant-licensed data, so it
// stays in this tenant's rows.
type Preview struct {
	DisplayName string   `json:"display_name"`
	Domain      string   `json:"domain,omitempty"`
	Country     string   `json:"country,omitempty"`
	Industry    string   `json:"industry,omitempty"`
	Employees   int      `json:"employees,omitempty"`
	Technology  []string `json:"technology,omitempty"`
	Licence     string   `json:"licence_class"`
}

type searchPayload struct {
	SearchID      uuid.UUID `json:"search_id"`
	ReservationID uuid.UUID `json:"reservation_id"`
	MaxResults    int       `json:"max_results"`
}

// searchNamespace makes a search ID from a tenant's idempotency key, so a repeated request finds
// the search it already started instead of charging twice.
var searchNamespace = uuid.MustParse("0b6f5f43-5d0c-4bd5-9a4e-3f7b0c3c9d10")

// StartSearch reserves credits and queues a search.
func (s *Service) StartSearch(ctx context.Context, p authctx.Principal, c Criteria, savedSearchID *uuid.UUID, maxResults int, idempotencyKey string) (repo.GetSearchJobRow, error) {
	if c.EmployeeMin > 0 && c.EmployeeMax > 0 && c.EmployeeMin > c.EmployeeMax {
		return repo.GetSearchJobRow{}, apperr.Validation(apperr.FieldError{Field: "criteria.employee_min", Code: "min_above_max"})
	}
	if maxResults <= 0 {
		maxResults = 25
	}
	id := uuid.NewSHA1(searchNamespace, []byte(p.TenantID.String()+"|"+idempotencyKey))
	var existing repo.GetSearchJobRow
	found := false
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		if err := shared.CheckCodes(ctx, tx, c.Countries, c.Industries); err != nil {
			return err
		}
		if savedSearchID != nil {
			if _, err := q.GetSavedSearch(ctx, repo.GetSavedSearchParams{TenantID: p.TenantID, ID: *savedSearchID}); err != nil {
				return notFound(err)
			}
		}
		row, err := q.GetSearchJob(ctx, repo.GetSearchJobParams{TenantID: p.TenantID, ID: id})
		if err == nil {
			existing, found = row, true
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		return nil
	})
	if err != nil || found {
		return existing, err
	}
	reserve := int64(maxResults * CreditsPerResult)
	reservation, err := s.Commercial.Reserve(ctx, p.TenantID, creditMeter, reserve, Module, 30*time.Minute)
	if err != nil {
		return repo.GetSearchJobRow{}, err
	}
	created := false
	err = s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		n, err := q.InsertSearchJob(ctx, repo.InsertSearchJobParams{ID: id, TenantID: p.TenantID, SavedSearchID: savedSearchID, CriteriaSnapshot: mustJSON(c), CreditsReserved: reserve, CreatedBy: uid(p)})
		if err != nil || n == 0 {
			return err // n == 0: a concurrent request with the same key won.
		}
		created = true
		jobID, _, err := jobs.Enqueue(ctx, tx, jobs.Spec{TenantID: &p.TenantID, Kind: JobSearch, Queue: jobs.QueueProviderIO, MaxAttempts: 3,
			Payload: searchPayload{SearchID: id, ReservationID: reservation, MaxResults: maxResults}, IdempotencyKey: "search:" + id.String()})
		if err != nil {
			return err
		}
		if err := q.SetSearchJobID(ctx, repo.SetSearchJobIDParams{TenantID: p.TenantID, ID: id, JobID: &jobID}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "lead.search_started", "search_job", &id, map[string]any{"criteria": c, "max_results": maxResults}))
	})
	if err != nil || !created {
		if rerr := s.Commercial.Release(ctx, p.TenantID, reservation); rerr != nil {
			slog.WarnContext(ctx, "release search reservation failed", "error", rerr)
		}
	}
	if err != nil {
		return repo.GetSearchJobRow{}, err
	}
	return s.GetSearch(ctx, p, id)
}

// GetSearch returns one search.
func (s *Service) GetSearch(ctx context.Context, p authctx.Principal, id uuid.UUID) (repo.GetSearchJobRow, error) {
	var out repo.GetSearchJobRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.GetSearchJob(ctx, repo.GetSearchJobParams{TenantID: p.TenantID, ID: id})
		return notFound(err)
	})
	return out, err
}

// ListSearches returns search history, newest first.
func (s *Service) ListSearches(ctx context.Context, p authctx.Principal, limit int32, before *uuid.UUID) ([]repo.ListSearchJobsRow, error) {
	var out []repo.ListSearchJobsRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.ListSearchJobs(ctx, repo.ListSearchJobsParams{TenantID: p.TenantID, Before: before, Lim: limit})
		return err
	})
	return out, err
}

// CancelSearch stops a search that has not finished. Its reserved credits are released by the job
// when it sees the cancellation, or by the reservation sweep.
func (s *Service) CancelSearch(ctx context.Context, p authctx.Principal, id uuid.UUID) (repo.GetSearchJobRow, error) {
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		jobID, err := q.CancelSearchJob(ctx, repo.CancelSearchJobParams{TenantID: p.TenantID, ID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			if _, err := q.GetSearchJob(ctx, repo.GetSearchJobParams{TenantID: p.TenantID, ID: id}); err != nil {
				return notFound(err)
			}
			return apperr.Conflict("search_finished", "This search has already finished")
		}
		if err != nil {
			return err
		}
		if jobID != nil {
			if _, err := jobs.RequestCancel(ctx, tx, *jobID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
		}
		return audit.Record(ctx, tx, userAudit(p, "lead.search_cancelled", "search_job", &id, nil))
	})
	if err != nil {
		return repo.GetSearchJobRow{}, err
	}
	return s.GetSearch(ctx, p, id)
}

// SearchResult is one result with its preview decoded.
type SearchResult struct {
	CompanyID  uuid.UUID
	Rank       int32
	SourceKey  string
	Preview    Preview
	ProspectID *uuid.UUID
}

// Results lists a search's results.
func (s *Service) Results(ctx context.Context, p authctx.Principal, searchID uuid.UUID) ([]SearchResult, error) {
	var out []SearchResult
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		if _, err := q.GetSearchJob(ctx, repo.GetSearchJobParams{TenantID: p.TenantID, ID: searchID}); err != nil {
			return notFound(err)
		}
		rows, err := q.ListSearchResults(ctx, repo.ListSearchResultsParams{TenantID: p.TenantID, SearchJobID: searchID})
		if err != nil {
			return err
		}
		for _, r := range rows {
			var pv Preview
			if err := json.Unmarshal(r.Preview, &pv); err != nil {
				return err
			}
			out = append(out, SearchResult{CompanyID: r.CompanyID, Rank: r.Rank, SourceKey: r.SourceKey, Preview: pv, ProspectID: r.PromotedProspectID})
		}
		return nil
	})
	return out, err
}

// HandleSearch runs a search against the company data provider.
func (s *Service) HandleSearch(ctx context.Context, j jobs.Job) (err error) {
	var pl searchPayload
	defer func() {
		if err != nil && j.Final(err) && j.TenantID != nil && pl.SearchID != uuid.Nil {
			tenant := *j.TenantID
			bg := context.WithoutCancel(ctx)
			if ferr := s.systemTx(bg, tenant, func(q *repo.Queries, _ pgx.Tx) error {
				return q.FailSearchIfOpen(bg, repo.FailSearchIfOpenParams{TenantID: tenant, ID: pl.SearchID})
			}); ferr != nil {
				slog.WarnContext(bg, "record failed search", "error", ferr)
			}
			s.release(bg, tenant, pl.ReservationID)
		}
	}()
	if err := j.Decode(&pl); err != nil {
		return err
	}
	if j.TenantID == nil {
		return jobs.Permanent(errors.New("search job without tenant"))
	}
	tenant := *j.TenantID
	var crit Criteria
	proceed := false
	err = s.systemTx(ctx, tenant, func(q *repo.Queries, _ pgx.Tx) error {
		row, err := q.GetSearchJob(ctx, repo.GetSearchJobParams{TenantID: tenant, ID: pl.SearchID})
		if err != nil {
			return err
		}
		if err := json.Unmarshal(row.CriteriaSnapshot, &crit); err != nil {
			return err
		}
		switch row.Status {
		case "queued":
			_, err = q.StartSearchJob(ctx, repo.StartSearchJobParams{TenantID: tenant, ID: pl.SearchID})
			proceed = true
			return err
		case "running":
			proceed = true // A retry after a crash.
		}
		return nil
	})
	if err != nil {
		return err
	}
	if !proceed {
		s.release(ctx, tenant, pl.ReservationID)
		return nil
	}
	provider, providerID, err := s.Integration.CompanyData(ctx)
	if err != nil {
		return s.failSearch(ctx, tenant, pl, "provider_unavailable", jobs.Permanent(err))
	}
	var recs []integration.CompanyRecord
	err = s.Integration.Call(ctx, provider.Key(), providerID, &tenant, "search_companies", 30*time.Second, func(cctx context.Context) error {
		var err error
		recs, err = provider.SearchCompanies(cctx, integration.CompanyQuery{Countries: crit.Countries, Industries: crit.Industries, EmployeeMin: crit.EmployeeMin, EmployeeMax: crit.EmployeeMax, Technology: crit.Technologies, Limit: pl.MaxResults})
		return err
	})
	if err != nil {
		if j.Attempt >= 3 {
			return s.failSearch(ctx, tenant, pl, "provider_failed", jobs.Permanent(err))
		}
		return integration.ToJobError(err)
	}
	if len(recs) > pl.MaxResults {
		recs = recs[:pl.MaxResults]
	}
	type resolved struct {
		company uuid.UUID
		preview Preview
	}
	var results []resolved
	skipped := 0
	for _, r := range recs {
		id, err := s.Shared.ResolveOrCreate(ctx, canonicalSignal(provider.Key(), r))
		if err != nil {
			skipped++
			slog.WarnContext(ctx, "search result could not be resolved", "error", err)
			continue
		}
		results = append(results, resolved{id, Preview{DisplayName: r.Name, Domain: r.Domain, Country: r.Country, Industry: r.Industry, Employees: r.Employees, Technology: r.Technology, Licence: r.Licence}})
	}
	used := int64(len(results) * CreditsPerResult)
	status := "completed"
	if skipped > 0 {
		status = "partial"
	}
	cancelled := false
	err = s.systemTx(ctx, tenant, func(q *repo.Queries, tx pgx.Tx) error {
		row, err := q.GetSearchJob(ctx, repo.GetSearchJobParams{TenantID: tenant, ID: pl.SearchID})
		if err != nil {
			return err
		}
		if row.Status != "running" {
			cancelled = true
			return nil
		}
		for i, r := range results {
			if err := q.InsertSearchResult(ctx, repo.InsertSearchResultParams{TenantID: tenant, SearchJobID: pl.SearchID, CompanyID: r.company, Rank: int32(i + 1), SourceKey: provider.Key(), Preview: mustJSON(r.preview)}); err != nil { //nolint:gosec // Bounded by max_results.
				return err
			}
		}
		if err := q.FinishSearchJob(ctx, repo.FinishSearchJobParams{TenantID: tenant, ID: pl.SearchID, Status: status, ResultCount: int32(len(results)), CreditsUsed: used, ProviderKeys: []string{provider.Key()}}); err != nil { //nolint:gosec // Bounded.
			return err
		}
		return emit(ctx, tx, tenant, "search.completed", "search_job", pl.SearchID, map[string]any{"search_id": pl.SearchID, "result_count": len(results), "status": status})
	})
	if err != nil {
		return err
	}
	if cancelled {
		s.release(ctx, tenant, pl.ReservationID)
		return nil
	}
	if err := s.Commercial.Commit(ctx, tenant, pl.ReservationID, used); err != nil {
		slog.ErrorContext(ctx, "commit search credits failed", "error", err) // The sweep releases it: the tenant is never overcharged.
	}
	return nil
}

func (s *Service) failSearch(ctx context.Context, tenant uuid.UUID, pl searchPayload, code string, cause error) error {
	err := s.systemTx(ctx, tenant, func(q *repo.Queries, tx pgx.Tx) error {
		if err := q.FinishSearchJob(ctx, repo.FinishSearchJobParams{TenantID: tenant, ID: pl.SearchID, Status: "failed", ProviderKeys: []string{}}); err != nil {
			return err
		}
		return emit(ctx, tx, tenant, "search.failed", "search_job", pl.SearchID, map[string]any{"search_id": pl.SearchID, "code": code})
	})
	s.release(ctx, tenant, pl.ReservationID)
	if err != nil {
		return fmt.Errorf("record failed search: %w", err)
	}
	return cause
}

func (s *Service) release(ctx context.Context, tenant, reservation uuid.UUID) {
	if reservation == uuid.Nil {
		return
	}
	if err := s.Commercial.Release(ctx, tenant, reservation); err != nil {
		slog.WarnContext(ctx, "release reservation failed", "error", err)
	}
}

// canonicalSignal builds what may enter the shared company record. A name or fact licensed to one
// tenant must not, so for those only the domain and provider ID go in.
func canonicalSignal(provider string, r integration.CompanyRecord) shared.Signal {
	sig := shared.Signal{Domain: r.Domain, ProviderID: r.ProviderID, Provider: provider}
	if r.Licence != shared.LicencePublic && r.Licence != shared.LicenceShareable {
		return sig
	}
	sig.SourceName = r.Name
	now := time.Now()
	add := func(attr string, v any) {
		sig.Facts = append(sig.Facts, shared.Fact{Attribute: attr, Value: v, SourceKey: provider, Licence: r.Licence, Confidence: 0.8, ObservedAt: now})
	}
	if r.Country != "" {
		add("country", r.Country)
	}
	if r.Industry != "" {
		add("industry", r.Industry)
	}
	if r.Employees > 0 {
		add("employees", r.Employees)
	}
	return sig
}

// Saved searches.

// ListSavedSearches returns the tenant's saved searches.
func (s *Service) ListSavedSearches(ctx context.Context, p authctx.Principal) ([]repo.ListSavedSearchesRow, error) {
	var out []repo.ListSavedSearchesRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.ListSavedSearches(ctx, p.TenantID)
		return err
	})
	return out, err
}

// CreateSavedSearch saves criteria under a name.
func (s *Service) CreateSavedSearch(ctx context.Context, p authctx.Principal, name string, c Criteria) (repo.InsertSavedSearchRow, error) {
	var out repo.InsertSavedSearchRow
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		if err := shared.CheckCodes(ctx, tx, c.Countries, c.Industries); err != nil {
			return err
		}
		var err error
		out, err = q.InsertSavedSearch(ctx, repo.InsertSavedSearchParams{TenantID: p.TenantID, Name: name, Criteria: mustJSON(c), CreatedBy: uid(p)})
		return err
	})
	return out, err
}

// UpdateSavedSearch renames or changes a saved search.
func (s *Service) UpdateSavedSearch(ctx context.Context, p authctx.Principal, id uuid.UUID, name *string, c *Criteria) (repo.UpdateSavedSearchRow, error) {
	var out repo.UpdateSavedSearchRow
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		var crit []byte
		if c != nil {
			if err := shared.CheckCodes(ctx, tx, c.Countries, c.Industries); err != nil {
				return err
			}
			crit = mustJSON(c)
		}
		var err error
		out, err = q.UpdateSavedSearch(ctx, repo.UpdateSavedSearchParams{TenantID: p.TenantID, ID: id, Name: name, Criteria: crit})
		return notFound(err)
	})
	return out, err
}

// DeleteSavedSearch removes a saved search.
func (s *Service) DeleteSavedSearch(ctx context.Context, p authctx.Principal, id uuid.UUID) error {
	return s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		n, err := q.DeleteSavedSearch(ctx, repo.DeleteSavedSearchParams{TenantID: p.TenantID, ID: id})
		if err == nil && n == 0 {
			return apperr.NotFound()
		}
		return err
	})
}
