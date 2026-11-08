package jobs

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/jobs/repo"
)

// Ops is what operators need: queue health and decisions on failed work.
type Ops struct{ pool *pgxpool.Pool }

// NewOps creates Ops on the given pool.
func NewOps(pool *pgxpool.Pool) *Ops { return &Ops{pool: pool} }

// Stats describes queues and events.
type Stats struct {
	Queues []repo.QueueStatsRow
	Events repo.EventStatsRow
}

// Stats reads current health.
func (o *Ops) Stats(ctx context.Context) (Stats, error) {
	q := repo.New(o.pool)
	queues, err := q.QueueStats(ctx)
	if err != nil {
		return Stats{}, err
	}
	ev, err := q.EventStats(ctx)
	return Stats{Queues: queues, Events: ev}, err
}

// DeadLetters lists failed work awaiting a decision.
func (o *Ops) DeadLetters(ctx context.Context) ([]repo.ListDeadLettersRow, error) {
	return repo.New(o.pool).ListDeadLetters(ctx, false)
}

// Resolve requeues or discards a dead letter inside tx, so its audit record commits with it.
func Resolve(ctx context.Context, tx pgx.Tx, id, adminID uuid.UUID, requeue bool) (*uuid.UUID, error) {
	q := repo.New(tx)
	d, err := q.GetDeadLetter(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.NotFound()
	}
	if err != nil {
		return nil, err
	}
	if d.Resolution != nil {
		return nil, apperr.Conflict("invalid_transition", "This item has already been resolved")
	}
	resolution := "discarded"
	if requeue {
		resolution = "requeued"
		switch d.Origin {
		case "job":
			err = q.RequeueJob(ctx, d.OriginID)
		default:
			err = q.RequeueDelivery(ctx, d.OriginID)
		}
		if err != nil {
			return nil, err
		}
	}
	if err := q.ResolveDeadLetter(ctx, repo.ResolveDeadLetterParams{ID: id, Resolution: &resolution, ResolvedBy: &adminID}); err != nil {
		return nil, err
	}
	return d.TenantID, nil
}

// GetForTenant reads a job under the caller's tenant context, so another tenant's job is not found.
func GetForTenant(ctx context.Context, tx pgx.Tx, id uuid.UUID) (repo.GetJobRow, error) {
	j, err := repo.New(tx).GetJob(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return j, apperr.NotFound()
	}
	return j, err
}
