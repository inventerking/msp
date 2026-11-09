package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/jobs"
	"mspengine/internal/modules/competitorintel/domain"
	"mspengine/internal/modules/competitorintel/repo"
	"mspengine/internal/platform/audit"
	"mspengine/internal/platform/files"
	"mspengine/internal/platform/integration/aigateway"
	"mspengine/internal/platform/pdf"
)

// Observations returns current facts, or the full history.
func (s *Service) Observations(ctx context.Context, p authctx.Principal, competitorID uuid.UUID, history bool) ([]repo.ListObservationsRow, error) {
	var out []repo.ListObservationsRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		if _, err := q.GetCompetitor(ctx, repo.GetCompetitorParams{TenantID: p.TenantID, ID: competitorID}); err != nil {
			return notFound(err)
		}
		var err error
		out, err = q.ListObservations(ctx, repo.ListObservationsParams{TenantID: p.TenantID, CompetitorID: competitorID, IncludeHistory: history})
		return err
	})
	return out, err
}

// Changes returns a competitor's change feed.
func (s *Service) Changes(ctx context.Context, p authctx.Principal, competitorID uuid.UUID) ([]repo.ListChangesRow, error) {
	var out []repo.ListChangesRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		if _, err := q.GetCompetitor(ctx, repo.GetCompetitorParams{TenantID: p.TenantID, ID: competitorID}); err != nil {
			return notFound(err)
		}
		var err error
		out, err = q.ListChanges(ctx, repo.ListChangesParams{TenantID: p.TenantID, CompetitorID: competitorID})
		return err
	})
	return out, err
}

// Acknowledge marks a change as seen.
func (s *Service) Acknowledge(ctx context.Context, p authctx.Principal, id uuid.UUID) error {
	return s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		n, err := q.AcknowledgeChange(ctx, repo.AcknowledgeChangeParams{TenantID: p.TenantID, ID: id})
		if err != nil || n > 0 {
			return err
		}
		if ok, err := q.ChangeExists(ctx, repo.ChangeExistsParams{TenantID: p.TenantID, ID: id}); err != nil || ok {
			return err // Already acknowledged.
		}
		return apperr.NotFound()
	})
}

// Opportunities.

// Opportunity is an opportunity with its evidence.
type Opportunity struct {
	repo.GetOpportunityRow
	Evidence []repo.OpportunityEvidenceRow
}

// Opportunities lists the tenant's market opportunities.
func (s *Service) Opportunities(ctx context.Context, p authctx.Principal) ([]repo.ListOpportunitiesRow, error) {
	var out []repo.ListOpportunitiesRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.ListOpportunities(ctx, p.TenantID)
		return err
	})
	return out, err
}

// AddOpportunity records an opportunity a user identified. It is the user's inference, so it is
// labelled inferred, never observed.
func (s *Service) AddOpportunity(ctx context.Context, p authctx.Principal, title, rationale string, geography *string, industryCode *string, observationIDs []uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		industry, err := s.industryID(ctx, industryCode)
		if err != nil {
			return err
		}
		id, err = q.InsertOpportunity(ctx, repo.InsertOpportunityParams{TenantID: p.TenantID, Geography: geography, IndustryID: industry, Title: title, Rationale: rationale, EvidenceClass: domain.Inferred})
		if err != nil {
			return err
		}
		for _, o := range observationIDs {
			if err := q.InsertOpportunityEvidence(ctx, repo.InsertOpportunityEvidenceParams{TenantID: p.TenantID, OpportunityID: id, ObservationID: &o}); err != nil {
				if containsCode(err, "23503") {
					return apperr.Validation(apperr.FieldError{Field: "observation_ids", Code: "unknown_observation"})
				}
				return err
			}
		}
		if err := emit(ctx, tx, p.TenantID, "opportunity.identified", "opportunity", id, map[string]any{"opportunity_id": id, "evidence_class": domain.Inferred}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "competitor.opportunity_added", "opportunity", &id, map[string]any{"title": title}))
	})
	return id, err
}

