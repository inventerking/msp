package api_test

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// TestRLSPerformanceComparison is the Phase 6 load comparison (risk R16): the queries a metrics
// rollup runs, timed with row-level security enforced and with it bypassed, on large fact tables
// shared by many tenants. It is slow, so it runs only with MSP_LOAD=1.
func TestRLSPerformanceComparison(t *testing.T) {
	if os.Getenv("MSP_LOAD") != "1" {
		t.Skip("set MSP_LOAD=1 to run the RLS load comparison")
	}
	const tenants, deals, revenue, outreach = 40, 25000, 5000, 10000
	ctx := context.Background()
	super := appConn(t, "postgres", "test_superuser") // Superuser: bypasses RLS, for seeding and the baseline.
	var ids []uuid.UUID
	start := time.Now()
	for i := range tenants {
		id := uuid.New()
		ids = append(ids, id)
		if _, err := super.Exec(ctx, `INSERT INTO identity.tenants (id, slug, name, status, default_locale, default_timezone, reporting_currency, data_region)
			VALUES ($1, $2::text, $2::text, 'active', 'en', 'UTC', 'GBP', 'eu')`, id, fmt.Sprintf("load-%d-%s", i, id.String()[:8])); err != nil {
			t.Fatal(err)
		}
		if _, err := super.Exec(ctx, `
			INSERT INTO sales_intelligence.fact_deal_events (tenant_id, source_event_id, event_type, deal_ref, stage_key, status, amount_minor, mrr_minor, currency, probability, occurred_at)
			SELECT $1::uuid, gen_random_uuid(), (ARRAY['crm.deal.created','crm.deal.stage_changed','crm.deal.won','crm.deal.lost'])[1 + g % 4],
			  md5($1::text || (g / 4)::text)::uuid, 'stage_' || (g % 6), (ARRAY['open','open','won','lost'])[1 + g % 4], g * 10, g, 'GBP', 50,
			  now() - (g % 700) * interval '1 day'
			FROM generate_series(1, $2::int) g`, id, deals); err != nil {
			t.Fatal(err)
		}
		if _, err := super.Exec(ctx, `
			INSERT INTO sales_intelligence.fact_revenue_events (tenant_id, source_event_id, movement, contract_ref, mrr_delta_minor, contract_mrr_minor, currency,
			  reporting_mrr_delta_minor, reporting_currency, occurred_at)
			SELECT $1::uuid, gen_random_uuid(), (ARRAY['new','expansion','contraction'])[1 + g % 3], gen_random_uuid(), 1000, 1000, 'GBP', 1000, 'GBP',
			  now() - (g % 700) * interval '1 day'
			FROM generate_series(1, $2::int) g`, id, revenue); err != nil {
			t.Fatal(err)
		}
		if _, err := super.Exec(ctx, `
			INSERT INTO sales_intelligence.fact_outreach_events (tenant_id, source_event_id, event_type, campaign_ref, occurred_at)
			SELECT $1::uuid, gen_random_uuid(), 'outreach.message.sent', md5($1::text || (g % 20)::text)::uuid, now() - (g % 700) * interval '1 day'
			FROM generate_series(1, $2::int) g`, id, outreach); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := super.Exec(ctx, `ANALYZE sales_intelligence.fact_deal_events, sales_intelligence.fact_revenue_events, sales_intelligence.fact_outreach_events`); err != nil {
		t.Fatal(err)
	}
	t.Logf("seeded %d tenants with %d fact rows in %s", tenants, tenants*(deals+revenue+outreach), time.Since(start).Round(time.Millisecond))

	queries := []struct{ name, sql string }{
		{"deal facts for one tenant (rollup read)", `SELECT event_type, deal_ref, stage_key, status, amount_minor, mrr_minor, currency, probability, occurred_at
			FROM sales_intelligence.fact_deal_events WHERE tenant_id = $1 ORDER BY occurred_at, created_at, id`},
		{"revenue facts for one tenant (rollup read)", `SELECT movement, contract_ref, mrr_delta_minor, currency, occurred_at
			FROM sales_intelligence.fact_revenue_events WHERE tenant_id = $1 ORDER BY occurred_at, created_at`},
		{"won deals by month for one tenant (aggregate)", `SELECT date_trunc('month', occurred_at), count(*) FROM sales_intelligence.fact_deal_events
			WHERE tenant_id = $1 AND status = 'won' GROUP BY 1`},
		{"campaign sends in the last 90 days (indexed range)", `SELECT campaign_ref, count(*) FROM sales_intelligence.fact_outreach_events
			WHERE tenant_id = $1 AND occurred_at > now() - interval '90 days' GROUP BY 1`},
	}
	target := ids[tenants/2]
	app := appConn(t, "msp_app", "dev_app")
	const runs = 25
	var report []string
	for _, q := range queries {
		bypass := timeQuery(t, super, nil, q.sql, target, runs)
		enforced := timeQuery(t, app, &target, q.sql, target, runs)
		// The policy alone, without the tenant condition in the query, is the case RLS protects.
		policyOnly := timeQuery(t, app, &target, strings.Replace(q.sql, "tenant_id = $1", "$1::uuid IS NOT NULL", 1), target, runs)
		ratio := float64(enforced.median) / float64(bypass.median)
		line := fmt.Sprintf("%-52s bypass median %8s p95 %8s | RLS median %8s p95 %8s | RLS only %8s | ratio %.2f",
			q.name, bypass.median, bypass.p95, enforced.median, enforced.p95, policyOnly.median, ratio)
		report = append(report, line)
		t.Log(line)
		// Tenant-leading indexes should keep the policy's cost small. A large gap means a plan change.
		if enforced.median > 2*bypass.median+5*time.Millisecond {
			t.Errorf("%s: RLS median %s is more than twice the bypass median %s", q.name, enforced.median, bypass.median)
		}
	}
	// The whole rollup for one large tenant, as the worker runs it.
	rolled := time.Now()
	if err := graph.SalesIntel.Rollup(ctx, target); err != nil {
		t.Fatal(err)
	}
	t.Logf("full metrics rollup for one tenant with %d facts: %s", deals+revenue+outreach, time.Since(rolled).Round(time.Millisecond))
	if out := os.Getenv("MSP_LOAD_REPORT"); out != "" {
		if err := os.WriteFile(out, []byte(strings.Join(report, "\n")+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Clean up so the shared test database stays small for other tests in the same run.
	for _, id := range ids {
		for _, table := range []string{"fact_deal_events", "fact_revenue_events", "fact_outreach_events", "metric_snapshots", "rollup_state"} {
			if _, err := super.Exec(ctx, `DELETE FROM sales_intelligence.`+table+` WHERE tenant_id = $1`, id); err != nil {
				t.Fatal(err)
			}
		}
	}
}

type timing struct{ median, p95 time.Duration }

func timeQuery(t *testing.T, c *pgx.Conn, tenant *uuid.UUID, sql string, arg uuid.UUID, runs int) timing {
	t.Helper()
	ctx := context.Background()
	var ds []time.Duration
	for i := 0; i < runs+3; i++ {
		tx, err := c.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if tenant != nil {
			if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenant.String()); err != nil {
				t.Fatal(err)
			}
		}
		began := time.Now()
		rows, err := tx.Query(ctx, sql, arg)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for rows.Next() {
			n++
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		elapsed := time.Since(began)
		_ = tx.Rollback(ctx)
		if n == 0 {
			t.Fatalf("query returned nothing: %s", sql)
		}
		if i >= 3 { // The first runs warm the cache.
			ds = append(ds, elapsed)
		}
	}
	sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
	return timing{median: ds[len(ds)/2].Round(10 * time.Microsecond), p95: ds[len(ds)*95/100].Round(10 * time.Microsecond)}
}
