// Package jobs is the background job system. PostgreSQL holds all job state, so nothing is lost if
// a worker or Redis dies. A job is inserted in the same transaction as the business change that
// needs it, so neither can exist without the other.
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/jobs/repo"
)

// Pools from the background jobs design. They are separate so that a slow provider cannot starve
// unrelated work. Sizes are configuration, set after load tests.
const (
	QueueCritical   = "critical"
	QueueDefault    = "default"
	QueueProviderIO = "provider_io"
	QueueResearch   = "research_fetch"
	QueueAssessment = "assessment"
	QueueEmailSend  = "email_send"
	QueueAI         = "ai"
	QueuePDF        = "pdf"
	QueueAnalytics  = "analytics"
)

// Spec describes a job to enqueue.
type Spec struct {
	TenantID       *uuid.UUID
	Kind           string
	Queue          string
	Payload        any
	PayloadVersion int32
	RunAt          time.Time
	Priority       int32
	MaxAttempts    int32
	// IdempotencyKey makes a duplicate request return the existing job.
	IdempotencyKey string
}

// Enqueue inserts a job in the caller's transaction. It reports whether a new job was created.
func Enqueue(ctx context.Context, tx pgx.Tx, s Spec) (uuid.UUID, bool, error) {
	payload, err := json.Marshal(s.Payload)
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("encode job payload: %w", err)
	}
	if s.IdempotencyKey == "" {
		s.IdempotencyKey = uuid.NewString()
	}
	if s.Queue == "" {
		s.Queue = QueueDefault
	}
	if s.MaxAttempts == 0 {
		s.MaxAttempts = 5
	}
	if s.PayloadVersion == 0 {
		s.PayloadVersion = 1
	}
	if s.RunAt.IsZero() {
		s.RunAt = time.Now()
	}
	var corr *uuid.UUID
	if m := authctx.MetaFrom(ctx); m.RequestID != uuid.Nil {
		corr = &m.RequestID
	}
	q := repo.New(tx)
	id, err := q.InsertJob(ctx, repo.InsertJobParams{
		TenantID: s.TenantID, Queue: s.Queue, Kind: s.Kind, Payload: payload, PayloadVersion: s.PayloadVersion,
		Priority: s.Priority, RunAt: s.RunAt, MaxAttempts: s.MaxAttempts, IdempotencyKey: &s.IdempotencyKey, CorrelationID: corr,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		existing, err := q.FindJobByKey(ctx, repo.FindJobByKeyParams{TenantID: s.TenantID, Kind: s.Kind, IdempotencyKey: &s.IdempotencyKey})
		return existing, false, err
	}
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("enqueue %s: %w", s.Kind, err)
	}
	return id, true, nil
}

// RequestCancel cancels a queued job at once, or flags a running one. A handler that is running
// sees its context cancelled at the next heartbeat.
func RequestCancel(ctx context.Context, tx pgx.Tx, id uuid.UUID) (string, error) {
	return repo.New(tx).RequestCancel(ctx, id)
}

// Error classes decide what happens after a failure.
type permanentError struct{ err error }

func (e permanentError) Error() string { return e.err.Error() }
func (e permanentError) Unwrap() error { return e.err }

// Permanent marks an error that retrying cannot fix, so the job fails at once.
func Permanent(err error) error { return permanentError{err} }

type laterError struct {
	err   error
	after time.Duration
}

func (e laterError) Error() string { return e.err.Error() }
func (e laterError) Unwrap() error { return e.err }

// Later reschedules without using an attempt, for a rate limit or an open circuit breaker.
func Later(after time.Duration, err error) error { return laterError{err, after} }

// Job is what a handler receives. Payloads hold IDs, and the tenant comes from the job row.
type Job struct {
	ID             uuid.UUID
	TenantID       *uuid.UUID
	Kind           string
	Payload        []byte
	PayloadVersion int32
	Attempt        int32
	setProgress    func(ctx context.Context, v any) error
}

// Decode reads the payload.
func (j Job) Decode(into any) error {
	if err := json.Unmarshal(j.Payload, into); err != nil {
		return Permanent(fmt.Errorf("decode payload of %s: %w", j.Kind, err))
	}
	return nil
}

