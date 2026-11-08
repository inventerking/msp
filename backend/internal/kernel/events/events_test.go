package events_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/db"
	"mspengine/internal/kernel/events"
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

func newBus(t *testing.T, entitled bool) *events.Bus {
	t.Helper()
	b := events.New(env.Pools.System, env.Pools.App, func(context.Context, uuid.UUID, string) (bool, error) { return entitled, nil })
	b.BackoffBase, b.BackoffMax = time.Millisecond, time.Millisecond
	return b
}

func publish(t *testing.T, ten uuid.UUID, typ string, agg uuid.UUID, payload any) {
	t.Helper()
	err := db.TenantTx(context.Background(), env.Pools.App, ten, uuid.Nil, func(tx pgx.Tx) error {
		return events.Append(context.Background(), tx, events.Event{Type: typ, TenantID: &ten, SourceModule: "test", AggregateType: "thing", AggregateID: agg}, payload)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// pump runs dispatch until nothing is left to do.
func pump(t *testing.T, b *events.Bus, rounds int) {
	t.Helper()
	for range rounds {
		if _, err := b.PublishPending(context.Background()); err != nil {
			t.Fatal(err)
		}
		if _, err := b.DeliverPending(context.Background()); err != nil {
			t.Fatal(err)
		}
		time.Sleep(3 * time.Millisecond)
	}
}

func typ(t *testing.T) string { return "test." + uuid.NewString()[:8] + ".happened" }

func serviceCount(t *testing.T, ten uuid.UUID, name string) int {
	t.Helper()
	var n int
	err := db.TenantTx(context.Background(), env.Pools.App, ten, uuid.Nil, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM shared.tenant_services WHERE name = $1`, name).Scan(&n)
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestRollbackWritesNoEvent(t *testing.T) {
	ten, ty := tenant(t), typ(t)
	sentinel := errors.New("failed")
	err := db.TenantTx(context.Background(), env.Pools.App, ten, uuid.Nil, func(tx pgx.Tx) error {
		if err := events.Append(context.Background(), tx, events.Event{Type: ty, TenantID: &ten, SourceModule: "t", AggregateType: "a", AggregateID: uuid.New()}, nil); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	var n int
	if err := env.Pools.System.QueryRow(context.Background(), `SELECT count(*) FROM jobs.outbox_events WHERE event_type = $1`, ty).Scan(&n); err != nil || n != 0 {
		t.Fatalf("got %d: %v", n, err)
	}
}

// The event survives whatever happens to the process between commit and dispatch, because a new
// bus finds it in the table.
func TestCrashBetweenCommitAndDispatchStillDelivers(t *testing.T) {
	ten, ty := tenant(t), typ(t)
	publish(t, ten, ty, uuid.New(), map[string]string{"a": "b"})
	b := newBus(t, true)
	var got []events.Event
	var mu sync.Mutex
	b.Subscribe(events.Consumer{Name: "c-" + ty, Types: []string{ty}, Class: events.Passive, Handler: func(_ context.Context, _ pgx.Tx, e events.Event) error {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, e)
		return nil
	}})
	pump(t, b, 2)
	if len(got) != 1 {
		t.Fatalf("delivered %d", len(got))
	}
	e := got[0]
	if e.TenantID == nil || *e.TenantID != ten || e.SourceModule != "test" || e.Type != ty || e.SchemaVersion != 1 || e.CorrelationID == uuid.Nil {
		t.Fatalf("envelope wrong: %+v", e)
	}
	var p map[string]string
	if err := e.Decode(&p); err != nil || p["a"] != "b" {
		t.Fatalf("payload %v %v", p, err)
	}
	pump(t, b, 2)
	if len(got) != 1 {
		t.Fatalf("delivered %d times", len(got))
	}
}

// The effect and the delivery mark commit together, so a failure leaves neither behind.
func TestFailureRollsBackEffectAndRetries(t *testing.T) {
	ten, ty := tenant(t), typ(t)
	publish(t, ten, ty, uuid.New(), nil)
	b := newBus(t, true)
	calls := 0
	name := "svc-" + ty
	b.Subscribe(events.Consumer{Name: "c-" + ty, Types: []string{ty}, Class: events.Passive, Handler: func(ctx context.Context, tx pgx.Tx, e events.Event) error {
		calls++
		if _, err := tx.Exec(ctx, `INSERT INTO shared.tenant_services (tenant_id, name, status) VALUES ($1, $2, 'active')`, *e.TenantID, name); err != nil {
			return err
		}
		if calls == 1 {
			return errors.New("fails after doing its work")
		}
		return nil
	}})
	pump(t, b, 8)
	if calls != 2 {
		t.Fatalf("calls %d", calls)
	}
	if n := serviceCount(t, ten, name); n != 1 {
		t.Fatalf("effect applied %d times", n)
	}
}

func TestHandlerRunsUnderTenantRLS(t *testing.T) {
	a, other, ty := tenant(t), tenant(t), typ(t)
	name := "secret-" + ty
	err := db.TenantTx(context.Background(), env.Pools.App, other, uuid.Nil, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `INSERT INTO shared.tenant_services (tenant_id, name, status) VALUES ($1, $2, 'active')`, other, name)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	publish(t, a, ty, uuid.New(), nil)
	b := newBus(t, true)
	seen := -1
	b.Subscribe(events.Consumer{Name: "c-" + ty, Types: []string{ty}, Class: events.Passive, Handler: func(ctx context.Context, tx pgx.Tx, _ events.Event) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM shared.tenant_services WHERE name = $1`, name).Scan(&seen)
	}})
	pump(t, b, 2)
	if seen != 0 {
		t.Fatalf("a consumer for tenant A saw %d of another tenant's rows", seen)
	}
}

