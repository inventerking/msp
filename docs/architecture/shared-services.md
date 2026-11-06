# Shared platform services

Status: planning, pending approval. These 26 services sit under `internal/platform` or `internal/kernel`. Business modules call them through contract interfaces.

Rules that apply to all, so they are not repeated below:

- **Failure:** a failure returns a typed error, with no partial write, as each operation is transactional.
- **Security:** RLS and tenant context apply to all tenant data, and all mutations are audited unless stated otherwise.
- **Tests:** every service has unit, repository and tenant-isolation tests. Only tests specific to the service are listed.

## 1. Authentication and Identity

- **Purpose:** prove who a user or platform admin is.
- **Responsibilities:** registration, Argon2id credentials, email verification, password reset, sessions, MFA, throttling. Two separate domains, MSP users and platform admins.
- **Data:** `identity.users`, `user_credentials`, `sessions`, `verification_tokens`, `mfa_factors`, `mfa_recovery_codes`, `login_attempts`, `platform_admin*`.
- **Interfaces:** `AuthenticateSession`, `RevokeSessions(user)`, `RequireRecentAuth`.
- **Dependencies:** Notification (Phase 1: minimal mail adapter), Audit, Redis for throttling.
- **API:** `/auth/register`, `/auth/login`, `/auth/logout`, `/auth/verify-email`, `/auth/password/forgot`, `/auth/password/reset`, `/auth/mfa/*`, `/auth/sessions`; `/admin/auth/*`.
- **Events:** `identity.user.registered`, `identity.user.email_verified`.
- **Security:** see [authentication-and-authorization](../security/authentication-and-authorization.md). Generic login and reset responses prevent account enumeration.
- **Failure:** Redis down: fall back to database counts for throttling, stricter limits. Mail down: verification is queued, and the user can resend.
- **Tests:** enumeration, token reuse, session fixation, lockout, admin and user sessions not interchangeable.

## 2. Tenant Management

- **Purpose:** workspace lifecycle and profile.
- **Responsibilities:** create tenant on registration, profile, ICPs, status changes, offboarding and purge orchestration.
- **Data:** `identity.tenants`, `tenant_profiles`, `tenant_icps`.
- **Interfaces:** `GetTenant`, `GetDefaultICP`, `RegisterPurger(module)`.
- **Dependencies:** Identity, Audit, Jobs.
- **API:** `/tenant`, `/tenant/profile`, `/tenant/icps`; `/admin/tenants`, `/admin/tenants/{id}/suspend`, `/reactivate`.
- **Events:** `identity.tenant.created`, `identity.tenant.suspended`, `identity.tenant.reactivated`, `identity.tenant.offboarding_started`.
- **Security:** only Owner can start offboarding, with recent authentication.
- **Failure:** purge is a resumable job per module; a failed module retries and alerts.
- **Tests:** purge completeness over all tenant-owned tables; suspension stops sends.

## 3. Tenant Membership

- **Purpose:** who belongs to a workspace and with which role.
- **Responsibilities:** invitations, role changes, removal, last-owner protection, active tenant switch.
- **Data:** `identity.memberships`, `roles`, `permissions`, `role_permissions`, `invitations`.
- **Interfaces:** `GetMembership`, `HasPermission`, `ListMembers`.
- **Dependencies:** Identity, Notification, Audit.
- **API:** `/tenant/members`, `/tenant/invitations`, `/tenant/roles`, `/me/active-tenant`, `/me/context`.
- **Events:** `identity.member.joined`, `identity.member.removed`, `identity.member.role_changed`.
- **Security:** an actor cannot grant a permission they lack. Removal revokes tenant access in that user's sessions immediately.
- **Failure:** last-owner removal is rejected with `409`.
- **Tests:** full permission matrix; privilege escalation attempts; invitation token misuse.

## 4. Platform Administration

