// Package api is the HTTP layer: it mounts the generated contract routes and the module routes
// behind the guards, and refuses to start if any route lacks a declared access rule.
package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"mspengine/internal/api/adminapi"
	"mspengine/internal/api/modulesapi"
	"mspengine/internal/api/tenantapi"
	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/config"
	"mspengine/internal/kernel/crypto"
	"mspengine/internal/kernel/db"
	"mspengine/internal/kernel/jobs"
	"mspengine/internal/kernel/metrics"
	"mspengine/internal/kernel/modulekit"
	"mspengine/internal/kernel/ratelimit"
	"mspengine/internal/kernel/realtime"
	"mspengine/internal/platform/commercial"
	"mspengine/internal/platform/files"
	"mspengine/internal/platform/identity"
	"mspengine/internal/platform/integration"
	"mspengine/internal/platform/integration/aigateway"
	"mspengine/internal/platform/integration/fakes"
	"mspengine/internal/platform/notification"
	"mspengine/internal/platform/shared"
	"mspengine/internal/platform/trust"
)

// Deps are the services the HTTP layer needs.
type Deps struct {
	Config     config.Config
	Identity   *identity.Service
	Commercial *commercial.Service
	Keys       crypto.Keys
	Limiter    *ratelimit.Limiter
	Hub        *realtime.Hub
	DB         *pgxpool.Pool
	Redis      *redis.Client
	Shared     *shared.Service
	Files      *files.Service
	Notify     *notification.Service
	Integrate  *integration.Service
	AI         *aigateway.Gateway
	Pools      *db.Pools
	Ops        *jobs.Ops
	// Modules replaces the skeleton of any module with the same key.
	Modules []modulekit.Module
	// OutreachPolicy manages the platform's outreach rules. Nil returns 503 for those routes.
	OutreachPolicy OutreachPolicy
	// Trust publishes legal documents and takes abuse reports.
	Trust *trust.Service
	// FakeCheckout is the test payment provider's checkout. Nil (outside test environments) means
	// its routes answer not found.
	FakeCheckout FakeCheckout
	// Metrics records HTTP requests. Nil records nothing.
	Metrics *metrics.Registry
}

// FakeCheckout is what the fake checkout page needs from the test payment provider.
type FakeCheckout interface {
	Info(ctx context.Context, ref string) (fakes.CheckoutInfo, error)
	Complete(ctx context.Context, ref, outcome string) (integration.PaymentEvent, error)
}

// Server holds handler dependencies.
type Server struct {
	Deps
	identity      *identity.Service
	commercial    *commercial.Service
	policies      map[string]*policy
	sessionCookie string
	adminCookie   string
}

const (
	tenantBase = "/api/v1"
	adminBase  = "/api/v1/admin"
)

