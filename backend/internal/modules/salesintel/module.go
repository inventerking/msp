// Package salesintel is the Sales Intelligence business module: metrics, forecasts and reports from
// facts that other modules' events supply. It imports no other module and writes to none.
package salesintel

import (
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/go-chi/chi/v5"

	"mspengine/internal/kernel/modulekit"
	"mspengine/internal/modules/salesintel/app"
	"mspengine/internal/modules/salesintel/httpapi"
)

// Key is this module's entitlement key.
const Key = app.Module

// Module is the skeleton used when the module is not built with its dependencies.
var Module = modulekit.Module{
	Key:    Key,
	Prefix: "/sales-intelligence",
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
	m.Schedules = []modulekit.Schedule{{Key: "sales_intel_rollup_sweep", Every: "@every 1h", Kind: app.JobSweep}}
	return m, svc
}
