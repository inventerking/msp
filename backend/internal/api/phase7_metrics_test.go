package api_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The worker's health gauges read real tables; every source must be readable, and no tenant
// identifier may appear in a label.
func TestWorkerMetricsReadEverySource(t *testing.T) {
	c, _, tenant := newTenant(t, "Metrics Co")
	plan := billingPlan(t, "crm", 1500, 0, 0, 0)
	purchase(t, c, plan, "bank_transfer", "metrics-1-key")
	rec := httptest.NewRecorder()
	graph.WorkerMetrics().Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	b, _ := io.ReadAll(rec.Body)
	out := string(b)
	for _, src := range []string{"jobs", "billing", "trust", "db_pools", "postgres"} {
		if !strings.Contains(out, `msp_metrics_source_up{source="`+src+`"} 1`) {
			t.Fatalf("source %s not readable:\n%s", src, out)
		}
	}
	for _, name := range []string{"msp_billing_bank_transfers_pending", "msp_billing_review_target_seconds 86400", "msp_abuse_reports_open", "msp_outbox_oldest_unpublished_seconds"} {
		if !strings.Contains(out, name) {
			t.Fatalf("missing %s", name)
		}
	}
	if strings.Contains(out, tenant) {
		t.Fatal("a tenant ID appeared in the metrics")
	}
}
