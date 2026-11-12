package app

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/jobs"
	"mspengine/internal/modules/itassessment/domain"
	"mspengine/internal/modules/itassessment/repo"
	"mspengine/internal/platform/audit"
	"mspengine/internal/platform/files"
	"mspengine/internal/platform/pdf"
)

type reportPayload struct {
	ReportID uuid.UUID `json:"report_id"`
}

// CreateReport queues a PDF of a finished assessment.
func (s *Service) CreateReport(ctx context.Context, p authctx.Principal, assessment uuid.UUID) (repo.GetReportRow, error) {
	if s.PDF == nil {
		return repo.GetReportRow{}, apperr.Conflict("pdf_unavailable", "PDF rendering is not configured")
	}
	var id uuid.UUID
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		a, err := q.GetAssessment(ctx, repo.GetAssessmentParams{TenantID: p.TenantID, ID: assessment})
		if err != nil {
			return notFoundErr(err)
		}
		if a.Status != "completed" && a.Status != "partial" {
			return apperr.Conflict("assessment_not_finished", "The assessment has not finished")
		}
		if id, err = q.InsertReport(ctx, repo.InsertReportParams{TenantID: p.TenantID, AssessmentID: assessment}); err != nil {
			return err
		}
		if _, _, err := jobs.Enqueue(ctx, tx, jobs.Spec{TenantID: &p.TenantID, Kind: JobReport, Queue: jobs.QueueDefault, MaxAttempts: 3, Payload: reportPayload{ReportID: id}, IdempotencyKey: "assessment-report:" + id.String()}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "assessment.report_requested", "assessment", &assessment, nil))
	})
	if err != nil {
		return repo.GetReportRow{}, err
	}
	return s.Report(ctx, p, id)
}

// Report returns a report.
func (s *Service) Report(ctx context.Context, p authctx.Principal, id uuid.UUID) (repo.GetReportRow, error) {
	var out repo.GetReportRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.GetReport(ctx, repo.GetReportParams{TenantID: p.TenantID, ID: id})
		return notFoundErr(err)
	})
	return out, err
}

// ReportPDF returns a report's file.
func (s *Service) ReportPDF(ctx context.Context, p authctx.Principal, id uuid.UUID) (files.Download, error) {
	r, err := s.Report(ctx, p, id)
	if err != nil {
		return files.Download{}, err
	}
	if r.FileID == nil {
		return files.Download{}, apperr.Conflict("report_not_ready", "This report is not ready yet")
	}
	return s.Files.Download(ctx, p, *r.FileID)
}

const reportHTML = `<!doctype html><html><head><meta charset="utf-8"><style>
body{font-family:sans-serif;font-size:11pt;color:#111} h1{font-size:18pt} .sev{font-size:8pt;text-transform:uppercase;border:1px solid #888;padding:1px 4px;border-radius:3px}
table{border-collapse:collapse;width:100%} td,th{border:1px solid #ccc;padding:4px;text-align:left} footer{margin-top:24px;font-size:9pt;color:#444}
</style></head><body>
<h1>IT assessment: {{.Domain}}</h1>
<p>{{if .Scored}}Score {{.Score}} out of 100, {{.Risk}} risk. Based on {{.CoveragePct}}% of the checks, the rest could not be completed.{{else}}Not enough checks completed to give a score.{{end}}</p>
<h2>Findings</h2>{{if not .Findings}}<p>No findings.</p>{{end}}
{{range .Findings}}<h3>{{.Title}} <span class="sev">{{.Severity}}</span> <span class="sev">{{.Classification}}</span></h3><p>{{.Description}}</p><p><b>Recommendation:</b> {{.Recommendation}}</p>{{end}}
<h2>Checks</h2><table><tr><th>Check</th><th>Result</th><th>Target</th></tr>{{range .Checks}}<tr><td>{{.CheckKey}}</td><td>{{.Status}}</td><td>{{.Target}}</td></tr>{{end}}</table>
<footer>{{.Notice}}</footer></body></html>`

// HandleReport renders a report from stored results.
func (s *Service) HandleReport(ctx context.Context, j jobs.Job) error {
	var pl reportPayload
	if err := j.Decode(&pl); err != nil {
		return err
	}
	if j.TenantID == nil {
		return jobs.Permanent(errors.New("report without tenant"))
	}
	tenant := *j.TenantID
	var a repo.GetAssessmentRow
	var checks []repo.ListChecksRow
	var findings []repo.ListFindingsRow
	err := s.systemTx(ctx, tenant, func(q *repo.Queries, _ pgx.Tx) error {
		r, err := q.GetReport(ctx, repo.GetReportParams{TenantID: tenant, ID: pl.ReportID})
		if err != nil {
			return err
		}
		if a, err = q.GetAssessment(ctx, repo.GetAssessmentParams{TenantID: tenant, ID: r.AssessmentID}); err != nil {
			return err
		}
		if checks, err = q.ListChecks(ctx, repo.ListChecksParams{TenantID: tenant, AssessmentID: a.ID}); err != nil {
			return err
		}
		findings, err = q.ListFindings(ctx, repo.ListFindingsParams{TenantID: tenant, AssessmentID: a.ID})
		return err
	})
	if err != nil {
		return err
	}
	res := make([]domain.Result, len(checks))
	for i, c := range checks {
		res[i] = domain.Result{Key: c.CheckKey, Status: c.Status}
	}
	score, coverage, risk, scored := domain.Score(res)
	html, err := pdf.HTML(reportHTML, map[string]any{"Domain": a.Domain, "Scored": scored, "Score": score, "Risk": risk, "CoveragePct": int(coverage * 100),
		"Findings": findings, "Checks": checks, "Notice": domain.ReportNotice})
	if err != nil {
		return jobs.Permanent(err)
	}
	body, err := s.PDF.Render(ctx, html)
	if err != nil {
		return err
	}
	fileID, err := s.Files.SaveGenerated(ctx, tenant, Module, "assessment-"+a.Domain+".pdf", "application/pdf", body)
	if err != nil {
		return err
	}
	return s.systemTx(ctx, tenant, func(q *repo.Queries, _ pgx.Tx) error {
		return q.FinishReport(ctx, repo.FinishReportParams{TenantID: tenant, ID: pl.ReportID, Status: "ready", FileID: &fileID})
	})
}

// Jobs are the module's job handlers.
func (s *Service) Jobs() map[string]jobs.Handler {
	return map[string]jobs.Handler{JobRun: s.HandleRun, JobReport: s.HandleReport}
}
