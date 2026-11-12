// Package httpapi serves IT Assessment's contract. The route guard has already authenticated,
// checked the entitlement and permission, and validated the request.
package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"

	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/httpx"
	"mspengine/internal/modules/itassessment/app"
	"mspengine/internal/modules/itassessment/domain"
	"mspengine/internal/modules/itassessment/repo"
)

// Handlers implements the generated server interface.
type Handlers struct{ S *app.Service }

var _ ServerInterface = Handlers{}

func principal(r *http.Request) authctx.Principal { return authctx.PrincipalFrom(r.Context()) }

func fail(w http.ResponseWriter, r *http.Request, err error) { httpx.Error(w, r, err) }

func ok(w http.ResponseWriter, status int, v any) { httpx.JSON(w, status, v) }

// ListAssessmentTemplates handles GET /it-assessments/templates.
func (h Handlers) ListAssessmentTemplates(w http.ResponseWriter, r *http.Request) {
	rows, err := h.S.Templates(r.Context(), principal(r))
	if err != nil {
		fail(w, r, err)
		return
	}
	out := TemplateList{Data: []Template{}}
	for _, t := range rows {
		out.Data = append(out.Data, Template{Id: t.ID, Name: t.Name, Tier: Tier(t.Tier), CheckKeys: t.CheckKeys})
	}
	ok(w, 200, out)
}

// CreateAssessmentTemplate handles POST /it-assessments/templates.
func (h Handlers) CreateAssessmentTemplate(w http.ResponseWriter, r *http.Request) {
	var b TemplateInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	keys := make([]string, len(b.CheckKeys))
	for i, k := range b.CheckKeys {
		keys[i] = string(k)
	}
	id, err := h.S.AddTemplate(r.Context(), principal(r), b.Name, string(b.Tier), keys)
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, 201, Ref{Id: id})
}

func verificationOut(id uuid.UUID, d, method, status string, verified *time.Time, expires time.Time) Verification {
	return Verification{Id: id, Domain: d, Method: method, Status: VerificationStatus(status), VerifiedAt: verified, ExpiresAt: expires}
}

// ListVerifications handles GET /it-assessments/domain-verifications.
func (h Handlers) ListVerifications(w http.ResponseWriter, r *http.Request) {
	rows, err := h.S.Verifications(r.Context(), principal(r))
	if err != nil {
		fail(w, r, err)
		return
	}
	out := VerificationList{Data: []Verification{}}
	for _, v := range rows {
		out.Data = append(out.Data, verificationOut(v.ID, v.Domain, v.Method, v.Status, v.VerifiedAt, v.ExpiresAt))
	}
	ok(w, 200, out)
}

// StartVerification handles POST /it-assessments/domain-verifications.
func (h Handlers) StartVerification(w http.ResponseWriter, r *http.Request) {
	var b VerificationInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	v, err := h.S.StartVerification(r.Context(), principal(r), b.Domain, string(b.Method))
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, 201, struct {
		Verification
		Instructions string `json:"instructions"`
	}{verificationOut(v.ID, v.Domain, v.Method, "pending", nil, v.ExpiresAt), v.Instructions})
}

// CheckVerification handles POST /it-assessments/domain-verifications/{id}/check.
func (h Handlers) CheckVerification(w http.ResponseWriter, r *http.Request, id Id) {
	v, err := h.S.CheckVerification(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, 200, verificationOut(v.ID, v.Domain, v.Method, v.Status, v.VerifiedAt, v.ExpiresAt))
}

func authOut(a repo.GetAuthorizationRow) Authorization {
	return Authorization{Id: a.ID, Domain: a.Domain, Tier: Tier(a.Tier), Method: AuthorizationMethod(a.Method), AuthorizerName: a.AuthorizerName, AuthorizerEmail: a.AuthorizerEmail,
		AuthorizerRole: a.AuthorizerRole, TermsVersion: a.TermsVersion, Status: AuthorizationStatus(a.Status), GrantedAt: a.GrantedAt, ExpiresAt: a.ExpiresAt}
}

// ListAuthorizations handles GET /it-assessments/authorizations.
func (h Handlers) ListAuthorizations(w http.ResponseWriter, r *http.Request) {
	rows, err := h.S.Authorizations(r.Context(), principal(r))
	if err != nil {
		fail(w, r, err)
		return
	}
	out := AuthorizationList{Data: []Authorization{}}
	for _, a := range rows {
		x := authOut(repo.GetAuthorizationRow(a))
		if a.Status == "active" && a.ExpiresAt.Before(time.Now()) {
			x.Status = "expired"
		}
		out.Data = append(out.Data, x)
	}
	ok(w, 200, out)
}

// CreateAuthorization handles POST /it-assessments/authorizations.
func (h Handlers) CreateAuthorization(w http.ResponseWriter, r *http.Request) {
	var b AuthorizationInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	in := app.AuthorizeInput{Domain: b.Domain, Tier: string(b.Tier), Name: b.AuthorizerName, Email: string(b.AuthorizerEmail), VerificationID: b.DomainVerificationId, Attested: b.Attested}
	if b.AuthorizerRole != nil {
		in.Role = *b.AuthorizerRole
	}
	a, err := h.S.Authorize(r.Context(), principal(r), in)
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, 201, authOut(a))
}

