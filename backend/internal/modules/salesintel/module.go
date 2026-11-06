// Package salesintel is the Sales Intelligence business module. It is a skeleton until Phase 6.
package salesintel

import "mspengine/internal/kernel/modulekit"

// Key is this module's entitlement key.
const Key = "sales_intelligence"

// Module describes this module to the application.
var Module = modulekit.Module{
	Key:    Key,
	Prefix: "/sales-intelligence",
	Routes: []modulekit.Route{modulekit.StatusRoute(Key)},
}
