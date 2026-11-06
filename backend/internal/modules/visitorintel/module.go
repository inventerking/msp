// Package visitorintel is the Visitor Intelligence business module. It is a skeleton until Phase 5.
package visitorintel

import "mspengine/internal/kernel/modulekit"

// Key is this module's entitlement key.
const Key = "visitor_intelligence"

// Module describes this module to the application.
var Module = modulekit.Module{
	Key:    Key,
	Prefix: "/visitor-intelligence",
	Routes: []modulekit.Route{modulekit.StatusRoute(Key)},
}
