// Package realtime fans out small signals to connected clients. Messages only tell a client
// to refetch, so a lost message costs freshness and never data.
package realtime

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// Hub publishes and subscribes per tenant.
type Hub struct{ rdb *redis.Client }

// New creates a hub. rdb may be nil, in which case clients rely on refetching.
func New(rdb *redis.Client) *Hub { return &Hub{rdb: rdb} }

func channel(tenantID uuid.UUID) string { return "tenant:" + tenantID.String() + ":events" }

// Publish sends a signal. Failure is logged and is not an error for the caller.
func (h *Hub) Publish(ctx context.Context, tenantID uuid.UUID, eventType string) {
	if h.rdb == nil {
		return
	}
	if err := h.rdb.Publish(ctx, channel(tenantID), eventType).Err(); err != nil {
		slog.WarnContext(ctx, "realtime publish failed", "error", err)
	}
}

// Subscribe returns a stream of signal types for a tenant and a function to close it.
func (h *Hub) Subscribe(ctx context.Context, tenantID uuid.UUID) (<-chan string, func()) {
	out := make(chan string, 8)
	if h.rdb == nil {
		return out, func() {}
	}
	sub := h.rdb.Subscribe(ctx, channel(tenantID))
	go func() {
		defer close(out)
		for m := range sub.Channel() {
			select {
			case out <- m.Payload:
			default:
			}
		}
	}()
	return out, func() { _ = sub.Close() }
}
