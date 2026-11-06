# Frontend application structure

Status: planning, pending approval. Principles are in [frontend-architecture](../architecture/frontend-architecture.md).

## Layout

```text
apps/
  client/
    src/
      app/          bootstrap, router, providers, error boundaries
      shell/        layout, sidebar, global search, notifications, command menu
      platform/     login, registration, onboarding, profile, team, settings, billing
      public/       assessment landing, proposal view, unsubscribe, invitations
      modules/
        lead-generation/  competitor-intelligence/  outreach/  it-assessment/  crm/
        proposals/  visitor-intelligence/  chatbot/  customer-expansion/  sales-intelligence/
  admin/
    src/
      app/  shell/
      features/     tenants, users, catalog, bundles, subscriptions, payments, activation,
                    usage, ai, providers, integrations, audit, settings, jobs
packages/
  ui/               shadcn/ui-based components and app patterns
  api-client/       generated client, fetch wrapper, error mapping
  types/            generated API types and shared enums
  validation/       shared validation schemas
  config/           lint, TypeScript, Tailwind and Vite presets
  design-tokens/    colours, type, spacing, radii; Tailwind theme
  embeds/           tracker script and chat widget loader; no React dependency for the tracker
```

A workspace manager is proposed in D-21. It's a monorepo, with one lockfile.

## Inside a feature module

```text
modules/crm/
  manifest.ts       key, routes, navigation, permissions, dashboard widgets
  routes/           lazy-loaded route components (screens)
  components/       private to this module
  api/              query and mutation hooks over packages/api-client
  state/            local stores, only if necessary
  validation/       module-specific schemas
  __tests__/
```

## Manifest fields

| Field | Purpose |
|---|---|
| `key` | Must match a backend module key |
| `routes` | Paths and lazy components |
| `navigation` | Items, icon, order, permission |
| `permissions` | For route guards |
| `dashboardWidgets` | Optional shell dashboard cards |
| `globalSearch` | Optional search provider |

## Route guard

1. No session: go to login, keeping the return path.
2. No tenant: go to workspace selection or creation.
3. Module not active: show `ModuleStatusPage` (not purchased, pending activation, suspended, expired).
4. Permission lacking: show `NoPermissionPage`.
5. Otherwise, render. A backend `403` still takes precedence over what the guard thought.

## Shared patterns in `packages/ui`

`DataTable` (server-side pagination, URL state), `PageHeader`, `EmptyState`, `ErrorState`, `SkeletonTable`, `SkeletonCard`, `JobProgress`, `ConfirmDialog` (typed confirmation for dangerous actions), `EvidenceBadge`, `ValueKindBadge`, `MoneyDisplay`, `AiContentNotice`, `ApprovalPanel`, `DomainStatusBadge`.

`EvidenceBadge` and `ValueKindBadge` exist so that modules can't present evidence labels inconsistently, and a lint rule prevents showing `forecast` values through plain numeric cells.

## Testing

Component tests per module, accessibility checks in component tests, mocked-network tests from OpenAPI examples, and Playwright end-to-end tests at the application level.

## Build outputs

| Output | Deployed to | Notes |
|---|---|---|
| `apps/client` | `app.<domain>` | Lazy chunks per module |
| `apps/admin` | `admin.<domain>` | Optionally behind an IP allowlist or VPN |
| `embeds/tracker.js` | `ingest.<domain>/embed/` | Small size budget |
| `embeds/chat-loader.js` and chat frame | Static host | Frame isolated from host page |
