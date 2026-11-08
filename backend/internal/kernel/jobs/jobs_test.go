package jobs_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/db"
	"mspengine/internal/kernel/jobs"
	"mspengine/internal/testenv"
)

var env *testenv.Env

func TestMain(m *testing.M) {
	var err error
	env, err = testenv.Start(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	env.Close()
	os.Exit(code)
}

func tenant(t *testing.T) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := env.Super.Exec(context.Background(), `INSERT INTO identity.tenants (id, slug, name, status, default_locale, default_timezone, reporting_currency, data_region) VALUES ($1, $2, 'T', 'active', 'en', 'UTC', 'USD', 'eu')`, id, id.String())
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// queue gives each test its own queue, so tests do not see each other's jobs.
func queue(t *testing.T) string { return "q_" + uuid.NewString()[:8] + "_" + t.Name()[:4] }

func enqueue(t *testing.T, tenantID uuid.UUID, s jobs.Spec) uuid.UUID {
	t.Helper()
	s.TenantID = &tenantID
	var id uuid.UUID
	err := db.TenantTx(context.Background(), env.Pools.App, tenantID, uuid.Nil, func(tx pgx.Tx) error {
		var err error
		id, _, err = jobs.Enqueue(context.Background(), tx, s)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func status(t *testing.T, id uuid.UUID) (string, int32) {
	t.Helper()
	var s string
	var a int32
	if err := env.Pools.System.QueryRow(context.Background(), `SELECT status, attempt FROM jobs.jobs WHERE id = $1`, id).Scan(&s, &a); err != nil {
		t.Fatal(err)
	}
	return s, a
}

func runner(t *testing.T) *jobs.Runner {
	t.Helper()
	r := jobs.NewRunner(env.Pools.System, "w-"+uuid.NewString()[:6])
	r.BackoffBase, r.BackoffMax = time.Millisecond, time.Millisecond
	return r
}

// makeDue moves a retry to now, so tests do not wait for backoff.
func makeDue(t *testing.T, id uuid.UUID) {
	t.Helper()
	if _, err := env.Pools.System.Exec(context.Background(), `UPDATE jobs.jobs SET run_at = now() - interval '1 second' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
}

func TestRollbackLeavesNoJob(t *testing.T) {
	ten, q := tenant(t), queue(t)
	sentinel := errors.New("business change failed")
	var id uuid.UUID
	err := db.TenantTx(context.Background(), env.Pools.App, ten, uuid.Nil, func(tx pgx.Tx) error {
		var err error
		id, _, err = jobs.Enqueue(context.Background(), tx, jobs.Spec{TenantID: &ten, Kind: "k", Queue: q})
		if err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	var n int
	if err := env.Pools.System.QueryRow(context.Background(), `SELECT count(*) FROM jobs.jobs WHERE id = $1`, id).Scan(&n); err != nil || n != 0 {
		t.Fatalf("job survived rollback: %d %v", n, err)
	}
}

func TestSuccessAndPayload(t *testing.T) {
	ten, q := tenant(t), queue(t)
	type payload struct {
		Thing string `json:"thing"`
	}
	r := runner(t)
	var got payload
	var gotTenant *uuid.UUID
	r.Register("ok", func(_ context.Context, j jobs.Job) error {
		gotTenant = j.TenantID
		return j.Decode(&got)
	})
	id := enqueue(t, ten, jobs.Spec{Kind: "ok", Queue: q, Payload: payload{"x"}})
	did, err := r.RunOne(context.Background(), q)
	if err != nil || !did {
		t.Fatalf("did=%v err=%v", did, err)
	}
	if s, _ := status(t, id); s != "succeeded" {
		t.Fatalf("status %s", s)
	}
	if got.Thing != "x" || gotTenant == nil || *gotTenant != ten {
		t.Fatalf("handler got %v %v", got, gotTenant)
	}
	if did, _ := r.RunOne(context.Background(), q); did {
		t.Fatal("a finished job ran again")
	}
}

func TestIdempotencyKeyReturnsExistingJob(t *testing.T) {
	ten, q := tenant(t), queue(t)
	a := enqueue(t, ten, jobs.Spec{Kind: "dup", Queue: q, IdempotencyKey: "same"})
	b := enqueue(t, ten, jobs.Spec{Kind: "dup", Queue: q, IdempotencyKey: "same"})
	if a != b {
		t.Fatalf("duplicate created a new job: %s %s", a, b)
	}
	other := tenant(t)
	if c := enqueue(t, other, jobs.Spec{Kind: "dup", Queue: q, IdempotencyKey: "same"}); c == a {
		t.Fatal("key collided across tenants")
	}
}

func TestRetryThenSuccess(t *testing.T) {
	ten, q := tenant(t), queue(t)
	r := runner(t)
	calls := 0
	r.Register("flaky", func(context.Context, jobs.Job) error {
		calls++
		if calls < 3 {
			return errors.New("provider timeout")
		}
		return nil
	})
	id := enqueue(t, ten, jobs.Spec{Kind: "flaky", Queue: q})
	for range 3 {
		if _, err := r.RunOne(context.Background(), q); err != nil {
			t.Fatal(err)
		}
		makeDue(t, id)
	}
	if s, a := status(t, id); s != "succeeded" || a != 3 {
		t.Fatalf("status %s attempt %d", s, a)
	}
	var attempts int
	if err := env.Pools.System.QueryRow(context.Background(), `SELECT count(*) FROM jobs.job_attempts WHERE job_id = $1`, id).Scan(&attempts); err != nil || attempts != 3 {
		t.Fatalf("attempts %d %v", attempts, err)
	}
}

func TestExhaustedJobGoesToDeadLetters(t *testing.T) {
	ten, q := tenant(t), queue(t)
	r := runner(t)
	r.Register("bad", func(context.Context, jobs.Job) error { return errors.New("always fails") })
	id := enqueue(t, ten, jobs.Spec{Kind: "bad", Queue: q, MaxAttempts: 2})
	for range 2 {
		if _, err := r.RunOne(context.Background(), q); err != nil {
			t.Fatal(err)
		}
		makeDue(t, id)
	}
	if s, _ := status(t, id); s != "dead" {
		t.Fatalf("status %s", s)
	}
	var n int
	if err := env.Pools.System.QueryRow(context.Background(), `SELECT count(*) FROM jobs.dead_letters WHERE origin_id = $1 AND origin = 'job'`, id).Scan(&n); err != nil || n != 1 {
		t.Fatalf("dead letters %d %v", n, err)
	}
}

func TestPermanentErrorFailsAtOnce(t *testing.T) {
	ten, q := tenant(t), queue(t)
	r := runner(t)
	r.Register("perm", func(context.Context, jobs.Job) error { return jobs.Permanent(errors.New("bad input")) })
	id := enqueue(t, ten, jobs.Spec{Kind: "perm", Queue: q})
	if _, err := r.RunOne(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	if s, a := status(t, id); s != "failed" || a != 1 {
		t.Fatalf("status %s attempt %d", s, a)
	}
}

func TestRateLimitDoesNotUseAnAttempt(t *testing.T) {
	ten, q := tenant(t), queue(t)
	r := runner(t)
	r.Register("limited", func(context.Context, jobs.Job) error { return jobs.Later(time.Hour, errors.New("rate limited")) })
	id := enqueue(t, ten, jobs.Spec{Kind: "limited", Queue: q, MaxAttempts: 1})
	if _, err := r.RunOne(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	s, a := status(t, id)
	if s != "retry_scheduled" || a != 0 {
		t.Fatalf("status %s attempt %d", s, a)
	}
	if did, _ := r.RunOne(context.Background(), q); did {
		t.Fatal("ran before Retry-After")
	}
}

func TestPanicIsAFailureNotACrash(t *testing.T) {
	ten, q := tenant(t), queue(t)
	r := runner(t)
	r.Register("boom", func(context.Context, jobs.Job) error { panic("bug") })
	id := enqueue(t, ten, jobs.Spec{Kind: "boom", Queue: q})
	if _, err := r.RunOne(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	if s, _ := status(t, id); s != "retry_scheduled" {
		t.Fatalf("status %s", s)
	}
}

func TestUnknownKindFailsPermanently(t *testing.T) {
	ten, q := tenant(t), queue(t)
	r := runner(t)
	id := enqueue(t, ten, jobs.Spec{Kind: "nobody", Queue: q})
	if _, err := r.RunOne(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	if s, _ := status(t, id); s != "failed" {
		t.Fatalf("status %s", s)
	}
}

// A worker that dies mid-job must not lose the job, and must not overwrite the retry if it returns.
func TestCrashedWorkerJobIsRecovered(t *testing.T) {
	ten, q := tenant(t), queue(t)
	first := runner(t)
	first.LockTTL = 20 * time.Millisecond
	release := make(chan struct{})
	started := make(chan struct{})
	first.Register("slow", func(ctx context.Context, _ jobs.Job) error {
		close(started)
		<-release // The worker stalls, and its heartbeat cannot save it once the row is taken.
		return nil
	})
	id := enqueue(t, ten, jobs.Spec{Kind: "slow", Queue: q})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = first.RunOne(context.Background(), q)
	}()
	<-started
	// Simulate the crash: the lock is expired, and it is reaped before any heartbeat wins.
	if _, err := env.Pools.System.Exec(context.Background(), `UPDATE jobs.jobs SET lock_expires_at = now() - interval '1 minute', locked_by = 'dead-worker' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	second := runner(t)
	if err := second.ReapExpired(context.Background()); err != nil {
		t.Fatal(err)
	}
	var ranAgain atomic.Bool
	second.Register("slow", func(context.Context, jobs.Job) error { ranAgain.Store(true); return nil })
	if _, err := second.RunOne(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	if !ranAgain.Load() {
		t.Fatal("job was not retried")
	}
	close(release)
	<-done
	if s, a := status(t, id); s != "succeeded" || a != 2 {
		t.Fatalf("status %s attempt %d", s, a)
	}
}

func TestCrashOnFinalAttemptGoesToDeadLetters(t *testing.T) {
	ten, q := tenant(t), queue(t)
	r := runner(t)
	id := enqueue(t, ten, jobs.Spec{Kind: "x", Queue: q, MaxAttempts: 1})
	if _, err := env.Pools.System.Exec(context.Background(), `UPDATE jobs.jobs SET status = 'running', attempt = 1, lock_expires_at = now() - interval '1 minute', locked_by = 'gone' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if err := r.ReapExpired(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s, _ := status(t, id); s != "dead" {
		t.Fatalf("status %s", s)
	}
}

func TestCancelQueuedJob(t *testing.T) {
	ten, q := tenant(t), queue(t)
	id := enqueue(t, ten, jobs.Spec{Kind: "c", Queue: q})
	err := db.TenantTx(context.Background(), env.Pools.App, ten, uuid.Nil, func(tx pgx.Tx) error {
		_, err := jobs.RequestCancel(context.Background(), tx, id)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if s, _ := status(t, id); s != "cancelled" {
		t.Fatalf("status %s", s)
	}
	r := runner(t)
	if did, _ := r.RunOne(context.Background(), q); did {
		t.Fatal("a cancelled job ran")
	}
}

func TestCancelRunningJobStopsItAtNextHeartbeat(t *testing.T) {
	ten, q := tenant(t), queue(t)
	r := runner(t)
	r.LockTTL = 60 * time.Millisecond
	started := make(chan struct{})
	r.Register("long", func(ctx context.Context, _ jobs.Job) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})
	id := enqueue(t, ten, jobs.Spec{Kind: "long", Queue: q})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = r.RunOne(context.Background(), q)
	}()
	<-started
	err := db.TenantTx(context.Background(), env.Pools.App, ten, uuid.Nil, func(tx pgx.Tx) error {
		_, err := jobs.RequestCancel(context.Background(), tx, id)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("running job was not cancelled")
	}
	if s, _ := status(t, id); s != "cancelled" {
		t.Fatalf("status %s", s)
	}
}

// One tenant's huge batch must not block another tenant's job.
func TestFairSharingPassesOverTenantAtLimit(t *testing.T) {
	heavy, light, q := tenant(t), tenant(t), queue(t)
	for range 5 {
		enqueue(t, heavy, jobs.Spec{Kind: "work", Queue: q})
	}
	time.Sleep(5 * time.Millisecond)
	enqueue(t, light, jobs.Spec{Kind: "work", Queue: q})
	r := runner(t)
	r.TenantLimit = 2
	var mu sync.Mutex
	var seen []uuid.UUID
	release := make(chan struct{})
	var entered sync.WaitGroup
	entered.Add(3)
	r.Register("work", func(_ context.Context, j jobs.Job) error {
		mu.Lock()
		seen = append(seen, *j.TenantID)
		mu.Unlock()
		entered.Done()
		<-release
		return nil
	})
	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = r.RunOne(context.Background(), q)
		}()
		time.Sleep(100 * time.Millisecond) // Claim one at a time, so the limit is visible.
	}
	entered.Wait()
	close(release)
	wg.Wait()
	heavyRuns, lightRuns := 0, 0
	for _, s := range seen {
		if s == heavy {
			heavyRuns++
		} else {
			lightRuns++
		}
	}
	if heavyRuns != 2 || lightRuns != 1 {
		t.Fatalf("heavy=%d light=%d; the third slot should go to the light tenant", heavyRuns, lightRuns)
	}
}

func TestConcurrentWorkersNeverShareAJob(t *testing.T) {
	ten, q := tenant(t), queue(t)
	const n = 20
	for range n {
		enqueue(t, ten, jobs.Spec{Kind: "once", Queue: q})
	}
	r := runner(t)
	r.TenantLimit = 100
	var runs atomic.Int64
	counts := sync.Map{}
	r.Register("once", func(_ context.Context, j jobs.Job) error {
		runs.Add(1)
		if _, loaded := counts.LoadOrStore(j.ID, true); loaded {
			t.Errorf("job %s ran twice", j.ID)
		}
		return nil
	})
	var wg sync.WaitGroup
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				did, err := r.RunOne(context.Background(), q)
				if err != nil {
					t.Error(err)
					return
				}
				if !did {
					return
				}
			}
		}()
	}
	wg.Wait()
	if runs.Load() != n {
		t.Fatalf("ran %d of %d", runs.Load(), n)
	}
}

func TestJobsAreTenantIsolated(t *testing.T) {
	a, b, q := tenant(t), tenant(t), queue(t)
	id := enqueue(t, a, jobs.Spec{Kind: "private", Queue: q})
	err := db.TenantTx(context.Background(), env.Pools.App, b, uuid.Nil, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM jobs.jobs WHERE id = $1`, id).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			t.Error("tenant B can see tenant A's job")
		}
		if _, err := jobs.RequestCancel(context.Background(), tx, id); !errors.Is(err, pgx.ErrNoRows) {
			t.Errorf("tenant B cancelled tenant A's job: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	err = db.TenantTx(context.Background(), env.Pools.App, b, uuid.Nil, func(tx pgx.Tx) error {
		_, _, err := jobs.Enqueue(context.Background(), tx, jobs.Spec{TenantID: &a, Kind: "forged", Queue: q})
		return err
	})
	if err == nil {
		t.Fatal("tenant B enqueued a job for tenant A")
	}
	if s, _ := status(t, id); s != "queued" {
		t.Fatalf("status %s", s)
	}
}

func TestSchedulesCreateOneJobPerTick(t *testing.T) {
	key, kind := "s-"+uuid.NewString()[:8], "scheduled-"+uuid.NewString()[:8]
	if err := jobs.EnsureSchedule(context.Background(), env.Pools.System, key, "@every 1h", kind); err != nil {
		t.Fatal(err)
	}
	r := runner(t)
	for range 3 {
		if err := r.RunSchedules(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	if err := env.Pools.System.QueryRow(context.Background(), `SELECT count(*) FROM jobs.jobs WHERE kind = $1`, kind).Scan(&n); err != nil || n != 1 {
		t.Fatalf("got %d jobs: %v", n, err)
	}
	if err := jobs.EnsureSchedule(context.Background(), env.Pools.System, key, "every hour", kind); err == nil {
		t.Fatal("an invalid schedule was accepted")
	}
}
