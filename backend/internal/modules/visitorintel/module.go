// Package visitorintel is the Visitor Intelligence business module: website visits, conversions
// and business identification, with consent modes and no stored IP addresses.
package visitorintel

import (
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/go-chi/chi/v5"

	"mspengine/internal/kernel/modulekit"
	"mspengine/internal/modules/visitorintel/app"
	"mspengine/internal/modules/visitorintel/httpapi"
)

// Key is this module's entitlement key.
const Key = app.Module

// Module is the skeleton used when the module is not built with its dependencies.
var Module = modulekit.Module{
	Key:    Key,
	Prefix: "/visitor-intelligence",
	Routes: []modulekit.Route{modulekit.StatusRoute(Key)},
}

// New builds the module.
func New(d app.Deps) (modulekit.Module, *app.Service) {
	svc := app.New(d)
	if d.Shared != nil {
		d.Shared.RegisterEraser(svc)
	}
	m := Module
	m.Contract = func() (*openapi3.T, error) { return httpapi.GetSwagger() }
	m.Mount = func(r chi.Router, base string, guard func(http.Handler) http.Handler, bindError func(http.ResponseWriter, *http.Request, error)) {
		httpapi.HandlerWithOptions(httpapi.Handlers{S: svc}, httpapi.ChiServerOptions{BaseURL: base, BaseRouter: r, Middlewares: []httpapi.MiddlewareFunc{guard}, ErrorHandlerFunc: bindError})
	}
	return m, svc
}
