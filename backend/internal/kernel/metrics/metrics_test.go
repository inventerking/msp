package metrics

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func scrape(t *testing.T, r *Registry) string {
	t.Helper()
	rec := httptest.NewRecorder()
	r.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	b, _ := io.ReadAll(rec.Body)
	return string(b)
}

func TestHTTPUsesRouteTemplatesOnly(t *testing.T) {
	r := New("api")
	mux := chi.NewRouter()
	mux.Use(r.HTTP)
	mux.Get("/things/{id}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
	for _, p := range []string{"/things/secret-id-1", "/things/secret-id-2", "/random/probe"} {
		mux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, p, nil))
	}
	out := scrape(t, r)
	if strings.Contains(out, "secret-id") || strings.Contains(out, "probe") {
		t.Fatal("raw paths leaked into labels")
	}
	if !strings.Contains(out, `msp_http_requests_total{code="204",method="GET",route="/things/{id}"} 2`) {
		t.Fatalf("missing templated counter:\n%s", out)
	}
	if !strings.Contains(out, `route="unmatched"`) {
		t.Fatal("unmatched requests are not grouped")
	}
}

func TestFailingSourceDoesNotBreakTheScrape(t *testing.T) {
	r := New("worker")
	r.Gauges("good", func(context.Context) ([]Sample, error) {
		return []Sample{{Name: "msp_test_value", Help: "x", Value: 3, Labels: map[string]string{"queue": "q"}}}, nil
	})
	r.Gauges("bad", func(context.Context) ([]Sample, error) { return nil, errors.New("db down") })
	out := scrape(t, r)
	for _, want := range []string{`msp_test_value{queue="q"} 3`, `msp_metrics_source_up{source="bad"} 0`, `msp_metrics_source_up{source="good"} 1`} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
