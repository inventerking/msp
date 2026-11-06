// Package db provides connection pools per database role and the only sanctioned way
// to run tenant-scoped work: a transaction with row-level security context applied.
package db

import (
	"context"
	"fmt"
	"net/url"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Pools holds one bounded pool per runtime database role.
type Pools struct {
	App      *pgxpool.Pool // tenant requests; row-level security enforced
	Platform *pgxpool.Pool // platform admin use cases
	System   *pgxpool.Pool // housekeeping only
}

// DSN builds a connection string for a role.
func DSN(host, port, name, role, password string) string {
	u := url.URL{Scheme: "postgres", User: url.UserPassword(role, password), Host: host + ":" + port, Path: name, RawQuery: "sslmode=disable"}
	return u.String()
}

// Open connects a bounded pool.
func Open(ctx context.Context, dsn string, maxConns int32) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse database config: %w", err)
	}
	cfg.MaxConns = maxConns
	return pgxpool.NewWithConfig(ctx, cfg)
}

// Close closes all pools.
func (p *Pools) Close() {
	p.App.Close()
	p.Platform.Close()
	p.System.Close()
}

// Tx runs fn in a transaction with no tenant or user context. RLS shows no tenant rows.
func Tx(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, pool, fn)
}

// UserTx runs fn with only the user context set, for reading the user's own memberships.
func UserTx(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID, fn func(pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := setLocal(ctx, tx, "app.user_id", userID.String()); err != nil {
			return err
		}
		return fn(tx)
	})
}

// TenantTx runs fn with tenant context applied through SET LOCAL semantics, which end with
// the transaction and so cannot leak to the next user of a pooled connection.
func TenantTx(ctx context.Context, pool *pgxpool.Pool, tenantID, userID uuid.UUID, fn func(pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := setLocal(ctx, tx, "app.tenant_id", tenantID.String()); err != nil {
			return err
		}
		if userID != uuid.Nil {
			if err := setLocal(ctx, tx, "app.user_id", userID.String()); err != nil {
				return err
			}
		}
		return fn(tx)
	})
}

func setLocal(ctx context.Context, tx pgx.Tx, key, value string) error {
	if _, err := tx.Exec(ctx, "SELECT set_config($1, $2, true)", key, value); err != nil {
		return fmt.Errorf("set %s: %w", key, err)
	}
	return nil
}
