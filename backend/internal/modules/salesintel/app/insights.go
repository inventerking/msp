package app

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/modules/salesintel/domain"
	"mspengine/internal/modules/salesintel/repo"
	"mspengine/internal/platform/audit"
	"mspengine/internal/platform/integration/aigateway"
)

// insightScopes are the screens commentary can be asked for, and their sections.
var insightScopes = map[string]string{"dashboard": "", "revenue": "revenue", "funnel": "win_loss", "campaigns": "campaigns"}

// InsightMetric is a metric the commentary was given.
type InsightMetric struct {
	ID        uuid.UUID `json:"id"`
	Key       string    `json:"key"`
	Period    string    `json:"period"`
	Display   string    `json:"display"`
	ValueKind string    `json:"value_kind"`
}

// Insight is AI commentary with the metrics it cites.
type Insight struct {
	ID         uuid.UUID
	Scope      string
	Commentary string
	Metrics    []InsightMetric
	Cited      []uuid.UUID
	CreatedAt  time.Time
}

// RequestInsight asks AI for commentary on aggregate metrics only: no names, contacts or deals. It
// must cite the metric IDs it uses, and every number in it is checked against those metrics; if
// any is not, nothing is kept.
func (s *Service) RequestInsight(ctx context.Context, p authctx.Principal, scope string) (Insight, error) {
	section, ok := insightScopes[scope]
	if !ok {
		return Insight{}, invalid("scope", "invalid_scope")
	}
	if s.AI == nil {
		return Insight{}, apperr.New(http.StatusServiceUnavailable, "ai_unavailable", "AI commentary is not available")
	}
	var keys []string
	if section == "" {
		for _, c := range dashboardCards {
			keys = append(keys, c.keys...)
		}
	} else {
		keys = sections[section].keys
	}
	cur, err := s.Currency(ctx, p.TenantID)
	if err != nil {
		return Insight{}, err
	}
	to := domain.MonthStart(s.Now())
	var rows []repo.ListSnapshotsRow
	if err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		rows, err = q.ListSnapshots(ctx, repo.ListSnapshotsParams{TenantID: p.TenantID, Keys: keys, FromMonth: to.AddDate(0, -2, 0), ToMonth: to, Dimension: ptr(domain.All)})
		return err
	}); err != nil {
		return Insight{}, err
	}
	var metrics []InsightMetric
	given := map[string]string{}
	var lines []string
	for _, r := range rows {
		if r.Value == nil {
			continue
		}
		def, _ := domain.Lookup(r.MetricKey)
		m := InsightMetric{ID: r.ID, Key: r.MetricKey, Period: r.PeriodStart.Format("2006-01"), Display: domain.Display(def, r.Value, cur), ValueKind: r.ValueKind}
		metrics = append(metrics, m)
		text := def.Name + " " + m.Period + ": " + m.Display + " (" + m.ValueKind + ")"
		given[r.ID.String()] = text
		lines = append(lines, "id="+r.ID.String()+" | "+text)
	}
	if len(metrics) == 0 {
		return Insight{}, apperr.Conflict("no_metrics", "There are no metrics to comment on yet")
	}
	res, err := s.AI.Run(ctx, aigateway.Request{TenantID: p.TenantID, Task: TaskCommentary, IdempotencyKey: uuid.NewString(),
		Input: aigateway.Input{Trusted: map[string]string{"metrics": strings.Join(lines, "\n")}}})
	if err != nil {
		return Insight{}, err
	}
	var out struct {
		Commentary string   `json:"commentary"`
		Cited      []string `json:"cited_metric_ids"`
	}
	if err := json.Unmarshal(res.Output, &out); err != nil || !domain.CommentaryValid(out.Commentary, out.Cited, given) {
		return Insight{}, apperr.New(http.StatusUnprocessableEntity, "ai_output_rejected", "The commentary used numbers that are not in your metrics, so it was not kept")
	}
	cited := make([]uuid.UUID, 0, len(out.Cited))
	for _, c := range out.Cited {
		cited = append(cited, uuid.MustParse(c))
	}
	content, _ := json.Marshal(map[string]any{"commentary": strings.TrimSpace(out.Commentary), "metrics": metrics})
	var row repo.InsertInsightRow
	err = s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		var err error
		if row, err = q.InsertInsight(ctx, repo.InsertInsightParams{TenantID: p.TenantID, Scope: scope, Content: content, CitedMetricIds: cited, AiRequestID: &res.RequestID, CreatedBy: uid(p)}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "sales_intel.insight_created", "ai_insight", &row.ID, map[string]any{"scope": scope, "ai_request_id": res.RequestID}))
	})
	if err != nil {
		return Insight{}, err
	}
	return insightOf(row.ID, row.Scope, row.Content, row.CitedMetricIds, row.CreatedAt), nil
}

func insightOf(id uuid.UUID, scope string, content []byte, cited []uuid.UUID, created time.Time) Insight {
	var c struct {
		Commentary string          `json:"commentary"`
		Metrics    []InsightMetric `json:"metrics"`
	}
	_ = json.Unmarshal(content, &c)
	return Insight{ID: id, Scope: scope, Commentary: c.Commentary, Metrics: c.Metrics, Cited: cited, CreatedAt: created}
}

// Insights lists recent commentary.
func (s *Service) Insights(ctx context.Context, p authctx.Principal) ([]Insight, error) {
	var rows []repo.ListInsightsRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		rows, err = q.ListInsights(ctx, p.TenantID)
		return err
	})
	out := make([]Insight, 0, len(rows))
	for _, r := range rows {
		out = append(out, insightOf(r.ID, r.Scope, r.Content, r.CitedMetricIds, r.CreatedAt))
	}
	return out, err
}
