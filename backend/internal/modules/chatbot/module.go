// Package chatbot is the Chatbot business module. It is a skeleton until Phase 5.
package chatbot

import "mspengine/internal/kernel/modulekit"

// Key is this module's entitlement key.
const Key = "chatbot"

// Module describes this module to the application.
var Module = modulekit.Module{
	Key:    Key,
	Prefix: "/chatbot",
	Routes: []modulekit.Route{modulekit.StatusRoute(Key)},
}
