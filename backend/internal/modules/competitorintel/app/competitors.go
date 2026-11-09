package app

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/modules/competitorintel/repo"
	"mspengine/internal/platform/audit"
	"mspengine/internal/platform/integration"
	"mspengine/internal/platform/shared"
)

// Candidate is a suggested competitor. Nothing is saved until the user adds it.
type Candidate struct {
	DisplayName, Domain, Country, Licence string
	Employees                             int
}

// Discover suggests IT service companies in the tenant's target countries that it does not track.
func (s *Service) Discover(ctx context.Context, p authctx.Principal, countries []string) ([]Candidate, error) {
	if len(countries) == 0 {
		if icp, found, err := s.Identity.DefaultICP(ctx, p.TenantID); err == nil && found {
			countries = icp.Countries
		}
	}
	provider, providerID, err := s.Integration.CompanyData(ctx)
	if errors.Is(err, integration.ErrNoProvider) {
		return nil, apperr.Conflict("provider_unavailable", "No company data provider is connected")
	}
	if err != nil {
		return nil, err
	}
	var recs []integration.CompanyRecord
	if err := s.Integration.Call(ctx, provider.Key(), providerID, &p.TenantID, "discover_competitors", 30*time.Second, func(cctx context.Context) error {
		var err error
		recs, err = provider.SearchCompanies(cctx, integration.CompanyQuery{Countries: countries, Industries: []string{"technology"}, Limit: 50})
		return err
	}); err != nil {
		return nil, apperr.New(http.StatusBadGateway, "provider_failed", "The company data provider did not respond")
	}
	tracked, err := s.List(ctx, p)
	if err != nil {
		return nil, err
	}
	var out []Candidate
	for _, r := range recs {
		norm, err := shared.NormalizeDomain(r.Domain)
		if err != nil || slices.ContainsFunc(tracked, func(c repo.ListCompetitorsRow) bool { return c.Domain != nil && *c.Domain == norm }) {
			continue
		}
		out = append(out, Candidate{DisplayName: r.Name, Domain: norm, Country: r.Country, Employees: r.Employees, Licence: r.Licence})
	}
	return out, nil
}

// AddInput describes a new competitor.
type AddInput struct {
	Domain, DisplayName string
	Tier, Notes         *string
	Monitoring          bool
}

// Add starts tracking a competitor, with its home page as the first source. That a company is a
// competitor is private to the tenant, so only the domain reaches the shared company record.
func (s *Service) Add(ctx context.Context, p authctx.Principal, in AddInput) (repo.GetCompetitorRow, error) {
	norm, err := shared.NormalizeDomain(in.Domain)
	if err != nil {
		return repo.GetCompetitorRow{}, apperr.Validation(apperr.FieldError{Field: "domain", Code: "invalid_company_domain"})
	}
	if strings.TrimSpace(in.DisplayName) == "" {
		in.DisplayName = norm
	}
	companyID, err := s.Shared.ResolveOrCreate(ctx, shared.Signal{Domain: norm})
	if err != nil {
		return repo.GetCompetitorRow{}, err
	}
	var id uuid.UUID
	err = s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		var err error
		id, err = q.InsertCompetitor(ctx, repo.InsertCompetitorParams{TenantID: p.TenantID, CompanyID: companyID, DisplayName: in.DisplayName, Domain: &norm, Tier: in.Tier, Notes: in.Notes, MonitoringEnabled: in.Monitoring, CreatedBy: uid(p)})
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.Conflict("competitor_exists", "This company is already a competitor")
		}
		if err != nil {
			return err
		}
		if _, err := q.InsertSource(ctx, repo.InsertSourceParams{TenantID: p.TenantID, CompetitorID: id, Url: "https://" + norm + "/", Kind: "website"}); err != nil {
			return err
		}
		if err := emit(ctx, tx, p.TenantID, "competitor.designated", "competitor", id, map[string]any{"competitor_id": id, "company_id": companyID}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "competitor.added", "competitor", &id, map[string]any{"domain": norm}))
	})
	if err != nil {
		return repo.GetCompetitorRow{}, err
	}
	return s.Get(ctx, p, id)
}

// Get returns one competitor.
func (s *Service) Get(ctx context.Context, p authctx.Principal, id uuid.UUID) (repo.GetCompetitorRow, error) {
	var out repo.GetCompetitorRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.GetCompetitor(ctx, repo.GetCompetitorParams{TenantID: p.TenantID, ID: id})
		return notFound(err)
	})
	return out, err
}

// List returns the tenant's competitors.
func (s *Service) List(ctx context.Context, p authctx.Principal) ([]repo.ListCompetitorsRow, error) {
	var out []repo.ListCompetitorsRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.ListCompetitors(ctx, p.TenantID)
		return err
	})
	return out, err
}

// UpdateInput changes a competitor.
type UpdateInput struct {
	Status, Tier, Notes, DisplayName *string
	Monitoring                       *bool
}

