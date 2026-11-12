// Package customerexpansion is the Customer Expansion business module. Its screens arrive in
// Phase 6; from Phase 4 it records contracts from won deals (D-03).
package customerexpansion

import (
	"mspengine/internal/kernel/db"
	"mspengine/internal/kernel/modulekit"
	crm "mspengine/internal/modules/crm/contract"
	"mspengine/internal/modules/customerexpansion/app"
)

// Key is this module's entitlement key.
const Key = app.Module

// Module describes this module to the application.
var Module = modulekit.Module{
	Key:    Key,
	Prefix: "/customer-expansion",
	Routes: []modulekit.Route{modulekit.StatusRoute(Key)},
}

// New builds the module with its contract recorder.
func New(pools *db.Pools, registry crm.Registry) (modulekit.Module, *app.Service) {
	svc := app.New(pools, registry)
	m := Module
	m.Consumers = svc.Consumers()
	return m, svc
}
