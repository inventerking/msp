package servicetest

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/db"
	"mspengine/internal/kernel/events"
	"mspengine/internal/kernel/jobs"
	"mspengine/internal/kernel/mailer"
	"mspengine/internal/kernel/realtime"
	"mspengine/internal/platform/notification"
)

type mail struct {
	mu   sync.Mutex
	sent []mailer.Message
	err  error
}

func (m *mail) Send(_ context.Context, msg mailer.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.sent = append(m.sent, msg)
	return nil
}

// member creates a user with the Owner role in a tenant.
func member(t *testing.T, tenant uuid.UUID) uuid.UUID {
	t.Helper()
	u := uuid.New()
	email := u.String()[:8] + "@example.com"
	if _, err := env.Super.Exec(ctx, `INSERT INTO identity.users (id, email, display_name, locale, timezone, status) VALUES ($1, $2, 'U', 'en', 'UTC', 'active')`, u, email); err != nil {
		t.Fatal(err)
	}
	if _, err := env.Super.Exec(ctx, `INSERT INTO identity.memberships (tenant_id, user_id, role_id, status) SELECT $1, $2, id, 'active' FROM identity.roles WHERE key = 'owner' AND tenant_id IS NULL`, tenant, u); err != nil {
		t.Fatal(err)
	}
	return u
}

