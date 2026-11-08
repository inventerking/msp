// Package servicetest exercises the Phase 2 services against a real, migrated PostgreSQL.
package servicetest

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/google/uuid"

	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/crypto"
	"mspengine/internal/kernel/jobs"
	"mspengine/internal/kernel/realtime"
	"mspengine/internal/platform/commercial"
	"mspengine/internal/platform/integration"
	"mspengine/internal/platform/shared"
	"mspengine/internal/testenv"
)

var (
	env  *testenv.Env
	keys = crypto.Keys{HMAC: bytes.Repeat([]byte{7}, 32), Encryption: bytes.Repeat([]byte{9}, 32)}
	com  *commercial.Service
	ints *integration.Service
	sh   *shared.Service
	ctx  = context.Background()
)

func TestMain(m *testing.M) {
	var err error
	env, err = testenv.Start(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	com = commercial.New(env.Pools, realtime.New(nil))
	ints = integration.New(env.Pools, keys)
	sh = shared.New(env.Pools, keys)
	code := m.Run()
	env.Close()
	os.Exit(code)
}

func newTenant(t *testing.T) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := env.Super.Exec(ctx, `INSERT INTO identity.tenants (id, slug, name, status, default_locale, default_timezone, reporting_currency, data_region) VALUES ($1, $2, 'T', 'active', 'en', 'UTC', 'USD', 'eu')`, id, id.String())
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func entitle(t *testing.T, tenant uuid.UUID, module string) {
	t.Helper()
	a := authctx.AdminPrincipal{AdminID: uuid.New(), Roles: []string{"super_admin"}}
	if _, err := com.ChangeEntitlement(authctx.WithAdmin(ctx, a), tenant, module, "activate", "test setup", nil); err != nil {
		t.Fatal(err)
	}
}

func grant(t *testing.T, tenant uuid.UUID, amount int64) {
	t.Helper()
	if err := com.Grant(ctx, tenant, commercial.MeterAI, amount, uuid.NewString(), nil, "test"); err != nil {
		t.Fatal(err)
	}
}

func balance(t *testing.T, tenant uuid.UUID) (int64, int64) {
	t.Helper()
	rows, err := com.Balances(ctx, tenant)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.MeterKey == commercial.MeterAI {
			return r.Balance, r.Reserved
		}
	}
	return 0, 0
}

// runJobs runs a queue until it is empty. Handlers come from the caller.
func runJobs(t *testing.T, r *jobs.Runner, queue string) {
	t.Helper()
	for range 50 {
		did, err := r.RunOne(ctx, queue)
		if err != nil {
			t.Fatal(err)
		}
		if !did {
			return
		}
	}
	t.Fatal("queue did not drain")
}