- **Purpose:** the operator's control plane.
- **Responsibilities:** admin accounts and RBAC, tenant oversight, support access, settings, dead-letter handling.
- **Data:** `identity.platform_admin*`, `platform.settings`, `audit.support_access_grants`.
- **Interfaces:** only admin HTTP APIs, which call other services' admin contracts.
- **Dependencies:** nearly all platform services.
- **API:** `/admin/*`, see [api-overview](../api/api-overview.md).
- **Events:** `platform.support_access.granted`.
- **Security:** MFA mandatory; separate host, cookie and CSP; short sessions; each sensitive action needs recent authentication and a reason. Super Admin actions alert. Access to tenant content requires a time-limited grant (D-22).
- **Failure:** admin outage must not affect tenants, since it is a separate app using the same API role.
- **Tests:** each admin role against each endpoint; tenant cookies rejected on admin routes.

## 5. Module Catalog

- **Purpose:** the authoritative list of ten technical modules.
- **Responsibilities:** module metadata, availability status, and declared dependencies.
- **Data:** `platform.modules`, `module_dependencies`.
- **Interfaces:** `ListModules`, `GetDependencies`.
- **Dependencies:** none.
- **API:** `GET /catalog/modules`; `/admin/modules`.
- **Events:** `platform.module.status_changed`.
- **Security:** Super Admin writes only. Module keys are seeded by migration because code depends on them.
- **Failure:** read-heavy and cacheable; stale data is safe because entitlements are checked separately.
- **Tests:** seed matches code registry.

## 6. Product Catalog

- **Purpose:** what can be sold.
- **Responsibilities:** products, immutable versions, plans, prices, promotions, usage limits by plan.
- **Data:** `platform.products`, `product_versions`, `product_version_modules`, `plans`, `prices`, `promotions`, `promotion_redemptions`.
- **Interfaces:** `ListPublished`, `QuotePlan(plan, currency, promo)`.
- **Dependencies:** Module Catalog.
- **API:** `GET /catalog/products`, `/catalog/plans`, `POST /catalog/quote`; `/admin/products`, `/admin/plans`, `/admin/prices`, `/admin/promotions`.
- **Events:** `platform.product.published`.
- **Security:** Billing Admin or Super Admin. Published versions are immutable.
- **Failure:** a quote without a price in the requested currency returns `422 price_unavailable`.
- **Tests:** quote calculations; changing a version doesn't affect existing subscriptions.

## 7. Bundle Management

- **Purpose:** configure bundles without code.
- **Responsibilities:** a bundle is a product of `kind = bundle` or `suite` whose version lists modules. The five bundles in the brief are seed data, not rules.
- **Data:** the product tables above.
- **Interfaces:** `ExpandModules(productVersion)`.
- **Dependencies:** Product Catalog, Module Catalog.
- **API:** `/admin/products?kind=bundle`, `/admin/products/{id}/versions`.
- **Events:** `platform.product.published`.
- **Security:** publishing warns about missing dependencies, such as the Growth bundle (D-02).
- **Failure:** publishing a version with a retired module is rejected.
- **Tests:** bundles grant exactly their modules; overlapping purchases work.

## 8. Subscription Management

- **Purpose:** the commercial agreement.
- **Responsibilities:** purchase requests, subscription lifecycle, trial, renewal, cancellation.
- **Data:** `platform.purchase_requests`, `subscriptions`.
- **Interfaces:** `SubmitPurchase`, `GetSubscriptions`.
- **Dependencies:** Product Catalog, Billing, Entitlements, Notification.
- **API:** `/subscriptions`, `/purchase-requests`; `/admin/purchase-requests`, `/admin/subscriptions`.
- **Events:** `platform.purchase.submitted|approved|rejected`, `platform.subscription.changed`.
- **Security:** Owner or MSP Admin with `billing.manage`.
- **Failure:** retried submissions are safe through `idempotency_key`.
- **Tests:** full state machine; no entitlement before admin activation.

## 9. Billing and Payment Management

