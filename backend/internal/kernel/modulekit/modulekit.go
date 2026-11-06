// Package modulekit defines what every business module provides to the application.
package modulekit

import "net/http"

// Module registers one business module's routes. The route guard enforces its entitlement and
// permissions from the OpenAPI contract, so a module cannot serve a route without declaring them.
type Module struct {
	Key    string
	Prefix string
	Routes []Route
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
