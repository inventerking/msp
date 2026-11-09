// Package httpapi serves Lead Generation's contract. The route guard has already authenticated,
// checked the entitlement and permission, and validated the request before a handler runs.
package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/httpx"
	"mspengine/internal/modules/leadgeneration/app"
	"mspengine/internal/modules/leadgeneration/domain"
	"mspengine/internal/modules/leadgeneration/repo"
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

func criteriaIn(c Criteria) app.Criteria {
	return app.Criteria{Countries: upper(deref(c.Countries)), Industries: deref(c.Industries), EmployeeMin: deref(c.EmployeeMin), EmployeeMax: deref(c.EmployeeMax), Technologies: deref(c.Technologies)}
}

func upper(s []string) []string {
	out := make([]string, len(s))
	for i, v := range s {
		out[i] = strings.ToUpper(v)
	}
	return out
}

func criteriaOut(raw []byte) Criteria {
	var c app.Criteria
	_ = json.Unmarshal(raw, &c)
	out := Criteria{}
	if len(c.Countries) > 0 {
		out.Countries = &c.Countries
	}
	if len(c.Industries) > 0 {
		out.Industries = &c.Industries
	}
	if len(c.Technologies) > 0 {
		out.Technologies = &c.Technologies
	}
	if c.EmployeeMin > 0 {
		out.EmployeeMin = &c.EmployeeMin
	}
	if c.EmployeeMax > 0 {
		out.EmployeeMax = &c.EmployeeMax
	}
	return out
}

func searchOut(s repo.GetSearchJobRow) Search {
	return Search{Id: s.ID, Criteria: criteriaOut(s.CriteriaSnapshot), Status: SearchStatus(s.Status), ProgressPercent: int(s.ProgressPercent), ResultCount: int(s.ResultCount),
		CreditsReserved: s.CreditsReserved, CreditsUsed: s.CreditsUsed, JobId: s.JobID, SavedSearchId: s.SavedSearchID, CreatedAt: s.CreatedAt}
}

// StartSearch handles POST /lead-generation/searches.
func (h Handlers) StartSearch(w http.ResponseWriter, r *http.Request, params StartSearchParams) {
	var b StartSearch
	if !httpx.Decode(w, r, &b) {
		return
	}
	s, err := h.S.StartSearch(r.Context(), principal(r), criteriaIn(b.Criteria), b.SavedSearchId, deref(b.MaxResults), params.IdempotencyKey)
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, searchOut(s))
}

// ListSearches handles GET /lead-generation/searches.
func (h Handlers) ListSearches(w http.ResponseWriter, r *http.Request, params ListSearchesParams) {
	limit, before, err := httpx.Page(params.Limit, params.Cursor)
	if err != nil {
		fail(w, r, err)
		return
	}
	rows, err := h.S.ListSearches(r.Context(), principal(r), limit+1, before)
	if err != nil {
		fail(w, r, err)
		return
	}
	out := SearchList{Data: []Search{}}
	for i, s := range rows {
		if int32(i) == limit { //nolint:gosec // Small.
			next := rows[i-1].ID.String()
			out.NextCursor = &next
			break
		}
		out.Data = append(out.Data, searchOut(repo.GetSearchJobRow(s)))
	}
	httpx.JSON(w, http.StatusOK, out)
}

// GetSearch handles GET /lead-generation/searches/{id}.
func (h Handlers) GetSearch(w http.ResponseWriter, r *http.Request, id Id) {
	s, err := h.S.GetSearch(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, searchOut(s))
}

// CancelSearch handles POST /lead-generation/searches/{id}/cancel.
func (h Handlers) CancelSearch(w http.ResponseWriter, r *http.Request, id Id) {
	s, err := h.S.CancelSearch(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, searchOut(s))
}

