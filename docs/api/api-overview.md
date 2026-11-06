# API overview

Status: planning, pending approval.

## Hostnames and prefix

| Host (placeholder) | Serves | Session |
|---|---|---|
| `api.<domain>/api/v1/...` | Tenant and public APIs, webhooks | Tenant cookie |
| `api.<domain>/api/v1/admin/...` | Platform admin APIs | Admin cookie, which is scoped so tenant cookies can't apply |
| `ingest.<domain>/v1/...` | Tracker ingest | Site key |

The tenant cookie is issued by the API host, and browsers call it from the app host under a strict CORS allowlist. An alternative that also avoids CORS is same-origin routing through Traefik (`app.<domain>/api/...`), which is simpler and recommended unless deployment rules it out. The admin app would do the same on its own host, keeping cookies fully separate. D-23 confirms this.

## Endpoint groups

| Group | Prefix | Authentication | Details |
|---|---|---|---|
| Authentication | `/auth` | Public, then session | [shared-services](../architecture/shared-services.md) §1 |
| Current user | `/me` | Session | Below |
| Tenant, members | `/tenant` | Session | §2, §3 |
| Catalog | `/catalog` | Session | §5–7 |
| Subscriptions, purchases | `/subscriptions`, `/purchase-requests` | `billing.manage` | §8 |
| Billing | `/billing` | `billing.manage` | §9 |
| Entitlements | `/entitlements` | Session | §10 |
| Usage | `/usage` | `billing.read` | §25 |
| Companies | `/companies` | Any active module that needs them | §11 |
| Privacy | `/privacy` | `privacy.manage` | §12 |
| Files | `/files` | Session | §18 |
| Notifications | `/notifications` | Session | §23 |
| Integrations | `/integrations` | `integrations.manage` | §14 |
| Real-time | `/events/stream` | Session | SSE |
| Lead Generation | `/lead-generation` | Entitlement, permission | [module doc](../modules/lead-generation.md) |
| Competitor Intelligence | `/competitor-intelligence` | Entitlement, permission | [module doc](../modules/competitor-intelligence.md) |
| Outreach | `/outreach` | Entitlement, permission | [module doc](../modules/outreach.md) |
| IT Assessment | `/it-assessments` | Entitlement, permission | [module doc](../modules/it-assessment.md) |
| CRM | `/crm` | Entitlement, permission | [module doc](../modules/crm.md) |
| Proposals | `/proposals` | Entitlement, permission | [module doc](../modules/proposals.md) |
| Visitor Intelligence | `/visitor-intelligence` | Entitlement, permission | [module doc](../modules/visitor-intelligence.md) |
| Chatbot | `/chatbot` | Entitlement, permission | [module doc](../modules/chatbot.md) |
| Customer Expansion | `/customer-expansion` | Entitlement, permission | [module doc](../modules/customer-expansion.md) |
| Sales Intelligence | `/sales-intelligence` | Entitlement, permission | [module doc](../modules/sales-intelligence.md) |
| AI | `/ai/usage` (read only) | `billing.read` | [ai-gateway](../architecture/ai-gateway.md). There's deliberately no general "ask AI" endpoint: AI is only reached through module use cases |
| Public | `/public/*` | Scoped credential | Module docs |
| Webhooks | `/webhooks/{providerKind}/{pathToken}` | Signature | [integration-architecture](../architecture/integration-architecture.md) |
| Platform admin | `/admin/*` | Admin session, MFA, role | Below |

## Platform admin endpoints

| Prefix | Purpose | Roles |
|---|---|---|
| `/admin/auth` | Admin sign-in, MFA | All |
| `/admin/dashboard` | Platform summary | All |
| `/admin/tenants`, `/admin/tenants/{id}` | Tenant oversight, status | Super Admin, Support (status: Super Admin) |
| `/admin/tenants/{id}/users` | Tenant user support actions | Super Admin, Support |
| `/admin/admins` | Platform admin management | Super Admin |
| `/admin/modules`, `/admin/products`, `/admin/plans`, `/admin/prices`, `/admin/promotions` | Catalog | Billing Admin, Super Admin |
| `/admin/purchase-requests` | Review | Billing Admin, Super Admin |
| `/admin/subscriptions`, `/admin/invoices`, `/admin/payments` | Commercial | Billing Admin, Super Admin; Support and Analyst read |
| `/admin/tenants/{id}/entitlements/{module}/...` | Activate, suspend, expire, revoke | Billing Admin, Super Admin; revoke: Super Admin |
| `/admin/usage`, `/admin/tenants/{id}/credits` | Usage and credits | Billing Admin, Super Admin |
| `/admin/integrations/providers`, `/admin/ai/routes`, `/admin/ai/prompts` | Provider configuration | Integration Admin, Super Admin |
| `/admin/outreach/jurisdiction-rules`, `/admin/outreach/abuse` | Sending policy | Super Admin |
| `/admin/jobs`, `/admin/dead-letters` | Operations | Super Admin, Support |
| `/admin/audit-events`, `/admin/security-events` | Audit | Super Admin; Support limited |
| `/admin/support-access` | Time-limited tenant access | Super Admin approves |
| `/admin/settings` | Platform settings | Super Admin |

## Common conventions

**Format.** JSON. Field names `snake_case`. Timestamps in ISO 8601 UTC. Money as `{ *_minor, currency }`. IDs as strings.

**Lists.** `GET` with `?limit=` (default 25, maximum 100) and `?cursor=`. The response has `{ data: [...], next_cursor }`. Sort and filter fields are allowlisted per endpoint. Exact totals are optional, because counting can be costly.

**Idempotency.** `Idempotency-Key` is required on `POST` calls that spend credits, contact outsiders, or move money. The server stores tenant, key, route and request hash for 24 hours (proposed).

| Case | Result |
|---|---|
| New key | Runs normally |
| Same key, same body, finished | Returns the original result |
| Same key, same body, in progress | `409 request_in_progress` |
| Same key, different body | `422 idempotency_key_reused` |

**Concurrency.** Resources with `row_version` require it on update, and a mismatch returns `409 version_conflict`.

**Asynchronous operations.** Return `202` with a job resource: `{ id, status, progress_percent, links: { self } }`.

**Security headers and limits.** Rate limits by user, tenant and IP, and returned as `RateLimit-*` headers. Request bodies have size limits. See [security-architecture](../security/security-architecture.md).

**Versioning.** `/api/v1`. Adding fields is non-breaking. Breaking changes need `/api/v2` for that resource group, with a deprecation period and `Deprecation` and `Sunset` headers.

## Real-time

`GET /api/v1/events/stream` is SSE, with the same session and tenant context. Typed messages are `job.progress`, `notification.created`, `tenant.entitlements_changed`, `crm.board_changed`. Payloads hold IDs and status. Clients refetch via REST. Live chat uses WebSocket, as in the [chatbot](../modules/chatbot.md) doc.