// Progress records progress for the UI. Failure to record it does not fail the job.
func (j Job) Progress(ctx context.Context, v any) {
	if j.setProgress == nil {
		return
	}
	if err := j.setProgress(ctx, v); err != nil {
		slog.WarnContext(ctx, "record job progress failed", "error", err)
	}
}

// Handler does the work. It must be idempotent, because a crash causes a retry.
type Handler func(ctx context.Context, job Job) error

// Runner claims and runs jobs. It uses the system role to find work across tenants. Handlers then
// do their work under each tenant's own context.
type Runner struct {
	pool        *pgxpool.Pool
	workerID    string
	handlers    map[string]Handler
	concurrency map[string]int
	// LockTTL is how long a worker owns a job without a heartbeat.
	LockTTL time.Duration
	// Poll is how often an idle worker looks for work.
	Poll time.Duration
	// TenantLimit is how many jobs of one tenant may run at once in a queue.
	TenantLimit int64
	// Backoff settings for retries, with full jitter.
	BackoffBase time.Duration
	BackoffMax  time.Duration
}

// NewRunner creates a runner.
func NewRunner(pool *pgxpool.Pool, workerID string) *Runner {
	return &Runner{
		pool: pool, workerID: workerID, handlers: map[string]Handler{},
		concurrency: map[string]int{QueueCritical: 4, QueueDefault: 4, QueueAI: 2, QueueAnalytics: 1},
		LockTTL:     time.Minute, Poll: time.Second, TenantLimit: 3, BackoffBase: 2 * time.Second, BackoffMax: 15 * time.Minute,
	}
}

// Register adds a handler for a job kind.
func (r *Runner) Register(kind string, h Handler) { r.handlers[kind] = h }

// SetConcurrency sets how many jobs of a queue this worker runs at once.
func (r *Runner) SetConcurrency(queue string, n int) { r.concurrency[queue] = n }

// Run processes jobs until the context ends, then lets running jobs finish or be reclaimed.
func (r *Runner) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for queue, n := range r.concurrency {
		for range n {
			wg.Add(1)
			go func() {
				defer wg.Done()
				r.loop(ctx, queue)
			}()
		}
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		r.housekeepingLoop(ctx)
	}()
	wg.Wait()
}

func (r *Runner) loop(ctx context.Context, queue string) {
	for ctx.Err() == nil {
		did, err := r.RunOne(ctx, queue)
		if err != nil && ctx.Err() == nil {
			slog.ErrorContext(ctx, "job claim failed", "queue", queue, "error", err)
		}
		if !did {
			select {
			case <-ctx.Done():
			case <-time.After(jitter(r.Poll)):
			}
		}
	}
}

func jitter(d time.Duration) time.Duration {
	return d/2 + time.Duration(rand.Int64N(int64(d)/2+1)) //nolint:gosec // Timing jitter, not security.
}