func (s *Service) industryID(ctx context.Context, code *string) (*uuid.UUID, error) {
	if code == nil || *code == "" {
		return nil, nil
	}
	_, inds, err := s.Shared.Reference(ctx)
	if err != nil {
		return nil, err
	}
	for _, i := range inds {
		if i.Code == *code {
			return &i.ID, nil
		}
	}
	return nil, apperr.Validation(apperr.FieldError{Field: "industry", Code: "unknown_industry"})
}

// GetOpportunity returns one opportunity.
func (s *Service) GetOpportunity(ctx context.Context, p authctx.Principal, id uuid.UUID) (Opportunity, error) {
	var out Opportunity
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		if out.GetOpportunityRow, err = q.GetOpportunity(ctx, repo.GetOpportunityParams{TenantID: p.TenantID, ID: id}); err != nil {
			return notFound(err)
		}
		out.Evidence, err = q.OpportunityEvidence(ctx, repo.OpportunityEvidenceParams{TenantID: p.TenantID, OpportunityID: id})
		return err
	})
	return out, err
}

// SetOpportunityStatus moves an opportunity through review.
func (s *Service) SetOpportunityStatus(ctx context.Context, p authctx.Principal, id uuid.UUID, status string) (Opportunity, error) {
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		n, err := q.SetOpportunityStatus(ctx, repo.SetOpportunityStatusParams{TenantID: p.TenantID, ID: id, Status: status})
		if err == nil && n == 0 {
			return apperr.NotFound()
		}
		return err
	})
	if err != nil {
		return Opportunity{}, err
	}
	return s.GetOpportunity(ctx, p, id)
}

// Comparisons.

// ComparisonView is a comparison with each member's current facts by attribute.
type ComparisonView struct {
	repo.GetComparisonRow
	Members []ComparisonMember
}

// ComparisonMember is one competitor's column.
type ComparisonMember struct {
	ID          uuid.UUID
	DisplayName string
	Facts       map[string][]Fact
}

// Fact is a value with its label.
type Fact struct {
	Value, EvidenceClass string
}

// Comparisons lists comparison sets.
func (s *Service) Comparisons(ctx context.Context, p authctx.Principal) ([]repo.ListComparisonsRow, error) {
	var out []repo.ListComparisonsRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.ListComparisons(ctx, p.TenantID)
		return err
	})
	return out, err
}

func checkCompetitors(ctx context.Context, q *repo.Queries, tenant uuid.UUID, ids []uuid.UUID) error {
	uniq := map[uuid.UUID]bool{}
	for _, id := range ids {
		uniq[id] = true
	}
	n, err := q.CountLiveCompetitors(ctx, repo.CountLiveCompetitorsParams{TenantID: tenant, Ids: ids})
	if err != nil {
		return err
	}
	if int(n) != len(uniq) {
		return apperr.Validation(apperr.FieldError{Field: "competitor_ids", Code: "unknown_competitor"})
	}
	return nil
}

// CreateComparison saves a set of competitors to compare.
func (s *Service) CreateComparison(ctx context.Context, p authctx.Principal, name string, competitorIDs []uuid.UUID, dimensions []string) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		if err := checkCompetitors(ctx, q, p.TenantID, competitorIDs); err != nil {
			return err
		}
		if dimensions == nil {
			dimensions = []string{}
		}
		var err error
		if id, err = q.InsertComparison(ctx, repo.InsertComparisonParams{TenantID: p.TenantID, Name: name, Dimensions: dimensions, CreatedBy: uid(p)}); err != nil {
			return err
		}
		for _, c := range unique(competitorIDs) {
			if err := q.InsertComparisonMember(ctx, repo.InsertComparisonMemberParams{TenantID: p.TenantID, ComparisonSetID: id, CompetitorID: c}); err != nil {
				return err
			}
		}
		return nil
	})
	return id, err
}