func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ListSearchResults handles GET /lead-generation/searches/{id}/results.
func (h Handlers) ListSearchResults(w http.ResponseWriter, r *http.Request, id Id) {
	rows, err := h.S.Results(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	out := SearchResultList{Data: []SearchResult{}}
	for _, x := range rows {
		res := SearchResult{CompanyId: x.CompanyID, Rank: int(x.Rank), SourceKey: x.SourceKey, DisplayName: x.Preview.DisplayName, Domain: nonEmpty(x.Preview.Domain),
			Country: nonEmpty(x.Preview.Country), Industry: nonEmpty(x.Preview.Industry), LicenceClass: x.Preview.Licence, ProspectId: x.ProspectID}
		if x.Preview.Employees > 0 {
			res.Employees = &x.Preview.Employees
		}
		out.Data = append(out.Data, res)
	}
	httpx.JSON(w, http.StatusOK, out)
}

// PromoteResults handles POST /lead-generation/searches/{id}/results/promote.
func (h Handlers) PromoteResults(w http.ResponseWriter, r *http.Request, id Id) {
	var b Promote
	if !httpx.Decode(w, r, &b) {
		return
	}
	res, err := h.S.Promote(r.Context(), principal(r), id, deref(b.CompanyIds), deref(b.All))
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, PromoteResult{Created: res.Created, Existing: res.Existing, ProspectIds: res.ProspectIDs})
}

// ListSavedSearches handles GET /lead-generation/saved-searches.
func (h Handlers) ListSavedSearches(w http.ResponseWriter, r *http.Request) {
	rows, err := h.S.ListSavedSearches(r.Context(), principal(r))
	if err != nil {
		fail(w, r, err)
		return
	}
	out := SavedSearchList{Data: []SavedSearch{}}
	for _, s := range rows {
		out.Data = append(out.Data, SavedSearch{Id: s.ID, Name: s.Name, Criteria: criteriaOut(s.Criteria), UpdatedAt: s.UpdatedAt})
	}
	httpx.JSON(w, http.StatusOK, out)
}

// CreateSavedSearch handles POST /lead-generation/saved-searches.
func (h Handlers) CreateSavedSearch(w http.ResponseWriter, r *http.Request) {
	var b SavedSearchInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	s, err := h.S.CreateSavedSearch(r.Context(), principal(r), b.Name, criteriaIn(b.Criteria))
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, SavedSearch{Id: s.ID, Name: s.Name, Criteria: criteriaOut(s.Criteria), UpdatedAt: s.UpdatedAt})
}

// UpdateSavedSearch handles PATCH /lead-generation/saved-searches/{id}.
func (h Handlers) UpdateSavedSearch(w http.ResponseWriter, r *http.Request, id Id) {
	var b SavedSearchPatch
	if !httpx.Decode(w, r, &b) {
		return
	}
	var c *app.Criteria
	if b.Criteria != nil {
		x := criteriaIn(*b.Criteria)
		c = &x
	}
	s, err := h.S.UpdateSavedSearch(r.Context(), principal(r), id, b.Name, c)
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, SavedSearch{Id: s.ID, Name: s.Name, Criteria: criteriaOut(s.Criteria), UpdatedAt: s.UpdatedAt})
}