// RevokeAuthorization handles POST /it-assessments/authorizations/{id}/revoke.
func (h Handlers) RevokeAuthorization(w http.ResponseWriter, r *http.Request, id Id) {
	if err := h.S.Revoke(r.Context(), principal(r), id); err != nil {
		fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func assessmentOut(a repo.GetAssessmentRow) Assessment {
	out := Assessment{Id: a.ID, Domain: a.Domain, CrmCompanyId: a.CrmCompanyID, Status: AssessmentStatus(a.Status), CreatedAt: a.CreatedAt, CompletedAt: a.CompletedAt}
	if a.Score != nil {
		v := int(*a.Score)
		out.Score = &v
	}
	if a.RiskLevel != nil {
		rl := AssessmentRiskLevel(*a.RiskLevel)
		out.RiskLevel = &rl
	}
	return out
}

// ListAssessments handles GET /it-assessments/assessments.
func (h Handlers) ListAssessments(w http.ResponseWriter, r *http.Request) {
	rows, err := h.S.Assessments(r.Context(), principal(r))
	if err != nil {
		fail(w, r, err)
		return
	}
	out := AssessmentList{Data: []Assessment{}}
	for _, a := range rows {
		out.Data = append(out.Data, assessmentOut(repo.GetAssessmentRow{ID: a.ID, Domain: a.Domain, CrmCompanyID: a.CrmCompanyID, Status: a.Status, Score: a.Score, RiskLevel: a.RiskLevel, CreatedAt: a.CreatedAt, CompletedAt: a.CompletedAt}))
	}
	ok(w, 200, out)
}

// StartAssessment handles POST /it-assessments/assessments.
func (h Handlers) StartAssessment(w http.ResponseWriter, r *http.Request) {
	var b AssessmentInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	a, err := h.S.Start(r.Context(), principal(r), b.AuthorizationId, b.TemplateId, b.CrmCompanyId)
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, 202, assessmentOut(a))
}

type check struct {
	CheckKey string         `json:"check_key"`
	Status   string         `json:"status"`
	Target   string         `json:"target"`
	Evidence map[string]any `json:"evidence,omitempty"`
}

type finding struct {
	ID             uuid.UUID      `json:"id"`
	CheckKey       string         `json:"check_key"`
	Severity       string         `json:"severity"`
	Classification string         `json:"classification"`
	Title          string         `json:"title"`
	Description    string         `json:"description"`
	Recommendation string         `json:"recommendation"`
	Evidence       map[string]any `json:"evidence,omitempty"`
}

// GetAssessment handles GET /it-assessments/assessments/{id}.
func (h Handlers) GetAssessment(w http.ResponseWriter, r *http.Request, id Id) {
	a, err := h.S.Assessment(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	d, err := h.S.Results(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	checks, findings, counts := []check{}, []finding{}, map[string]int{}
	for _, c := range d.Checks {
		var ev map[string]any
		_ = json.Unmarshal(c.RawResult, &ev)
		checks = append(checks, check{c.CheckKey, c.Status, c.Target, ev})
		counts[c.Status]++
	}
	for _, f := range d.Findings {
		var ev map[string]any
		_ = json.Unmarshal(f.Evidence, &ev)
		findings = append(findings, finding{f.ID, f.CheckKey, f.Severity, f.Classification, f.Title, f.Description, f.Recommendation, ev})
	}
	ok(w, 200, struct {
		Assessment
		Coverage float64        `json:"coverage"`
		Notice   string         `json:"notice"`
		Counts   map[string]int `json:"counts"`
		Checks   []check        `json:"checks"`
		Findings []finding      `json:"findings"`
	}{assessmentOut(a), d.Coverage, domain.ReportNotice, counts, checks, findings})
}

// CancelAssessment handles POST /it-assessments/assessments/{id}/cancel.
func (h Handlers) CancelAssessment(w http.ResponseWriter, r *http.Request, id Id) {
	a, err := h.S.Cancel(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, 200, assessmentOut(a))
}

func reportOut(rep repo.GetReportRow) Report {
	has := rep.FileID != nil
	return Report{Id: rep.ID, AssessmentId: rep.AssessmentID, Version: int(rep.Version), Status: ReportStatus(rep.Status), HasPdf: &has, CreatedAt: rep.CreatedAt}
}

// CreateAssessmentReport handles POST /it-assessments/assessments/{id}/reports.
func (h Handlers) CreateAssessmentReport(w http.ResponseWriter, r *http.Request, id Id) {
	rep, err := h.S.CreateReport(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, 202, reportOut(rep))
}

// GetAssessmentReport handles GET /it-assessments/reports/{id}.
func (h Handlers) GetAssessmentReport(w http.ResponseWriter, r *http.Request, id Id) {
	rep, err := h.S.Report(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, 200, reportOut(rep))
}

// DownloadAssessmentReport handles GET /it-assessments/reports/{id}/pdf.
func (h Handlers) DownloadAssessmentReport(w http.ResponseWriter, r *http.Request, id Id) {
	d, err := h.S.ReportPDF(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.Attachment(w, d.Name, d.ContentType, d.Body)
}
