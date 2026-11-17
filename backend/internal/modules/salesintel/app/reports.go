package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/csvx"
	"mspengine/internal/kernel/jobs"
	"mspengine/internal/modules/salesintel/domain"
	"mspengine/internal/modules/salesintel/repo"
	"mspengine/internal/platform/audit"
)

// Definition is a saved report: which metrics, over which months, by which dimension.
type Definition struct {
	Metrics   []string `json:"metrics"`
	From      string   `json:"from"`
	To        string   `json:"to"`
	Dimension string   `json:"dimension,omitempty"`
}

func (d Definition) check() error {
	if len(d.Metrics) == 0 || len(d.Metrics) > 40 {
		return invalid("definition.metrics", "between_1_and_40")
	}
	for _, k := range d.Metrics {
		if _, ok := domain.Lookup(k); !ok {
			return invalid("definition.metrics", "unknown_metric")
		}
	}
	from, err1 := time.Parse(time.DateOnly, d.From)
	to, err2 := time.Parse(time.DateOnly, d.To)
	if err1 != nil || err2 != nil || to.Before(from) {
		return invalid("definition.from", "invalid_range")
	}
	switch d.Dimension {
	case "", domain.All, domain.BySource, domain.ByStage, domain.ByCamp:
	default:
		// Owner breakdowns are on the team screen only, behind their own permission.
		return invalid("definition.dimension", "not_available")
	}
	return nil
}

// CreateReport saves a report definition.
func (s *Service) CreateReport(ctx context.Context, p authctx.Principal, name string, def Definition) (repo.InsertDefinitionRow, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 120 {
		return repo.InsertDefinitionRow{}, invalid("name", "invalid_length")
	}
	if err := def.check(); err != nil {
		return repo.InsertDefinitionRow{}, err
	}
	body, _ := json.Marshal(def)
	var out repo.InsertDefinitionRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.InsertDefinition(ctx, repo.InsertDefinitionParams{TenantID: p.TenantID, Name: name, Definition: body, CreatedBy: uid(p)})
		return err
	})
	return out, err
}

// Reports lists saved reports.
func (s *Service) Reports(ctx context.Context, p authctx.Principal) ([]repo.ListDefinitionsRow, error) {
	var out []repo.ListDefinitionsRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.ListDefinitions(ctx, p.TenantID)
		return err
	})
	return out, err
}

// RequestExport queues a CSV of a saved report. Exports are audited.
func (s *Service) RequestExport(ctx context.Context, p authctx.Principal, definition uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		if _, err := q.GetDefinition(ctx, repo.GetDefinitionParams{TenantID: p.TenantID, ID: definition}); err != nil {
			return notFound(err)
		}
		var err error
		if id, err = q.InsertExport(ctx, repo.InsertExportParams{TenantID: p.TenantID, DefinitionID: &definition, CreatedBy: uid(p)}); err != nil {
			return err
		}
		if _, _, err := jobs.Enqueue(ctx, tx, jobs.Spec{TenantID: &p.TenantID, Kind: JobExport, Queue: jobs.QueueAnalytics, MaxAttempts: 3,
			Payload: map[string]any{"export_id": id}, IdempotencyKey: "si-export:" + id.String()}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "sales_intel.report_exported", "report_export", &id, map[string]any{"definition_id": definition}))
	})
	return id, err
}