// Update changes a competitor's details.
func (s *Service) Update(ctx context.Context, p authctx.Principal, id uuid.UUID, in UpdateInput) (repo.GetCompetitorRow, error) {
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		if _, err := q.UpdateCompetitor(ctx, repo.UpdateCompetitorParams{TenantID: p.TenantID, ID: id, Status: in.Status, Tier: in.Tier, Notes: in.Notes, MonitoringEnabled: in.Monitoring, DisplayName: in.DisplayName}); err != nil {
			return notFound(err)
		}
		return audit.Record(ctx, tx, userAudit(p, "competitor.updated", "competitor", &id, map[string]any{"status": in.Status, "monitoring": in.Monitoring}))
	})
	if err != nil {
		return repo.GetCompetitorRow{}, err
	}
	return s.Get(ctx, p, id)
}

// Delete stops tracking a competitor.
func (s *Service) Delete(ctx context.Context, p authctx.Principal, id uuid.UUID) error {
	return s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		n, err := q.DeleteCompetitor(ctx, repo.DeleteCompetitorParams{TenantID: p.TenantID, ID: id})
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.NotFound()
		}
		return audit.Record(ctx, tx, userAudit(p, "competitor.removed", "competitor", &id, nil))
	})
}

// IsCompetitor implements the contract Lead Generation uses.
func (s *Service) IsCompetitor(ctx context.Context, tenantID, companyID uuid.UUID) (bool, error) {
	var out bool
	err := s.systemTx(ctx, tenantID, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.IsCompetitor(ctx, repo.IsCompetitorParams{TenantID: tenantID, CompanyID: companyID})
		return err
	})
	return out, err
}

// Sources.

// Sources lists a competitor's sources.
func (s *Service) Sources(ctx context.Context, p authctx.Principal, competitorID uuid.UUID) ([]repo.ListSourcesRow, error) {
	var out []repo.ListSourcesRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		if _, err := q.GetCompetitor(ctx, repo.GetCompetitorParams{TenantID: p.TenantID, ID: competitorID}); err != nil {
			return notFound(err)
		}
		var err error
		out, err = q.ListSources(ctx, repo.ListSourcesParams{TenantID: p.TenantID, CompetitorID: competitorID})
		return err
	})
	return out, err
}

// AddSource adds a public page to research. The URL is checked by the Egress Guard now, and again
// at fetch time, when its address is resolved.
func (s *Service) AddSource(ctx context.Context, p authctx.Principal, competitorID uuid.UUID, rawURL, kind string) (repo.InsertSourceRow, error) {
	u, err := s.guard.CheckURL(strings.TrimSpace(rawURL))
	if err != nil {
		return repo.InsertSourceRow{}, apperr.Validation(apperr.FieldError{Field: "url", Code: "url_not_allowed"})
	}
	u.Fragment = ""
	var out repo.InsertSourceRow
	err = s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		if _, err := q.GetCompetitor(ctx, repo.GetCompetitorParams{TenantID: p.TenantID, ID: competitorID}); err != nil {
			return notFound(err)
		}
		var err error
		out, err = q.InsertSource(ctx, repo.InsertSourceParams{TenantID: p.TenantID, CompetitorID: competitorID, Url: u.String(), Kind: kind})
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.Conflict("source_exists", "This source is already listed")
		}
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "competitor.source_added", "competitor", &competitorID, map[string]any{"url": u.String(), "kind": kind}))
	})
	return out, err
}

// DeleteSource removes a source.
func (s *Service) DeleteSource(ctx context.Context, p authctx.Principal, id uuid.UUID) error {
	return s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		n, err := q.DeleteSource(ctx, repo.DeleteSourceParams{TenantID: p.TenantID, ID: id})
		if err == nil && n == 0 {
			return apperr.NotFound()
		}
		return err
	})
}

// Research configuration.

var cadences = map[string]time.Duration{"weekly": 7 * 24 * time.Hour, "monthly": 30 * 24 * time.Hour}

// Config returns a competitor's research schedule, or a manual default.
func (s *Service) Config(ctx context.Context, p authctx.Principal, competitorID uuid.UUID) (repo.GetConfigRow, error) {
	var out repo.GetConfigRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		if _, err := q.GetCompetitor(ctx, repo.GetCompetitorParams{TenantID: p.TenantID, ID: competitorID}); err != nil {
			return notFound(err)
		}
		var err error
		out, err = q.GetConfig(ctx, repo.GetConfigParams{TenantID: p.TenantID, CompetitorID: competitorID})
		if errors.Is(err, pgx.ErrNoRows) {
			out = repo.GetConfigRow{CompetitorID: competitorID, Cadence: "manual"}
			return nil
		}
		return err
	})
	return out, err
}

// SetConfig sets the schedule. An enabled schedule is due at once, then repeats at its cadence.
func (s *Service) SetConfig(ctx context.Context, p authctx.Principal, competitorID uuid.UUID, cadence string, enabled bool) (repo.UpsertConfigRow, error) {
	var next *time.Time
	if enabled && cadence != "manual" {
		next = ptr(time.Now())
	}
	var out repo.UpsertConfigRow
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		if _, err := q.GetCompetitor(ctx, repo.GetCompetitorParams{TenantID: p.TenantID, ID: competitorID}); err != nil {
			return notFound(err)
		}
		var err error
		out, err = q.UpsertConfig(ctx, repo.UpsertConfigParams{TenantID: p.TenantID, CompetitorID: competitorID, Cadence: cadence, Enabled: enabled, NextRunAt: next})
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "competitor.research_config_changed", "competitor", &competitorID, map[string]any{"cadence": cadence, "enabled": enabled}))
	})
	return out, err
}
