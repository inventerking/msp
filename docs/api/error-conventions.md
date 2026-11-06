# Error conventions

Status: planning, pending approval.

Errors follow RFC 9457 Problem Details, with a stable `code` for programs and safe text for people.

```json
{
  "type": "https://docs.example/errors/validation_failed",
  "title": "Some fields need attention",
  "status": 422,
  "code": "validation_failed",
  "detail": "Check the highlighted fields.",
  "request_id": "0194...",
  "errors": [{ "field": "criteria.countries[0]", "code": "unknown_country" }]
}
```

Rules:

- `code` values never change once released. Clients branch on `code`, never on `title` or `detail`.
- Messages never include SQL, stack traces, internal hostnames, provider responses or another tenant's data.
- A resource in another tenant returns `404`, never `403`, so its existence isn't revealed.
- Messages are translatable: the client maps `code` to text and uses `detail` as a fallback.

## Standard codes

| Status | Code | Meaning |
|---|---|---|
| 400 | `malformed_request` | Unparseable |
| 401 | `authentication_required` | No valid session |
| 401 | `invalid_credentials` | Login failed, always generic |
| 401 | `mfa_required` | Second factor needed |
| 401 | `recent_auth_required` | Sensitive action needs a fresh login |
| 402 | `credits_exhausted`, `storage_limit` | Usage limit reached |
| 403 | `csrf_failed` | CSRF check failed |
| 403 | `email_not_verified` | Verification required |
| 403 | `insufficient_permission` | Role lacks permission |
| 403 | `insufficient_role` | Admin role lacks permission |
| 403 | `module_not_entitled` | Module not active; includes `module_key` and `status` |
| 403 | `tenant_suspended` | Workspace suspended |
| 403 | `self_approval_not_allowed` | Approver is the author |
| 404 | `not_found` | Missing, or belongs to another tenant |
| 409 | `version_conflict` | Stale `row_version`; includes current state |
| 409 | `invalid_transition` | State machine forbids it |
| 409 | `request_in_progress` | Same idempotency key is running |
| 409 | `approval_required` | Action needs an approval |
| 409 | `content_changed` | Approved hash no longer matches |
| 409 | `slot_unavailable` | Booking slot gone |
| 409 | `already_entitled` | Purchase covers no new modules |
| 413 | `file_too_large`, `payload_too_large` | Size limit |
| 422 | `validation_failed` | Field errors |
| 422 | `idempotency_key_reused` | Key used with a different body |
| 422 | `preflight_failed` | Campaign blockers |
| 422 | `price_unavailable`, `promotion_invalid` | Commercial validation |
| 429 | `rate_limited` | Includes `Retry-After` |
| 500 | `internal_error` | Generic; correlation ID only |
| 502 | `provider_error` | Provider gave an unusable response |
| 503 | `provider_unavailable`, `calendar_unavailable`, `ai_unavailable` | Dependency unavailable |
| 503 | `service_unavailable` | Maintenance or overload |

## Module-specific rules

Modules may add codes prefixed by domain where a client needs to branch, such as `use_close_endpoint` or `slot_expired`. New codes are listed in the OpenAPI document.

## Frontend treatment

| Code type | Treatment |
|---|---|
| `validation_failed` | Inline per field |
| `version_conflict` | Show changes and let the user retry |
| `module_not_entitled` | Refresh context and show status page |
| `insufficient_permission` | Inline "ask an admin" message |
| `credits_exhausted` | Usage banner with a link |
| `rate_limited` | Wait and retry for reads; message for writes |
| `5xx`, `provider_*` | Non-technical message and `request_id` for support |
