// Package httpapi serves Competitor Intelligence's contract. The route guard has already
// authenticated, checked the entitlement and permission, and validated the request.
package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/httpx"
	"mspengine/internal/modules/competitorintel/app"
	"mspengine/internal/modules/competitorintel/repo"
)

// Handlers implements the generated server interface.
type Handlers struct{ S *app.Service }

var _ ServerInterface = Handlers{}

func principal(r *http.Request) authctx.Principal { return authctx.PrincipalFrom(r.Context()) }

func fail(w http.ResponseWriter, r *http.Request, err error) { httpx.Error(w, r, err) }

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}

// DiscoverCompetitors handles POST /competitor-intelligence/discover.
func (h Handlers) DiscoverCompetitors(w http.ResponseWriter, r *http.Request) {
	var b Discover
	if !httpx.Decode(w, r, &b) {
		return
	}
	countries := deref(b.Countries)
	for i := range countries {
		countries[i] = strings.ToUpper(countries[i])
	}
	cs, err := h.S.Discover(r.Context(), principal(r), countries)
	if err != nil {
		fail(w, r, err)
		return
	}
	out := CandidateList{Data: []Candidate{}}
	for _, c := range cs {
		x := Candidate{DisplayName: c.DisplayName, Domain: c.Domain, LicenceClass: c.Licence}
		if c.Country != "" {
			x.Country = &c.Country
		}
		if c.Employees > 0 {
			x.Employees = &c.Employees
		}
		out.Data = append(out.Data, x)
	}
	httpx.JSON(w, http.StatusOK, out)
}

func competitorOut(c repo.GetCompetitorRow) Competitor {
	return Competitor{Id: c.ID, CompanyId: c.CompanyID, DisplayName: c.DisplayName, Domain: c.Domain, Status: CompetitorStatus(c.Status), Tier: c.Tier, Notes: c.Notes, MonitoringEnabled: c.MonitoringEnabled, UpdatedAt: c.UpdatedAt}
}

// ListCompetitors handles GET /competitor-intelligence/competitors.
func (h Handlers) ListCompetitors(w http.ResponseWriter, r *http.Request) {
	rows, err := h.S.List(r.Context(), principal(r))
	if err != nil {
		fail(w, r, err)
		return
	}
	out := CompetitorList{Data: []Competitor{}}
	for _, c := range rows {
		x := Competitor{Id: c.ID, CompanyId: c.CompanyID, DisplayName: c.DisplayName, Domain: c.Domain, Status: CompetitorStatus(c.Status), Tier: c.Tier, Notes: c.Notes,
			MonitoringEnabled: c.MonitoringEnabled, OpenChanges: int(c.OpenChanges), UpdatedAt: c.UpdatedAt}
		x.LastResearchedAt = c.LastResearchedAt
		out.Data = append(out.Data, x)
	}
	httpx.JSON(w, http.StatusOK, out)
}

// AddCompetitor handles POST /competitor-intelligence/competitors.
func (h Handlers) AddCompetitor(w http.ResponseWriter, r *http.Request) {
	var b AddCompetitor
	if !httpx.Decode(w, r, &b) {
		return
	}
	c, err := h.S.Add(r.Context(), principal(r), app.AddInput{Domain: b.Domain, DisplayName: deref(b.DisplayName), Tier: b.Tier, Notes: b.Notes, Monitoring: deref(b.MonitoringEnabled)})
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, competitorOut(c))
}

// GetCompetitor handles GET /competitor-intelligence/competitors/{id}.
func (h Handlers) GetCompetitor(w http.ResponseWriter, r *http.Request, id Id) {
	c, err := h.S.Get(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, competitorOut(c))
}

// UpdateCompetitor handles PATCH /competitor-intelligence/competitors/{id}.
func (h Handlers) UpdateCompetitor(w http.ResponseWriter, r *http.Request, id Id) {
	var b CompetitorPatch
	if !httpx.Decode(w, r, &b) {
		return
	}
	in := app.UpdateInput{Tier: b.Tier, Notes: b.Notes, DisplayName: b.DisplayName, Monitoring: b.MonitoringEnabled}
	if b.Status != nil {
		s := string(*b.Status)
		in.Status = &s
	}
	c, err := h.S.Update(r.Context(), principal(r), id, in)
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, competitorOut(c))
}

