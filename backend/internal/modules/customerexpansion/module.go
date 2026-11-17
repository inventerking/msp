// Package customerexpansion is the Customer Expansion business module. It records contracts as the
// source of record for MRR for every tenant (D-03), and offers coverage, renewals and evidence-backed
// expansion suggestions to tenants that have it.
package customerexpansion

import (
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/go-chi/chi/v5"

	"mspengine/internal/kernel/modulekit"
	"mspengine/internal/modules/customerexpansion/app"
	"mspengine/internal/modules/customerexpansion/httpapi"
)

// Key is this module's entitlement key.
const Key = app.Module

// Module is the skeleton used when the module is not built with its dependencies.
var Module = modulekit.Module{
	Key:    Key,
	Prefix: "/customer-expansion",
	Routes: []modulekit.Route{modulekit.StatusRoute(Key)},
}

// New builds the module.
func New(d app.Deps) (modulekit.Module, *app.Service) {
	svc := app.New(d)
	m := Module
	m.Contract = func() (*openapi3.T, error) { return httpapi.GetSwagger() }
	m.Mount = func(r chi.Router, base string, guard func(http.Handler) http.Handler, bindError func(http.ResponseWriter, *http.Request, error)) {
		httpapi.HandlerWithOptions(httpapi.Handlers{S: svc}, httpapi.ChiServerOptions{BaseURL: base, BaseRouter: r, Middlewares: []httpapi.MiddlewareFunc{guard}, ErrorHandlerFunc: bindError})
	}
	m.Consumers = svc.Consumers()
	m.Jobs = svc.Jobs()
	m.Schedules = []modulekit.Schedule{{Key: "expansion_renewal_scan", Every: "@every 6h", Kind: app.JobRenewalScan}}
	return m, svc
}
