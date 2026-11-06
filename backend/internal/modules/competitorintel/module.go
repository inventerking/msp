// Package competitorintel is the Competitor Intelligence business module. It is a skeleton until Phase 3.
package competitorintel

import "mspengine/internal/kernel/modulekit"

// Key is this module's entitlement key.
const Key = "competitor_intelligence"

// Module describes this module to the application.
var Module = modulekit.Module{
	Key:    Key,
	Prefix: "/competitor-intelligence",
	Routes: []modulekit.Route{modulekit.StatusRoute(Key)},
}