// DeleteSavedSearch handles DELETE /lead-generation/saved-searches/{id}.
func (h Handlers) DeleteSavedSearch(w http.ResponseWriter, r *http.Request, id Id) {
	if err := h.S.DeleteSavedSearch(r.Context(), principal(r), id); err != nil {
		fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func prospectOut(p repo.GetProspectRow) Prospect {
	out := Prospect{Id: p.ID, CompanyId: p.CompanyID, DisplayName: p.DisplayName, Domain: p.Domain, Status: ProspectStatus(p.Status), SourceKey: p.SourceKey,
		DataQuality: DataQuality(p.DataQuality), Coverage: p.LatestCoverage, CrmCompanyId: p.CrmCompanyID, OwnerUserId: p.OwnerUserID, UpdatedAt: p.UpdatedAt}
	if p.LatestScore != nil {
		v := int(*p.LatestScore)
		out.Score = &v
	}
	return out
}

// ListProspects handles GET /lead-generation/prospects.
func (h Handlers) ListProspects(w http.ResponseWriter, r *http.Request, params ListProspectsParams) {
	limit, before, err := httpx.Page(params.Limit, params.Cursor)
	if err != nil {
		fail(w, r, err)
		return
	}
	f := app.ProspectFilter{ListID: params.ListId, Query: params.Q}
	if params.Status != nil {
		s := string(*params.Status)
		f.Status = &s
	}
	if params.MinScore != nil {
		v := int32(*params.MinScore) //nolint:gosec // 0-100.
		f.MinScore = &v
	}
	rows, err := h.S.ListProspects(r.Context(), principal(r), f, limit+1, before)
	if err != nil {
		fail(w, r, err)
		return
	}
	out := ProspectList{Data: []Prospect{}}
	for i, p := range rows {
		if int32(i) == limit { //nolint:gosec // Small.
			next := rows[i-1].ID.String()
			out.NextCursor = &next
			break
		}
		out.Data = append(out.Data, prospectOut(repo.GetProspectRow(p)))
	}
	httpx.JSON(w, http.StatusOK, out)
}

// CreateProspect handles POST /lead-generation/prospects.
func (h Handlers) CreateProspect(w http.ResponseWriter, r *http.Request) {
	var b CreateProspect
	if !httpx.Decode(w, r, &b) {
		return
	}
	p, err := h.S.CreateProspect(r.Context(), principal(r), b.Domain, deref(b.DisplayName))
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, prospectOut(p))
}

func runOut(e repo.ListEnrichmentRunsRow) EnrichmentRun {
	return EnrichmentRun{Id: e.ID, Kind: EnrichmentRunKind(e.Kind), ProviderKey: e.ProviderKey, Status: EnrichmentRunStatus(e.Status), ErrorCode: e.ErrorCode, CreatedAt: e.CreatedAt, FinishedAt: e.FinishedAt}
}

// GetProspect handles GET /lead-generation/prospects/{id}.
func (h Handlers) GetProspect(w http.ResponseWriter, r *http.Request, id Id) {
	d, err := h.S.Detail(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	p := prospectOut(d.Prospect)
	out := ProspectDetail{Id: p.Id, CompanyId: p.CompanyId, DisplayName: p.DisplayName, Domain: p.Domain, Status: p.Status, SourceKey: p.SourceKey, DataQuality: p.DataQuality,
		Score: p.Score, Coverage: p.Coverage, CrmCompanyId: p.CrmCompanyId, OwnerUserId: p.OwnerUserId, UpdatedAt: p.UpdatedAt,
		Attributes: []Attribute{}, Contacts: []ProspectContact{}, Enrichments: []EnrichmentRun{}, IsCompetitor: d.IsCompetitor}
	for _, a := range d.Attributes {
		var v any
		_ = json.Unmarshal(a.Value, &v)
		out.Attributes = append(out.Attributes, Attribute{Attribute: a.Attribute, Value: v, SourceKey: a.SourceKey, LicenceClass: a.LicenceClass, Confidence: a.Confidence, ObservedAt: a.ObservedAt})
	}
	for _, c := range d.Contacts {
		out.Contacts = append(out.Contacts, ProspectContact{ContactId: c.ContactID, RoleTitle: c.RoleTitle, IsDecisionMaker: c.IsDecisionMaker, Confidence: c.Confidence, SourceKey: c.SourceKey})
	}
	for _, e := range d.Enrichments {
		out.Enrichments = append(out.Enrichments, runOut(e))
	}
	httpx.JSON(w, http.StatusOK, out)
}

// UpdateProspect handles PATCH /lead-generation/prospects/{id}. An explicit null owner clears it.
func (h Handlers) UpdateProspect(w http.ResponseWriter, r *http.Request, id Id) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, httpx.MaxBodyBytes))
	if err != nil {
		fail(w, r, err)
		return
	}
	var b ProspectPatch
	var keys map[string]json.RawMessage
	if json.Unmarshal(raw, &b) != nil || json.Unmarshal(raw, &keys) != nil {
		httpx.Error(w, r, errMalformed)
		return
	}
	_, setOwner := keys["owner_user_id"]
	var status *string
	if b.Status != nil {
		s := string(*b.Status)
		status = &s
	}
	p, err := h.S.UpdateProspect(r.Context(), principal(r), id, status, setOwner, b.OwnerUserId)
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, prospectOut(p))
}

