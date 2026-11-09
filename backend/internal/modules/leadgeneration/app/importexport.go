package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/jobs"
	"mspengine/internal/modules/leadgeneration/domain"
	"mspengine/internal/modules/leadgeneration/repo"
	"mspengine/internal/platform/audit"
	"mspengine/internal/platform/files"
	"mspengine/internal/platform/shared"
)

// The file purpose an import must have been uploaded with.
const importPurpose = "csv_import"

// Licence classes that may leave the system in an export. Data licensed to the tenant from a
// provider is withheld, because provider terms are not known yet (D-05). Legal review may change this.
var exportable = []string{shared.LicencePublic, shared.LicenceShareable, licenceTenantProvided}

type importPayload struct {
	ImportID uuid.UUID  `json:"import_id"`
	ListID   *uuid.UUID `json:"list_id,omitempty"`
}

// ImportView is an import with its job.
type ImportView struct {
	repo.GetImportJobRow
	JobID uuid.UUID
}

// StartImport queues a CSV import. The user must attest they may lawfully use the data.
func (s *Service) StartImport(ctx context.Context, p authctx.Principal, fileID uuid.UUID, mapping map[string]string, listID *uuid.UUID) (ImportView, error) {
	meta, err := s.Files.Meta(ctx, p, fileID)
	if err != nil {
		return ImportView{}, err
	}
	if meta.OwnerModule != importPurpose {
		return ImportView{}, apperr.Validation(apperr.FieldError{Field: "file_id", Code: "wrong_file_purpose"})
	}
	var out ImportView
	err = s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		if listID != nil {
			if _, err := q.GetList(ctx, repo.GetListParams{TenantID: p.TenantID, ID: *listID}); err != nil {
				return notFound(err)
			}
		}
		id, err := q.InsertImportJob(ctx, repo.InsertImportJobParams{TenantID: p.TenantID, SourceFileID: fileID, Mapping: mustJSON(mapping), CreatedBy: uid(p)})
		if err != nil {
			return err
		}
		if out.JobID, _, err = jobs.Enqueue(ctx, tx, jobs.Spec{TenantID: &p.TenantID, Kind: JobImport, Payload: importPayload{ImportID: id, ListID: listID}, IdempotencyKey: "import:" + id.String(), MaxAttempts: 3}); err != nil {
			return err
		}
		if err := audit.Record(ctx, tx, userAudit(p, "lead.import_started", "import_job", &id, map[string]any{"file_id": fileID, "lawful_source_attested": true})); err != nil {
			return err
		}
		out.GetImportJobRow, err = q.GetImportJob(ctx, repo.GetImportJobParams{TenantID: p.TenantID, ID: id})
		return err
	})
	return out, err
}

// GetImport returns an import.
func (s *Service) GetImport(ctx context.Context, p authctx.Principal, id uuid.UUID) (repo.GetImportJobRow, error) {
	var out repo.GetImportJobRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.GetImportJob(ctx, repo.GetImportJobParams{TenantID: p.TenantID, ID: id})
		return notFound(err)
	})
	return out, err
}

type rowOutcome struct {
	line            int
	domain, outcome string
	reason          string
}

