// Package customerexpansion is the Customer Expansion business module. It is a skeleton until Phase 6.
package customerexpansion

import "mspengine/internal/kernel/modulekit"

// Key is this module's entitlement key.
const Key = "customer_expansion"

// Module describes this module to the application.
var Module = modulekit.Module{
	Key:    Key,
	Prefix: "/customer-expansion",
	Routes: []modulekit.Route{modulekit.StatusRoute(Key)},
}
