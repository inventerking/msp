package servicetest

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/jobs"
	"mspengine/internal/platform/commercial"
)

func isExhausted(err error) bool {
	var e *apperr.Error
	return errors.As(err, &e) && e.Code == "credits_exhausted"
}

func TestReserveCommitRelease(t *testing.T) {
	ten := newTenant(t)
	grant(t, ten, 100)
	res, err := com.Reserve(ctx, ten, commercial.MeterAI, 30, "crm", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if b, r := balance(t, ten); b != 100 || r != 30 {
		t.Fatalf("balance %d reserved %d", b, r)
	}
	if err := com.Commit(ctx, ten, res, 10); err != nil {
		t.Fatal(err)
	}
	if b, r := balance(t, ten); b != 90 || r != 0 {
		t.Fatalf("balance %d reserved %d", b, r)
	}
	// Repeats, and a release after a commit, change nothing.
	if err := com.Commit(ctx, ten, res, 10); err != nil {
		t.Fatal(err)
	}
	if err := com.Release(ctx, ten, res); err != nil {
		t.Fatal(err)
	}
	if b, r := balance(t, ten); b != 90 || r != 0 {
		t.Fatalf("balance %d reserved %d", b, r)
	}
}

func TestCommitIsCappedAtTheReservation(t *testing.T) {
	ten := newTenant(t)
	grant(t, ten, 100)
	res, err := com.Reserve(ctx, ten, commercial.MeterAI, 10, "crm", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := com.Commit(ctx, ten, res, 999); err != nil {
		t.Fatal(err)
	}
	if b, _ := balance(t, ten); b != 90 {
		t.Fatalf("balance %d", b)
	}
}

func TestConcurrentReservationsCannotOverspend(t *testing.T) {
	ten := newTenant(t)
	grant(t, ten, 100)
	var ok, refused atomic.Int64
	var wg sync.WaitGroup
	for range 30 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := com.Reserve(ctx, ten, commercial.MeterAI, 10, "crm", time.Hour)
			switch {
			case err == nil:
				ok.Add(1)
			case isExhausted(err):
				refused.Add(1)
			default:
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if ok.Load() != 10 || refused.Load() != 20 {
		t.Fatalf("ok=%d refused=%d", ok.Load(), refused.Load())
	}
	if b, r := balance(t, ten); b != 100 || r != 100 {
		t.Fatalf("balance %d reserved %d", b, r)
	}
}

func TestNoCreditsMeansExhausted(t *testing.T) {
	if _, err := com.Reserve(ctx, newTenant(t), commercial.MeterAI, 1, "crm", time.Hour); !isExhausted(err) {
		t.Fatalf("got %v", err)
	}
}

func TestGrantIsIdempotent(t *testing.T) {
	ten := newTenant(t)
	key := uuid.NewString()
	for range 3 {
		if err := com.Grant(ctx, ten, commercial.MeterAI, 50, key, nil, "test"); err != nil {
			t.Fatal(err)
		}
	}
	if b, _ := balance(t, ten); b != 50 {
		t.Fatalf("balance %d", b)
	}
}

func TestLedgerReconcilesWithBalance(t *testing.T) {
	ten := newTenant(t)
	grant(t, ten, 200)
	var wg sync.WaitGroup
	for i := range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := com.Reserve(ctx, ten, commercial.MeterAI, 10, "crm", time.Hour)
			if err != nil {
				t.Error(err)
				return
			}
			if i%2 == 0 {
				err = com.Commit(ctx, ten, res, 7)
			} else {
				err = com.Release(ctx, ten, res)
			}
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	b, r := balance(t, ten)
	if b != 200-6*7 || r != 0 {
		t.Fatalf("balance %d reserved %d", b, r)
	}
	var net int64
	if err := env.Super.QueryRow(ctx, `SELECT COALESCE(SUM(CASE entry_type WHEN 'grant' THEN amount WHEN 'commit' THEN -amount ELSE 0 END), 0) FROM platform.credit_ledger WHERE tenant_id = $1`, ten).Scan(&net); err != nil {
		t.Fatal(err)
	}
	if net != b {
		t.Fatalf("ledger says %d, balance says %d", net, b)
	}
}

func TestCrashedReservationIsSwept(t *testing.T) {
	ten := newTenant(t)
	grant(t, ten, 100)
	if _, err := com.Reserve(ctx, ten, commercial.MeterAI, 40, "crm", -time.Minute); err != nil {
		t.Fatal(err)
	}
	if b, r := balance(t, ten); b != 100 || r != 40 {
		t.Fatalf("balance %d reserved %d", b, r)
	}
	if err := com.HandleSweepReservations(ctx, jobs.Job{}); err != nil {
		t.Fatal(err)
	}
	if b, r := balance(t, ten); b != 100 || r != 0 {
		t.Fatalf("after sweep: balance %d reserved %d", b, r)
	}
	if err := com.HandleSweepReservations(ctx, jobs.Job{}); err != nil {
		t.Fatal(err)
	}
	if b, r := balance(t, ten); b != 100 || r != 0 {
		t.Fatalf("after a second sweep: balance %d reserved %d", b, r)
	}
}

func TestTenantRoleCannotChangeCredits(t *testing.T) {
	for _, sql := range []string{
		`UPDATE platform.credit_accounts SET balance = 1000000`,
		`INSERT INTO platform.credit_ledger (tenant_id, account_id, entry_type, amount, idempotency_key, actor_type) VALUES (gen_random_uuid(), gen_random_uuid(), 'grant', 1, 'x', 'system')`,
		`SELECT * FROM platform.credit_ledger`,
	} {
		if _, err := env.Pools.App.Exec(ctx, sql); err == nil || !strings.Contains(err.Error(), "permission denied") {
			t.Errorf("%s: %v", sql, err)
		}
	}
}

func TestLowBalanceEventFiresOnce(t *testing.T) {
	ten := newTenant(t)
	grant(t, ten, 110)
	for range 3 {
		res, err := com.Reserve(ctx, ten, commercial.MeterAI, 8, "crm", time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if err := com.Commit(ctx, ten, res, 8); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	if err := env.Pools.System.QueryRow(ctx, `SELECT count(*) FROM jobs.outbox_events WHERE event_type = 'platform.credits.low' AND tenant_id = $1`, ten).Scan(&n); err != nil || n != 1 {
		t.Fatalf("events %d %v", n, err)
	}
}

func TestRevokedModuleStopsQueuedWork(t *testing.T) {
	ten := newTenant(t)
	entitle(t, ten, "crm")
	if err := com.RequireEntitled(ctx, ten, "crm"); err != nil {
		t.Fatal(err)
	}
	if err := com.RequireEntitled(ctx, ten, "outreach"); err == nil {
		t.Fatal("work ran for a module the tenant never had")
	}
}