// HandleImport reads the file and adds a prospect per new company. Imported names and facts are
// tenant data: only domains reach the shared company record.
func (s *Service) HandleImport(ctx context.Context, j jobs.Job) (err error) {
	var pl importPayload
	defer func() {
		if err != nil && j.Final(err) && j.TenantID != nil && pl.ImportID != uuid.Nil {
			tenant := *j.TenantID
			bg := context.WithoutCancel(ctx)
			if ferr := s.systemTx(bg, tenant, func(q *repo.Queries, _ pgx.Tx) error {
				return q.SetImportStatus(bg, repo.SetImportStatusParams{TenantID: tenant, ID: pl.ImportID, Status: "failed"})
			}); ferr != nil {
				slog.WarnContext(bg, "record failed import", "error", ferr)
			}
		}
	}()
	if err := j.Decode(&pl); err != nil {
		return err
	}
	if j.TenantID == nil {
		return jobs.Permanent(errors.New("import job without tenant"))
	}
	tenant := *j.TenantID
	var imp repo.GetImportJobRow
	err = s.systemTx(ctx, tenant, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		if imp, err = q.GetImportJob(ctx, repo.GetImportJobParams{TenantID: tenant, ID: pl.ImportID}); err != nil {
			return err
		}
		if imp.Status != "uploaded" && imp.Status != "validating" && imp.Status != "importing" {
			return nil
		}
		return q.SetImportStatus(ctx, repo.SetImportStatusParams{TenantID: tenant, ID: imp.ID, Status: "validating"})
	})
	if err != nil {
		return err
	}
	if imp.Status == "completed" || imp.Status == "failed" || imp.Status == "cancelled" {
		return nil
	}
	fail := func(reason string) error {
		report, _ := domain.WriteCSV([]string{"line", "domain", "outcome", "reason"}, [][]string{{"", "", "failed", reason}})
		var reportID *uuid.UUID
		if id, err := s.Files.SaveGenerated(ctx, tenant, Module, "import-report.csv", "text/csv", report); err == nil {
			reportID = &id
		}
		return s.systemTx(ctx, tenant, func(q *repo.Queries, _ pgx.Tx) error {
			return q.FinishImport(ctx, repo.FinishImportParams{TenantID: tenant, ID: imp.ID, Status: "failed", RowsTotal: ptr(int32(0)), RowsImported: ptr(int32(0)), RowsDuplicate: ptr(int32(0)), RowsRejected: ptr(int32(0)), ReportFileID: reportID})
		})
	}
	file, purpose, err := s.Files.ReadForTenant(ctx, tenant, imp.SourceFileID)
	if err != nil {
		var ae *apperr.Error
		if errors.As(err, &ae) {
			return fail("the file is not available: " + ae.Code)
		}
		return err
	}
	if purpose != importPurpose {
		return fail("the file was not uploaded for import")
	}
	var mapping map[string]string
	if err := json.Unmarshal(imp.Mapping, &mapping); err != nil {
		return jobs.Permanent(err)
	}
	rows, err := domain.ParseImport(file.Body, mapping)
	if err != nil {
		return fail(err.Error())
	}
	var outcomes []rowOutcome
	type candidate struct {
		line    int
		domain  string
		company uuid.UUID
		fields  map[string]string
	}
	var cands []candidate
	seen := map[string]bool{}
	for _, r := range rows {
		norm, err := shared.NormalizeDomain(r.Fields["domain"])
		if err != nil {
			outcomes = append(outcomes, rowOutcome{r.Line, r.Fields["domain"], "rejected", "invalid_domain"})
			continue
		}
		if seen[norm] {
			outcomes = append(outcomes, rowOutcome{r.Line, norm, "duplicate", "repeated_in_file"})
			continue
		}
		seen[norm] = true
		if c := strings.ToUpper(r.Fields["country"]); c != "" {
			r.Fields["country"] = c
		}
		if e := r.Fields["employees"]; e != "" {
			if n, err := strconv.Atoi(strings.ReplaceAll(e, ",", "")); err != nil || n <= 0 {
				outcomes = append(outcomes, rowOutcome{r.Line, norm, "rejected", "invalid_employees"})
				continue
			}
		}
		company, err := s.Shared.ResolveOrCreate(ctx, shared.Signal{Domain: norm})
		if err != nil {
			outcomes = append(outcomes, rowOutcome{r.Line, norm, "rejected", "invalid_domain"})
			continue
		}
		cands = append(cands, candidate{r.Line, norm, company, r.Fields})
	}
	var created []uuid.UUID
	imported, duplicate := 0, 0
	err = s.systemTx(ctx, tenant, func(q *repo.Queries, tx pgx.Tx) error {
		if err := q.SetImportStatus(ctx, repo.SetImportStatusParams{TenantID: tenant, ID: imp.ID, Status: "importing"}); err != nil {
			return err
		}
		actor := authctx.Principal{TenantID: tenant}
		if imp.CreatedBy != nil {
			actor.UserID = *imp.CreatedBy
		}
		var members []uuid.UUID
		for _, c := range cands {
			if err := checkRowCodes(ctx, tx, c.fields); err != nil {
				outcomes = append(outcomes, rowOutcome{c.line, c.domain, "rejected", err.Error()})
				continue
			}
			name := c.fields["name"]
			if name == "" {
				name = c.domain
			}
			id, isNew, err := s.insertProspect(ctx, q, tx, actor, c.company, name, c.domain, "import", ptr(imp.ID.String()))
			if err != nil {
				return err
			}
			members = append(members, id)
			if !isNew {
				duplicate++
				outcomes = append(outcomes, rowOutcome{c.line, c.domain, "duplicate", "already_a_prospect"})
				continue
			}
			imported++
			created = append(created, id)
			pv := Preview{DisplayName: c.fields["name"], Domain: c.domain, Country: c.fields["country"], Industry: c.fields["industry"], Licence: licenceTenantProvided}
			if n, err := strconv.Atoi(strings.ReplaceAll(c.fields["employees"], ",", "")); err == nil {
				pv.Employees = n
			}
			if err := insertPreviewAttributes(ctx, q, tenant, id, "import", pv); err != nil {
				return err
			}
		}
		if pl.ListID != nil && len(members) > 0 {
			if err := addMembers(ctx, q, tenant, imp.CreatedBy, *pl.ListID, members); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	rejected := 0
	report := [][]string{}
	slices.SortFunc(outcomes, func(a, b rowOutcome) int { return a.line - b.line })
	for _, o := range outcomes {
		if o.outcome == "rejected" {
			rejected++
		}
		if o.outcome == "duplicate" && o.reason == "repeated_in_file" {
			duplicate++
		}
		report = append(report, []string{strconv.Itoa(o.line), o.domain, o.outcome, o.reason})
	}
	var reportID *uuid.UUID
	if len(report) > 0 {
		body, err := domain.WriteCSV([]string{"line", "domain", "outcome", "reason"}, report)
		if err != nil {
			return err
		}
		id, err := s.Files.SaveGenerated(ctx, tenant, Module, "import-report.csv", "text/csv", body)
		if err != nil {
			return err
		}
		reportID = &id
	}
	err = s.systemTx(ctx, tenant, func(q *repo.Queries, _ pgx.Tx) error {
		return q.FinishImport(ctx, repo.FinishImportParams{TenantID: tenant, ID: imp.ID, Status: "completed", RowsTotal: ptr(int32(len(rows))), RowsImported: ptr(int32(imported)), //nolint:gosec // Bounded by MaxImportRows.
			RowsDuplicate: ptr(int32(duplicate)), RowsRejected: ptr(int32(rejected)), ReportFileID: reportID}) //nolint:gosec // Bounded.
	})
	if err != nil {
		return err
	}
	s.scoreQuietly(ctx, tenant, created)
	return nil
}

func checkRowCodes(ctx context.Context, tx pgx.Tx, f map[string]string) error {
	var cs, is []string
	if f["country"] != "" {
		cs = []string{f["country"]}
	}
	if f["industry"] != "" {
		is = []string{f["industry"]}
	}
	if len(cs) == 0 && len(is) == 0 {
		return nil
	}
	// A savepoint keeps one bad row from aborting the import's transaction.
	sp, err := tx.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = sp.Rollback(ctx) }()
	if err := shared.CheckCodes(ctx, sp, cs, is); err != nil {
		var ae *apperr.Error
		if errors.As(err, &ae) && len(ae.Fields) > 0 {
			return errors.New(ae.Fields[0].Code)
		}
		return err
	}
	return sp.Commit(ctx)
}

// Exports.

type exportCriteria struct {
	ListID *uuid.UUID `json:"list_id,omitempty"`
	Status *string    `json:"status,omitempty"`
}

type exportPayload struct {
	ExportID uuid.UUID `json:"export_id"`
}

// ExportView is an export with its job.
type ExportView struct {
	repo.GetExportJobRow
	JobID uuid.UUID
}

// StartExport queues a CSV export of prospects.
func (s *Service) StartExport(ctx context.Context, p authctx.Principal, listID *uuid.UUID, status *string) (ExportView, error) {
	var out ExportView
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		if listID != nil {
			if _, err := q.GetList(ctx, repo.GetListParams{TenantID: p.TenantID, ID: *listID}); err != nil {
				return notFound(err)
			}
		}
		crit := exportCriteria{ListID: listID, Status: status}
		id, err := q.InsertExportJob(ctx, repo.InsertExportJobParams{TenantID: p.TenantID, Criteria: mustJSON(crit), CreatedBy: uid(p)})
		if err != nil {
			return err
		}
		if out.JobID, _, err = jobs.Enqueue(ctx, tx, jobs.Spec{TenantID: &p.TenantID, Kind: JobExport, Payload: exportPayload{ExportID: id}, IdempotencyKey: "export:" + id.String(), MaxAttempts: 3}); err != nil {
			return err
		}
		if err := audit.Record(ctx, tx, userAudit(p, "lead.export_started", "export_job", &id, crit)); err != nil {
			return err
		}
		out.GetExportJobRow, err = q.GetExportJob(ctx, repo.GetExportJobParams{TenantID: p.TenantID, ID: id})
		return err
	})
	return out, err
}

