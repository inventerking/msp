// Package itassessment is the IT Assessment business module. It is a skeleton until Phase 4.
package itassessment

import "mspengine/internal/kernel/modulekit"

// Key is this module's entitlement key.
const Key = "it_assessment"

// Module describes this module to the application.
var Module = modulekit.Module{
	Key:    Key,
	Prefix: "/it-assessments",
	Routes: []modulekit.Route{modulekit.StatusRoute(Key)},
}
