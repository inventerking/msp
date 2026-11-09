// Package modulekit defines what every business module provides to the application.
package modulekit

import (
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/go-chi/chi/v5"

	"mspengine/internal/kernel/events"
	"mspengine/internal/kernel/jobs"
)

// Module registers one business module's routes and background work. The route guard enforces
// its entitlement and permissions from the OpenAPI contract, so a module cannot serve a route
// without declaring them.
type Module struct {
	Key    string
	Prefix string
	// Routes are hand-registered routes, described in the shared module contract.
	Routes []Route
	// Contract is the module's own OpenAPI document, with paths under the tenant API base.
	Contract func() (*openapi3.T, error)
	// Mount registers the contract's routes on r. Every route must be wrapped in guard, and
	// parameter binding errors must go to bindError.
	Mount func(r chi.Router, base string, guard func(http.Handler) http.Handler, bindError func(http.ResponseWriter, *http.Request, error))
	// Jobs are the module's background job handlers, by kind.
	Jobs map[string]jobs.Handler
	// Consumers subscribe to events.
	Consumers []events.Consumer
	// Schedules create recurring jobs.
	Schedules []Schedule
}

// Schedule is one recurring job.
type Schedule struct {
	Key, Every, Kind string
}

// Route is one module HTTP route, relative to Prefix.
type Route struct {
	Method  string
	Path    string
	Handler http.HandlerFunc
}

// StatusRoute is the placeholder route each module serves until its phase is built.
func StatusRoute(key string) Route {
	return Route{Method: http.MethodGet, Path: "/status", Handler: func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"module":"` + key + `","status":"active"}`))
	}}
}