// DeleteCompetitor handles DELETE /competitor-intelligence/competitors/{id}.
func (h Handlers) DeleteCompetitor(w http.ResponseWriter, r *http.Request, id Id) {
	if err := h.S.Delete(r.Context(), principal(r), id); err != nil {
		fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListSources handles GET /competitor-intelligence/competitors/{id}/sources.
func (h Handlers) ListSources(w http.ResponseWriter, r *http.Request, id Id) {
	rows, err := h.S.Sources(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	out := SourceList{Data: []Source{}}
	for _, s := range rows {
		out.Data = append(out.Data, Source{Id: s.ID, Url: s.Url, Kind: SourceKind(s.Kind), AccessStatus: SourceAccessStatus(s.AccessStatus), LastFetchedAt: s.LastFetchedAt, CreatedAt: s.CreatedAt})
	}
	httpx.JSON(w, http.StatusOK, out)
}

// AddSource handles POST /competitor-intelligence/competitors/{id}/sources.
func (h Handlers) AddSource(w http.ResponseWriter, r *http.Request, id Id) {
	var b AddSource
	if !httpx.Decode(w, r, &b) {
		return
	}
	s, err := h.S.AddSource(r.Context(), principal(r), id, b.Url, string(b.Kind))
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, Source{Id: s.ID, Url: s.Url, Kind: SourceKind(s.Kind), AccessStatus: SourceAccessStatus(s.AccessStatus), LastFetchedAt: s.LastFetchedAt, CreatedAt: s.CreatedAt})
}

// DeleteSource handles DELETE /competitor-intelligence/sources/{id}.
func (h Handlers) DeleteSource(w http.ResponseWriter, r *http.Request, id Id) {
	if err := h.S.DeleteSource(r.Context(), principal(r), id); err != nil {
		fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetResearchConfig handles GET /competitor-intelligence/competitors/{id}/research-config.
func (h Handlers) GetResearchConfig(w http.ResponseWriter, r *http.Request, id Id) {
	c, err := h.S.Config(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, ResearchConfig{CompetitorId: c.CompetitorID, Cadence: ResearchConfigCadence(c.Cadence), Enabled: c.Enabled, NextRunAt: c.NextRunAt})
}

// PutResearchConfig handles PUT /competitor-intelligence/competitors/{id}/research-config.
func (h Handlers) PutResearchConfig(w http.ResponseWriter, r *http.Request, id Id) {
	var b ResearchConfigInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	c, err := h.S.SetConfig(r.Context(), principal(r), id, string(b.Cadence), b.Enabled)
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, ResearchConfig{CompetitorId: c.CompetitorID, Cadence: ResearchConfigCadence(c.Cadence), Enabled: c.Enabled, NextRunAt: c.NextRunAt})
}

func runOut(r repo.GetRunRow) ResearchRun {
	scheduled := r.ConfigID != nil
	return ResearchRun{Id: r.ID, CompetitorId: r.CompetitorID, Status: ResearchRunStatus(r.Status), PagesFetched: int(r.PagesFetched), JobId: r.JobID, Scheduled: &scheduled, CreatedAt: r.CreatedAt, FinishedAt: r.FinishedAt}
}

// ListResearchRuns handles GET /competitor-intelligence/competitors/{id}/research-runs.
func (h Handlers) ListResearchRuns(w http.ResponseWriter, r *http.Request, id Id) {
	rows, err := h.S.Runs(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	out := ResearchRunList{Data: []ResearchRun{}}
	for _, x := range rows {
		out.Data = append(out.Data, runOut(repo.GetRunRow(x)))
	}
	httpx.JSON(w, http.StatusOK, out)
}

// StartResearchRun handles POST /competitor-intelligence/competitors/{id}/research-runs.
func (h Handlers) StartResearchRun(w http.ResponseWriter, r *http.Request, id Id) {
	run, err := h.S.StartRun(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, runOut(run))
}

// GetResearchRun handles GET /competitor-intelligence/research-runs/{id}.
func (h Handlers) GetResearchRun(w http.ResponseWriter, r *http.Request, id Id) {
	run, err := h.S.Run(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, runOut(run))
}

// CancelResearchRun handles POST /competitor-intelligence/research-runs/{id}/cancel.
func (h Handlers) CancelResearchRun(w http.ResponseWriter, r *http.Request, id Id) {
	run, err := h.S.CancelRun(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, runOut(run))
}

// ListObservations handles GET /competitor-intelligence/competitors/{id}/observations.
func (h Handlers) ListObservations(w http.ResponseWriter, r *http.Request, id Id, params ListObservationsParams) {
	rows, err := h.S.Observations(r.Context(), principal(r), id, params.History != nil && *params.History)
	if err != nil {
		fail(w, r, err)
		return
	}
	out := ObservationList{Data: []Observation{}}
	for _, o := range rows {
		var v struct {
			Text  string `json:"text"`
			Quote string `json:"quote"`
		}
		_ = json.Unmarshal(o.Value, &v)
		x := Observation{Id: o.ID, AttributeType: o.AttributeType, Value: v.Text, EvidenceClass: EvidenceClass(o.EvidenceClass), Confidence: o.Confidence, SourceUrl: o.SourceUrl,
			SourceFetchedAt: o.SourceFetchedAt, ObservedAt: o.ObservedAt, SupersededById: o.SupersededByID}
		if v.Quote != "" {
			x.Quote = &v.Quote
		}
		out.Data = append(out.Data, x)
	}
	httpx.JSON(w, http.StatusOK, out)
}

// ListChanges handles GET /competitor-intelligence/competitors/{id}/changes.
func (h Handlers) ListChanges(w http.ResponseWriter, r *http.Request, id Id) {
	rows, err := h.S.Changes(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	out := ChangeList{Data: []Change{}}
	for _, c := range rows {
		out.Data = append(out.Data, Change{Id: c.ID, AttributeType: c.AttributeType, Summary: c.Summary, CreatedAt: c.CreatedAt, AcknowledgedAt: c.AcknowledgedAt})
	}
	httpx.JSON(w, http.StatusOK, out)
}

// AcknowledgeChange handles POST /competitor-intelligence/changes/{id}/acknowledge.
func (h Handlers) AcknowledgeChange(w http.ResponseWriter, r *http.Request, id Id) {
	if err := h.S.Acknowledge(r.Context(), principal(r), id); err != nil {
		fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type evidenceOut struct {
	ObservationID *uuid.UUID `json:"observation_id,omitempty"`
	Note          *string    `json:"note,omitempty"`
}

type opportunityOut struct {
	Opportunity
	Evidence []evidenceOut `json:"evidence"`
}

func oppOut(o repo.GetOpportunityRow, ev []repo.OpportunityEvidenceRow) opportunityOut {
	out := opportunityOut{Opportunity: Opportunity{Id: o.ID, Title: o.Title, Rationale: o.Rationale, Geography: o.Geography, IndustryId: o.IndustryID, EvidenceClass: EvidenceClass(o.EvidenceClass), Status: OpportunityStatus(o.Status), CreatedAt: o.CreatedAt}, Evidence: []evidenceOut{}}
	if o.Score != nil {
		v := int(*o.Score)
		out.Score = &v
	}
	for _, e := range ev {
		out.Evidence = append(out.Evidence, evidenceOut{ObservationID: e.ObservationID, Note: e.Note})
	}
	return out
}

// ListOpportunities handles GET /competitor-intelligence/opportunities.
func (h Handlers) ListOpportunities(w http.ResponseWriter, r *http.Request) {
	rows, err := h.S.Opportunities(r.Context(), principal(r))
	if err != nil {
		fail(w, r, err)
		return
	}
	out := struct {
		Data []opportunityOut `json:"data"`
	}{Data: []opportunityOut{}}
	for _, o := range rows {
		out.Data = append(out.Data, oppOut(repo.GetOpportunityRow(o), nil))
	}
	httpx.JSON(w, http.StatusOK, out)
}

// AddOpportunity handles POST /competitor-intelligence/opportunities.
func (h Handlers) AddOpportunity(w http.ResponseWriter, r *http.Request) {
	var b AddOpportunity
	if !httpx.Decode(w, r, &b) {
		return
	}
	id, err := h.S.AddOpportunity(r.Context(), principal(r), b.Title, b.Rationale, b.Geography, b.Industry, deref(b.ObservationIds))
	if err != nil {
		fail(w, r, err)
		return
	}
	o, err := h.S.GetOpportunity(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, oppOut(o.GetOpportunityRow, o.Evidence))
}

// UpdateOpportunity handles PATCH /competitor-intelligence/opportunities/{id}.
func (h Handlers) UpdateOpportunity(w http.ResponseWriter, r *http.Request, id Id) {
	var b OpportunityPatch
	if !httpx.Decode(w, r, &b) {
		return
	}
	o, err := h.S.SetOpportunityStatus(r.Context(), principal(r), id, string(b.Status))
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, oppOut(o.GetOpportunityRow, o.Evidence))
}

// ListComparisons handles GET /competitor-intelligence/comparisons.
func (h Handlers) ListComparisons(w http.ResponseWriter, r *http.Request) {
	rows, err := h.S.Comparisons(r.Context(), principal(r))
	if err != nil {
		fail(w, r, err)
		return
	}
	out := ComparisonList{Data: []ComparisonSummary{}}
	for _, c := range rows {
		out.Data = append(out.Data, ComparisonSummary{Id: c.ID, Name: c.Name, MemberCount: int(c.MemberCount), CreatedAt: c.CreatedAt})
	}
	httpx.JSON(w, http.StatusOK, out)
}

type memberOut struct {
	CompetitorID uuid.UUID         `json:"competitor_id"`
	DisplayName  string            `json:"display_name"`
	Facts        map[string][]Fact `json:"facts"`
}

func (h Handlers) comparison(w http.ResponseWriter, r *http.Request, id uuid.UUID, status int) {
	c, err := h.S.Comparison(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	out := struct {
		ID         uuid.UUID   `json:"id"`
		Name       string      `json:"name"`
		Dimensions []string    `json:"dimensions"`
		Members    []memberOut `json:"members"`
	}{ID: c.ID, Name: c.Name, Dimensions: c.Dimensions, Members: []memberOut{}}
	for _, m := range c.Members {
		mo := memberOut{CompetitorID: m.ID, DisplayName: m.DisplayName, Facts: map[string][]Fact{}}
		for k, fs := range m.Facts {
			for _, f := range fs {
				mo.Facts[k] = append(mo.Facts[k], Fact{Value: f.Value, EvidenceClass: EvidenceClass(f.EvidenceClass)})
			}
		}
		out.Members = append(out.Members, mo)
	}
	httpx.JSON(w, status, out)
}

// CreateComparison handles POST /competitor-intelligence/comparisons.
func (h Handlers) CreateComparison(w http.ResponseWriter, r *http.Request) {
	var b CreateComparison
	if !httpx.Decode(w, r, &b) {
		return
	}
	var dims []string
	for _, d := range deref(b.Dimensions) {
		dims = append(dims, string(d))
	}
	id, err := h.S.CreateComparison(r.Context(), principal(r), b.Name, b.CompetitorIds, dims)
	if err != nil {
		fail(w, r, err)
		return
	}
	h.comparison(w, r, id, http.StatusCreated)
}

// GetComparison handles GET /competitor-intelligence/comparisons/{id}.
func (h Handlers) GetComparison(w http.ResponseWriter, r *http.Request, id Id) {
	h.comparison(w, r, id, http.StatusOK)
}

// ListReports handles GET /competitor-intelligence/reports.
func (h Handlers) ListReports(w http.ResponseWriter, r *http.Request) {
	rows, err := h.S.Reports(r.Context(), principal(r))
	if err != nil {
		fail(w, r, err)
		return
	}
	out := ReportList{Data: []ReportSummary{}}
	for _, x := range rows {
		out.Data = append(out.Data, ReportSummary{Id: x.ID, Title: x.Title, Kind: x.Kind, Status: ReportStatus(x.Status), HasPdf: x.FileID != nil, CreatedAt: x.CreatedAt})
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h Handlers) report(w http.ResponseWriter, r *http.Request, id uuid.UUID, status int) {
	rep, err := h.S.Report(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	var c app.ReportContent
	_ = json.Unmarshal(rep.Content, &c)
	out := struct {
		ReportSummary
		app.ReportContent
	}{ReportSummary{Id: rep.ID, Title: rep.Title, Kind: rep.Kind, Status: ReportStatus(rep.Status), HasPdf: rep.FileID != nil, CreatedAt: rep.CreatedAt}, c}
	httpx.JSON(w, status, out)
}

// CreateReport handles POST /competitor-intelligence/reports.
func (h Handlers) CreateReport(w http.ResponseWriter, r *http.Request) {
	var b CreateReport
	if !httpx.Decode(w, r, &b) {
		return
	}
	title := deref(b.Title)
	if title == "" {
		title = "Competitor report"
	}
	id, err := h.S.CreateReport(r.Context(), principal(r), string(b.Kind), title, b.CompetitorIds)
	if err != nil {
		fail(w, r, err)
		return
	}
	h.report(w, r, id, http.StatusAccepted)
}

// GetReport handles GET /competitor-intelligence/reports/{id}.
func (h Handlers) GetReport(w http.ResponseWriter, r *http.Request, id Id) {
	h.report(w, r, id, http.StatusOK)
}

// DownloadReportPdf handles GET /competitor-intelligence/reports/{id}/pdf.
func (h Handlers) DownloadReportPdf(w http.ResponseWriter, r *http.Request, id Id) {
	d, err := h.S.ReportPDF(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.Attachment(w, d.Name, d.ContentType, d.Body)
}
