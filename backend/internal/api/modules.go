package api

import (
	"mspengine/internal/kernel/modulekit"
	"mspengine/internal/modules/chatbot"
	"mspengine/internal/modules/competitorintel"
	"mspengine/internal/modules/crm"
	"mspengine/internal/modules/customerexpansion"
	"mspengine/internal/modules/itassessment"
	"mspengine/internal/modules/leadgeneration"
	"mspengine/internal/modules/outreach"
	"mspengine/internal/modules/proposals"
	"mspengine/internal/modules/salesintel"
	"mspengine/internal/modules/visitorintel"
)

// allModules lists the ten business modules.
var allModules = []modulekit.Module{
	leadgeneration.Module,
	competitorintel.Module,
	outreach.Module,
	itassessment.Module,
	crm.Module,
	proposals.Module,
	visitorintel.Module,
	chatbot.Module,
	customerexpansion.Module,
	salesintel.Module,
}
