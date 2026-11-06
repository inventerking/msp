# Dependency matrix

Status: planning, pending approval. The definitive contract list is in [cross-module-contracts](../modules/cross-module-contracts.md).

## Module to module

**S** is a synchronous contract call (required direction for layering), **s** a synchronous optional or degradable call, **E** an event consumed by the row's module from the column's module. Sales Intelligence's backfill calls aren't shown, because they aren't used in routine operation.

| Row depends on column → | LG | CI | OR | IT | CRM | PR | VI | CB | CE | SI |
|---|---|---|---|---|---|---|---|---|---|---|
| **Lead Generation (LG)** | · | s | | | S, E | | | | | |
| **Competitor Intelligence (CI)** | | · | | | | | | | | |
| **Outreach (OR)** | s | | · | | E | | | | | |
| **IT Assessment (IT)** | | | s | · | S | | | | | |
| **CRM** | E | | E | E | · | E | E | E | E | |
| **Proposals (PR)** | | | | | S, E | · | | | | |
| **Visitor Intelligence (VI)** | | | | | s | | · | E | | |
| **Chatbot (CB)** | | | | | S | | s, E | · | | |
| **Customer Expansion (CE)** | | | | E | S, E | s, E | | | · | |
| **Sales Intelligence (SI)** | E | | E | E | E | E | E | E | E | · |

**How to read it.** Moving down the columns shows who needs a module. CRM's column is the busiest, since it's the registry. CRM's row has only events, so it can be built and run without any other module.

Removing any `S` from the diagram leaves an acyclic graph. The layers are in [cross-module-contracts](../modules/cross-module-contracts.md).

## Module to shared service

**R** required, **O** optional.

| Service | LG | CI | OR | IT | CRM | PR | VI | CB | CE | SI |
|---|---|---|---|---|---|---|---|---|---|---|
| Identity, tenancy, entitlements, audit | R | R | R | R | R | R | R | R | R | R |
| Jobs, events, outbox | R | R | R | R | R | R | R | R | R | R |
| Company Identity | R | R | | O | R | | O | | O | |
| Contact Identity | R | | R | R | R | | O | R | | |
| AI Gateway | O | R | O | O | O | O | | R | O | O |
| Company Data Provider | R | O | | | | | O | | | |
| Email Provider | | | R | O | | R | | | | |
| Calendar Provider | | | | O | O | | | R | | |
| Files | R | R | | R | R | R | | R | O | R |
| PDF | | O | | R | | R | | | | O |
| Notification | R | R | R | R | R | R | O | R | R | O |
| Usage and credits | R | R | R | R | | O | O | R | O | |
| Egress Guard | | R | | R | | | | O | | |
| Analytics infrastructure | | | O | | | | R | O | | R |

## Modules to phases

| Phase | Requires from earlier phases |
|---|---|
| 1 Foundation | None |
| 2 Shared services | Tenancy, audit, entitlements |
| 3 Prospecting | Identity services, jobs, AI, integration, files, usage, Egress Guard |
| 4 Sales | Phase 2; Lead Generation for conversion |
| 5 Marketing | Phase 2; CRM for activity sync; Lead Generation for audience |
| 6 Growth | CRM, Proposals, and all event producers |
| 7 Production | All |

## External dependencies

| Capability | First needed | Impact if unavailable |
|---|---|---|
| Company data | Phase 3 | Lead Generation has no discovery. CSV still works |
| AI | Phase 2 | AI features off; deterministic parts still work; Chatbot and competitor analysis are limited |
| Malware scanning | Phase 2 | Uploads blocked |
| E-signature | Phase 4 | Proposal signing manual |
| Email sending | Phase 1 (transactional), Phase 5 (outreach) | Auth blocked; Outreach blocked |
| Calendar | Phase 4–5 | No automatic booking |
| Payment | Phase 7 | Manual verification only |
| Business identification | Phase 5 | Analytics only, no business matches |
| FX | Phase 6 | Single-currency reporting only |