// RunOne claims and runs one job. It reports whether it found one. Tests call it directly.
func (r *Runner) RunOne(ctx context.Context, queue string) (bool, error) {
	row, err := repo.New(r.pool).ClaimJob(ctx, repo.ClaimJobParams{
		WorkerID: &r.workerID, LockExpiresAt: timePtr(time.Now().Add(r.LockTTL)), Queue: queue, TenantLimit: r.TenantLimit,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	r.execute(ctx, row)
	return true, nil
}

func timePtr(t time.Time) *time.Time { return &t }

func (r *Runner) execute(parent context.Context, row repo.ClaimJobRow) {
	// A shutdown should not abort a job at once, so it runs on its own context until the lock lapses.
	ctx, cancel := context.WithCancel(context.WithoutCancel(parent))
	defer cancel()
	m := authctx.Meta{}
	if row.CorrelationID != nil {
		m.RequestID = *row.CorrelationID
	}
	ctx = authctx.WithMeta(ctx, m)
	started := time.Now()
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		r.heartbeat(ctx, row.ID, cancel, stop)
	}()

	var err error
	switch {
	case row.CancelRequestedAt != nil:
		err = context.Canceled
	default:
		if h, ok := r.handlers[row.Kind]; ok {
			err = safely(ctx, h, Job{ID: row.ID, TenantID: row.TenantID, Kind: row.Kind, Payload: row.Payload, PayloadVersion: row.PayloadVersion, Attempt: row.Attempt,
				setProgress: func(c context.Context, v any) error {
					b, err := json.Marshal(v)
					if err != nil {
						return err
					}
					return repo.New(r.pool).SetProgress(c, repo.SetProgressParams{ID: row.ID, Progress: b})
				}})
		} else {
			err = Permanent(fmt.Errorf("no handler for job kind %s", row.Kind))
		}
	}
	close(stop)
	wg.Wait()
	// The job's own context may be cancelled by now, so recording the result uses a fresh one.
	done, doneCancel := context.WithTimeout(context.WithoutCancel(parent), 10*time.Second)
	defer doneCancel()
	if rerr := r.record(done, row, started, err); rerr != nil {
		slog.ErrorContext(done, "record job result failed", "job", row.ID, "error", rerr)
	}
}

// safely turns a panic into a failure, so one bad job cannot take down the worker.
func safely(ctx context.Context, h Handler, j Job) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("job panicked: %v", v)
		}
	}()
	return h(ctx, j)
}

func (r *Runner) heartbeat(ctx context.Context, id uuid.UUID, cancel context.CancelFunc, stop <-chan struct{}) {
	t := time.NewTicker(max(r.LockTTL/3, 10*time.Millisecond))
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			hctx, c := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			cancelledAt, err := repo.New(r.pool).Heartbeat(hctx, repo.HeartbeatParams{LockExpiresAt: timePtr(time.Now().Add(r.LockTTL)), ID: id, WorkerID: &r.workerID})
			c()
			if errors.Is(err, pgx.ErrNoRows) || cancelledAt != nil {
				cancel() // The lock was lost or cancellation was requested, so stop working.
				return
			}
			if err != nil {
				slog.WarnContext(ctx, "job heartbeat failed", "job", id, "error", err)
			}
		}
	}
}

// Truncates errors before storing, because error text can be long.
func errText(err error) *string {
	s := err.Error()
	if len(s) > 2000 {
		s = s[:2000]
	}
	return &s
}

func (r *Runner) record(ctx context.Context, row repo.ClaimJobRow, started time.Time, jobErr error) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		q := repo.New(tx)
		attempt := func(outcome string, msg *string) error {
			return q.InsertAttempt(ctx, repo.InsertAttemptParams{JobID: row.ID, Attempt: row.Attempt, WorkerID: r.workerID, StartedAt: started, Outcome: &outcome, Error: msg})
		}
		finish := func(status string, msg *string) error {
			n, err := q.FinishJob(ctx, repo.FinishJobParams{Status: status, LastError: msg, ID: row.ID, WorkerID: &r.workerID})
			if err != nil {
				return err
			}
			if n == 0 {
				return nil // Another worker has taken over after this lock lapsed.
			}
			return attempt(status, msg)
		}
		if jobErr == nil {
			return finish("succeeded", nil)
		}
		msg := errText(jobErr)
		if errors.Is(jobErr, context.Canceled) {
			return finish("cancelled", msg)
		}
		var perm permanentError
		if errors.As(jobErr, &perm) {
			return finish("failed", msg)
		}
		var later laterError
		if errors.As(jobErr, &later) {
			if err := attempt("rescheduled", msg); err != nil {
				return err
			}
			_, err := q.RescheduleJob(ctx, repo.RescheduleJobParams{RunAt: time.Now().Add(later.after), LastError: msg, RefundAttempt: 1, ID: row.ID, WorkerID: &r.workerID})
			return err
		}
		if row.Attempt >= row.MaxAttempts {
			if err := finish("dead", msg); err != nil {
				return err
			}
			return q.InsertDeadLetter(ctx, repo.InsertDeadLetterParams{TenantID: row.TenantID, Origin: "job", OriginID: row.ID, Kind: row.Kind, Payload: row.Payload, Error: *msg})
		}
		if err := attempt("retry", msg); err != nil {
			return err
		}
		_, err := q.RescheduleJob(ctx, repo.RescheduleJobParams{RunAt: time.Now().Add(r.backoff(row.Attempt)), LastError: msg, ID: row.ID, WorkerID: &r.workerID})
		return err
	})
}

