// Package app is Visitor Intelligence's application service: sites with a tracking key, a public
// collection endpoint, reports, and business identification. Raw IP addresses are never stored.
package app

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/netip"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/crypto"
	"mspengine/internal/kernel/db"
	"mspengine/internal/kernel/events"
	crm "mspengine/internal/modules/crm/contract"
	"mspengine/internal/modules/visitorintel/repo"
	"mspengine/internal/platform/audit"
	"mspengine/internal/platform/commercial"
	"mspengine/internal/platform/shared"
)

// Module is the entitlement key.
const Module = "visitor_intelligence"

// Deps are the services Visitor Intelligence uses.
type Deps struct {
	Pools      *db.Pools
	Keys       crypto.Keys
	Commercial *commercial.Service
	Shared     *shared.Service
	Registry   crm.Registry
	// Identifier names the business behind a network. Nil means the fake one.
	Identifier Identifier
}

// Service implements Visitor Intelligence.
type Service struct {
	Deps
	hot *hotCache
}

// New creates the service.
func New(d Deps) *Service {
	if d.Identifier == nil {
		d.Identifier = FakeIdentifier{}
	}
	return &Service{Deps: d, hot: newHotCache()}
}

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

func userAudit(p authctx.Principal, action, resource string, id *uuid.UUID, after any) audit.Event {
	return audit.Event{TenantID: &p.TenantID, ActorType: "user", ActorID: &p.UserID, Action: action, ResourceType: resource, ResourceID: id, After: after}
}

// Identifier names the business behind a network address. It sees the address only in memory.
type Identifier interface {
	Key() string
	Identify(ctx context.Context, ip netip.Addr) (domain string, confidence float64, ok bool)
}

// FakeIdentifier recognises only loopback and documentation addresses (RFC 5737), so it never
// names a real visitor's employer. Real providers wait for D-05 and legal review.
type FakeIdentifier struct{}

// Key names the provider.
func (FakeIdentifier) Key() string { return "fake_identification" }

// Identify maps reserved addresses to invented companies.
func (FakeIdentifier) Identify(_ context.Context, ip netip.Addr) (string, float64, bool) {
	switch {
	case ip.IsLoopback():
		return "loopback-visitor.example", 0.6, true
	case netip.MustParsePrefix("192.0.2.0/24").Contains(ip):
		return "harbour-dental.example", 0.8, true
	case netip.MustParsePrefix("198.51.100.0/24").Contains(ip):
		return "northgate-legal.example", 0.7, true
	case netip.MustParsePrefix("203.0.113.0/24").Contains(ip):
		return "riverside-accounts.example", 0.9, true
	}
	return "", 0, false
}

// Sites.

// SiteInput is a new site or the fields to change.
type SiteInput struct {
	Domain         string
	AllowedOrigins *[]string
	ConsentMode    *string
	RetentionDays  *int32
	IPHandling     *string
	Status         *string
}

func normalizeOrigins(in []string) ([]string, error) {
	out := make([]string, 0, len(in))
	for _, o := range in {
		o = strings.TrimRight(strings.ToLower(strings.TrimSpace(o)), "/")
		h, ok := strings.CutPrefix(o, "https://")
		if !ok {
			h, ok = strings.CutPrefix(o, "http://")
		}
		if !ok || h == "" || strings.ContainsAny(h, "/?#@ ") {
			return nil, apperr.Validation(apperr.FieldError{Field: "allowed_origins", Code: "invalid_origin"})
		}
		out = append(out, o)
	}
	return out, nil
}

func newSiteKey() (string, error) {
	b := make([]byte, 18)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "vi_" + base64.RawURLEncoding.EncodeToString(b), nil
}

// CreateSite adds a website to track. Its key is public: it goes in the page's script tag.
func (s *Service) CreateSite(ctx context.Context, p authctx.Principal, in SiteInput) (repo.GetSiteRow, error) {
	d, err := shared.NormalizeDomain(in.Domain)
	if err != nil {
		return repo.GetSiteRow{}, apperr.Validation(apperr.FieldError{Field: "domain", Code: "invalid_company_domain"})
	}
	origins := []string{"https://" + d, "https://www." + d}
	if in.AllowedOrigins != nil {
		if origins, err = normalizeOrigins(*in.AllowedOrigins); err != nil {
			return repo.GetSiteRow{}, err
		}
	}
	mode, retention, ipHandling := "opt_in", int32(395), "discard"
	if in.ConsentMode != nil {
		mode = *in.ConsentMode
	}
	if in.RetentionDays != nil {
		retention = *in.RetentionDays
	}
	if in.IPHandling != nil {
		ipHandling = *in.IPHandling
	}
	key, err := newSiteKey()
	if err != nil {
		return repo.GetSiteRow{}, err
	}
	var id uuid.UUID
	err = s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		var err error
		id, err = q.InsertSite(ctx, repo.InsertSiteParams{TenantID: p.TenantID, Domain: d, SiteKey: key, AllowedOrigins: origins, ConsentMode: mode, RetentionDays: retention, IpHandling: ipHandling})
		if err != nil {
			if strings.Contains(err.Error(), "23505") {
				return apperr.Conflict("site_exists", "This website is already added")
			}
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "visitor.site_created", "site", &id, map[string]any{"domain": d, "consent_mode": mode, "ip_handling": ipHandling}))
	})
	if err != nil {
		return repo.GetSiteRow{}, err
	}
	return s.Site(ctx, p, id)
}