// New builds the router. It returns an error if a route and the contract disagree.
func New(d Deps) (http.Handler, error) {
	s := &Server{Deps: d, identity: d.Identity, commercial: d.Commercial, policies: map[string]*policy{}, sessionCookie: "__Host-session", adminCookie: "__Host-admin_session"}
	if d.Config.IsLocal() {
		// Browsers refuse the __Host- prefix without HTTPS, so local development omits it.
		s.sessionCookie, s.adminCookie = "session", "admin_session"
	}
	tenantSpec, err := tenantapi.GetSwagger()
	if err != nil {
		return nil, fmt.Errorf("load tenant contract: %w", err)
	}
	adminSpec, err := adminapi.GetSwagger()
	if err != nil {
		return nil, fmt.Errorf("load admin contract: %w", err)
	}
	moduleSpec, err := modulesapi.GetSwagger()
	if err != nil {
		return nil, fmt.Errorf("load module contract: %w", err)
	}
	modules := s.modules()
	contracts := []struct {
		spec *openapi3.T
		base string
	}{{tenantSpec, tenantBase}, {moduleSpec, tenantBase}, {adminSpec, adminBase}}
	for _, m := range modules {
		if m.Contract == nil {
			continue
		}
		spec, err := m.Contract()
		if err != nil {
			return nil, fmt.Errorf("load %s contract: %w", m.Key, err)
		}
		contracts = append(contracts, struct {
			spec *openapi3.T
			base string
		}{spec, tenantBase})
	}
	for _, c := range contracts {
		ps, err := loadPolicies(c.spec, c.base)
		if err != nil {
			return nil, err
		}
		for k, v := range ps {
			if s.policies[k] != nil {
				return nil, fmt.Errorf("%s is declared by two contracts", k)
			}
			s.policies[k] = v
		}
	}

	r := chi.NewRouter()
	r.Use(s.requestMeta, recoverer, requestLog, d.Metrics.HTTP, securityHeaders(!d.Config.IsLocal()), embedCORS, s.originCheck, s.rateLimit)
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	r.Get("/readyz", s.ready)
	r.NotFound(func(w http.ResponseWriter, r *http.Request) { writeError(w, r, apperr.NotFound()) })
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, apperr.New(http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed"))
	})
	// Generated code binds parameters before middleware runs. When binding fails, the caller is still
	// authenticated and authorized first, so an anonymous request never learns about validation.
	bindErr := func(w http.ResponseWriter, r *http.Request, _ error) {
		if pol := s.policyFor(r); pol != nil {
			authorize := s.authorizeTenant
			if strings.HasPrefix(r.URL.Path, adminBase+"/") {
				authorize = s.authorizeAdmin
			}
			if _, err := authorize(r, pol); err != nil {
				writeError(w, r, err)
				return
			}
		}
		writeError(w, r, apperr.Validation(apperr.FieldError{Field: "parameters", Code: "invalid"}))
	}
	tenantapi.HandlerWithOptions(tenantHandlers{s}, tenantapi.ChiServerOptions{BaseURL: tenantBase, BaseRouter: r, Middlewares: []tenantapi.MiddlewareFunc{s.guard}, ErrorHandlerFunc: bindErr})
	adminapi.HandlerWithOptions(adminHandlers{s}, adminapi.ChiServerOptions{BaseURL: adminBase, BaseRouter: r, Middlewares: []adminapi.MiddlewareFunc{s.adminGuard}, ErrorHandlerFunc: bindErr})
	for _, m := range modules {
		for _, rt := range m.Routes {
			r.With(s.guard).Method(rt.Method, tenantBase+m.Prefix+rt.Path, rt.Handler)
		}
		if m.Mount != nil {
			m.Mount(r, tenantBase, s.guard, bindErr)
		}
	}
	if err := s.checkParity(r); err != nil {
		return nil, err
	}
	return r, nil
}

// modules returns the ten modules, with built ones in place of their skeletons.
func (s *Server) modules() []modulekit.Module {
	out := make([]modulekit.Module, len(allModules))
	copy(out, allModules)
	for _, m := range s.Modules {
		for i := range out {
			if out[i].Key == m.Key {
				out[i] = m
			}
		}
	}
	return out
}

// checkParity fails startup if a served route has no contract entry, or the reverse.
func (s *Server) checkParity(r chi.Routes) error {
	served := map[string]bool{}
	var missing []string
	err := chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if !strings.HasPrefix(route, "/api/") {
			return nil
		}
		k := routeKey(method, route)
		served[k] = true
		if s.policies[k] == nil {
			missing = append(missing, "route without contract: "+k)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for k := range s.policies {
		if !served[k] {
			missing = append(missing, "contract without route: "+k)
		}
	}
	if len(missing) > 0 {
		slices.Sort(missing)
		return fmt.Errorf("route and contract disagree:\n%s", strings.Join(missing, "\n"))
	}
	return nil
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.DB.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
		return
	}
	cache := "ok"
	if s.Redis == nil || s.Redis.Ping(ctx).Err() != nil {
		cache = "degraded" // Redis loss degrades the API; it does not stop it.
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "redis": cache})
}

