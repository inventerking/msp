// Package chatbot is the Chatbot business module: website chat answered from the tenant's own
// knowledge, with lead capture, consent and handoff to a person.
package chatbot

import (
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/go-chi/chi/v5"

	"mspengine/internal/kernel/modulekit"
	"mspengine/internal/modules/chatbot/app"
	"mspengine/internal/modules/chatbot/httpapi"
)

// Key is this module's entitlement key.
const Key = app.Module

// Module is the skeleton used when the module is not built with its dependencies.
var Module = modulekit.Module{
	Key:    Key,
	Prefix: "/chatbot",
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
