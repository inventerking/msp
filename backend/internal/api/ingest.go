package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/config"
	"mspengine/internal/kernel/crypto"
	"mspengine/internal/kernel/metrics"
	"mspengine/internal/kernel/ratelimit"
)

// CollectPath is the visitor tracker's endpoint, the only route the ingest process serves.
const CollectPath = tenantBase + "/public/collect"

// IngestDeps are what the ingest process needs: no sessions, no other module, and its own pool.
type IngestDeps struct {
	Config  config.Config
	Keys    crypto.Keys
	Limiter *ratelimit.Limiter
	DB      *pgxpool.Pool
	Redis   *redis.Client
	Metrics *metrics.Registry
	// Collect records one tracker hit.
	Collect http.HandlerFunc
}

// NewIngest builds the ingest process's handler. It serves only the tracker endpoint and health
// checks, with the same request metadata, logging, CORS and rate limits as the API, so visitor
// traffic runs in its own process and cannot use the API's database connections.
func NewIngest(d IngestDeps) http.Handler {
	s := &Server{Deps: Deps{Config: d.Config, Keys: d.Keys, Limiter: d.Limiter, DB: d.DB, Redis: d.Redis, Metrics: d.Metrics}}
	r := chi.NewRouter()
	r.Use(s.requestMeta, recoverer, requestLog, d.Metrics.HTTP, securityHeaders(!d.Config.IsLocal()), embedCORS, s.rateLimit)
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	r.Get("/readyz", s.ready)
	r.Post(CollectPath, d.Collect)
	r.Options(CollectPath, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	r.NotFound(func(w http.ResponseWriter, r *http.Request) { writeError(w, r, apperr.NotFound()) })
	return r
}
