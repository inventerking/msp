// Package httpapi serves Visitor Intelligence's contract. The route guard has already
// authenticated, checked the entitlement and permission, and validated the request.
package httpapi

import (
	"encoding/json"
	"io"
	"net/http"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/httpx"
	"mspengine/internal/modules/visitorintel/app"
	"mspengine/internal/modules/visitorintel/repo"
)

// Handlers implements the generated server interface.
type Handlers struct{ S *app.Service }

var _ ServerInterface = Handlers{}

func principal(r *http.Request) authctx.Principal { return authctx.PrincipalFrom(r.Context()) }

func fail(w http.ResponseWriter, r *http.Request, err error) { httpx.Error(w, r, err) }

func ok(w http.ResponseWriter, status int, v any) { httpx.JSON(w, status, v) }

func done(w http.ResponseWriter, r *http.Request, err error) {
	if err != nil {
		fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func i32(p *int) *int32 {
	if p == nil {
		return nil
	}
	v := int32(*p) //nolint:gosec // Bounded by the contract.
	return &v
}

func str[T ~string](p *T) *string {
	if p == nil {
		return nil
	}
	s := string(*p)
	return &s
}

func siteOut(s repo.GetSiteRow) Site {
	origins := s.AllowedOrigins
	if origins == nil {
		origins = []string{}
	}
	return Site{Id: s.ID, Domain: s.Domain, SiteKey: s.SiteKey, AllowedOrigins: origins, ConsentMode: s.ConsentMode, RetentionDays: int(s.RetentionDays),
		IpHandling: s.IpHandling, Status: s.Status, LastEventAt: s.LastEventAt, CreatedAt: s.CreatedAt}
}

// ListSites handles GET /visitor-intelligence/sites.
func (h Handlers) ListSites(w http.ResponseWriter, r *http.Request) {
	rows, err := h.S.Sites(r.Context(), principal(r))
	if err != nil {
		fail(w, r, err)
		return
	}
	out := SiteList{Data: []Site{}}
	for _, s := range rows {
		out.Data = append(out.Data, siteOut(repo.GetSiteRow(s)))
	}
	ok(w, 200, out)
}

// CreateSite handles POST /visitor-intelligence/sites.
func (h Handlers) CreateSite(w http.ResponseWriter, r *http.Request) {
	var b SiteInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	s, err := h.S.CreateSite(r.Context(), principal(r), app.SiteInput{Domain: b.Domain, AllowedOrigins: b.AllowedOrigins, ConsentMode: str(b.ConsentMode), RetentionDays: i32(b.RetentionDays), IPHandling: str(b.IpHandling)})
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, 201, siteOut(s))
}

// GetSite handles GET /visitor-intelligence/sites/{id}.
func (h Handlers) GetSite(w http.ResponseWriter, r *http.Request, id Id) {
	s, err := h.S.Site(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, 200, siteOut(s))
}

// UpdateSite handles PATCH /visitor-intelligence/sites/{id}.
func (h Handlers) UpdateSite(w http.ResponseWriter, r *http.Request, id Id) {
	var b SitePatch
	if !httpx.Decode(w, r, &b) {
		return
	}
	s, err := h.S.UpdateSite(r.Context(), principal(r), id, app.SiteInput{AllowedOrigins: b.AllowedOrigins, ConsentMode: str(b.ConsentMode), RetentionDays: i32(b.RetentionDays), IPHandling: str(b.IpHandling), Status: str(b.Status)})
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, 200, siteOut(s))
}

// DeleteSite handles DELETE /visitor-intelligence/sites/{id}.
func (h Handlers) DeleteSite(w http.ResponseWriter, r *http.Request, id Id) {
	done(w, r, h.S.DeleteSite(r.Context(), principal(r), id))
}