// GetExport returns an export.
func (s *Service) GetExport(ctx context.Context, p authctx.Principal, id uuid.UUID) (repo.GetExportJobRow, error) {
	var out repo.GetExportJobRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.GetExportJob(ctx, repo.GetExportJobParams{TenantID: p.TenantID, ID: id})
		return notFound(err)
	})
	return out, err
}

// DownloadExport returns a finished export's file.
func (s *Service) DownloadExport(ctx context.Context, p authctx.Principal, id uuid.UUID) (files.Download, error) {
	e, err := s.GetExport(ctx, p, id)
	if err != nil {
		return files.Download{}, err
	}
	if e.FileID == nil {
		return files.Download{}, apperr.Conflict("export_not_ready", "This export is not ready yet")
	}
	return s.Files.Download(ctx, p, *e.FileID)
}

// DownloadImportReport returns an import's row report.
func (s *Service) DownloadImportReport(ctx context.Context, p authctx.Principal, id uuid.UUID) (files.Download, error) {
	imp, err := s.GetImport(ctx, p, id)
	if err != nil {
		return files.Download{}, err
	}
	if imp.ReportFileID == nil {
		return files.Download{}, apperr.NotFound()
	}
	return s.Files.Download(ctx, p, *imp.ReportFileID)
}

var exportHeader = []string{"name", "domain", "country", "industry", "employees", "technology", "status", "score", "data_quality"}

