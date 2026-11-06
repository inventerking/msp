// Package leadgeneration is the Lead Generation business module. It is a skeleton until Phase 3.
package leadgeneration

import "mspengine/internal/kernel/modulekit"

// Key is this module's entitlement key.
const Key = "lead_generation"

// Module describes this module to the application.
var Module = modulekit.Module{
	Key:    Key,
	Prefix: "/lead-generation",
	Routes: []modulekit.Route{modulekit.StatusRoute(Key)},
}