func TestOrderIsKeptPerAggregate(t *testing.T) {
	ten, ty := tenant(t), typ(t)
	agg := uuid.New()
	for i := range 5 {
		publish(t, ten, ty, agg, map[string]int{"i": i})
	}
	b := newBus(t, true)
	var got []int
	var mu sync.Mutex
	failed := false
	b.Subscribe(events.Consumer{Name: "c-" + ty, Types: []string{ty}, Class: events.Passive, Handler: func(_ context.Context, _ pgx.Tx, e events.Event) error {
		var p map[string]int
		if err := e.Decode(&p); err != nil {
			return err
		}
		mu.Lock()
		defer mu.Unlock()
		if p["i"] == 2 && !failed {
			failed = true
			return errors.New("retry the third event")
		}
		got = append(got, p["i"])
		return nil
	}})
	pump(t, b, 12)
	if fmt.Sprint(got) != "[0 1 2 3 4]" {
		t.Fatalf("order %v", got)
	}
}

// A poison event goes to dead letters and blocks neither other aggregates, later events, nor others.
func TestPoisonEventDoesNotBlockOthers(t *testing.T) {
	ten, ty := tenant(t), typ(t)
	poison, clean := uuid.New(), uuid.New()
	publish(t, ten, ty, poison, map[string]string{"k": "poison"})
	publish(t, ten, ty, poison, map[string]string{"k": "later"})
	publish(t, ten, ty, clean, map[string]string{"k": "clean"})
	b := newBus(t, true)
	b.MaxAttempts = 3
	var got []string
	var other int
	var mu sync.Mutex
	b.Subscribe(events.Consumer{Name: "bad-" + ty, Types: []string{ty}, Class: events.Passive, Handler: func(_ context.Context, _ pgx.Tx, e events.Event) error {
		var p map[string]string
		_ = e.Decode(&p)
		if p["k"] == "poison" {
			return errors.New("cannot process this")
		}
		mu.Lock()
		defer mu.Unlock()
		got = append(got, p["k"])
		return nil
	}})
	b.Subscribe(events.Consumer{Name: "good-" + ty, Types: []string{ty}, Class: events.Passive, Handler: func(context.Context, pgx.Tx, events.Event) error {
		mu.Lock()
		defer mu.Unlock()
		other++
		return nil
	}})
	pump(t, b, 15)
	if len(got) != 2 {
		t.Fatalf("later and clean should both run, got %v", got)
	}
	if other != 3 {
		t.Fatalf("another consumer got %d of 3", other)
	}
	var dead int
	if err := env.Pools.System.QueryRow(context.Background(), `SELECT count(*) FROM jobs.dead_letters WHERE origin = 'event_delivery' AND kind = $1`, "bad-"+ty+":"+ty).Scan(&dead); err != nil || dead != 1 {
		t.Fatalf("dead letters %d %v", dead, err)
	}
}

