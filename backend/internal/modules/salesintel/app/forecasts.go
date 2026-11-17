package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/jobs"
	"mspengine/internal/modules/salesintel/domain"
	"mspengine/internal/modules/salesintel/repo"
	"mspengine/internal/platform/audit"
)

// RunView is a forecast run with its points. Previous marks a completed run that a newer completed
// run of the same kind has replaced, so a failed new run can still show the last good one.
type RunView struct {
	repo.GetRunRow
	Points   []repo.PointsForRow
	Previous bool
}

// RequestForecast queues a forecast. Asking again the same day for the same kind and horizon returns
// the same run. A revenue forecast is refused at once without enough history.
func (s *Service) RequestForecast(ctx context.Context, p authctx.Principal, kind string, horizon int) (RunView, error) {
	if kind != "revenue" && kind != "pipeline" {
		return RunView{}, invalid("kind", "invalid_kind")
	}
	if horizon == 0 {
		horizon = 6
	}
	if horizon < 1 || horizon > 24 {
		return RunView{}, invalid("horizon_months", "out_of_range")
	}
	if kind == "revenue" {
		f, err := s.loadFacts(ctx, p.TenantID)
		if err != nil {
			return RunView{}, err
		}
		if n := domain.HistoryMonths(f.in.Revenue, s.Now()); n < domain.MinHistoryMonths {
			return RunView{}, &apperr.Error{Status: http.StatusUnprocessableEntity, Code: "insufficient_history",
				Title: "A revenue forecast needs at least 3 complete months of contract history", Detail: "There is not enough history yet."}
		}
	}
	asOf := s.Now().UTC().Truncate(24 * time.Hour)
	var id uuid.UUID
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		existing, err := q.FindLiveRun(ctx, repo.FindLiveRunParams{TenantID: p.TenantID, Kind: kind, AsOf: asOf, HorizonMonths: int32(horizon)}) //nolint:gosec // 1 to 24.
		if err == nil {
			id = existing
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		id, err = q.InsertRun(ctx, repo.InsertRunParams{TenantID: p.TenantID, Kind: kind, MethodVersion: domain.MethodVersion, HorizonMonths: int32(horizon), AsOf: asOf, RequestedBy: uid(p)}) //nolint:gosec // 1 to 24.
		if err != nil {
			return err
		}
		job, _, err := jobs.Enqueue(ctx, tx, jobs.Spec{TenantID: &p.TenantID, Kind: JobForecast, Queue: jobs.QueueAnalytics, MaxAttempts: 3,
			Payload: map[string]any{"run_id": id}, IdempotencyKey: "si-forecast:" + id.String()})
		if err != nil {
			return err
		}
		if err := q.SetRunJob(ctx, repo.SetRunJobParams{TenantID: p.TenantID, ID: id, JobID: &job}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "sales_intel.forecast_requested", "forecast_run", &id, map[string]any{"kind": kind, "horizon_months": horizon}))
	})
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "23505" {
		return s.RequestForecast(ctx, p, kind, horizon) // Another request created it first.
	}
	if err != nil {
		return RunView{}, err
	}
	return s.Forecast(ctx, p, id)
}