// Site returns one site.
func (s *Service) Site(ctx context.Context, p authctx.Principal, id uuid.UUID) (repo.GetSiteRow, error) {
	var out repo.GetSiteRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.GetSite(ctx, repo.GetSiteParams{TenantID: p.TenantID, ID: id})
		return notFound(err)
	})
	return out, err
}

// Sites lists sites.
func (s *Service) Sites(ctx context.Context, p authctx.Principal) ([]repo.ListSitesRow, error) {
	var out []repo.ListSitesRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.ListSites(ctx, p.TenantID)
		return err
	})
	return out, err
}

// UpdateSite changes a site's settings.
func (s *Service) UpdateSite(ctx context.Context, p authctx.Principal, id uuid.UUID, in SiteInput) (repo.GetSiteRow, error) {
	var origins []string
	if in.AllowedOrigins != nil {
		var err error
		if origins, err = normalizeOrigins(*in.AllowedOrigins); err != nil {
			return repo.GetSiteRow{}, err
		}
	}
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		n, err := q.UpdateSite(ctx, repo.UpdateSiteParams{TenantID: p.TenantID, ID: id, AllowedOrigins: origins, ConsentMode: in.ConsentMode, RetentionDays: in.RetentionDays, IpHandling: in.IPHandling, Status: in.Status})
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.NotFound()
		}
		return audit.Record(ctx, tx, userAudit(p, "visitor.site_updated", "site", &id, map[string]any{"allowed_origins": origins, "consent_mode": in.ConsentMode, "retention_days": in.RetentionDays, "ip_handling": in.IPHandling, "status": in.Status}))
	})
	if err != nil {
		return repo.GetSiteRow{}, err
	}
	return s.Site(ctx, p, id)
}

// DeleteSite stops tracking a site. Its key stops working at once.
func (s *Service) DeleteSite(ctx context.Context, p authctx.Principal, id uuid.UUID) error {
	return s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		n, err := q.DeleteSite(ctx, repo.DeleteSiteParams{TenantID: p.TenantID, ID: id})
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.NotFound()
		}
		return audit.Record(ctx, tx, userAudit(p, "visitor.site_deleted", "site", &id, nil))
	})
}

// Goals.

// Goal kinds and their rules: url_match {"contains": "/thanks"}; custom_event {"event": "demo_booked"}.
var goalKinds = map[string]string{"url_match": "contains", "custom_event": "event"}

// CreateGoal adds a conversion goal to a site.
func (s *Service) CreateGoal(ctx context.Context, p authctx.Principal, site uuid.UUID, name, kind, value string) (uuid.UUID, error) {
	field, ok := goalKinds[kind]
	if !ok || strings.TrimSpace(value) == "" {
		return uuid.Nil, apperr.Validation(apperr.FieldError{Field: "kind", Code: "invalid"})
	}
	rule := []byte(`{"` + field + `":` + jsonString(value) + `}`)
	var id uuid.UUID
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		if _, err := q.GetSite(ctx, repo.GetSiteParams{TenantID: p.TenantID, ID: site}); err != nil {
			return notFound(err)
		}
		var err error
		id, err = q.InsertGoal(ctx, repo.InsertGoalParams{TenantID: p.TenantID, SiteID: site, Name: name, Kind: kind, Rule: rule})
		return err
	})
	s.hot.forgetGoals()
	return id, err
}

// Goals lists a site's goals.
func (s *Service) Goals(ctx context.Context, p authctx.Principal, site uuid.UUID) ([]repo.ListGoalsRow, error) {
	var out []repo.ListGoalsRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		if _, err := q.GetSite(ctx, repo.GetSiteParams{TenantID: p.TenantID, ID: site}); err != nil {
			return notFound(err)
		}
		var err error
		out, err = q.ListGoals(ctx, repo.ListGoalsParams{TenantID: p.TenantID, SiteID: site})
		return err
	})
	return out, err
}

// DeleteGoal removes a goal. Conversions already recorded stay.
func (s *Service) DeleteGoal(ctx context.Context, p authctx.Principal, id uuid.UUID) error {
	defer s.hot.forgetGoals()
	return s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		n, err := q.DeleteGoal(ctx, repo.DeleteGoalParams{TenantID: p.TenantID, ID: id})
		if err == nil && n == 0 {
			return apperr.NotFound()
		}
		return err
	})
}

