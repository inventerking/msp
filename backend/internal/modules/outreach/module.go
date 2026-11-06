// Package outreach is the Outreach business module. It is a skeleton until Phase 5.
package outreach

import "mspengine/internal/kernel/modulekit"

// Key is this module's entitlement key.
const Key = "outreach"

// Module describes this module to the application.
var Module = modulekit.Module{
	Key:    Key,
	Prefix: "/outreach",
	Routes: []modulekit.Route{modulekit.StatusRoute(Key)},
}
