// Package events is the transactional outbox and event dispatch. An event is written in the same
// transaction as the business change, so a rollback writes no event, and a crash after commit
// still delivers one. Delivery is at-least-once, and consumers commit their effect together with
// the delivery mark, so a repeat has no effect.
package events

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
	"mspengine/internal/kernel/db"
	"mspengine/internal/kernel/jobs/repo"
)

// Event is the standard envelope. Payloads hold IDs and facts, never names, emails or free text.
type Event struct {
	ID            uuid.UUID
	Type          string
	SchemaVersion int32
	TenantID      *uuid.UUID
	SourceModule  string
	AggregateType string
	AggregateID   uuid.UUID
	OccurredAt    time.Time
	CorrelationID uuid.UUID
	CausationID   *uuid.UUID
	Payload       []byte
}

// Decode reads the payload, ignoring unknown fields as consumers must.
func (e Event) Decode(into any) error { return json.Unmarshal(e.Payload, into) }

// Append writes an event inside the caller's open transaction. Requiring one is the point.
func Append(ctx context.Context, tx pgx.Tx, e Event, payload any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode event %s: %w", e.Type, err)
	}
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	if e.SchemaVersion == 0 {
		e.SchemaVersion = 1
	}
	corr := authctx.MetaFrom(ctx).RequestID
	if corr == uuid.Nil {
		corr = id
	}
	return repo.New(tx).AppendEvent(ctx, repo.AppendEventParams{
		EventID: id, EventType: e.Type, SchemaVersion: e.SchemaVersion, TenantID: e.TenantID, SourceModule: e.SourceModule,
		AggregateType: e.AggregateType, AggregateID: e.AggregateID, OccurredAt: time.Now(), CorrelationID: corr, CausationID: e.CausationID, Payload: b,
	})
}

// Class says when a consumer runs, relative to the tenant's entitlement.
type Class int

const (
	// Active consumers are skipped when their module is not entitled.
	Active Class = iota
	// Passive consumers always run, to record facts a tenant may later buy access to.
	Passive
	// Lifecycle consumers always run, because they enforce safety or privacy.
	Lifecycle
)

// Handler applies an event. It runs in a transaction under the event's tenant context.
type Handler func(ctx context.Context, tx pgx.Tx, e Event) error

// Consumer is a named subscriber.
type Consumer struct {
	Name    string
	Types   []string
	Class   Class
	Module  string // The entitlement an Active consumer needs.
	Handler Handler
}

// EntitlementCheck reports whether a tenant holds a module.
type EntitlementCheck func(ctx context.Context, tenantID uuid.UUID, module string) (bool, error)

// Bus dispatches events to consumers.
type Bus struct {
	system      *pgxpool.Pool // Finds work across tenants.
	app         *pgxpool.Pool // Runs handlers under row-level security.
	consumers   map[string]Consumer
	byType      map[string][]string
	entitled    EntitlementCheck
	MaxAttempts int32
	BackoffBase time.Duration
	BackoffMax  time.Duration
	Poll        time.Duration
	StuckAfter  time.Duration
}

// New creates a bus.
func New(system, app *pgxpool.Pool, entitled EntitlementCheck) *Bus {
	return &Bus{system: system, app: app, consumers: map[string]Consumer{}, byType: map[string][]string{}, entitled: entitled,
		MaxAttempts: 8, BackoffBase: 2 * time.Second, BackoffMax: 30 * time.Minute, Poll: 500 * time.Millisecond, StuckAfter: 5 * time.Minute}
}

// Subscribe registers a consumer. Registration happens in code, so it is reviewed like other code.
func (b *Bus) Subscribe(c Consumer) {
	if _, dup := b.consumers[c.Name]; dup {
		panic("duplicate event consumer: " + c.Name)
	}
	b.consumers[c.Name] = c
	for _, t := range c.Types {
		b.byType[t] = append(b.byType[t], c.Name)
	}
}

// Run dispatches until the context ends.
func (b *Bus) Run(ctx context.Context) {
	for ctx.Err() == nil {
		pub, err := b.PublishPending(ctx)
		if err != nil && ctx.Err() == nil {
			slog.ErrorContext(ctx, "publish events failed", "error", err)
		}
		del, err := b.DeliverPending(ctx)
		if err != nil && ctx.Err() == nil {
			slog.ErrorContext(ctx, "deliver events failed", "error", err)
		}
		if err := b.ReleaseStuck(ctx); err != nil && ctx.Err() == nil {
			slog.ErrorContext(ctx, "release stuck deliveries failed", "error", err)
		}
		if pub == 0 && del == 0 {
			select {
			case <-ctx.Done():
			case <-time.After(b.Poll):
			}
		}
	}
}

// PublishPending fans unpublished events out to one delivery per subscribed consumer.
func (b *Bus) PublishPending(ctx context.Context) (int, error) {
	n := 0
	err := pgx.BeginFunc(ctx, b.system, func(tx pgx.Tx) error {
		q := repo.New(tx)
		rows, err := q.ClaimUnpublished(ctx, 200)
		if err != nil {
			return err
		}
		n = len(rows)
		for _, ev := range rows {
			for _, consumer := range b.byType[ev.EventType] {
				if err := q.InsertDelivery(ctx, repo.InsertDeliveryParams{TenantID: ev.TenantID, EventID: ev.EventID, Consumer: consumer}); err != nil {
					return err
				}
			}
			if err := q.MarkPublished(ctx, repo.MarkPublishedParams{EventID: ev.EventID, OccurredAt: ev.OccurredAt}); err != nil {
				return err
			}
		}
		return nil
	})
	return n, err
}

