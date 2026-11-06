// Package audit records accountable actions in the same transaction as the action itself,
// so an action cannot succeed without its audit record.
package audit

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mspengine/internal/kernel/authctx"
	"mspengine/internal/platform/audit/repo"
)

// Event describes one audited action. Before and After must not contain secrets.
type Event struct {
	TenantID     *uuid.UUID
	ActorType    string // user, platform_admin, system or public
	ActorID      *uuid.UUID
	Action       string
	ResourceType string
	ResourceID   *uuid.UUID
	Outcome      string // defaults to success
	Before       any
	After        any
}

// Record writes the event inside tx.
func Record(ctx context.Context, tx pgx.Tx, e Event) error {
	m := authctx.MetaFrom(ctx)
	if e.Outcome == "" {
		e.Outcome = "success"
	}
	before, err := marshal(e.Before)
	if err != nil {
		return err
	}
	after, err := marshal(e.After)
	if err != nil {
		return err
	}
	var corr *uuid.UUID
	if m.RequestID != uuid.Nil {
		corr = &m.RequestID
	}
	var ua *string
	if m.UserAgent != "" {
		ua = &m.UserAgent
	}
	if err := repo.New(tx).InsertEvent(ctx, repo.InsertEventParams{
		TenantID: e.TenantID, ActorType: e.ActorType, ActorID: e.ActorID, Action: e.Action,
		ResourceType: e.ResourceType, ResourceID: e.ResourceID, Outcome: e.Outcome,
		IpHash: m.IPHash, UserAgent: ua, CorrelationID: corr, Before: before, After: after,
	}); err != nil {
		return fmt.Errorf("record audit event %s: %w", e.Action, err)
	}
	return nil
}

// Security records a security event in its own transaction, so it survives a failed request.
func Security(ctx context.Context, pool *pgxpool.Pool, eventType, severity string, tenantID *uuid.UUID, detail map[string]any) error {
	m := authctx.MetaFrom(ctx)
	d, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	var corr *uuid.UUID
	if m.RequestID != uuid.Nil {
		corr = &m.RequestID
	}
	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return repo.New(tx).InsertSecurityEvent(ctx, repo.InsertSecurityEventParams{
			TenantID: tenantID, EventType: eventType, Severity: severity, IpHash: m.IPHash, Detail: d, CorrelationID: corr,
		})
	})
}

// List returns recent events visible to the transaction's database context.
func List(ctx context.Context, tx pgx.Tx, before *uuid.UUID, limit int32) ([]repo.ListEventsRow, error) {
	return repo.New(tx).ListEvents(ctx, repo.ListEventsParams{BeforeID: before, RowLimit: limit})
}

func marshal(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	return json.Marshal(v)
}
