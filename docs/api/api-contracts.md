# API contracts for important endpoints

Status: planning, pending approval. Complete endpoint lists are in each module document. This file provides full contracts for the highest-risk or most-used endpoints. Standard errors, in addition to those listed, are in [error-conventions](error-conventions.md). Examples are illustrative.

## POST `/api/v1/auth/login`
**Purpose:** sign in. **Authentication:** public. **Authorization:** none.

```json
{ "email": "owner@example-msp.com", "password": "••••••••" }
```
**Response `200`:** sets the session cookie.
```json
{ "user": { "id": "0194...", "display_name": "Sam Lee" }, "mfa_required": false, "memberships": [{ "tenant_id": "0194...", "tenant_name": "Example MSP", "role": "owner" }] }
```
**Validation:** email format, password length limit before hashing.
**Errors:** `401 invalid_credentials` for a wrong password or unknown account, with the same response and comparable timing. `429 rate_limited`. `403 email_not_verified`.
**Idempotency:** not applicable. A new session gets a new token, which prevents session fixation.

## GET `/api/v1/me/context`
**Purpose:** data for the app shell. **Authentication:** session.
**Response `200`:**
```json
{
  "user": { "id": "0194...", "display_name": "Sam Lee", "mfa_enabled": false },
  "tenant": { "id": "0194...", "name": "Example MSP", "status": "active" },
  "role": "owner",
  "permissions": ["crm.read", "crm.write", "billing.manage"],
  "modules": [
    { "key": "crm", "status": "active", "effective_until": "2027-01-01T00:00:00Z" },
    { "key": "outreach", "status": "inactive" }
  ],
  "csrf_token": "..."
}
```
This is for display only. The backend enforces every entitlement and permission again per request.

## POST `/api/v1/purchase-requests`
**Purpose:** request a module, bundle or suite. **Authorization:** `billing.manage`. **Idempotency:** required.
```json
{ "plan_id": "0194...", "currency": "USD", "promotion_code": "WELCOME10", "quantity": 1 }
```
**Response `201`:**
```json
{ "id": "0194...", "status": "payment_pending", "quote": { "product": "Sales bundle", "modules": ["crm","it_assessment","proposals"], "amount_minor": 0, "currency": "USD", "billing_period": "monthly" }, "payment": { "checkout_url": "https://..." } }
```
`amount_minor` here is a placeholder, since prices are admin-configured.
**Validation:** plan is active; a price exists in that currency; promotion valid.
**Errors:** `422 price_unavailable`, `422 promotion_invalid`, `409 already_entitled` for a fully covered purchase. A partly overlapping purchase returns a warning, and doesn't block.

## POST `/api/v1/admin/tenants/{tenantId}/entitlements/{moduleKey}/activate`
**Purpose:** activate a module. **Authentication:** admin session with MFA. **Authorization:** Billing Admin or Super Admin. **Idempotency:** naturally idempotent; a repeat call returns the current state.
```json
{ "purchase_request_id": "0194...", "reason": "Payment verified", "effective_until": null }
```
**Response `200`:**
```json
{ "tenant_id": "0194...", "module": "crm", "status": "active", "entitlement_version": 12, "grant_id": "0194..." }
```
**Validation:** reason required; module valid; tenant not deleted; an override without a purchase needs Super Admin.
**Errors:** `403 insufficient_role`, `404 not_found`, `409 invalid_transition`.
**Audit:** actor, reason, before and after. **Effect:** cache invalidation, `platform.entitlement.activated`, SSE message.

## POST `/api/v1/lead-generation/searches`
**Purpose:** start a company search. **Authorization:** entitlement and `lead.search`. **Idempotency:** required.
```json
{ "criteria": { "countries": ["GB"], "industries": ["legal_services"], "employee_min": 20, "employee_max": 200, "technologies": [] }, "max_results": 200 }
```
**Response `202`:**
```json
{ "id": "0194...", "status": "queued", "progress_percent": 0, "credits_reserved": 200, "links": { "self": "/api/v1/lead-generation/searches/0194..." } }
```
**Validation:** at least one narrowing criterion; codes from reference data; `max_results` within plan.
**Errors:** `402 credits_exhausted`, `403 module_not_entitled`, `422 validation_failed`, `503 provider_unavailable` if every provider is down.

## GET `/api/v1/lead-generation/prospects/{id}/score`
**Authorization:** entitlement and `lead.read`.
**Response `200`:**
```json
{
  "value": 63, "coverage": 0.75, "model_version": 3,
  "factors": [
    { "key": "industry", "state": "matched", "weight": 25, "earned": 25, "evidence": { "value": "Legal services", "source": "provider_x" } },
    { "key": "geography", "state": "matched", "weight": 25, "earned": 25, "evidence": { "value": "United Kingdom", "source": "provider_x" } },
    { "key": "employees", "state": "not_matched", "weight": 25, "earned": 0, "evidence": { "value": "1000+" } },
    { "key": "technology", "state": "unknown", "weight": 25, "earned": null, "evidence": { "note": "No data available" } }
  ],
  "explanation": "Strong industry and location fit. Size is above target. Technology unknown, so it does not lower the score."
}
```

