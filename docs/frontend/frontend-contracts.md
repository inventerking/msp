# Frontend contracts

Status: planning, pending approval.

## API client

- Generated from OpenAPI into `packages/api-client`. Hand-written HTTP calls in features are blocked by lint.
- A wrapper sets `X-CSRF-Token`, sends credentials, adds a request ID, and maps errors into a typed `ApiError` with `code`.
- A `401` redirects to login. A `403 module_not_entitled` refreshes context.

## Query keys

`[tenantId, moduleKey, resource, params]`. Switching tenant clears the entire query cache, and open SSE connections restart. This prevents one workspace's data being shown in another.

## Types

| Type | Rule |
|---|---|
| Money | `{ amount_minor, currency }`, always formatted through `MoneyDisplay` |
| Timestamps | ISO strings; displayed in user's timezone with locale formatting |
| Evidence labels | `verified_public`, `observed`, `inferred`, `ai_estimate`; displayed with `EvidenceBadge` |
| Value kinds | `actual`, `estimated`, `forecast`; displayed with `ValueKindBadge` |
| Module status | `active`, `inactive`, `pending`, `suspended`, `expired`, `revoked` |
| Jobs | Common `JobStatus`, rendered by `JobProgress` |

## Validation

`packages/validation` schemas give immediate feedback and mirror the OpenAPI constraints. The server remains the authority, and its field errors are mapped back to the form. Contract tests compare key schemas with OpenAPI.

## Rendering untrusted content

| Content | Treatment |
|---|---|
| Plain text from users, prospects, AI | React escaping; no raw HTML |
| Rich text notes and proposal content | One sanitizer in `packages/ui`, with an allowlist |
| Email previews | Sandboxed iframe with no scripts and blocked remote images |
| URLs | Scheme allowlist (`https`, `http`, `mailto`); external links with `noopener` |
| Fetched competitor content | Extracted text only |
| File downloads | Separate host; attachment disposition |

Lint forbids `dangerouslySetInnerHTML` outside the sanitizer component.

## Common screen states

The [screen inventory](screen-inventory.md) uses these codes for repeated behaviour.

**Loading**

| Code | Behaviour |
|---|---|
| L-T | Table skeleton with stable layout |
| L-C | Card or chart skeleton |
| L-D | Detail page skeleton |
| L-F | Form skeleton, submit disabled until ready |
| L-J | Job progress, updated via SSE with polling fallback |

**Error**

| Code | Behaviour |
|---|---|
| E-R | Inline error with retry and `request_id` |
| E-F | Field errors, preserved input, and a summary |
| E-C | Conflict dialog showing current values |
| E-P | Provider problem message; the rest of the page still works |

All screens also handle: `403` (permission page), `404` (not found, used for other tenants' IDs), `module_not_entitled` (status page), and offline (banner).

**Responsive**

| Code | Behaviour |
|---|---|
| R-L | Tables become stacked rows; key columns first |
| R-S | Layout stacks into one column; fully usable |
| R-D | Desktop-first: mobile shows a read-only summary and a "use a larger screen" note for editing |
| R-C | Charts resize; a table alternative is available on small screens |

## Accessibility

Keyboard operation everywhere, including an accessible alternative to Kanban drag-and-drop (a "Move to stage" menu). Visible focus, ARIA live regions for job progress and new chat messages, colour never the only signal (badges have text), and reduced-motion support.

## Internationalization

All text in catalogs from the start. Locale-aware formatting for numbers, dates and money. Layouts tolerate longer text. Right-to-left is not in initial scope, but logical CSS properties keep it possible.