// DeleteProspect handles DELETE /lead-generation/prospects/{id}.
func (h Handlers) DeleteProspect(w http.ResponseWriter, r *http.Request, id Id) {
	if err := h.S.DeleteProspect(r.Context(), principal(r), id); err != nil {
		fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// EnrichProspect handles POST /lead-generation/prospects/{id}/enrich.
func (h Handlers) EnrichProspect(w http.ResponseWriter, r *http.Request, id Id) {
	var b Enrich
	if !httpx.Decode(w, r, &b) {
		return
	}
	kinds := make([]string, len(b.Kinds))
	for i, k := range b.Kinds {
		kinds[i] = string(k)
	}
	runs, err := h.S.Enrich(r.Context(), principal(r), id, kinds)
	if err != nil {
		fail(w, r, err)
		return
	}
	out := EnrichAccepted{Runs: []EnrichmentRun{}}
	for _, e := range runs {
		out.Runs = append(out.Runs, runOut(e))
	}
	httpx.JSON(w, http.StatusAccepted, out)
}

func scoreOut(s app.ScoreView) Score {
	out := Score{Value: int(s.Value), Coverage: s.Coverage, ModelName: s.ModelName, ModelVersion: int(s.ModelVersion), Factors: []Factor{}, Explanation: s.Explanation, ScoredAt: s.ScoredAt}
	if s.Explanation != nil {
		ai := s.ExplainedBy != nil
		out.ExplanationIsAi = &ai
	}
	for _, f := range s.Factors {
		out.Factors = append(out.Factors, factorOut(f))
	}
	return out
}

func factorOut(f domain.Factor) Factor {
	ev := f.Evidence
	if ev == nil {
		ev = map[string]any{}
	}
	return Factor{Key: f.Key, State: FactorState(f.State), Weight: f.Weight, Earned: f.Earned, Evidence: ev}
}

// GetProspectScore handles GET /lead-generation/prospects/{id}/score.
func (h Handlers) GetProspectScore(w http.ResponseWriter, r *http.Request, id Id) {
	s, err := h.S.Score(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, scoreOut(s))
}

// RescoreProspect handles POST /lead-generation/prospects/{id}/rescore.
func (h Handlers) RescoreProspect(w http.ResponseWriter, r *http.Request, id Id) {
	s, err := h.S.Rescore(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, scoreOut(s))
}

// ExplainProspectScore handles POST /lead-generation/prospects/{id}/explain.
func (h Handlers) ExplainProspectScore(w http.ResponseWriter, r *http.Request, id Id) {
	job, err := h.S.Explain(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, JobRef{JobId: job})
}

// ConvertProspect handles POST /lead-generation/prospects/{id}/convert.
func (h Handlers) ConvertProspect(w http.ResponseWriter, r *http.Request, id Id) {
	p, err := h.S.Convert(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, prospectOut(p))
}

func weightsOut(raw []byte) Weights {
	var w domain.Weights
	_ = json.Unmarshal(raw, &w)
	return Weights{Industry: w.Industry, Geography: w.Geography, Employees: w.Employees, Technology: w.Technology}
}

// ListScoringModels handles GET /lead-generation/scoring-models.
func (h Handlers) ListScoringModels(w http.ResponseWriter, r *http.Request) {
	rows, err := h.S.ListModels(r.Context(), principal(r))
	if err != nil {
		fail(w, r, err)
		return
	}
	out := ScoringModelList{Data: []ScoringModel{}}
	for _, m := range rows {
		out.Data = append(out.Data, ScoringModel{Id: m.ID, Name: m.Name, Version: int(m.Version), Weights: weightsOut(m.Weights), Status: ScoringModelStatus(m.Status), CreatedAt: m.CreatedAt})
	}
	httpx.JSON(w, http.StatusOK, out)
}

// CreateScoringModel handles POST /lead-generation/scoring-models.
func (h Handlers) CreateScoringModel(w http.ResponseWriter, r *http.Request) {
	var b ScoringModelInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	m, err := h.S.CreateModel(r.Context(), principal(r), b.Name, domain.Weights{Industry: b.Weights.Industry, Geography: b.Weights.Geography, Employees: b.Weights.Employees, Technology: b.Weights.Technology})
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, ScoringModel{Id: m.ID, Name: m.Name, Version: int(m.Version), Weights: weightsOut(m.Weights), Status: ScoringModelStatus(m.Status), CreatedAt: m.CreatedAt})
}

// PublishScoringModel handles POST /lead-generation/scoring-models/{id}/publish.
func (h Handlers) PublishScoringModel(w http.ResponseWriter, r *http.Request, id Id) {
	m, err := h.S.PublishModel(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, ScoringModel{Id: m.ID, Name: m.Name, Version: int(m.Version), Weights: weightsOut(m.Weights), Status: ScoringModelStatus(m.Status), CreatedAt: m.CreatedAt})
}

// ListProspectLists handles GET /lead-generation/lists.
func (h Handlers) ListProspectLists(w http.ResponseWriter, r *http.Request) {
	rows, err := h.S.Lists(r.Context(), principal(r))
	if err != nil {
		fail(w, r, err)
		return
	}
	out := ProspectListList{Data: []ProspectListItem{}}
	for _, l := range rows {
		out.Data = append(out.Data, ProspectListItem{Id: l.ID, Name: l.Name, Description: l.Description, MemberCount: int(l.MemberCount), UpdatedAt: l.UpdatedAt})
	}
	httpx.JSON(w, http.StatusOK, out)
}

// CreateProspectList handles POST /lead-generation/lists.
func (h Handlers) CreateProspectList(w http.ResponseWriter, r *http.Request) {
	var b ListInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	l, err := h.S.CreateList(r.Context(), principal(r), b.Name, b.Description)
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, ProspectListItem{Id: l.ID, Name: l.Name, Description: l.Description, UpdatedAt: l.UpdatedAt})
}