func unique(ids []uuid.UUID) []uuid.UUID {
	seen := map[uuid.UUID]bool{}
	var out []uuid.UUID
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// Comparison returns a comparison's grid.
func (s *Service) Comparison(ctx context.Context, p authctx.Principal, id uuid.UUID) (ComparisonView, error) {
	var out ComparisonView
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		if out.GetComparisonRow, err = q.GetComparison(ctx, repo.GetComparisonParams{TenantID: p.TenantID, ID: id}); err != nil {
			return notFound(err)
		}
		members, err := q.ComparisonMembers(ctx, repo.ComparisonMembersParams{TenantID: p.TenantID, ComparisonSetID: id})
		if err != nil {
			return err
		}
		ids := make([]uuid.UUID, len(members))
		for i, m := range members {
			ids[i] = m.ID
		}
		obs, err := q.CurrentObservationsFor(ctx, repo.CurrentObservationsForParams{TenantID: p.TenantID, CompetitorIds: ids})
		if err != nil {
			return err
		}
		for _, m := range members {
			col := ComparisonMember{ID: m.ID, DisplayName: m.DisplayName, Facts: map[string][]Fact{}}
			for _, o := range obs {
				if o.CompetitorID == m.ID && (len(out.Dimensions) == 0 || contains(out.Dimensions, o.AttributeType)) {
					col.Facts[o.AttributeType] = append(col.Facts[o.AttributeType], Fact{Value: valueText(o.Value), EvidenceClass: o.EvidenceClass})
				}
			}
			out.Members = append(out.Members, col)
		}
		return nil
	})
	return out, err
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// Reports.

type reportPayload struct {
	ReportID uuid.UUID `json:"report_id"`
}

// ReportContent is what a finished report holds. Every statement carries its label.
type ReportContent struct {
	CompetitorIDs []uuid.UUID         `json:"competitor_ids"`
	Summary       string              `json:"summary,omitempty"`
	SummaryLabel  string              `json:"summary_label,omitempty"`
	Opportunities []ReportOpportunity `json:"opportunities,omitempty"`
	Facts         []ReportFact        `json:"facts,omitempty"`
	Notice        string              `json:"notice,omitempty"`
}

// ReportOpportunity is an opportunity the AI suggested.
type ReportOpportunity struct {
	ID        uuid.UUID `json:"id"`
	Title     string    `json:"title"`
	Rationale string    `json:"rationale"`
	Label     string    `json:"evidence_class"`
}

// ReportFact is an observation the report drew on.
type ReportFact struct {
	Competitor    string `json:"competitor"`
	AttributeType string `json:"attribute_type"`
	Value         string `json:"value"`
	EvidenceClass string `json:"evidence_class"`
}

// ReportNotice is printed on every report, on screen and in the PDF.
const ReportNotice = "Statements marked observed were copied from the competitor's public page on the date shown. AI estimates are generated and may be wrong: check them before relying on or sharing them."

// CreateReport queues an AI report over chosen competitors.
func (s *Service) CreateReport(ctx context.Context, p authctx.Principal, kind, title string, competitorIDs []uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		if err := checkCompetitors(ctx, q, p.TenantID, competitorIDs); err != nil {
			return err
		}
		var err error
		id, err = q.InsertReport(ctx, repo.InsertReportParams{TenantID: p.TenantID, Title: title, Kind: kind, Content: mustJSON(ReportContent{CompetitorIDs: unique(competitorIDs)}), CreatedBy: uid(p)})
		if err != nil {
			return err
		}
		if _, _, err := jobs.Enqueue(ctx, tx, jobs.Spec{TenantID: &p.TenantID, Kind: JobReport, Queue: jobs.QueueAI, MaxAttempts: 3, Payload: reportPayload{ReportID: id}, IdempotencyKey: "report:" + id.String()}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "competitor.report_requested", "report", &id, map[string]any{"kind": kind, "competitors": len(competitorIDs)}))
	})
	return id, err
}

// Report returns a report.
func (s *Service) Report(ctx context.Context, p authctx.Principal, id uuid.UUID) (repo.GetReportRow, error) {
	var out repo.GetReportRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.GetReport(ctx, repo.GetReportParams{TenantID: p.TenantID, ID: id})
		return notFound(err)
	})
	return out, err
}

// Reports lists reports.
func (s *Service) Reports(ctx context.Context, p authctx.Principal) ([]repo.ListReportsRow, error) {
	var out []repo.ListReportsRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.ListReports(ctx, p.TenantID)
		return err
	})
	return out, err
}