- **Purpose:** invoices and payment state.
- **Responsibilities:** invoice records, payment records, provider webhooks, reconciliation, manual verification by a Billing Admin.
- **Data:** `platform.invoices`, `invoice_lines`, `payments`.
- **Interfaces:** `CreateCheckout`, `RecordPaymentEvent`, `ReconcilePending`.
- **Dependencies:** Integration, Files, PDF.
- **API:** `/billing/invoices`, `/billing/checkout`; `/admin/payments`, `/admin/invoices`; `/webhooks/payment/*`.
- **Events:** `platform.payment.verified|failed|refunded`, `platform.invoice.issued`.
- **Security:** no raw card data enters the system (hosted provider pages only, which also reduces PCI scope). Webhook signatures are required.
- **Failure:** a daily reconciliation job corrects missed webhooks.
- **Tests:** webhook replay and forgery; rounding; refunds.
- **Open:** D-01, D-04.

## 10. Module Entitlement Management

- **Purpose:** decide whether a tenant may use a module, right now.
- **Responsibilities:** grants, projection into effective status, activate, suspend, expire, revoke, history.
- **Data:** `platform.entitlement_grants`, `tenant_module_entitlements`, `entitlement_status_history`.
- **Interfaces:** `IsEntitled(tenant, module)`, `RequireEntitled`, `ListForTenant`.
- **Dependencies:** Subscription, Audit, Redis.
- **API:** `GET /entitlements`; `/admin/tenants/{id}/entitlements/{module}/activate|suspend|expire|revoke`.
- **Events:** `platform.entitlement.activated|suspended|expired|revoked`.
- **Security:** enforced in middleware and in job start. Admin-only changes need a reason.
- **Failure:** fail closed. Cache miss reads the database; if both are unavailable, the request is denied. The cache has a short TTL and is invalidated on change.
- **Tests:** a suspended tenant is blocked on every module route, in UI-independent API tests. Overlapping grants. Cache invalidation.

## 11. Company Identity

- **Purpose:** one canonical record per real company.
- **Responsibilities:** normalize domains and identifiers, match, create, merge, store shareable facts.
- **Data:** `shared.companies`, `company_identifiers`, `company_source_facts`, `company_merges`.
- **Interfaces:** `ResolveByDomain`, `ResolveOrCreate(signals)`, `Get`, `Search`, `Merge`.
- **Dependencies:** Integration.
- **API:** `GET /companies/search`; `/admin/companies/merge`.
- **Events:** `shared.company.merged`, so modules can repoint relationships.
- **Security:** see ADR-010. The write API accepts only normalized, source-attributed input, never free tenant text. Search has rate limits to discourage extracting the database.
- **Failure:** concurrent creation is handled by unique identifiers followed by a re-read. Merges are reversible only through a corrective merge, so uncertain matches are not auto-merged.
- **Tests:** internationalized domains, subdomains, free-mail domains are rejected as company domains, merge consumer repointing.

## 12. Contact Identity

- **Purpose:** one tenant-scoped contact record shared by modules in the tenant.
- **Responsibilities:** dedupe by normalized email, sources, consent records, erasure.
- **Data:** `shared.contacts`, `contact_sources`, `contact_consents`.
- **Interfaces:** `ResolveOrCreate`, `Get`, `RecordConsent`, `Erase`.
- **Dependencies:** none outside the kernel.
- **API:** `POST /privacy/contacts/{id}/erase`, `GET /privacy/contacts/{id}/export`.
- **Events:** `shared.contact.erased`, `shared.contact.consent_changed`.
- **Security:** PII is excluded from logs and event payloads. See [privacy-and-compliance](../security/privacy-and-compliance.md).
- **Failure:** erasure runs as a tracked job through module contracts and is retried until complete.
- **Tests:** erasure reaches all modules, while keeping the suppression hash.

## 13. AI Gateway

See [ai-gateway](ai-gateway.md).

## 14. Integration Management

