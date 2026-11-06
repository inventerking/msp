# Navigation

Status: planning, pending approval.

## Client application

```text
Dashboard
PROSPECT
  Lead Generation
  Competitor Intelligence
ENGAGE
  Outreach
  Visitor Intelligence
  Chatbot
SELL
  CRM
  IT Assessment
  Proposals
GROW
  Customer Expansion
  Sales Intelligence
───────────
Team · Settings · Subscription · Billing
```

Groups mirror the workflow, and also happen to match the seed bundles. They're fixed navigation, independent of what's sold.

### Module item states

| Entitlement | Sidebar shows | On click |
|---|---|---|
| Active | Normal | Module |
| Inactive | Dimmed with a lock icon | Module info and purchase options, if user can see billing |
| Pending | "Pending" tag | Status page, showing what's being reviewed |
| Suspended | Warning icon | Status page, with reason category and contact |
| Expired | "Expired" tag | Read-only per D-14, or status page |
| Revoked | Warning icon | Status page |

Users without billing permission see "ask your workspace admin", not a purchase button.

### Module routes

| Module | Base | Sections |
|---|---|---|
| Lead Generation | `/leads` | Search, Prospects, Lists, Imports and exports, Scoring |
| Competitor Intelligence | `/competitors` | Directory, Opportunities, Comparisons, Reports |
| Outreach | `/outreach` | Campaigns, Templates, Approvals, Replies, History, Suppressions, Senders, Analytics |
| IT Assessment | `/assessments` | Assessments, Templates, Landing pages, Authorizations, Reports |
| CRM | `/crm` | Pipeline, Deals, Companies, Contacts, Activities, Tasks, Calendar, Assistant |
| Proposals | `/proposals` | Proposals, Signatures, Catalog, Templates |
| Visitor Intelligence | `/visitors` | Overview, Sessions, Conversions, Attribution, Websites |
| Chatbot | `/chatbot` | Bots, Conversations, Knowledge, Qualification, Booking, Analytics |
| Customer Expansion | `/expansion` | Customers, Renewals, Opportunities, Recommendations |
| Sales Intelligence | `/insights` | Overview, Leads, Campaigns, Funnel, Revenue, Forecasts, Team |

### Header

Workspace switcher; global search; notifications; help; profile menu.

**Global search** queries only entitled modules. Each module exposes a search provider, and the shell merges the results, so no cross-module database search exists. A result is shown only if the backend has confirmed access.

### Cross-module links

Modules link by URL, and do not import each other. A deal page links to a proposal only if the Proposals module is active and the user has permission. Where it isn't active, an unobtrusive prompt or nothing appears.

## Admin application

```text
Dashboard
TENANTS: Tenants · Tenant users
COMMERCIAL: Purchase requests · Subscriptions · Payments · Invoices · Module catalog · Products · Bundles · Plans and prices · Promotions
ACCESS: Module activation · Usage and credits
INTEGRATIONS: AI providers · Company data providers · Email providers · Calendar and e-sign · All integrations
OPERATIONS: Jobs and dead letters · Sending policy and abuse
GOVERNANCE: Audit logs · Security events · Support access · Platform admins · Platform settings
```

Items are shown according to the admin's role, and the backend enforces all of them. The admin app shows a distinct environment banner and visual treatment, to reduce mistakes.

## Public routes

| Route | Purpose |
|---|---|
| `/a/{slug}` | Assessment landing page |
| `/a/results/{token}` | Assessment results |
| `/p/{token}` | Proposal view |
| `/u/{token}` | Unsubscribe |
| `/invite/{token}` | Accept invitation |
| `/login`, `/register`, `/forgot-password`, `/reset-password`, `/verify-email` | Authentication |

Public routes use `noindex`, except published landing pages, and a strict referrer policy.

## Responsive rules

| Width | Navigation |
|---|---|
| Desktop | Fixed sidebar, can collapse to icons |
| Tablet | Collapsed by default |
| Mobile | Drawer; header keeps search and notifications |