// backoff is exponential with full jitter, which spreads retries out after a shared outage.
func (r *Runner) backoff(attempt int32) time.Duration {
	limit := r.BackoffBase << min(attempt, 20)
	if limit <= 0 || limit > r.BackoffMax {
		limit = r.BackoffMax
	}
	return time.Duration(rand.Int64N(int64(limit) + 1)) //nolint:gosec // Timing jitter, not security.
}

// ReapExpired returns jobs whose worker vanished. Tests call it directly.
func (r *Runner) ReapExpired(ctx context.Context) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		q := repo.New(tx)
		rows, err := q.ListExpiredLocks(ctx)
		if err != nil {
			return err
		}
		for _, row := range rows {
			if row.Attempt >= row.MaxAttempts {
				msg := "worker lost its lock on the final attempt"
				if err := q.MarkDead(ctx, repo.MarkDeadParams{ID: row.ID, LastError: &msg}); err != nil {
					return err
				}
				if err := q.InsertDeadLetter(ctx, repo.InsertDeadLetterParams{TenantID: row.TenantID, Origin: "job", OriginID: row.ID, Kind: row.Kind, Payload: row.Payload, Error: msg}); err != nil {
					return err
				}
				continue
			}
			if err := q.ReleaseExpired(ctx, row.ID); err != nil {
				return err
			}
		}
		return nil
	})
}

// RunSchedules enqueues due schedules. An advisory lock means only one worker does this at a time,
// and idempotency keys make a doubled tick harmless. Tests call it directly.
func (r *Runner) RunSchedules(ctx context.Context) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var got bool
		if err := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended('jobs:scheduler', 0))`).Scan(&got); err != nil {
			return err
		}
		if !got {
			return nil
		}
		q := repo.New(tx)
		due, err := q.ListDueSchedules(ctx)
		if err != nil {
			return err
		}
		now := time.Now()
		for _, s := range due {
			every, err := ParseEvery(s.Cron)
			if err != nil {
				slog.ErrorContext(ctx, "invalid schedule", "key", s.Key, "error", err)
				continue
			}
			key := fmt.Sprintf("schedule:%s:%d", s.Key, now.Truncate(every).Unix())
			if _, _, err := Enqueue(ctx, tx, Spec{TenantID: s.TenantID, Kind: s.Kind, Payload: s.Payload, IdempotencyKey: key}); err != nil {
				return err
			}
			if err := q.AdvanceSchedule(ctx, repo.AdvanceScheduleParams{ID: s.ID, NextRunAt: now.Add(every)}); err != nil {
				return err
			}
		}
		return nil
	})
}

// ParseEvery reads a schedule such as "@every 1h".
func ParseEvery(spec string) (time.Duration, error) {
	const prefix = "@every "
	if len(spec) <= len(prefix) || spec[:len(prefix)] != prefix {
		return 0, fmt.Errorf("unsupported schedule %q", spec)
	}
	d, err := time.ParseDuration(spec[len(prefix):])
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("unsupported schedule %q", spec)
	}
	return d, nil
}

// EnsureSchedule creates or updates a platform-level schedule.
func EnsureSchedule(ctx context.Context, pool *pgxpool.Pool, key, every, kind string) error {
	if _, err := ParseEvery(every); err != nil {
		return err
	}
	return repo.New(pool).UpsertSchedule(ctx, repo.UpsertScheduleParams{Key: key, Cron: every, Kind: kind})
}

func (r *Runner) housekeepingLoop(ctx context.Context) {
	t := time.NewTicker(max(r.Poll*5, 5*time.Second))
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := r.ReapExpired(ctx); err != nil && ctx.Err() == nil {
				slog.ErrorContext(ctx, "reap jobs failed", "error", err)
			}
			if err := r.RunSchedules(ctx); err != nil && ctx.Err() == nil {
				slog.ErrorContext(ctx, "run schedules failed", "error", err)
			}
		}
	}
}