- **Purpose:** register providers and hold connections and secrets.
- **Responsibilities:** provider registry, OAuth, encrypted credentials, health, webhook inbox.
- **Data:** `integration.providers`, `platform_credentials`, `connections`, `webhook_endpoints`, `webhook_deliveries`, `provider_call_logs`.
- **Interfaces:** `GetAdapter`, `GetConnection`, `AuthorizeConnection`.
- **Dependencies:** Secret store, Jobs.
- **API:** `/integrations`, `/integrations/connections`, `/integrations/oauth/*`; `/admin/integrations/providers`; `/webhooks/*`.
- **Events:** `integration.connection.connected|needs_reauth|disconnected`, `integration.provider.degraded|recovered`.
- **Security:** see [integration-architecture](integration-architecture.md).
- **Failure:** circuit breaker per provider.
- **Tests:** signature verification, secret redaction, OAuth `state` tampering.

## 15. Email Provider Management

- **Purpose:** provider-neutral email.
- **Responsibilities:** adapters for send and event parsing, domain authentication checks. Two separate streams: transactional (platform) and outreach (tenant), so outreach reputation problems can't stop password resets.
- **Data:** connections in `integration`; tenant sending state in `outreach`.
- **Interfaces:** `Send`, `CheckDomain`, `ParseEvent`, `ListReplies`.
- **Dependencies:** Integration.
- **API:** via Outreach and `/admin/integrations`.
- **Events:** normalized provider events, delivered to Outreach by command.
- **Security:** header injection is blocked, DKIM required for sending.
- **Failure:** retry without duplicate sends, using the message's idempotency key where the provider supports it.
- **Tests:** adapter contract suite run against each adapter.

## 16. Calendar Provider Management

- **Purpose:** provider-neutral calendars.
- **Responsibilities:** free/busy reads, event creation and cancellation, token refresh.
- **Data:** connections in `integration`.
- **Interfaces:** `GetAvailability`, `CreateEvent`, `CancelEvent`.
- **Dependencies:** Integration.
- **API:** via Chatbot and CRM.
- **Events:** `integration.calendar_event.changed`.
- **Security:** least-privilege scopes; returned data is only free/busy where possible.
- **Failure:** no availability data means no confirmed booking.
- **Tests:** double-booking race, time zones and daylight-saving changes.

## 17. Company Data Provider Management

- **Purpose:** provider-neutral company and contact data.
- **Responsibilities:** search, enrichment, normalization, licence labelling, quota management.
- **Data:** results flow to Company Identity or tenant schemas, according to licence class.
- **Interfaces:** `SearchCompanies`, `EnrichCompany`, `FindContacts`, `ResolveBusinessFromIP`.
- **Dependencies:** Integration, Usage.
- **API:** `/admin/integrations/providers`.
- **Events:** none.
- **Security:** licence class is required on all returned data. IP lookups pass only the IP needed, and nothing is retained.
- **Failure:** no results and provider failure stay distinct, so `no_data` is not misread as a negative signal.
- **Tests:** licence routing; provider results don't reach global tables if tenant-only.

## 18. File Storage

- **Purpose:** private object storage with policy.
- **Responsibilities:** upload sessions, scanning, signed links, retention, purge.
- **Data:** `files.objects`, `object_references`, `access_policies`, `access_grants`.
- **Interfaces:** `CreateUpload`, `ConfirmUpload`, `CreateDownloadGrant`, `AttachReference`.
- **Dependencies:** S3-compatible storage, Jobs.
- **API:** `POST /files/uploads`, `POST /files/uploads/{id}/complete`, `GET /files/{id}/download`.
- **Events:** `files.object.ready`, `files.object.quarantined`.
- **Security:** private buckets, tenant-prefixed keys, type sniffing rather than client claims, size limits, malware scanning (engine is D-21). Files are served with `Content-Disposition: attachment` from a separate host, except approved images.
- **Failure:** a file not `clean` cannot be downloaded or sent for processing.
- **Tests:** path traversal, polyglot files, expired grants, cross-tenant object access.

## 19. PDF Generation

- **Purpose:** branded reports and proposals.
- **Responsibilities:** render versioned templates to PDF in a sandbox and store the output.
- **Data:** output through File Storage.
- **Interfaces:** `Render(template, data)` as a job.
- **Dependencies:** Jobs, Files.
- **API:** none; owned by each module.
- **Events:** the requesting module publishes its own "ready" event.
- **Security:** a headless renderer, if used, is an SSRF risk. It runs with no network access, using inlined assets and sanitized HTML. Renderer choice is D-21.
- **Failure:** retried; the requested document state shows failure.
- **Tests:** template injection; deterministic output of commercial values.