func (s *Server) clientIP(r *http.Request) string {
	if s.Config.TrustForwarded {
		if f := r.Header.Get("X-Forwarded-For"); f != "" {
			parts := strings.Split(f, ",")
			return strings.TrimSpace(parts[len(parts)-1])
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) requestMeta(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.NewV7()
		if err != nil {
			id = uuid.New()
		}
		ip := s.clientIP(r)
		ua := r.UserAgent()
		if len(ua) > 512 {
			ua = ua[:512]
		}
		w.Header().Set("X-Request-Id", id.String())
		next.ServeHTTP(w, r.WithContext(authctx.WithMeta(r.Context(), authctx.Meta{RequestID: id, IP: ip, IPHash: s.Keys.HMACValue("ip", ip), UserAgent: ua})))
	})
}

func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if err, ok := v.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					panic(v)
				}
				writeError(w, r, fmt.Errorf("panic: %v", v))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(c int)           { w.status = c; w.ResponseWriter.WriteHeader(c) }
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// requestLog logs the route template, never raw IDs, addresses or bodies.
func requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(sw, r)
		slog.InfoContext(r.Context(), "request", "method", r.Method, "route", chi.RouteContext(r.Context()).RoutePattern(),
			"status", sw.status, "duration_ms", time.Since(start).Milliseconds(), "request_id", authctx.MetaFrom(r.Context()).RequestID)
	})
}

func securityHeaders(hsts bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
			h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
			h.Set("Cache-Control", "no-store")
			if hsts {
				h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// embedPaths are public endpoints called from customers' own websites: the tracker and the chat
// widget. They carry no session, check the origin against the site's or bot's own list, and answer
// cross-origin requests without credentials.
var embedPaths = []string{"/api/v1/public/collect", "/api/v1/public/chat/"}

func isEmbedPath(p string) bool {
	for _, e := range embedPaths {
		if p == e || (strings.HasSuffix(e, "/") && strings.HasPrefix(p, e)) {
			return true
		}
	}
	return false
}

// embedCORS lets customers' pages call the embed endpoints. Credentials are never allowed, so a
// page cannot act with anyone's session; the handlers decide which origins may use each site or bot.
func embedCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isEmbedPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		if o := r.Header.Get("Origin"); o != "" {
			w.Header().Set("Access-Control-Allow-Origin", o)
			w.Header().Add("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// originCheck rejects state-changing requests from origins that are not ours.
func (s *Server) originCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if unsafeMethod(r.Method) && !isEmbedPath(r.URL.Path) {
			if o := r.Header.Get("Origin"); o != "" && !slices.Contains(s.Config.AllowedOrigins, o) {
				writeError(w, r, apperr.New(http.StatusForbidden, "csrf_failed", "This request came from an unrecognized site"))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// rateLimit applies a strict limit to authentication routes and a broad limit to everything else.
func (s *Server) rateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m := authctx.MetaFrom(r.Context())
		key := string(m.IPHash)
		if strings.HasPrefix(r.URL.Path, tenantBase+"/auth/") || strings.HasPrefix(r.URL.Path, adminBase+"/auth/") {
			if r.Method == http.MethodPost {
				if ok, wait := s.Limiter.Allow(r.Context(), "auth:"+key, s.Config.AuthRatePerMinute, time.Minute); !ok {
					writeError(w, r, apperr.RateLimited(wait))
					return
				}
			}
		}
		if ok, wait := s.Limiter.Allow(r.Context(), "all:"+key, s.Config.AuthRatePerMinute*40, time.Minute); !ok {
			writeError(w, r, apperr.RateLimited(wait))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) setCookie(w http.ResponseWriter, name, value string, maxAge int) {
	// Secure is off only in local development, which has no TLS. Every other environment sets it.
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", HttpOnly: true, Secure: !s.Config.IsLocal(), SameSite: http.SameSiteLaxMode, MaxAge: maxAge}) //nolint:gosec // See the comment above.
}
