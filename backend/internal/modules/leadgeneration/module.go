// Package leadgeneration is the Lead Generation business module.
package leadgeneration

import (
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/go-chi/chi/v5"

	"mspengine/internal/kernel/modulekit"
	"mspengine/internal/modules/leadgeneration/app"
	"mspengine/internal/modules/leadgeneration/httpapi"
)

// Key is this module's entitlement key.
const Key = app.Module

// Module is the skeleton used when the module is not built with its dependencies.
var Module = modulekit.Module{
	Key:    Key,
	Prefix: "/lead-generation",
	Routes: []modulekit.Route{modulekit.StatusRoute(Key)},
}

// New builds the module with its service, routes, jobs and consumers.
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
	m.Jobs = svc.Jobs()
	m.Consumers = svc.Consumers()
	return m, svc
}
