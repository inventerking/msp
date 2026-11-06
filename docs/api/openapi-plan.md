# OpenAPI plan

Status: Phase 1 contracts exist in `api/openapi/`: `tenant.yaml`, `admin.yaml` and `modules.yaml`. The rest of this plan describes where they are heading.

## Approach

- OpenAPI 3.0.3, with the contract as the source of truth, written before handlers (contract-first). The plan said 3.1, but oapi-codegen does not fully support it yet.
- One document per surface, so internal and public APIs can be published and reviewed separately.

| Document | Scope |
|---|---|
| `tenant.openapi.yaml` | Tenant APIs, bundled from per-module fragments |
| `admin.openapi.yaml` | `/admin/*`, never published publicly |
| `public.openapi.yaml` | `/public/*` and ingest |
| `webhooks.openapi.yaml` | Inbound webhook behaviour |
| `events.asyncapi.yaml` or JSON Schemas | Event payloads, which aren't HTTP (format decided in D-21) |

## Source layout

```text
api/openapi/
  shared/        common components
  modules/       one fragment per module
  platform/      auth, tenant, billing, catalog, and so on
  admin/
  public/
  generated/     bundled output, not edited by hand
```

## Outline of tenant document

```yaml
openapi: 3.1.0
info: { title: MSP Growth Engine Tenant API, version: 1.0.0 }
servers: [{ url: /api/v1 }]
security: [{ sessionCookie: [] }]
tags:
  - auth, me, tenant, catalog, subscriptions, billing, entitlements, usage
  - companies, privacy, files, notifications, integrations
  - lead-generation, competitor-intelligence, outreach, it-assessments, crm
  - proposals, visitor-intelligence, chatbot, customer-expansion, sales-intelligence
components:
  securitySchemes:
    sessionCookie: { type: apiKey, in: cookie, name: __Host-session }
    csrfToken: { type: apiKey, in: header, name: X-CSRF-Token }
  parameters: [Limit, Cursor, IdempotencyKey, From, To]
  headers: [X-Request-Id, RateLimit, Retry-After]
  schemas: [Problem, ValidationError, Page, Money, JobStatus, EvidenceLabel, ValueKind]
  responses: [Unauthorized, Forbidden, NotFound, Conflict, ValidationFailed, RateLimited, CreditsExhausted]
paths: { "...": "bundled from fragments" }
```

## Custom extensions

Each operation declares what the middleware must enforce, so the spec and enforcement can be checked against each other.

| Extension | Example | Purpose |
|---|---|---|
| `x-module` | `crm` | Which entitlement gate applies |
| `x-permission` | `crm.close_deal` | Required permission |
| `x-audit` | `true` | Must emit an audit event |
| `x-idempotency` | `required` | Requires the key |
| `x-rate-limit` | `search` | Rate limit class |

## Automation

| Step | Check |
|---|---|
| Lint | Spectral rules: naming, `4xx` responses, tags, and extensions present |
| Breaking change check | Diff against the last release; breaking changes fail unless approved |
| Generation | TypeScript client and types for `packages/api-client`; Go server types (tool in D-21) |
| Runtime parity | Test that every route is in the spec, and each spec route exists |
| Authorization parity | Test that every route declares `x-permission` or is explicitly public, and that the implementation denies without it |
| Docs | Reference docs generated for tenant and public APIs. Publishing external API docs is not in the initial scope |

## Public API note

External customer API keys aren't in the brief. Sessions are the only tenant API authentication at launch, so the API isn't offered to customers as a public integration surface. Adding that later would need its own authentication design.
