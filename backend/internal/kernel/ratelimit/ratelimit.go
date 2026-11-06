// Package ratelimit provides fixed-window limits in Redis, with an in-process fallback
// so that losing Redis reduces accuracy across instances but never removes the limit.
package ratelimit

import (
	"context"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Limiter counts events per key within a window.
type Limiter struct {
	rdb   *redis.Client
	mu    sync.Mutex
	local map[string]*window
}

type window struct {
	count int
	reset time.Time
}

// New creates a limiter. rdb may be nil.
func New(rdb *redis.Client) *Limiter { return &Limiter{rdb: rdb, local: map[string]*window{}} }

// Allow reports whether another event is allowed, and seconds to wait if it is not.
func (l *Limiter) Allow(ctx context.Context, key string, limit int, per time.Duration) (bool, int) {
	if l.rdb != nil {
		rk := "ratelimit:" + key
		cctx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
		defer cancel()
		pipe := l.rdb.TxPipeline()
		incr := pipe.Incr(cctx, rk)
		pipe.ExpireNX(cctx, rk, per)
		ttl := pipe.TTL(cctx, rk)
		if _, err := pipe.Exec(cctx); err == nil {
			if int(incr.Val()) > limit {
				return false, max(1, int(ttl.Val().Seconds()))
			}
			return true, 0
		}
	}
	return l.allowLocal(key, limit, per)
}

func (l *Limiter) allowLocal(key string, limit int, per time.Duration) (bool, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if len(l.local) > 100_000 {
		l.local = map[string]*window{}
	}
	w, ok := l.local[key]
	if !ok || now.After(w.reset) {
		w = &window{reset: now.Add(per)}
		l.local[key] = w
	}
	w.count++
	if w.count > limit {
		return false, max(1, int(time.Until(w.reset).Seconds()))
	}
	return true, 0
}