func TestActiveConsumerSkipsWithoutEntitlement(t *testing.T) {
	ten, ty := tenant(t), typ(t)
	publish(t, ten, ty, uuid.New(), nil)
	b := newBus(t, false)
	active, passive, lifecycle := 0, 0, 0
	count := func(n *int) events.Handler {
		return func(context.Context, pgx.Tx, events.Event) error { *n++; return nil }
	}
	b.Subscribe(events.Consumer{Name: "a-" + ty, Types: []string{ty}, Class: events.Active, Module: "crm", Handler: count(&active)})
	b.Subscribe(events.Consumer{Name: "p-" + ty, Types: []string{ty}, Class: events.Passive, Handler: count(&passive)})
	b.Subscribe(events.Consumer{Name: "l-" + ty, Types: []string{ty}, Class: events.Lifecycle, Handler: count(&lifecycle)})
	pump(t, b, 2)
	if active != 0 || passive != 1 || lifecycle != 1 {
		t.Fatalf("active=%d passive=%d lifecycle=%d", active, passive, lifecycle)
	}
	var pending int
	if err := env.Pools.System.QueryRow(context.Background(), `SELECT count(*) FROM jobs.event_deliveries WHERE consumer = $1 AND status <> 'processed'`, "a-"+ty).Scan(&pending); err != nil || pending != 0 {
		t.Fatalf("a skipped delivery should not back up: %d %v", pending, err)
	}
}

func TestReplayHasNoDoubleEffectForIdempotentConsumer(t *testing.T) {
	ten, ty := tenant(t), typ(t)
	publish(t, ten, ty, uuid.New(), nil)
	b := newBus(t, true)
	name := "replay-" + ty
	calls := 0
	consumer := "c-" + ty
	b.Subscribe(events.Consumer{Name: consumer, Types: []string{ty}, Class: events.Passive, Handler: func(ctx context.Context, tx pgx.Tx, e events.Event) error {
		calls++
		_, err := tx.Exec(ctx, `INSERT INTO shared.tenant_services (tenant_id, name, status) VALUES ($1, $2, 'active') ON CONFLICT DO NOTHING`, *e.TenantID, name)
		return err
	}})
	pump(t, b, 2)
	n, err := b.Replay(context.Background(), consumer, ty, time.Now().Add(-time.Hour))
	if err != nil || n != 1 {
		t.Fatalf("replayed %d: %v", n, err)
	}
	pump(t, b, 2)
	if calls != 2 {
		t.Fatalf("calls %d", calls)
	}
	if c := serviceCount(t, ten, name); c != 1 {
		t.Fatalf("effect applied %d times", c)
	}
}

func TestPlatformLevelEvent(t *testing.T) {
	ty := typ(t)
	err := db.Tx(context.Background(), env.Pools.App, func(tx pgx.Tx) error {
		return events.Append(context.Background(), tx, events.Event{Type: ty, SourceModule: "t", AggregateType: "a", AggregateID: uuid.New()}, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	b := newBus(t, true)
	calls := 0
	b.Subscribe(events.Consumer{Name: "c-" + ty, Types: []string{ty}, Class: events.Lifecycle, Handler: func(context.Context, pgx.Tx, events.Event) error { calls++; return nil }})
	pump(t, b, 2)
	if calls != 1 {
		t.Fatalf("calls %d", calls)
	}
}

func TestTenantCannotForgeEventForAnotherTenant(t *testing.T) {
	a, b := tenant(t), tenant(t)
	err := db.TenantTx(context.Background(), env.Pools.App, b, uuid.Nil, func(tx pgx.Tx) error {
		return events.Append(context.Background(), tx, events.Event{Type: "x", TenantID: &a, SourceModule: "t", AggregateType: "a", AggregateID: uuid.New()}, nil)
	})
	if err == nil {
		t.Fatal("tenant B wrote an event for tenant A")
	}
}

func TestStuckDeliveryIsReleased(t *testing.T) {
	ten, ty := tenant(t), typ(t)
	publish(t, ten, ty, uuid.New(), nil)
	b := newBus(t, true)
	calls := 0
	consumer := "c-" + ty
	b.Subscribe(events.Consumer{Name: consumer, Types: []string{ty}, Class: events.Passive, Handler: func(context.Context, pgx.Tx, events.Event) error { calls++; return nil }})
	if _, err := b.PublishPending(context.Background()); err != nil {
		t.Fatal(err)
	}
	// A worker died after claiming it. updated_at is moved back, as if this happened long ago.
	if _, err := env.Super.Exec(context.Background(), `ALTER TABLE jobs.event_deliveries DISABLE TRIGGER touch_updated_at`); err != nil {
		t.Fatal(err)
	}
	if _, err := env.Super.Exec(context.Background(), `UPDATE jobs.event_deliveries SET status = 'processing', updated_at = now() - interval '1 hour' WHERE consumer = $1`, consumer); err != nil {
		t.Fatal(err)
	}
	if _, err := env.Super.Exec(context.Background(), `ALTER TABLE jobs.event_deliveries ENABLE TRIGGER touch_updated_at`); err != nil {
		t.Fatal(err)
	}
	if err := b.ReleaseStuck(context.Background()); err != nil {
		t.Fatal(err)
	}
	pump(t, b, 2)
	if calls != 1 {
		t.Fatalf("calls %d", calls)
	}
}