// ListGoals handles GET /visitor-intelligence/sites/{id}/goals.
func (h Handlers) ListGoals(w http.ResponseWriter, r *http.Request, id Id) {
	rows, err := h.S.Goals(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	out := GoalList{Data: []Goal{}}
	for _, g := range rows {
		var rule struct{ Contains, Event string }
		_ = json.Unmarshal(g.Rule, &rule)
		value := rule.Contains
		if g.Kind == "custom_event" {
			value = rule.Event
		}
		out.Data = append(out.Data, Goal{Id: g.ID, Name: g.Name, Kind: g.Kind, Value: value, CreatedAt: g.CreatedAt})
	}
	ok(w, 200, out)
}

// CreateGoal handles POST /visitor-intelligence/sites/{id}/goals.
func (h Handlers) CreateGoal(w http.ResponseWriter, r *http.Request, id Id) {
	var b GoalInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	gid, err := h.S.CreateGoal(r.Context(), principal(r), id, b.Name, string(b.Kind), b.Value)
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, 201, Ref{Id: gid})
}

// DeleteGoal handles DELETE /visitor-intelligence/goals/{id}.
func (h Handlers) DeleteGoal(w http.ResponseWriter, r *http.Request, id Id) {
	done(w, r, h.S.DeleteGoal(r.Context(), principal(r), id))
}

// GetSiteReport handles GET /visitor-intelligence/sites/{id}/report.
func (h Handlers) GetSiteReport(w http.ResponseWriter, r *http.Request, id Id, params GetSiteReportParams) {
	days := 30
	if params.Days != nil {
		days = *params.Days
	}
	rep, err := h.S.Report(r.Context(), principal(r), id, days)
	if err != nil {
		fail(w, r, err)
		return
	}
	out := Report{Sessions: int(rep.Summary.Sessions), Visitors: int(rep.Summary.Visitors), PageViews: int(rep.Summary.PageViews), Conversions: int(rep.Summary.Conversions),
		Pages: []PageCount{}, Sources: []SourceCount{}, Daily: []DayCount{}, Companies: []CompanyVisit{}}
	for _, p := range rep.Pages {
		out.Pages = append(out.Pages, PageCount{Url: p.Url, Views: int(p.Views)})
	}
	for _, s := range rep.Sources {
		out.Sources = append(out.Sources, SourceCount{Source: deref(s.Source), Sessions: int(s.Sessions)})
	}
	for _, d := range rep.Daily {
		out.Daily = append(out.Daily, DayCount{Day: openapi_types.Date{Time: d.Day}, Sessions: int(d.Sessions)})
	}
	for _, c := range rep.Companies {
		out.Companies = append(out.Companies, CompanyVisit{CompanyId: c.CompanyID, Domain: c.Domain, Name: c.Name, Visits: int(c.Visits), LastSeen: c.LastSeen, Confidence: c.Confidence})
	}
	ok(w, 200, out)
}

// SendCompanyToCrm handles POST /visitor-intelligence/companies/{id}/send-to-crm.
func (h Handlers) SendCompanyToCrm(w http.ResponseWriter, r *http.Request, id Id) {
	crmID, err := h.S.SendToCRM(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, 200, Ref{Id: crmID})
}

// Collect handles POST /public/collect from the tracker. It accepts text/plain, which browsers send
// with navigator.sendBeacon without a preflight request, and a small body only.
func (h Handlers) Collect(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
	if err != nil {
		fail(w, r, apperr.New(http.StatusRequestEntityTooLarge, "payload_too_large", "That request is too large"))
		return
	}
	var hit app.Hit
	if err := json.Unmarshal(body, &hit); err != nil {
		fail(w, r, apperr.Validation(apperr.FieldError{Field: "body", Code: "invalid_json"}))
		return
	}
	m := authctx.MetaFrom(r.Context())
	done(w, r, h.S.Collect(r.Context(), hit, app.HitMeta{Origin: r.Header.Get("Origin"), UserAgent: r.UserAgent(), IP: m.IP}))
}

func deref(s *string) string {
	if s == nil {
		return "direct"
	}
	return *s
}