// ReportPDF returns a report's PDF.
func (s *Service) ReportPDF(ctx context.Context, p authctx.Principal, id uuid.UUID) (files.Download, error) {
	r, err := s.Report(ctx, p, id)
	if err != nil {
		return files.Download{}, err
	}
	if r.FileID == nil {
		return files.Download{}, apperr.Conflict("report_not_ready", "This report has no PDF yet")
	}
	return s.Files.Download(ctx, p, *r.FileID)
}

// HandleReport writes the report. Observations come from web pages, so they go to the AI as
// untrusted data; the output must fit the report schema whatever the pages said.
func (s *Service) HandleReport(ctx context.Context, j jobs.Job) (err error) {
	var pl reportPayload
	defer func() {
		if err != nil && j.Final(err) && j.TenantID != nil && pl.ReportID != uuid.Nil {
			tenant := *j.TenantID
			if ferr := s.systemTx(context.WithoutCancel(ctx), tenant, func(q *repo.Queries, _ pgx.Tx) error {
				return q.SetReportStatus(ctx, repo.SetReportStatusParams{TenantID: tenant, ID: pl.ReportID, Status: "failed"})
			}); ferr != nil {
				slog.WarnContext(ctx, "record failed report", "error", ferr)
			}
		}
	}()
	if err := j.Decode(&pl); err != nil {
		return err
	}
	if j.TenantID == nil {
		return jobs.Permanent(errors.New("report job without tenant"))
	}
	tenant := *j.TenantID
	var rep repo.GetReportRow
	var content ReportContent
	var facts []ReportFact
	err = s.systemTx(ctx, tenant, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		if rep, err = q.GetReport(ctx, repo.GetReportParams{TenantID: tenant, ID: pl.ReportID}); err != nil {
			return err
		}
		if rep.Status == "ready" || rep.Status == "failed" {
			return nil
		}
		if err := json.Unmarshal(rep.Content, &content); err != nil {
			return err
		}
		if err := q.SetReportStatus(ctx, repo.SetReportStatusParams{TenantID: tenant, ID: rep.ID, Status: "generating"}); err != nil {
			return err
		}
		names := map[uuid.UUID]string{}
		for _, id := range content.CompetitorIDs {
			c, err := q.GetCompetitor(ctx, repo.GetCompetitorParams{TenantID: tenant, ID: id})
			if err == nil {
				names[id] = c.DisplayName
			}
		}
		obs, err := q.CurrentObservationsFor(ctx, repo.CurrentObservationsForParams{TenantID: tenant, CompetitorIds: content.CompetitorIDs})
		if err != nil {
			return err
		}
		for _, o := range obs {
			facts = append(facts, ReportFact{Competitor: names[o.CompetitorID], AttributeType: o.AttributeType, Value: valueText(o.Value), EvidenceClass: o.EvidenceClass})
		}
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) || err == nil && (rep.Status == "ready" || rep.Status == "failed") {
		return nil
	}
	if err != nil {
		return err
	}
	var lines []string
	for _, f := range facts {
		lines = append(lines, fmt.Sprintf("- %s | %s | %s | %s", f.Competitor, f.AttributeType, f.Value, f.EvidenceClass))
	}
	focus := "not set"
	if icp, found, err := s.Identity.DefaultICP(ctx, tenant); err == nil && found {
		focus = "industries " + strings.Join(icp.Industries, ", ") + "; countries " + strings.Join(icp.Countries, ", ")
	}
	res, err := s.AI.Run(ctx, aigateway.Request{TenantID: tenant, Task: TaskReport, IdempotencyKey: "report:" + rep.ID.String(),
		Input: aigateway.Input{Trusted: map[string]string{"services": focus}, Untrusted: map[string]string{"observations": strings.Join(lines, "\n")}}})
	if err != nil {
		var ae *apperr.Error
		if errors.As(err, &ae) && ae.Status < 500 {
			s.failReport(ctx, tenant, rep.ID, content)
			return jobs.Permanent(err)
		}
		return err
	}
	var out struct {
		Summary       string `json:"summary"`
		Opportunities []struct {
			Title, Rationale, Geography, Industry string
		} `json:"opportunities"`
	}
	if err := json.Unmarshal(res.Output, &out); err != nil {
		s.failReport(ctx, tenant, rep.ID, content)
		return jobs.Permanent(err)
	}
	content.Summary, content.SummaryLabel, content.Facts, content.Notice = out.Summary, domain.AIEstimate, facts, ReportNotice
	var fileID *uuid.UUID
	err = s.systemTx(ctx, tenant, func(q *repo.Queries, tx pgx.Tx) error {
		for _, o := range out.Opportunities {
			var geo *string
			if o.Geography != "" {
				geo = &o.Geography
			}
			industry, _ := s.industryID(ctx, &o.Industry) // An industry the model made up is dropped.
			id, err := q.InsertOpportunity(ctx, repo.InsertOpportunityParams{TenantID: tenant, Geography: geo, IndustryID: industry, Title: o.Title, Rationale: o.Rationale, EvidenceClass: domain.AIEstimate, AiRequestID: &res.RequestID})
			if err != nil {
				return err
			}
			content.Opportunities = append(content.Opportunities, ReportOpportunity{ID: id, Title: o.Title, Rationale: o.Rationale, Label: domain.AIEstimate})
			if err := emit(ctx, tx, tenant, "opportunity.identified", "opportunity", id, map[string]any{"opportunity_id": id, "evidence_class": domain.AIEstimate}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if s.PDF != nil {
		if id, err := s.renderPDF(ctx, tenant, rep.Title, content); err != nil {
			slog.WarnContext(ctx, "report PDF failed; the report is still available on screen", "error", err)
		} else {
			fileID = &id
		}
	}
	return s.systemTx(ctx, tenant, func(q *repo.Queries, tx pgx.Tx) error {
		if err := q.FinishReport(ctx, repo.FinishReportParams{TenantID: tenant, ID: rep.ID, Status: "ready", Content: mustJSON(content), FileID: fileID, AiRequestID: &res.RequestID}); err != nil {
			return err
		}
		return emit(ctx, tx, tenant, "report.ready", "report", rep.ID, map[string]any{"report_id": rep.ID})
	})
}

func (s *Service) failReport(ctx context.Context, tenant, id uuid.UUID, content ReportContent) {
	if err := s.systemTx(ctx, tenant, func(q *repo.Queries, _ pgx.Tx) error {
		return q.FinishReport(ctx, repo.FinishReportParams{TenantID: tenant, ID: id, Status: "failed", Content: mustJSON(content)})
	}); err != nil {
		slog.WarnContext(ctx, "record failed report", "error", err)
	}
}

const reportTemplate = `<!doctype html><html><head><meta charset="utf-8"><style>
body{font-family:sans-serif;font-size:11pt;color:#111} h1{font-size:18pt} .label{font-size:8pt;text-transform:uppercase;padding:1px 4px;border:1px solid #888;border-radius:3px}
.ai{background:#fff3cd} table{border-collapse:collapse;width:100%} td,th{border:1px solid #ccc;padding:4px;text-align:left;vertical-align:top} footer{margin-top:24px;font-size:9pt;color:#444}
</style></head><body>
<h1>{{.Title}}</h1>
<h2>Summary <span class="label ai">{{.SummaryLabel}}</span></h2><p>{{.Summary}}</p>
{{if .Opportunities}}<h2>Opportunities</h2>{{range .Opportunities}}<h3>{{.Title}} <span class="label ai">{{.Label}}</span></h3><p>{{.Rationale}}</p>{{end}}{{end}}
{{if .Facts}}<h2>Facts used</h2><table><tr><th>Competitor</th><th>Attribute</th><th>Value</th><th>Evidence</th></tr>{{range .Facts}}<tr><td>{{.Competitor}}</td><td>{{.AttributeType}}</td><td>{{.Value}}</td><td><span class="label">{{.EvidenceClass}}</span></td></tr>{{end}}</table>{{end}}
<footer>{{.Notice}}</footer></body></html>`

func (s *Service) renderPDF(ctx context.Context, tenant uuid.UUID, title string, c ReportContent) (uuid.UUID, error) {
	html, err := pdf.HTML(reportTemplate, struct {
		Title string
		ReportContent
	}{title, c})
	if err != nil {
		return uuid.Nil, err
	}
	body, err := s.PDF.Render(ctx, html)
	if err != nil {
		return uuid.Nil, err
	}
	return s.Files.SaveGenerated(ctx, tenant, Module, "competitor-report.pdf", "application/pdf", body)
}