// DeliverPending runs consumers for the deliveries that are due.
func (b *Bus) DeliverPending(ctx context.Context) (int, error) {
	var rows []repo.ClaimDeliveriesRow
	err := pgx.BeginFunc(ctx, b.system, func(tx pgx.Tx) error {
		q := repo.New(tx)
		var err error
		rows, err = q.ClaimDeliveries(ctx, 50)
		if err != nil {
			return err
		}
		for _, d := range rows {
			if err := q.MarkDeliveryProcessing(ctx, d.ID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	// Deliveries for one aggregate stay in order, because a later one is not claimed until the
	// earlier one finishes. Different aggregates can run together.
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for _, d := range rows {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			b.deliver(ctx, d)
		}()
	}
	wg.Wait()
	return len(rows), nil
}

func (b *Bus) deliver(ctx context.Context, d repo.ClaimDeliveriesRow) {
	c, ok := b.consumers[d.Consumer]
	if !ok {
		b.fail(ctx, d, fmt.Errorf("no consumer named %s", d.Consumer))
		return
	}
	e := Event{ID: d.EventID, Type: d.EventType, SchemaVersion: d.SchemaVersion, TenantID: d.TenantID, SourceModule: d.SourceModule,
		AggregateType: d.AggregateType, AggregateID: d.AggregateID, OccurredAt: d.OccurredAt, CorrelationID: d.CorrelationID, CausationID: d.CausationID, Payload: d.Payload}
	hctx := authctx.WithMeta(ctx, authctx.Meta{RequestID: e.CorrelationID})
	apply := func(tx pgx.Tx) error {
		n, err := repo.New(tx).CompleteDelivery(hctx, d.ID)
		if err != nil {
			return err
		}
		if n == 0 {
			return nil // Already applied. A repeat must have no effect.
		}
		if c.Class == Active && e.TenantID != nil && c.Module != "" {
			ok, err := b.entitled(hctx, *e.TenantID, c.Module)
			if err != nil {
				return err
			}
			if !ok {
				return nil // Marked done with no effect, so nothing backs up while a module is off.
			}
		}
		return safely(hctx, tx, c.Handler, e)
	}
	var err error
	if e.TenantID != nil {
		err = db.TenantTx(hctx, b.app, *e.TenantID, uuid.Nil, apply)
	} else {
		err = db.Tx(hctx, b.app, apply)
	}
	if err != nil {
		b.fail(ctx, d, err)
	}
}

func safely(ctx context.Context, tx pgx.Tx, h Handler, e Event) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("consumer panicked: %v", v)
		}
	}()
	return h(ctx, tx, e)
}

func (b *Bus) fail(ctx context.Context, d repo.ClaimDeliveriesRow, cause error) {
	msg := cause.Error()
	if len(msg) > 2000 {
		msg = msg[:2000]
	}
	attempts := d.Attempts + 1
	err := pgx.BeginFunc(context.WithoutCancel(ctx), b.system, func(tx pgx.Tx) error {
		q := repo.New(tx)
		if attempts >= b.MaxAttempts {
			if err := q.FailDelivery(ctx, repo.FailDeliveryParams{ID: d.ID, Status: "dead", LastError: &msg}); err != nil {
				return err
			}
			return q.InsertDeadLetter(ctx, repo.InsertDeadLetterParams{TenantID: d.TenantID, Origin: "event_delivery", OriginID: d.ID, Kind: d.Consumer + ":" + d.EventType, Payload: d.Payload, Error: msg})
		}
		limit := b.BackoffBase << min(attempts, 20)
		if limit <= 0 || limit > b.BackoffMax {
			limit = b.BackoffMax
		}
		next := time.Now().Add(time.Duration(rand.Int64N(int64(limit) + 1))) //nolint:gosec // Timing jitter, not security.
		return q.FailDelivery(ctx, repo.FailDeliveryParams{ID: d.ID, Status: "failed", NextAttemptAt: &next, LastError: &msg})
	})
	if err != nil && !errors.Is(err, context.Canceled) {
		slog.ErrorContext(ctx, "record delivery failure failed", "delivery", d.ID, "error", err)
	}
}

// ReleaseStuck returns deliveries whose worker vanished mid-way.
func (b *Bus) ReleaseStuck(ctx context.Context) error {
	return repo.New(b.system).ReleaseStuckDeliveries(ctx, time.Now().Add(-b.StuckAfter))
}

// Replay sends a consumer's past events again, which is safe only because consumers are idempotent
// at the level of their own effects. It is an admin operation.
func (b *Bus) Replay(ctx context.Context, consumer, eventType string, since time.Time) (int64, error) {
	return repo.New(b.system).ReplayDeliveries(ctx, repo.ReplayDeliveriesParams{Consumer: consumer, EventType: eventType, Since: since})
}
