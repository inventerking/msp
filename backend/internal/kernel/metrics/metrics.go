// Package metrics exposes Prometheus metrics on a separate, unpublished listener.
//
// Labels never carry tenant IDs, user IDs, raw paths or anything a customer typed: HTTP metrics use
// the route template, and health gauges are platform-wide counts.
package metrics

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Registry holds one process's metrics.
type Registry struct {
	reg      *prometheus.Registry
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
}

// New creates a registry with Go runtime and process metrics, labelled by process role.
func New(role string) *Registry {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	up := prometheus.NewGauge(prometheus.GaugeOpts{Name: "msp_process_up", Help: "1 while this process runs.", ConstLabels: prometheus.Labels{"role": role}})
	up.Set(1)
	r := &Registry{
		reg: reg,
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "msp_http_requests_total", Help: "HTTP requests by route template and status code."},
			[]string{"method", "route", "code"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "msp_http_request_duration_seconds", Help: "HTTP request duration by route template.",
			Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10}}, []string{"method", "route"}),
	}
	reg.MustRegister(up, r.requests, r.duration)
	return r
}

type statusRecorder struct {
	http.ResponseWriter
	code int
}

func (s *statusRecorder) WriteHeader(c int)           { s.code = c; s.ResponseWriter.WriteHeader(c) }
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }
func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// HTTP records each request under its chi route template. Unmatched requests share one label, so
// scanners probing random paths cannot create unbounded series.
func (r *Registry) HTTP(next http.Handler) http.Handler {
	if r == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		start := time.Now()
		sw := &statusRecorder{ResponseWriter: w, code: 200}
		next.ServeHTTP(sw, req)
		route := "unmatched"
		if rc := chi.RouteContext(req.Context()); rc != nil && rc.RoutePattern() != "" {
			route = rc.RoutePattern()
		}
		r.requests.WithLabelValues(req.Method, route, strconv.Itoa(sw.code)).Inc()
		r.duration.WithLabelValues(req.Method, route).Observe(time.Since(start).Seconds())
	})
}

// Sample is one gauge value read at scrape time.
type Sample struct {
	Name   string
	Help   string
	Value  float64
	Labels map[string]string
}

// Source reads gauges when Prometheus scrapes. A failing source is reported as
// msp_metrics_source_up{source} = 0 rather than failing the whole scrape.
type Source func(ctx context.Context) ([]Sample, error)

// Gauges registers a source of gauges read on each scrape, with a time limit.
func (r *Registry) Gauges(name string, read Source) {
	r.reg.MustRegister(&sourceCollector{name: name, read: read})
}

// Pools reports connection pool use, by role.
func (r *Registry) Pools(pools map[string]*pgxpool.Pool) {
	r.Gauges("db_pools", func(context.Context) ([]Sample, error) {
		var out []Sample
		for role, p := range pools {
			if p == nil {
				continue
			}
			st := p.Stat()
			l := map[string]string{"pool": role}
			out = append(out,
				Sample{"msp_db_pool_acquired_connections", "Connections in use.", float64(st.AcquiredConns()), l},
				Sample{"msp_db_pool_max_connections", "Pool size limit.", float64(st.MaxConns()), l},
				Sample{"msp_db_pool_wait_seconds_total", "Total time spent waiting for a connection.", st.AcquireDuration().Seconds(), l},
			)
		}
		return out, nil
	})
}

type sourceCollector struct {
	name string
	read Source
}

// Describe sends nothing, which makes this an unchecked collector: its metrics are built per scrape.
func (c *sourceCollector) Describe(chan<- *prometheus.Desc) {}

func (c *sourceCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	samples, err := c.read(ctx)
	up := 1.0
	if err != nil {
		up = 0
		slog.Warn("metrics source failed", "source", c.name, "error", err)
	}
	ch <- prometheus.MustNewConstMetric(prometheus.NewDesc("msp_metrics_source_up", "1 if the metrics source could be read.", nil, prometheus.Labels{"source": c.name}), prometheus.GaugeValue, up)
	for _, s := range samples {
		keys := make([]string, 0, len(s.Labels))
		vals := make([]string, 0, len(s.Labels))
		for k, v := range s.Labels {
			keys = append(keys, k)
			vals = append(vals, v)
		}
		m, err := prometheus.NewConstMetric(prometheus.NewDesc(s.Name, s.Help, keys, nil), prometheus.GaugeValue, s.Value, vals...)
		if err != nil {
			slog.Warn("metrics sample rejected", "source", c.name, "name", s.Name, "error", err)
			continue
		}
		ch <- m
	}
}

// Handler serves the registry in the Prometheus text format.
func (r *Registry) Handler() http.Handler {
	return promhttp.HandlerFor(r.reg, promhttp.HandlerOpts{Registry: r.reg, MaxRequestsInFlight: 2, Timeout: 10 * time.Second})
}

// Serve runs the metrics listener until ctx ends. It does nothing when addr is empty.
func (r *Registry) Serve(ctx context.Context, addr string) {
	if addr == "" || r == nil {
		return
	}
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", r.Handler())
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 15 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	go func() {
		slog.Info("metrics listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("metrics listener stopped", "error", err)
		}
	}()
}