## POST `/api/v1/lead-generation/prospects/{id}/convert`
**Authorization:** entitlement and `lead.write`. **Idempotency:** unique by prospect.
```json
{ "create_deal": true, "deal": { "title": "Managed IT for Example Legal", "currency": "GBP" } }
```
**Response `200`:** `{ "crm_company_id": "0194...", "deal_id": "0194...", "already_converted": false }`. Without a CRM entitlement, `deal_id` is null and `crm_available` is false.

## POST `/api/v1/outreach/approvals/{id}/approve`
**Authorization:** entitlement and `outreach.approve`, and not the author if tenant policy requires separation.
```json
{ "content_hash": "b1946ac92492d2347c6235b4d2611184...", "note": "Reviewed" }
```
**Response `200`:** `{ "id": "0194...", "status": "approved", "decided_at": "2026-11-06T12:00:00Z" }`.
**Errors:** `409 content_changed` if the hash no longer matches. `403 self_approval_not_allowed`.
**Audit:** approver, hash, subject.

## POST `/api/v1/outreach/campaigns/{id}/start`
**Authorization:** entitlement and `outreach.send`. **Idempotency:** required.
**Response `200`:** `{ "id": "0194...", "status": "running", "next_send_at": "2026-11-09T09:00:00Z", "sendable": 412, "excluded": { "suppressed": 12, "jurisdiction_blocked": 31, "missing_email": 4 } }`.
**Errors:** `409 approval_required`, `422 preflight_failed` with a `details` list of blockers.

## POST `/api/v1/public/assessments/{slug}/submissions`
**Authentication:** none; abuse controls apply. **Tenant:** from slug.
```json
{ "domain": "example.org", "name": "Alex Doe", "email": "alex@example.org", "role": "IT Manager", "attestation": { "accepted": true, "terms_version": "2026-01" }, "marketing_consent": false, "captcha_token": "..." }
```
**Response `202`:** `{ "status": "received", "result_link_sent": true }`. It returns the same message for valid and blocked targets, so it can't be used to probe the blocklist.
**Validation:** public registrable domain; DNS resolves to public IPs; attestation accepted; rate limits by IP, target and tenant.
**Errors:** `422 validation_failed`, `429 rate_limited`.

## POST `/api/v1/crm/deals/{id}/move`
**Authorization:** entitlement and `crm.write`.
```json
{ "stage_id": "0194...", "row_version": 7 }
```
**Response `200`:** the updated deal, `row_version` 8.
**Errors:** `409 version_conflict` with current state, `422 invalid_stage` for another pipeline's stage, or `422 use_close_endpoint` for won or lost stages.

## POST `/api/v1/crm/deals/{id}/win`
**Authorization:** entitlement and `crm.close_deal`. **Idempotency:** natural; a repeat returns the current deal.
```json
{ "row_version": 8, "contract_value_minor": 1440000, "expected_mrr_minor": 40000, "currency": "USD", "closed_at": "2026-11-06T12:00:00Z" }
```
**Response `200`:** `{ "id": "0194...", "status": "won", "stage": { "key": "won" }, "row_version": 9 }`. **Effect:** one `crm.deal.won`.

## POST `/api/v1/proposals/proposals/{id}/versions/{v}/issue`
**Authorization:** entitlement and `proposal.send`; approved first if the policy requires. **Idempotency:** required; a repeat returns the existing issue.
```json
{ "recipients": [{ "name": "Alex Doe", "email": "alex@example.org" }], "content_hash": "..." }
```
**Response `200`:**
```json
{ "version": 2, "status": "issued", "snapshot_hash": "...", "totals": { "currency": "USD", "setup_total_minor": 50000, "mrr_total_minor": 90000, "term_months": 36, "tcv_minor": 3290000 }, "delivery": "queued" }
```
**Errors:** `409 approval_required`, `409 content_changed`, `422 validation_failed`.

## POST `/api/v1/public/chat/conversations/{id}/bookings`
**Authentication:** conversation token. **Idempotency:** required.
```json
{ "slot_id": "signed-opaque-token", "timezone": "Europe/London" }
```
**Response `201`:** `{ "id": "0194...", "status": "confirmed", "starts_at": "2026-11-10T10:00:00Z", "ends_at": "2026-11-10T10:30:00Z" }`.
**Errors:** `409 slot_unavailable` (with new options), `422 slot_expired`, `503 calendar_unavailable`.

## POST `/api/v1/webhooks/{providerKind}/{pathToken}`
**Authentication:** provider signature. **Response:** `200` after durable store, `401` on a bad signature, `200` on a duplicate, and `413` if too large. No business data is returned.

## POST `/api/v1/files/uploads`
**Authorization:** session and module permission of the file's purpose.
```json
{ "purpose": "chatbot_knowledge", "file_name": "services.pdf", "content_type": "application/pdf", "size_bytes": 1048576 }
```
**Response `201`:** `{ "id": "0194...", "upload_url": "https://...", "expires_at": "..." }`. The upload then finishes through `POST /files/uploads/{id}/complete`, and the file stays unusable until `clean`.
**Errors:** `422 file_type_not_allowed`, `413 file_too_large`, `402 storage_limit`.