// HandleForecast computes a run. A refusal or a final failure marks the run failed; earlier runs stay.
func (s *Service) HandleForecast(ctx context.Context, j jobs.Job) error {
	if j.TenantID == nil {
		return nil
	}
	tenant := *j.TenantID
	var in struct {
		Run uuid.UUID `json:"run_id"`
	}
	if err := j.Decode(&in); err != nil {
		return jobs.Permanent(err)
	}
	var run repo.GetRunRow
	if err := s.systemTx(ctx, tenant, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		if run, err = q.GetRun(ctx, repo.GetRunParams{TenantID: tenant, ID: in.Run}); err != nil {
			return err
		}
		if run.Status == "completed" || run.Status == "failed" {
			return nil
		}
		_, err = q.StartRun(ctx, repo.StartRunParams{TenantID: tenant, ID: in.Run})
		return err
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	if run.Status == "completed" || run.Status == "failed" {
		return nil
	}
	err := s.computeForecast(ctx, tenant, run)
	if err != nil && j.Final(err) {
		assumptions, _ := json.Marshal(map[string]any{"method_version": domain.MethodVersion})
		if ferr := s.systemTx(ctx, tenant, func(q *repo.Queries, _ pgx.Tx) error {
			return q.FailRun(ctx, repo.FailRunParams{TenantID: tenant, ID: run.ID, ErrorCode: ptr("forecast_failed"), Assumptions: assumptions})
		}); ferr != nil {
			return ferr
		}
	}
	return err
}

func (s *Service) computeForecast(ctx context.Context, tenant uuid.UUID, run repo.GetRunRow) error {
	f, err := s.loadFacts(ctx, tenant)
	if err != nil {
		return err
	}
	in := domain.ForecastInput{Revenue: f.in.Revenue, Deals: f.in.Deals, Convert: f.in.Convert, Now: s.Now(), Horizon: int(run.HorizonMonths)}
	var res domain.ForecastResult
	if run.Kind == "revenue" {
		res = domain.RevenueForecast(in)
	} else {
		res = domain.PipelineForecast(in)
	}
	assumptions, err := json.Marshal(res.Assumptions)
	if err != nil {
		return err
	}
	return s.systemTx(ctx, tenant, func(q *repo.Queries, tx pgx.Tx) error {
		if res.Refusal != "" {
			return q.FailRun(ctx, repo.FailRunParams{TenantID: tenant, ID: run.ID, ErrorCode: &res.Refusal, Assumptions: assumptions})
		}
		if err := q.DeletePoints(ctx, repo.DeletePointsParams{TenantID: tenant, RunID: run.ID}); err != nil {
			return err
		}
		for _, pt := range res.Points {
			if err := q.InsertPoint(ctx, repo.InsertPointParams{TenantID: tenant, RunID: run.ID, PeriodStart: pt.Period, ExpectedMinor: pt.Expected,
				LowMinor: pt.Low, HighMinor: pt.High, Currency: f.reporting}); err != nil {
				return err
			}
		}
		if err := q.CompleteRun(ctx, repo.CompleteRunParams{TenantID: tenant, ID: run.ID, Assumptions: assumptions, Currency: &f.reporting}); err != nil {
			return err
		}
		return emit(ctx, tx, tenant, "forecast.completed", "forecast_run", run.ID, map[string]any{"run_id": run.ID, "kind": run.Kind})
	})
}

// Forecasts lists runs, newest first, each with its points.
func (s *Service) Forecasts(ctx context.Context, p authctx.Principal, kind *string) ([]RunView, error) {
	var runs []repo.ListRunsRow
	var points []repo.PointsForRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		if runs, err = q.ListRuns(ctx, repo.ListRunsParams{TenantID: p.TenantID, Kind: kind}); err != nil {
			return err
		}
		ids := make([]uuid.UUID, len(runs))
		for i, r := range runs {
			ids[i] = r.ID
		}
		points, err = q.PointsFor(ctx, repo.PointsForParams{TenantID: p.TenantID, RunIds: ids})
		return err
	})
	if err != nil {
		return nil, err
	}
	byRun := map[uuid.UUID][]repo.PointsForRow{}
	for _, pt := range points {
		byRun[pt.RunID] = append(byRun[pt.RunID], pt)
	}
	latest := map[string]bool{}
	out := make([]RunView, 0, len(runs))
	for _, r := range runs {
		v := RunView{GetRunRow: repo.GetRunRow(r), Points: byRun[r.ID]}
		if r.Status == "completed" {
			v.Previous = latest[r.Kind]
			latest[r.Kind] = true
		}
		out = append(out, v)
	}
	return out, nil
}

// Forecast returns one run.
func (s *Service) Forecast(ctx context.Context, p authctx.Principal, id uuid.UUID) (RunView, error) {
	var v RunView
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		if v.GetRunRow, err = q.GetRun(ctx, repo.GetRunParams{TenantID: p.TenantID, ID: id}); err != nil {
			return notFound(err)
		}
		v.Points, err = q.PointsFor(ctx, repo.PointsForParams{TenantID: p.TenantID, RunIds: []uuid.UUID{id}})
		return err
	})
	return v, err
}
