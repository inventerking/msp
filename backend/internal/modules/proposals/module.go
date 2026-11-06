// Package proposals is the Proposals business module. It is a skeleton until Phase 4.
package proposals

import "mspengine/internal/kernel/modulekit"

// Key is this module's entitlement key.
const Key = "proposals"

// Module describes this module to the application.
var Module = modulekit.Module{
	Key:    Key,
	Prefix: "/proposals",
	Routes: []modulekit.Route{modulekit.StatusRoute(Key)},
}