## 20. Background Jobs

See [background-jobs](background-jobs.md).

## 21. Event Infrastructure

See [event-architecture](event-architecture.md).

## 22. Transactional Outbox

See [event-architecture](event-architecture.md). **Data:** `jobs.outbox_events`, `event_deliveries`. **Interface:** `Append(tx, event)`, which requires an open transaction. **Tests:** rollback writes no event; crash between commit and dispatch still delivers.

## 23. Notification Service

- **Purpose:** tell users and admins things.
- **Responsibilities:** templates, preferences, in-app and email delivery, retries.
- **Data:** `notification.*`.
- **Interfaces:** `Notify(recipient, key, data, idempotencyKey)`.
- **Dependencies:** Email, Jobs, SSE.
- **API:** `/notifications`, `/notifications/{id}/read`, `/notifications/preferences`.
- **Events:** consumes events from many modules through a mapping table.
- **Security:** security notices can't be disabled. Templates escape all values.
- **Failure:** in-app storage succeeds even when email fails, and email failures are retried.
- **Tests:** dedupe, opt-outs, template injection.

## 24. Audit Logging

- **Purpose:** an accountable, tamper-evident record.
- **Responsibilities:** write within the same transaction as the action, hash chain, redaction, search and export.
- **Data:** `audit.events`, `security_events`, `support_access_grants`.
- **Interfaces:** `Record(tx, action)`, `RecordSecurity`.
- **Dependencies:** none.
- **API:** `GET /tenant/audit-events` (own tenant only), `GET /admin/audit-events`.
- **Events:** none, to avoid loops.
- **Security:** the runtime role has insert and select only. Sensitive fields are redacted before storage. Periodic hash anchoring to separate storage is proposed.
- **Failure:** if audit writes fail, the action fails, because they share one transaction. Security events from unauthenticated traffic are sampled under flood, to prevent exhaustion attacks.
- **Tests:** coverage test: each mutating endpoint generates an audit event; update and delete are denied.

## 25. Usage and Credit Accounting

- **Purpose:** measure and limit consumption.
- **Responsibilities:** meters, reservations, commits, releases, limits, rollups.
- **Data:** `platform.usage_meters`, `credit_accounts`, `credit_ledger`, `tenant_usage_limits`, `usage_rollups`.
- **Interfaces:** `Reserve`, `Commit`, `Release`, `CheckLimit`, `Grant`.
- **Dependencies:** Audit.
- **API:** `GET /usage`; `/admin/tenants/{id}/credits`, `/admin/usage`.
- **Events:** `platform.credits.low`, `platform.credits.exhausted`.
- **Security:** the ledger is append-only and every entry has an idempotency key. Only admins can adjust balances, with a reason.
- **Failure:** a crashed job leaves a reservation, and a sweeper releases expired ones. Limit check failure blocks the action and is not treated as approval.
- **Tests:** concurrent reservations can't overspend; ledger equals balance.

## 26. Analytics Infrastructure

- **Purpose:** common plumbing for read-oriented metrics.
- **Responsibilities:** consumer framework for facts, rollup jobs, metric definitions, FX conversion, value-kind labelling.
- **Data:** module rollups, `sales_intelligence.*`, `platform.usage_rollups`, `shared.fx_rates`.
- **Interfaces:** `DefineMetric`, `RecordRollup`, `ConvertForReporting`.
- **Dependencies:** Events, Jobs.
- **API:** via Sales Intelligence; `/admin/analytics/*` for platform metrics.
- **Events:** none.
- **Security:** aggregates are tenant-private. Platform-wide analytics are built from operational usage data only, and never expose one tenant's business data to another (D-22).
- **Failure:** rollups are rebuildable and run in the analytics pool, so they can lag without affecting CRM.
- **Tests:** metric correctness against fixtures; `value_kind` is never mixed in one total.
