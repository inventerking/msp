package app

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"mspengine/internal/kernel/metrics"
)

// APIMetrics is the API process's registry: HTTP requests and its connection pools.
func (g *Graph) APIMetrics() *metrics.Registry {
	r := metrics.New("api")
	r.Pools(g.poolMap())
	return r
}

// WorkerMetrics is the worker's registry. Platform-wide health gauges are read here, in one place,
// so running more API replicas does not multiply them. Alert rules still use max() in case more
// than one worker runs.
func (g *Graph) WorkerMetrics() *metrics.Registry {
	r := metrics.New("worker")
	r.Pools(g.poolMap())
	r.Gauges("jobs", func(ctx context.Context) ([]metrics.Sample, error) {
		st, err := g.Ops.Stats(ctx)
		if err != nil {
			return nil, err
		}
		var out []metrics.Sample
		for _, q := range st.Queues {
			l := map[string]string{"queue": q.Queue, "status": q.Status}
			out = append(out,
				metrics.Sample{Name: "msp_jobs", Help: "Jobs by queue and status.", Value: float64(q.Jobs), Labels: l},
				metrics.Sample{Name: "msp_jobs_oldest_seconds", Help: "Age of the oldest job by queue and status.", Value: float64(q.OldestSeconds), Labels: l})
		}
		dead, err := g.Ops.DeadLetters(ctx)
		if err != nil {
			return nil, err
		}
		return append(out,
			metrics.Sample{Name: "msp_dead_letters_unresolved", Help: "Failed jobs and events waiting for an admin decision (counts up to 200).", Value: float64(len(dead))},
			metrics.Sample{Name: "msp_outbox_oldest_unpublished_seconds", Help: "Age of the oldest event not yet published.", Value: float64(st.Events.OldestUnpublishedSeconds)},
			metrics.Sample{Name: "msp_event_deliveries_pending", Help: "Event deliveries waiting for a consumer.", Value: float64(st.Events.PendingDeliveries)},
			metrics.Sample{Name: "msp_event_deliveries_dead", Help: "Event deliveries that gave up and need a decision.", Value: float64(st.Events.DeadDeliveries)},
		), nil
	})
	r.Gauges("billing", func(ctx context.Context) ([]metrics.Sample, error) {
		h, err := g.Commercial.Health(ctx)
		if err != nil {
			return nil, err
		}
		return []metrics.Sample{
			{Name: "msp_billing_review_waiting", Help: "Purchases waiting for admin review.", Value: float64(h.ReviewWaiting)},
			{Name: "msp_billing_review_oldest_seconds", Help: "How long the oldest purchase has waited for review.", Value: h.ReviewOldestSeconds},
			{Name: "msp_billing_review_target_seconds", Help: "The review target set in the dunning policy.", Value: float64(h.ReviewTargetHours * 3600)},
			{Name: "msp_billing_refunds_pending", Help: "Refunds pending or failed.", Value: float64(h.RefundsPending)},
			{Name: "msp_billing_bank_transfers_pending", Help: "Bank transfers waiting to be confirmed.", Value: float64(h.BankTransfersPending)},
			{Name: "msp_billing_subscriptions_past_due", Help: "Subscriptions with a failed or overdue payment.", Value: float64(h.SubscriptionsPastDue)},
			{Name: "msp_billing_reconciliation_open_issues", Help: "Open differences found by reconciliation.", Value: float64(h.ReconciliationOpen)},
			{Name: "msp_billing_reconciliation_age_seconds", Help: "Time since reconciliation last completed; -1 if never.", Value: h.ReconciliationAgeSeconds},
		}, nil
	})
	// WAL archiving is what backups depend on between base backups, so its health is reported here.
	r.Gauges("postgres", func(ctx context.Context) ([]metrics.Sample, error) {
		var failed int64
		var lastAge, size float64
		err := g.Pools.System.QueryRow(ctx, `SELECT failed_count,
			COALESCE(EXTRACT(EPOCH FROM now() - last_archived_time), -1)::float8,
			pg_database_size(current_database())::float8 FROM pg_stat_archiver`).Scan(&failed, &lastAge, &size)
		return []metrics.Sample{
			{Name: "msp_pg_archive_failed_total", Help: "WAL segments that failed to archive since statistics reset.", Value: float64(failed)},
			{Name: "msp_pg_archive_last_success_age_seconds", Help: "Time since a WAL segment was last archived; -1 if never.", Value: lastAge},
			{Name: "msp_pg_database_bytes", Help: "Database size.", Value: size},
		}, err
	})
		r.Gauges("trust", func(ctx context.Context) ([]metrics.Sample, error) {
		n, err := g.Trust.OpenReports(ctx)
		return []metrics.Sample{{Name: "msp_abuse_reports_open", Help: "Abuse reports new or under review.", Value: float64(n)}}, err
	})
	return r
}

func (g *Graph) poolMap() map[string]*pgxpool.Pool {
	return map[string]*pgxpool.Pool{"app": g.Pools.App, "platform": g.Pools.Platform, "system": g.Pools.System}
}
