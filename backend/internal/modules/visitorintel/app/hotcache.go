package app

import (
	"sync"
	"time"

	"github.com/google/uuid"

	"mspengine/internal/modules/visitorintel/repo"
)

// hotTTL is how long the ingest path trusts what it has read. A suspended module or a changed goal
// takes effect on tracking within this time when ingest runs in its own process; in the API
// process, goal changes take effect at once.
const hotTTL = 30 * time.Second

// touchEvery matches TouchSite's own one-minute throttle, so most hits skip that update.
const touchEvery = time.Minute

type cached[T any] struct {
	v  T
	at time.Time
}

// hotCache holds the per-tenant and per-site facts every tracked hit needs, so a hit usually costs
// one database transaction instead of three.
type hotCache struct {
	mu      sync.Mutex
	active  map[uuid.UUID]cached[bool]
	goals   map[uuid.UUID]cached[[]repo.ListGoalsRow]
	touched map[uuid.UUID]time.Time
}

func newHotCache() *hotCache {
	return &hotCache{active: map[uuid.UUID]cached[bool]{}, goals: map[uuid.UUID]cached[[]repo.ListGoalsRow]{}, touched: map[uuid.UUID]time.Time{}}
}

func (c *hotCache) moduleActive(tenant uuid.UUID, now time.Time) (bool, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.active[tenant]
	return e.v, ok && now.Sub(e.at) < hotTTL
}

func (c *hotCache) setModuleActive(tenant uuid.UUID, v bool, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.active[tenant] = cached[bool]{v, now}
}

func (c *hotCache) siteGoals(site uuid.UUID, now time.Time) ([]repo.ListGoalsRow, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.goals[site]
	return e.v, ok && now.Sub(e.at) < hotTTL
}

func (c *hotCache) setSiteGoals(site uuid.UUID, v []repo.ListGoalsRow, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.goals[site] = cached[[]repo.ListGoalsRow]{v, now}
}

// forgetGoals drops every cached goal list, after a goal is added or removed in this process.
func (c *hotCache) forgetGoals() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.goals = map[uuid.UUID]cached[[]repo.ListGoalsRow]{}
}

// touchDue reports whether the site's last-seen time should be updated, and if so claims it.
func (c *hotCache) touchDue(site uuid.UUID, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if now.Sub(c.touched[site]) < touchEvery {
		return false
	}
	c.touched[site] = now
	return true
}
