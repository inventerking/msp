// Package crm is the CRM business module. It is a skeleton until Phase 4.
package crm

import "mspengine/internal/kernel/modulekit"

// Key is this module's entitlement key.
const Key = "crm"

// Module describes this module to the application.
var Module = modulekit.Module{
	Key:    Key,
	Prefix: "/crm",
	Routes: []modulekit.Route{modulekit.StatusRoute(Key)},
}