// HandleExport writes the CSV. Every cell is made safe against spreadsheet formulas.
func (s *Service) HandleExport(ctx context.Context, j jobs.Job) error {
	if j.TenantID == nil {
		return nil
	}
	tenant := *j.TenantID
	var in struct {
		ID uuid.UUID `json:"export_id"`
	}
	if err := j.Decode(&in); err != nil {
		return jobs.Permanent(err)
	}
	var exp repo.GetExportRow
	var def repo.GetDefinitionRow
	var rows []repo.ListSnapshotsRow
	labels := map[string]string{}
	err := s.systemTx(ctx, tenant, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		if exp, err = q.GetExport(ctx, repo.GetExportParams{TenantID: tenant, ID: in.ID}); err != nil {
			return err
		}
		if exp.Status == "ready" || exp.DefinitionID == nil {
			return nil
		}
		if def, err = q.GetDefinition(ctx, repo.GetDefinitionParams{TenantID: tenant, ID: *exp.DefinitionID}); err != nil {
			return err
		}
		var d Definition
		if err := json.Unmarshal(def.Definition, &d); err != nil {
			return err
		}
		from, _ := time.Parse(time.DateOnly, d.From)
		to, _ := time.Parse(time.DateOnly, d.To)
		dim := d.Dimension
		if dim == "" {
			dim = domain.All
		}
		if rows, err = q.ListSnapshots(ctx, repo.ListSnapshotsParams{TenantID: tenant, Keys: d.Metrics, FromMonth: domain.MonthStart(from), ToMonth: domain.MonthStart(to), Dimension: &dim}); err != nil {
			return err
		}
		ls, err := q.ListLabels(ctx, tenant)
		for _, l := range ls {
			labels[l.Ref.String()] = l.Label
		}
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil || exp.Status == "ready" || exp.DefinitionID == nil {
		return err
	}
	out := make([][]string, 0, len(rows))
	for _, r := range rows {
		d, _ := domain.Lookup(r.MetricKey)
		cur := ""
		if r.Currency != nil {
			cur = *r.Currency
		}
		value := ""
		if r.Value != nil {
			value = strings.TrimRight(strings.TrimRight(formatFloat(*r.Value), "0"), ".")
		}
		label := r.DimensionValue
		if l, ok := labels[r.DimensionValue]; ok {
			label = l
		}
		out = append(out, []string{r.PeriodStart.Format("2006-01"), r.MetricKey, d.Name, r.Dimension, label, value, r.ValueKind, cur, strings.Join(r.Flags, " ")})
	}
	body, err := csvx.Write([]string{"month", "metric_key", "metric", "dimension", "dimension_value", "value", "value_kind", "currency", "flags"}, out)
	if err != nil {
		return err
	}
	name := "report-" + slug(def.Name) + ".csv"
	file, err := s.Files.SaveGenerated(ctx, tenant, Module, name, "text/csv", body)
	if err != nil {
		if j.Final(err) {
			_ = s.systemTx(ctx, tenant, func(q *repo.Queries, _ pgx.Tx) error {
				return q.FinishExport(ctx, repo.FinishExportParams{TenantID: tenant, ID: in.ID, Status: "failed", ErrorCode: ptr("save_failed")})
			})
		}
		return err
	}
	return s.systemTx(ctx, tenant, func(q *repo.Queries, _ pgx.Tx) error {
		return q.FinishExport(ctx, repo.FinishExportParams{TenantID: tenant, ID: in.ID, Status: "ready", FileID: &file})
	})
}

func formatFloat(v float64) string {
	b, _ := json.Marshal(v)
	s := string(b)
	if !strings.Contains(s, ".") {
		s += ".0"
	}
	return s
}

func slug(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case b.Len() > 0 && !strings.HasSuffix(b.String(), "-"):
			b.WriteByte('-')
		}
		if b.Len() >= 40 {
			break
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "export"
	}
	return out
}

// ExportView is an export's status, with a short-lived download link once it is ready.
type ExportView struct {
	repo.GetExportRow
	DownloadURL *string
	ExpiresAt   *time.Time
}

// ExportTTL is how long a download link works.
const ExportTTL = 5 * time.Minute

// Export returns an export. A ready one gets a fresh link that expires in minutes.
func (s *Service) Export(ctx context.Context, p authctx.Principal, id uuid.UUID) (ExportView, error) {
	var v ExportView
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		v.GetExportRow, err = q.GetExport(ctx, repo.GetExportParams{TenantID: p.TenantID, ID: id})
		return notFound(err)
	})
	if err != nil || v.Status != "ready" || v.FileID == nil {
		return v, err
	}
	token, err := s.Files.CreateGrant(ctx, p.TenantID, *v.FileID, ExportTTL, ptr(int32(3)))
	if err != nil {
		return v, err
	}
	url := "/api/v1/public/files/" + token
	exp := s.Now().Add(ExportTTL)
	v.DownloadURL, v.ExpiresAt = &url, &exp
	return v, nil
}