func notify(t *testing.T, svc *notification.Service, tenant, user uuid.UUID, key string) {
	t.Helper()
	err := db.TenantTx(ctx, env.Pools.App, tenant, uuid.Nil, func(tx pgx.Tx) error {
		return svc.Notify(ctx, tx, tenant, user, "entitlement.changed", map[string]string{"module": "crm", "status": "active"}, key)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func deliver(t *testing.T, svc *notification.Service) {
	t.Helper()
	r := jobs.NewRunner(env.Pools.System, "n")
	r.BackoffBase, r.BackoffMax = time.Millisecond, time.Millisecond
	r.TenantLimit = 100
	r.Register(notification.JobDeliver, svc.HandleDeliver)
	r.Register("integration.webhook_dispatch", func(context.Context, jobs.Job) error { return nil })
	for range 20 {
		did, err := r.RunOne(ctx, jobs.QueueCritical)
		if err != nil {
			t.Fatal(err)
		}
		if !did {
			return
		}
	}
}

func TestNotifyStoresAndEmailsOnce(t *testing.T) {
	ten := newTenant(t)
	u := member(t, ten)
	m := &mail{}
	svc := notification.New(env.Pools, m, realtime.New(nil))
	for range 3 {
		notify(t, svc, ten, u, "same-event")
	}
	p := authctx.Principal{TenantID: ten, UserID: u}
	rows, unread, err := svc.List(ctx, p)
	if err != nil || len(rows) != 1 || unread != 1 {
		t.Fatalf("%v rows=%d unread=%d", err, len(rows), unread)
	}
	if rows[0].Title != "Module access changed: crm is now active." {
		t.Fatalf("title %q", rows[0].Title)
	}
	deliver(t, svc)
	if len(m.sent) != 1 || m.sent[0].Subject != "Your module access changed" {
		t.Fatalf("sent %+v", m.sent)
	}
}

// The in-app record exists even when email is down, and email retries.
func TestEmailOutageDoesNotLoseTheNotification(t *testing.T) {
	ten := newTenant(t)
	u := member(t, ten)
	m := &mail{err: errors.New("smtp down")}
	svc := notification.New(env.Pools, m, realtime.New(nil))
	notify(t, svc, ten, u, "k")
	// A long backoff holds the retry, so one failed attempt is all that happens during the outage.
	held := jobs.NewRunner(env.Pools.System, "held")
	held.BackoffBase, held.BackoffMax = time.Hour, time.Hour
	held.TenantLimit = 100
	held.Register(notification.JobDeliver, svc.HandleDeliver)
	for range 20 {
		did, err := held.RunOne(ctx, jobs.QueueCritical)
		if err != nil {
			t.Fatal(err)
		}
		if !did {
			break
		}
	}
	if rows, _, err := svc.List(ctx, authctx.Principal{TenantID: ten, UserID: u}); err != nil || len(rows) != 1 {
		t.Fatalf("%v %d", err, len(rows))
	}
	var failed int
	if err := env.Super.QueryRow(ctx, `SELECT count(*) FROM notification.delivery_attempts a JOIN notification.notifications n ON n.id = a.notification_id WHERE n.tenant_id = $1 AND a.status = 'failed'`, ten).Scan(&failed); err != nil || failed == 0 {
		t.Fatalf("%d %v", failed, err)
	}
	m.mu.Lock()
	m.err = nil
	m.mu.Unlock()
	if _, err := env.Pools.System.Exec(ctx, `UPDATE jobs.jobs SET run_at = now() WHERE kind = $1 AND tenant_id = $2`, notification.JobDeliver, ten); err != nil {
		t.Fatal(err)
	}
	deliver(t, svc)
	if len(m.sent) != 1 {
		t.Fatalf("sent %d", len(m.sent))
	}
}

func TestPreferenceTurnsOffEmailOnly(t *testing.T) {
	ten := newTenant(t)
	u := member(t, ten)
	m := &mail{}
	svc := notification.New(env.Pools, m, realtime.New(nil))
	p := authctx.Principal{TenantID: ten, UserID: u}
	if err := svc.SetPreference(ctx, p, "entitlement.changed", "email", false); err != nil {
		t.Fatal(err)
	}
	notify(t, svc, ten, u, "k")
	deliver(t, svc)
	if len(m.sent) != 0 {
		t.Fatal("an email was sent against the user's preference")
	}
	if rows, _, _ := svc.List(ctx, p); len(rows) != 1 {
		t.Fatal("the in-app notification should remain")
	}
	if err := svc.SetPreference(ctx, p, "security.notice", "email", false); err == nil {
		t.Fatal("security notices were turned off")
	}
}

func TestNotificationsArePrivateToTheirRecipient(t *testing.T) {
	ten, other := newTenant(t), newTenant(t)
	u, colleague, outsider := member(t, ten), member(t, ten), member(t, other)
	svc := notification.New(env.Pools, &mail{}, realtime.New(nil))
	notify(t, svc, ten, u, "k")
	owner := authctx.Principal{TenantID: ten, UserID: u}
	rows, _, _ := svc.List(ctx, owner)
	id := rows[0].ID
	for name, p := range map[string]authctx.Principal{"colleague": {TenantID: ten, UserID: colleague}, "other tenant": {TenantID: other, UserID: outsider}} {
		if rows, _, err := svc.List(ctx, p); err != nil || len(rows) != 0 {
			t.Errorf("%s sees %d: %v", name, len(rows), err)
		}
		if err := svc.MarkRead(ctx, p, id); errCode(err) != "not_found" {
			t.Errorf("%s marked it read: %v", name, err)
		}
	}
	if err := svc.MarkRead(ctx, owner, id); err != nil {
		t.Fatal(err)
	}
	if _, unread, _ := svc.List(ctx, owner); unread != 0 {
		t.Fatalf("unread %d", unread)
	}
}

// The whole path: an admin suspends a module, an event fires, and members are told exactly once.
func TestEntitlementChangeNotifiesThroughEvents(t *testing.T) {
	ten := newTenant(t)
	u := member(t, ten)
	entitle(t, ten, "crm")
	svc := notification.New(env.Pools, &mail{}, realtime.New(nil))
	bus := events.New(env.Pools.System, env.Pools.App, func(context.Context, uuid.UUID, string) (bool, error) { return true, nil })
	bus.Subscribe(svc.Consumer())
	for range 3 {
		if _, err := bus.PublishPending(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := bus.DeliverPending(ctx); err != nil {
			t.Fatal(err)
		}
	}
	rows, _, err := svc.List(ctx, authctx.Principal{TenantID: ten, UserID: u})
	if err != nil || len(rows) != 1 {
		t.Fatalf("%v rows=%d", err, len(rows))
	}
	if rows[0].Title != "Module access changed: crm is now activated." {
		t.Fatalf("title %q", rows[0].Title)
	}
}
