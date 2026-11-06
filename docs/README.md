# MSP Growth Engine: planning documentation

**Status: Phase 1 is built and awaiting its approval gate. Later phases are not authorized.** See [phase-1-status](implementation/phase-1-status.md) for what exists, how it differs from the plan, and what is left.

Documents still marked "pending approval" follow the approved decisions in [open-decisions](implementation/open-decisions.md), which also lists the follow-up work those decisions still need.

## Reading order

| If you want to... | Read |
|---|---|
| Understand the whole system | [system-overview](architecture/system-overview.md), then [architecture-decisions](architecture/architecture-decisions.md) |
| Decide what is still open | [open-decisions](implementation/open-decisions.md) |
| Understand a module | [module-overview](modules/module-overview.md), then the module's own document |
| Follow a business process | [workflows](modules/workflows.md) |
| See how the data fits together | [database-architecture](database/database-architecture.md), then [erd-overview](database/erd-overview.md) |
| Plan the build | [implementation-roadmap](implementation/implementation-roadmap.md) |

## Index

**Architecture:** [system-overview](architecture/system-overview.md) · [architecture-decisions](architecture/architecture-decisions.md) · [backend-architecture](architecture/backend-architecture.md) · [frontend-architecture](architecture/frontend-architecture.md) · [multi-tenancy](architecture/multi-tenancy.md) · [shared-services](architecture/shared-services.md) · [commercial-and-activation](architecture/commercial-and-activation.md) · [ai-gateway](architecture/ai-gateway.md) · [background-jobs](architecture/background-jobs.md) · [event-architecture](architecture/event-architecture.md) · [integration-architecture](architecture/integration-architecture.md) · [observability-and-reliability](architecture/observability-and-reliability.md)

**Modules:** [module-overview](modules/module-overview.md) · [lead-generation](modules/lead-generation.md) · [competitor-intelligence](modules/competitor-intelligence.md) · [outreach](modules/outreach.md) · [it-assessment](modules/it-assessment.md) · [crm](modules/crm.md) · [proposals](modules/proposals.md) · [visitor-intelligence](modules/visitor-intelligence.md) · [chatbot](modules/chatbot.md) · [customer-expansion](modules/customer-expansion.md) · [sales-intelligence](modules/sales-intelligence.md) · [cross-module-contracts](modules/cross-module-contracts.md) · [workflows](modules/workflows.md)

**Database:** [database-architecture](database/database-architecture.md) · [erd-overview](database/erd-overview.md) · [identity-erd](database/identity-erd.md) · [commercial-erd](database/commercial-erd.md) · [shared-identity-erd](database/shared-identity-erd.md) · [module-er-ds](database/module-er-ds.md) · [data-dictionary](database/data-dictionary.md) · [constraints-and-indexes](database/constraints-and-indexes.md)

**API:** [api-overview](api/api-overview.md) · [api-contracts](api/api-contracts.md) · [event-contracts](api/event-contracts.md) · [error-conventions](api/error-conventions.md) · [openapi-plan](api/openapi-plan.md)

**Frontend:** [application-structure](frontend/application-structure.md) · [screen-inventory](frontend/screen-inventory.md) · [navigation](frontend/navigation.md) · [frontend-contracts](frontend/frontend-contracts.md)

**Security:** [security-architecture](security/security-architecture.md) · [authentication-and-authorization](security/authentication-and-authorization.md) · [tenant-isolation](security/tenant-isolation.md) · [privacy-and-compliance](security/privacy-and-compliance.md) · [threat-model](security/threat-model.md)

**Implementation:** [open-decisions](implementation/open-decisions.md) · [phase-1-status](implementation/phase-1-status.md) · [tooling-decisions](implementation/tooling-decisions.md) · [implementation-roadmap](implementation/implementation-roadmap.md) · [dependency-matrix](implementation/dependency-matrix.md) · [testing-strategy](implementation/testing-strategy.md) · [acceptance-criteria](implementation/acceptance-criteria.md) · [risk-register](implementation/risk-register.md)

## Conventions

- **`D-nn`** is an open decision and **`ADR-nnn`** is a recorded architecture decision.
- Words like *proposed* mark a starting value that has not been approved, such as a retention period.
- Nothing here is legal advice. Compliance statements describe design intent and need review by qualified counsel.
- Provider names are deliberately absent wherever a provider has not been chosen. Availability and pricing have not been assumed.