// Reports.

// Report summarises a site over a number of days.
type Report struct {
	Summary   repo.SiteSummaryRow
	Pages     []repo.TopPagesRow
	Sources   []repo.TopSourcesRow
	Daily     []repo.DailyCountsRow
	Companies []Company
}

// Company is an identified business, with its domain.
type Company struct {
	repo.IdentifiedCompaniesRow
	Domain string
	Name   string
}

// Report reads a site's figures.
func (s *Service) Report(ctx context.Context, p authctx.Principal, site uuid.UUID, days int) (Report, error) {
	if days < 1 || days > 366 {
		days = 30
	}
	since := time.Now().AddDate(0, 0, -days)
	var r Report
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		if _, err := q.GetSite(ctx, repo.GetSiteParams{TenantID: p.TenantID, ID: site}); err != nil {
			return notFound(err)
		}
		var err error
		if r.Summary, err = q.SiteSummary(ctx, repo.SiteSummaryParams{TenantID: p.TenantID, SiteID: site, Since: since}); err != nil {
			return err
		}
		if r.Pages, err = q.TopPages(ctx, repo.TopPagesParams{TenantID: p.TenantID, SiteID: site, Since: since}); err != nil {
			return err
		}
		if r.Sources, err = q.TopSources(ctx, repo.TopSourcesParams{TenantID: p.TenantID, SiteID: site, Since: since}); err != nil {
			return err
		}
		if r.Daily, err = q.DailyCounts(ctx, repo.DailyCountsParams{TenantID: p.TenantID, SiteID: site, Since: since}); err != nil {
			return err
		}
		ids, err := q.IdentifiedCompanies(ctx, repo.IdentifiedCompaniesParams{TenantID: p.TenantID, SiteID: site, Since: since})
		for _, c := range ids {
			r.Companies = append(r.Companies, Company{IdentifiedCompaniesRow: c})
		}
		return err
	})
	if err != nil {
		return r, err
	}
	for i, c := range r.Companies {
		if row, err := s.Shared.GetCompany(ctx, c.CompanyID); err == nil {
			r.Companies[i].Domain = deref(row.PrimaryDomain)
			r.Companies[i].Name = row.DisplayName
		}
	}
	return r, nil
}

// SendToCRM adds an identified company to CRM, through the registry, and records the visits on
// its timeline.
func (s *Service) SendToCRM(ctx context.Context, p authctx.Principal, company uuid.UUID) (uuid.UUID, error) {
	var visits int32
	if err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		visits, err = q.CompanyVisits(ctx, repo.CompanyVisitsParams{TenantID: p.TenantID, CompanyID: company})
		return err
	}); err != nil {
		return uuid.Nil, err
	}
	if visits == 0 {
		return uuid.Nil, apperr.NotFound() // Only companies this tenant's sites identified.
	}
	row, err := s.Shared.GetCompany(ctx, company)
	if err != nil {
		return uuid.Nil, err
	}
	name := row.DisplayName
	if name == "" {
		name = deref(row.PrimaryDomain)
	}
	crmID, err := s.Registry.EnsureCompany(ctx, crm.CompanyInput{TenantID: p.TenantID, CompanyID: &company, Name: name, Domain: row.PrimaryDomain, SourceModule: Module, ActorID: &p.UserID})
	if err != nil {
		return uuid.Nil, err
	}
	err = s.tx(ctx, p, func(_ *repo.Queries, tx pgx.Tx) error {
		if err := events.Append(ctx, tx, events.Event{Type: "visitor_intelligence.company.sent", TenantID: &p.TenantID, SourceModule: Module, AggregateType: "company", AggregateID: company},
			map[string]any{"crm_company_id": crmID, "title": itoa(int(visits)) + " identified visits"}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "visitor.company_sent_to_crm", "company", &company, map[string]any{"crm_company_id": crmID}))
	})
	return crmID, err
}

// Name identifies Visitor Intelligence to the erasure job.
func (s *Service) Name() string { return Module }

// EraseContact unlinks the person from their visitor records. The records hold no personal details
// beyond a keyed hash of a random browser ID.
func (s *Service) EraseContact(ctx context.Context, tenantID, contactID uuid.UUID) error {
	return s.systemTx(ctx, tenantID, func(q *repo.Queries, _ pgx.Tx) error {
		return q.UnlinkContact(ctx, repo.UnlinkContactParams{TenantID: tenantID, ContactID: &contactID})
	})
}

// ExportContact lists the visitor records linked to the person.
func (s *Service) ExportContact(ctx context.Context, tenantID, contactID uuid.UUID) (any, error) {
	var ids []uuid.UUID
	err := s.systemTx(ctx, tenantID, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		ids, err = q.VisitorsForContact(ctx, repo.VisitorsForContactParams{TenantID: tenantID, ContactID: &contactID})
		return err
	})
	return map[string]any{"visitor_ids": ids}, err
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