// UpdateProspectList handles PATCH /lead-generation/lists/{id}.
func (h Handlers) UpdateProspectList(w http.ResponseWriter, r *http.Request, id Id) {
	var b ListPatch
	if !httpx.Decode(w, r, &b) {
		return
	}
	l, err := h.S.UpdateList(r.Context(), principal(r), id, b.Name, b.Description)
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, ProspectListItem{Id: l.ID, Name: l.Name, Description: l.Description, UpdatedAt: l.UpdatedAt})
}

// DeleteProspectList handles DELETE /lead-generation/lists/{id}.
func (h Handlers) DeleteProspectList(w http.ResponseWriter, r *http.Request, id Id) {
	if err := h.S.DeleteList(r.Context(), principal(r), id); err != nil {
		fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// AddListMembers handles POST /lead-generation/lists/{id}/members.
func (h Handlers) AddListMembers(w http.ResponseWriter, r *http.Request, id Id) {
	var b ListMembers
	if !httpx.Decode(w, r, &b) {
		return
	}
	if err := h.S.AddMembers(r.Context(), principal(r), id, b.ProspectIds); err != nil {
		fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RemoveListMembers handles DELETE /lead-generation/lists/{id}/members.
func (h Handlers) RemoveListMembers(w http.ResponseWriter, r *http.Request, id Id) {
	var b ListMembers
	if !httpx.Decode(w, r, &b) {
		return
	}
	if err := h.S.RemoveMembers(r.Context(), principal(r), id, b.ProspectIds); err != nil {
		fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func importOut(i repo.GetImportJobRow, job *uuid.UUID) Import {
	toInt := func(p *int32) *int {
		if p == nil {
			return nil
		}
		v := int(*p)
		return &v
	}
	return Import{Id: i.ID, Status: ImportStatus(i.Status), RowsTotal: toInt(i.RowsTotal), RowsImported: toInt(i.RowsImported), RowsDuplicate: toInt(i.RowsDuplicate),
		RowsRejected: toInt(i.RowsRejected), ReportFileId: i.ReportFileID, JobId: job, CreatedAt: i.CreatedAt}
}

// StartImport handles POST /lead-generation/imports.
func (h Handlers) StartImport(w http.ResponseWriter, r *http.Request) {
	var b StartImport
	if !httpx.Decode(w, r, &b) {
		return
	}
	if !bool(b.LawfulSourceAttested) {
		httpx.Error(w, r, errAttest)
		return
	}
	mapping := map[string]string{"domain": b.Mapping.Domain}
	for k, v := range map[string]*string{"name": b.Mapping.Name, "country": b.Mapping.Country, "industry": b.Mapping.Industry, "employees": b.Mapping.Employees} {
		if v != nil && *v != "" {
			mapping[k] = *v
		}
	}
	imp, err := h.S.StartImport(r.Context(), principal(r), b.FileId, mapping, b.ListId)
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, importOut(imp.GetImportJobRow, &imp.JobID))
}

// GetImport handles GET /lead-generation/imports/{id}.
func (h Handlers) GetImport(w http.ResponseWriter, r *http.Request, id Id) {
	imp, err := h.S.GetImport(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, importOut(imp, nil))
}

// DownloadImportReport handles GET /lead-generation/imports/{id}/report.
func (h Handlers) DownloadImportReport(w http.ResponseWriter, r *http.Request, id Id) {
	d, err := h.S.DownloadImportReport(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.Attachment(w, d.Name, d.ContentType, d.Body)
}

func exportOut(e repo.GetExportJobRow, job *uuid.UUID) Export {
	toInt := func(p *int32) *int {
		if p == nil {
			return nil
		}
		v := int(*p)
		return &v
	}
	return Export{Id: e.ID, Status: ExportStatus(e.Status), RowsTotal: toInt(e.RowsTotal), ValuesWithheld: toInt(e.ValuesWithheld), FileId: e.FileID, JobId: job, CreatedAt: e.CreatedAt}
}

// StartExport handles POST /lead-generation/exports.
func (h Handlers) StartExport(w http.ResponseWriter, r *http.Request) {
	var b StartExport
	if !httpx.Decode(w, r, &b) {
		return
	}
	var status *string
	if b.Status != nil {
		s := string(*b.Status)
		status = &s
	}
	e, err := h.S.StartExport(r.Context(), principal(r), b.ListId, status)
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, exportOut(e.GetExportJobRow, &e.JobID))
}

// GetExport handles GET /lead-generation/exports/{id}.
func (h Handlers) GetExport(w http.ResponseWriter, r *http.Request, id Id) {
	e, err := h.S.GetExport(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, exportOut(e, nil))
}

// DownloadExport handles GET /lead-generation/exports/{id}/download.
func (h Handlers) DownloadExport(w http.ResponseWriter, r *http.Request, id Id) {
	d, err := h.S.DownloadExport(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.Attachment(w, d.Name, d.ContentType, d.Body)
}