// HandleExport writes the CSV. Values whose licence does not allow export are left blank and counted.
func (s *Service) HandleExport(ctx context.Context, j jobs.Job) (err error) {
	var pl exportPayload
	defer func() {
		if err != nil && j.Final(err) && j.TenantID != nil && pl.ExportID != uuid.Nil {
			tenant := *j.TenantID
			bg := context.WithoutCancel(ctx)
			if ferr := s.systemTx(bg, tenant, func(q *repo.Queries, _ pgx.Tx) error {
				return q.FinishExport(bg, repo.FinishExportParams{TenantID: tenant, ID: pl.ExportID, Status: "failed"})
			}); ferr != nil {
				slog.WarnContext(bg, "record failed export", "error", ferr)
			}
		}
	}()
	if err := j.Decode(&pl); err != nil {
		return err
	}
	if j.TenantID == nil {
		return jobs.Permanent(errors.New("export job without tenant"))
	}
	tenant := *j.TenantID
	var rows [][]string
	withheld := 0
	done := false
	err = s.systemTx(ctx, tenant, func(q *repo.Queries, _ pgx.Tx) error {
		e, err := q.GetExportJob(ctx, repo.GetExportJobParams{TenantID: tenant, ID: pl.ExportID})
		if err != nil {
			return err
		}
		if e.Status != "queued" && e.Status != "running" {
			done = true
			return nil
		}
		var crit exportCriteria
		if err := json.Unmarshal(e.Criteria, &crit); err != nil {
			return err
		}
		ps, err := q.ExportProspects(ctx, repo.ExportProspectsParams{TenantID: tenant, Status: crit.Status, ListID: crit.ListID})
		if err != nil {
			return err
		}
		ids := make([]uuid.UUID, len(ps))
		for i, p := range ps {
			ids[i] = p.ID
		}
		attrs, err := q.LatestAttributesFor(ctx, repo.LatestAttributesForParams{TenantID: tenant, ProspectIds: ids})
		if err != nil {
			return err
		}
		byProspect := map[uuid.UUID]map[string]repo.LatestAttributesForRow{}
		for _, a := range attrs {
			if byProspect[a.ProspectID] == nil {
				byProspect[a.ProspectID] = map[string]repo.LatestAttributesForRow{}
			}
			byProspect[a.ProspectID][a.Attribute] = a
		}
		for _, p := range ps {
			value := func(attr string) string {
				a, ok := byProspect[p.ID][attr]
				if !ok {
					return ""
				}
				if !slices.Contains(exportable, a.LicenceClass) {
					withheld++
					return ""
				}
				return attrText(a.Value)
			}
			name := value(attrName)
			if _, ok := byProspect[p.ID][attrName]; !ok {
				name = p.DisplayName // A name the tenant typed or imported.
			}
			domainName := ""
			if p.Domain != nil {
				domainName = *p.Domain // Domains identify the company and are not licensed data.
			}
			score := ""
			if p.LatestScore != nil {
				score = strconv.Itoa(int(*p.LatestScore))
			}
			rows = append(rows, []string{name, domainName, value(attrCountry), value(attrIndustry), value(attrEmployees), value(attrTechnology), p.Status, score, p.DataQuality})
		}
		return nil
	})
	if err != nil || done {
		return err
	}
	body, err := domain.WriteCSV(exportHeader, rows)
	if err != nil {
		return err
	}
	fileID, err := s.Files.SaveGenerated(ctx, tenant, Module, fmt.Sprintf("prospects-%s.csv", pl.ExportID.String()[:8]), "text/csv", body)
	if err != nil {
		return err
	}
	return s.systemTx(ctx, tenant, func(q *repo.Queries, _ pgx.Tx) error {
		return q.FinishExport(ctx, repo.FinishExportParams{TenantID: tenant, ID: pl.ExportID, Status: "completed", FileID: &fileID, RowsTotal: ptr(int32(len(rows))), ValuesWithheld: ptr(int32(withheld))}) //nolint:gosec // Bounded by the 10000-row query limit.
	})
}

func attrText(v []byte) string {
	var s string
	if json.Unmarshal(v, &s) == nil {
		return s
	}
	var list []string
	if json.Unmarshal(v, &list) == nil {
		return strings.Join(list, ";")
	}
	var n json.Number
	if json.Unmarshal(v, &n) == nil {
		return n.String()
	}
	return ""
}
