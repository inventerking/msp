// Package testenv starts a real, migrated PostgreSQL for tests. Nothing in production imports it.
package testenv

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"runtime"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // The SQL driver for migrations.
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"mspengine/db/migrations"
	"mspengine/internal/kernel/db"
)

// Env is a running database, with one pool per role.
type Env struct {
	Pools *db.Pools
	Owner *pgxpool.Pool
	Super *pgxpool.Pool
	DSN   func(role, password string) string
	stop  func()
}

// Close stops everything.
func (e *Env) Close() { e.stop() }

// Start runs PostgreSQL in Docker and applies all migrations.
func Start(ctx context.Context) (*Env, error) {
	_, self, _, _ := runtime.Caller(0)
	roles := filepath.Join(filepath.Dir(self), "../../../deploy/local/postgres/00-roles.sql")
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "pgvector/pgvector:0.8.7-pg18-trixie",
			Env:          map[string]string{"POSTGRES_PASSWORD": "test_superuser"},
			ExposedPorts: []string{"5432/tcp"},
			Files:        []testcontainers.ContainerFile{{HostFilePath: roles, ContainerFilePath: "/docker-entrypoint-initdb.d/00-roles.sql", FileMode: 0o644}},
			WaitingFor:   wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(90 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		return nil, err
	}
	host, err := c.Host(ctx)
	if err != nil {
		return nil, err
	}
	port, err := c.MappedPort(ctx, "5432/tcp")
	if err != nil {
		return nil, err
	}
	dsn := func(role, pw string) string { return db.DSN(host, port.Port(), "msp_engine", role, pw) }
	conn, err := sql.Open("pgx", dsn("msp_owner", "dev_owner"))
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		return nil, err
	}
	if err := goose.UpContext(ctx, conn, "."); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	open := func(role, pw string, n int32) *pgxpool.Pool {
		p, e := db.Open(ctx, dsn(role, pw), n)
		if e != nil {
			err = e
		}
		return p
	}
	env := &Env{
		Pools: &db.Pools{App: open("msp_app", "dev_app", 20), Platform: open("msp_platform", "dev_platform", 10), System: open("msp_system", "dev_system", 10)},
		Owner: open("msp_owner", "dev_owner", 3),
		Super: open("postgres", "test_superuser", 3),
		DSN:   dsn,
	}
	if err != nil {
		return nil, err
	}
	env.stop = func() {
		env.Pools.Close()
		env.Owner.Close()
		env.Super.Close()
		_ = c.Terminate(context.Background())
	}
	return env, nil
}
