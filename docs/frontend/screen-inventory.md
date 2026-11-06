# Screen inventory

Status: planning, pending approval. Loading (L-), error (E-) and responsive (R-) codes are defined in [frontend-contracts](frontend-contracts.md). API paths are relative to `/api/v1`. Unless stated, each module screen also requires the module's entitlement.

## Platform Admin application

Permission column uses SA = Super Admin, SUP = Platform Support, BA = Billing Admin, IA = Integration Admin, PA = Platform Analyst. Admin screens are desktop-first, but readable on tablets.

| Screen | Purpose | User actions | Data and API | Empty state | Load | Error | Permission | Resp. |
|---|---|---|---|---|---|---|---|---|
| Admin dashboard | Platform health and queues | Open queues | Tenant counts, pending reviews, alerts; `/admin/dashboard` | "No items need attention" | L-C | E-R | All | R-S |
| Tenant management | Find tenants | Search, filter, open | `/admin/tenants` | "No tenants match" | L-T | E-R | SA, SUP, BA, PA | R-L |
| Tenant details | One tenant's status | Suspend, reactivate, view subscriptions and entitlements, request support access | `/admin/tenants/{id}` | n/a | L-D | E-R, E-C | SA; others read | R-S |
| User management | Support tenant users; manage admins | Reset, disable, invite admin | `/admin/tenants/{id}/users`, `/admin/admins` | "No users" | L-T | E-R | SA; SUP limited | R-L |
| Module catalog | Ten modules' metadata, dependencies | Edit descriptions, status | `/admin/modules` | Never empty | L-T | E-R | SA, BA | R-L |
| Product catalog | Products and versions | Create, edit draft, publish, retire | `/admin/products` | "Create your first product" | L-T | E-F | SA, BA | R-D |
| Bundle management | Compose bundles | Pick modules, view dependency warnings, publish | `/admin/products?kind=bundle` | "No bundles" | L-T | E-F | SA, BA | R-D |
| Plans and prices | Periods, trials, limits, currencies | Create, end-date prices | `/admin/plans`, `/admin/prices` | "No plans for this product" | L-T | E-F | SA, BA | R-D |
| Promotions | Discount codes | Create, retire | `/admin/promotions` | "No promotions" | L-T | E-F | SA, BA | R-L |
| Subscription management | Lifecycle | Filter, cancel, change status | `/admin/subscriptions` | "No subscriptions" | L-T | E-R, E-C | SA, BA; others read | R-L |
| Payment management | Payments and invoices | Verify manual payment, refund, view reconciliation | `/admin/payments`, `/admin/invoices` | "No payments" | L-T | E-P | SA, BA | R-L |
| Purchase requests and module activation | Review and activate | Approve, reject with reason | `/admin/purchase-requests`, `/admin/tenants/{id}/entitlements/...` | "Queue is clear" | L-T | E-R, E-C | SA, BA | R-L |
| Module suspension | Suspend, expire, revoke per module | Choose action, type confirmation, give reason | `/admin/tenants/{id}/entitlements` | n/a | L-D | E-C | SA, BA; revoke SA | R-S |
| Usage and credit management | Consumption and balances | Grant credits, set limits, view ledger | `/admin/usage`, `/admin/tenants/{id}/credits` | "No usage yet" | L-C, L-T | E-R | SA, BA; PA read | R-C |
| AI provider configuration | Routes, prompts, quotas | Edit routes, activate prompt versions | `/admin/ai/routes`, `/admin/ai/prompts` | "No provider configured; AI features are off" | L-T | E-F | SA, IA | R-D |
| Company data provider configuration | Providers, licence classes | Enable, set credentials, test | `/admin/integrations/providers?kind=company_data` | "No provider; search unavailable" | L-T | E-F, E-P | SA, IA | R-D |
| Email provider configuration | Streams and limits | Enable, credentials, test, thresholds | `/admin/integrations/providers?kind=email` | "No provider; email is off" | L-T | E-F, E-P | SA, IA | R-D |
| Integration management | Health of all providers and connections | View health, disable | `/admin/integrations/providers` | "No integrations" | L-T | E-P | SA, IA; SUP read | R-L |
| Sending policy and abuse | Jurisdiction rules, reputation, pause sending | Edit rules, pause tenant | `/admin/outreach/*` | "No flagged tenants" | L-T | E-F | SA | R-D |
| Jobs and dead letters | Queues and failures | Requeue, discard with reason | `/admin/jobs`, `/admin/dead-letters` | "No failed jobs" | L-T | E-R | SA, SUP | R-L |
| Audit logs | Search audit and security events | Filter, export | `/admin/audit-events`, `/admin/security-events` | "No events match" | L-T | E-R | SA; SUP limited | R-L |
| Support access | Time-limited tenant access | Request, approve, revoke | `/admin/support-access` | "No active grants" | L-T | E-F | SA; SUP request | R-S |
| Platform settings | Global configuration | Edit, with recent auth | `/admin/settings` | Never empty | L-F | E-F | SA | R-S |

## Client application: shared shell

| Screen | Purpose | User actions | Data and API | Empty state | Load | Error | Permission | Resp. |
|---|---|---|---|---|---|---|---|---|
| Login | Sign in | Password, MFA, forgot password | `/auth/login` | n/a | L-F | E-F, generic | Public | R-S |
| Registration | Create account and workspace | Submit, verify email | `/auth/register` | n/a | L-F | E-F | Public | R-S |
| Workspace | Select or create | Switch, accept invitation | `/me/context`, `/me/active-tenant` | "Create your workspace" | L-C | E-R | Session | R-S |
| Onboarding | Profile, services, ICP | Complete steps, skip | `/tenant/profile`, `/tenant/icps` | Guided steps | L-F | E-F | Owner, Admin | R-S |
| Dashboard | Summary and next steps | Open modules | Module widgets, each from its own API | Setup checklist | L-C | E-R per widget | Member | R-S |
| Sidebar | Module navigation | Navigate | `/me/context` | Shows locked items | L-C | E-R | Member | Drawer |
| Global search | Find across modules | Search, open | Each active module's search | "No results" | L-C | Partial results note | Member | R-S |
| Notifications | Alerts | Read, open, preferences | `/notifications`, SSE | "You're all caught up" | L-T | E-R | Member | R-S |
| Profile | Own account | Update, change password, MFA, sessions | `/me`, `/auth/sessions`, `/auth/mfa/*` | n/a | L-F | E-F | Self | R-S |
| Team | Members and roles | Invite, change role, remove | `/tenant/members`, `/tenant/invitations` | "Invite your team" | L-T | E-F | `team.manage`; members read | R-L |
| Settings | Workspace configuration, integrations, privacy, audit | Edit, connect provider, export, offboard | `/tenant`, `/integrations`, `/privacy`, `/tenant/audit-events` | Setup prompts | L-F | E-F, E-P | `settings.manage`, Owner | R-S |
| Subscription | Plans, modules, statuses | Request purchase, cancel, view pending | `/catalog/*`, `/subscriptions`, `/purchase-requests`, `/entitlements`, `/usage` | Module catalog | L-C | E-R | `billing.read`, `billing.manage` | R-S |
| Billing | Invoices and payments | Pay, download | `/billing/*` | "No invoices" | L-T | E-P | `billing.read`, `billing.manage` | R-L |
| Module status | Why a module is unavailable | Purchase or contact admin | `/me/context`, `/catalog/products` | n/a | L-C | E-R | Member | R-S |

## Lead Generation

| Screen | Purpose | User actions | Data and API | Empty state | Load | Error | Permission | Resp. |
|---|---|---|---|---|---|---|---|---|
| Lead search | Define criteria | Fill filters, use ICP, save, run | `/lead-generation/saved-searches`, `POST /searches` | Suggests starting from ICP | L-F | E-F, credits notice | `lead.search` | R-S |
| Search results | Review matches | Filter, select, promote to prospects, export | `/searches/{id}/results` | "No matches; try widening" | L-T | E-P | `lead.read` | R-L |
| Search job progress | Follow search | Cancel, view history | `/searches/{id}`, SSE | "No searches yet" | L-J | E-P, partial notice | `lead.read` | R-S |
| Lead details | One prospect | Edit status, enrich, convert, view sources and contacts | `/prospects/{id}` | Sections say "No data yet" | L-D | E-R | `lead.read`, `lead.write` | R-S |
| Lead enrichment | Status of enrichment | Run, re-run; see no-data | `/prospects/{id}/enrich` | "Not enriched" | L-J | E-P | `lead.enrich` | R-S |
| Lead scoring | Explain and configure | View factors; edit, publish weights | `/prospects/{id}/score`, `/scoring-models` | "Set your ICP to score leads" | L-D | E-F | `lead.read`, `lead.configure` | R-S |
| Saved lists | Group prospects | Create, add, remove, send to outreach | `/lists` | "Create a list" | L-T | E-R | `lead.write` | R-L |
| CSV import/export | Bulk data | Upload, map, attest, track, download | `/imports`, `/exports` | "No imports yet" | L-J | Row-level report; licence block notice | `lead.import`, `lead.export` | R-D |

## Competitor Intelligence

| Screen | Purpose | User actions | Data and API | Empty state | Load | Error | Permission | Resp. |
|---|---|---|---|---|---|---|---|---|
| Competitor directory | List competitors | Add, discover, archive | `/competitor-intelligence/competitors`, `/discover` | "Add your first competitor" | L-T | E-R | `competitor.read`, `.write` | R-L |
| Competitor profile | Labelled facts and history | Review observations, acknowledge changes | `/competitors/{id}`, `/observations`, `/changes` | "Run research to populate" | L-D | E-R | `competitor.read` | R-S |
| Research configuration | Sources and schedule | Add source, set cadence, run now | `/sources`, `/research-config`, `/research-runs` | "No sources" | L-F, L-J | E-F, blocked-source notice | `competitor.configure`, `.research` | R-S |
| Market opportunities | Underserved segments | Filter, change status, view evidence | `/opportunities` | "No analysis yet" | L-T | E-R | `competitor.read` | R-L |
| Competitor comparison | Side by side | Choose competitors and dimensions | `/comparisons` | "Pick two or more" | L-T | E-R | `competitor.read` | R-D |
| Reports | AI market reports with labels | Generate, read, download | `/reports` | "No reports" | L-J | E-P | `competitor.read`, `.research` | R-S |

## Outreach

| Screen | Purpose | User actions | Data and API | Empty state | Load | Error | Permission | Resp. |
|---|---|---|---|---|---|---|---|---|
| Sender accounts | Mailboxes and domains | Connect, check DNS, set limits | `/outreach/senders`, `/domains` | "Connect a sender"; DNS guide | L-T | E-P, status details | `outreach.configure` | R-S |
| Campaigns | All campaigns | Create, pause, resume, cancel | `/campaigns` | "Create a campaign" | L-T | E-R, E-C | `outreach.read`, `.send` | R-L |
| Templates | Reusable content | Create, version, AI draft | `/templates`, `/templates/ai-draft` | "Create a template" | L-T | E-F, AI unavailable | `outreach.write` | R-S |
| Sequence builder | Steps and timing | Add steps, set delays and window, preflight, submit | `/campaigns/{id}/steps`, `/preflight` | One starter step | L-D | E-F, blockers list | `outreach.write` | R-D |
| Approval queue | Human review | Preview, approve, reject | `/approvals` | "Nothing to approve" | L-T | `content_changed` dialog | `outreach.approve` | R-S |
| Sending history | Message log | Filter, inspect delivery | `/messages` | "Nothing sent" | L-T | E-R | `outreach.read` | R-L |
| Replies | Response inbox | Read, reclassify, open CRM | `/replies` | "No replies yet" | L-T | E-R | `outreach.read` | R-L |
| Suppression list | Do-not-contact | Add, search by email, remove with reason | `/suppressions` | "No suppressions" | L-T | E-F | `outreach.read`, `.suppression_remove` | R-L |
| Analytics | Campaign results | Choose campaign and dates | `/analytics/campaigns/{id}` | "No data yet" | L-C | E-R | `outreach.read` | R-C |

## IT Assessment

| Screen | Purpose | User actions | Data and API | Empty state | Load | Error | Permission | Resp. |
|---|---|---|---|---|---|---|---|---|
| Assessment templates | Choose approved checks | Create, select checks and tier | `/it-assessments/templates` | Default template offered | L-T | E-F | `assessment.configure` | R-S |
| Landing page configuration | Branded public page | Edit, preview, publish | `/landing-pages` | "Create a landing page" | L-F | E-F | `assessment.configure` | R-D |
| Authorization | Domain and consent evidence | Record attestation, verify domain, revoke | `/authorizations`, `/domain-verifications` | "No authorizations" | L-F | E-F, DNS status help | `assessment.run` | R-S |
| Assessment jobs | Runs | Start, cancel, track | `/assessments` | "Run your first" | L-J | E-R, partial notice | `assessment.run`, `.read` | R-L |
| Findings | Results | Filter by class, severity | `/assessments/{id}/findings` | "No findings in the checks completed" with coverage | L-T | E-R | `assessment.read` | R-S |
| Reports | PDFs | Generate, download, share | `/reports` | "No reports" | L-J | E-R | `assessment.read` | R-S |
| Public landing page | Prospect submission | Submit domain, attest, consent | `/public/assessments/{slug}` | Paused-page message | L-F | E-F | Public | R-S |
| Public results | Prospect sees results | Read, download, book | `/public/assessments/results/{token}` | Pending state | L-J | Expired link message | Token | R-S |

## CRM

| Screen | Purpose | User actions | Data and API | Empty state | Load | Error | Permission | Resp. |
|---|---|---|---|---|---|---|---|---|
| Pipeline | Stage configuration and totals | Add, reorder stages, view values | `/crm/pipelines` | Seed pipeline always exists | L-C | E-F | `crm.read`, `.configure` | R-S |
| Kanban board | Work deals | Drag or use menu to move, quick edit | `/pipelines/{id}/board`, `/deals/{id}/move` | "Add your first deal" | L-C | E-C with re-sync | `crm.read`, `.write` | Stage tabs on mobile |
| Companies | Accounts | Search, create, edit, merge request | `/companies` | "Add or convert a company" | L-T | E-R | `crm.read`, `.write` | R-L |
| Contacts | People | Search, create, edit | `/contacts` | "Add a contact" | L-T | E-R | `crm.read`, `.write` | R-L |
| Deals | Deal list and detail | Edit, win, lose, reopen, attach | `/deals`, `/deals/{id}` | "No deals match" | L-T, L-D | E-F, E-C | `crm.read`, `.write`, `.close_deal` | R-L |
| Activities | Timeline | Filter, log a call or note | `/activities` | "No activity yet" | L-T | E-R | `crm.read`, `.write` | R-S |
| Tasks | Follow-ups | Create, assign, complete | `/tasks` | "No tasks" | L-T | E-R | `crm.write` | R-S |
| Calendar | Meetings | Create, edit, link deal | `/meetings` | "No meetings"; connect calendar | L-C | E-P, local save | `crm.read`, `.write` | Agenda on mobile |
| Sales assistant | AI suggestions | Analyze, accept, dismiss | `/assistant/*` | "No suggestions" | L-J | E-P, AI unavailable | `crm.read`, `.write` | R-S |

## Proposals

| Screen | Purpose | User actions | Data and API | Empty state | Load | Error | Permission | Resp. |
|---|---|---|---|---|---|---|---|---|
| Service catalog | Services | Create, retire | `/proposals/catalog` | Offer tenant services as a start | L-T | E-F | `proposal.configure` | R-L |
| Pricing | Models, setup, recurring | Edit prices, see impact note | `/catalog/{id}` | "Set a price" | L-F | E-F | `proposal.configure` | R-S |
| Templates | Proposal, SLA, terms | Edit, preview | `/templates`, `/sla-templates`, `/terms-templates` | Starter templates, "review with your counsel" | L-T | E-F | `proposal.configure` | R-D |
| Proposal editor | Build a version | Edit content, lines, term; AI draft; calculate; submit | `/proposals/{id}/versions/{v}`, `/calculate` | Template picker | L-D | E-F, AI unavailable | `proposal.write` | R-D |
| Proposal versions | History | Compare, create new version, approve | `/proposals/{id}` | First draft | L-T | E-R | `proposal.read`, `.approve` | R-S |
| Sent proposals | Issued items and views | Filter, revoke link, withdraw | `/proposals?status=issued`, `/activity` | "Nothing sent" | L-T | E-R | `proposal.read`, `.send` | R-L |
| Signature tracking | E-sign status | Send, remind, void | `/signature-requests` | "No signature requests" | L-T | E-P | `proposal.send` | R-L |
| Public proposal view | Customer reads | Read, download, sign | `/public/proposals/{token}` | n/a | L-D | Expired or superseded message | Token | R-S |

## Visitor Intelligence

| Screen | Purpose | User actions | Data and API | Empty state | Load | Error | Permission | Resp. |
|---|---|---|---|---|---|---|---|---|
| Website configurations | Sites, consent, retention | Add site, set consent mode, retention | `/visitor-intelligence/sites` | "Add a website" | L-T | E-F | `visitor.configure` | R-S |
| Tracking status | Install check | Copy snippet, verify | `/sites/{id}/snippet`, `/status` | "Waiting for the first event" | L-C | Troubleshooting steps | `visitor.read` | R-S |
| Visitor sessions | Anonymous sessions and business matches | Filter, view, send business to CRM | `/sites/{id}/sessions`, `/businesses` | "No sessions yet" | L-T | E-R | `visitor.read`, `.write` | R-L |
| Traffic analytics | Traffic | Choose range, dimension | `/analytics/traffic` | "No traffic yet" | L-C | E-R | `visitor.read` | R-C |
| Conversion analytics | Goals | Create goals, view conversions | `/goals`, `/analytics/conversions` | "Define a goal" | L-C | E-F | `visitor.read`, `.configure` | R-C |
| Attribution | Sources to conversions | Choose model and range | `/analytics/attribution` | "No conversions yet" | L-C | E-R | `visitor.read` | R-C |

## Chatbot

| Screen | Purpose | User actions | Data and API | Empty state | Load | Error | Permission | Resp. |
|---|---|---|---|---|---|---|---|---|
| Bot management | Create and publish | Edit persona, origins, test, publish, copy embed | `/chatbot/bots` | "Create your bot" | L-T | E-F | `chatbot.configure` | R-S |
| Knowledge sources | Content | Upload, add URL, reindex, remove | `/bots/{id}/knowledge` | "Add knowledge"; public-content warning | L-T, L-J | Failed source details | `chatbot.configure` | R-S |
| Conversation history | Transcripts and live handoff | Read, take over, resolve | `/conversations` | "No conversations" | L-T | E-R | `chatbot.read`, `.handle` | R-S |
| Qualification rules | Questions | Add, reorder, weigh | `/bots/{id}/qualification-rules` | Starter set | L-F | E-F | `chatbot.configure` | R-S |
| Booking configuration | Calendar | Connect, set duration, availability | `/bots/{id}/booking-config` | "Connect a calendar" | L-F | E-P | `chatbot.configure` | R-S |
| Analytics | Performance | Choose bot and range | `/bots/{id}/analytics` | "No data yet" | L-C | E-R | `chatbot.read` | R-C |
| Chat widget (visitor) | Website chat | Chat, give details, book, ask for a person | `/public/chat/*` | Greeting | Typing indicator | Falls back to contact form | Public | R-S |

## Customer Expansion

| Screen | Purpose | User actions | Data and API | Empty state | Load | Error | Permission | Resp. |
|---|---|---|---|---|---|---|---|---|
| Customers | Customer overlay | Search, add, import, view health | `/customer-expansion/customers` | "Win a deal or add a customer" | L-T | E-R | `expansion.read`, `.write` | R-L |
| Service coverage | What each customer has | Set active, absent, declined, unknown | `/customers/{id}/services` | "Add your services first" | L-T | E-F | `expansion.write` | R-S |
| Renewals | Upcoming contract ends | Filter, acknowledge, edit dates | `/renewals`, `/contracts` | "No renewals in this window" | L-T | E-R | `expansion.read` | R-L |
| Expansion opportunities | Revenue tracking | Filter, open deal | `/opportunities` | "No opportunities" | L-T, L-C | E-R | `expansion.read` | R-L |
| Upsell recommendations | Evidence-backed suggestions | Run analysis, accept, dismiss | `/recommendations`, `/customers/{id}/analyze` | "Run analysis" | L-J | E-P; rule-based still shown | `expansion.read`, `.analyze`, `.write` | R-S |

## Sales Intelligence

All charts show the value kind and "as of" time. Sections for inactive modules explain why they're unavailable.

| Screen | Purpose | User actions | Data and API | Empty state | Load | Error | Permission | Resp. |
|---|---|---|---|---|---|---|---|---|
| Sales dashboard | Overview | Date range, drill in | `/sales-intelligence/dashboard` | "Data appears as activity happens" | L-C | E-R per card | `sales_intel.read` | R-C |
| Lead analytics | Sources and quality | Filter by source | `/leads` | "No lead data" | L-C | E-R | `sales_intel.read` | R-C |
| Campaign analytics | Results and ROI | Choose campaign, add costs | `/campaigns`, `/costs` | "No campaigns"; ROI needs costs | L-C | E-R | `sales_intel.read`, `.costs` | R-C |
| Funnel analytics | Stage conversion, win/loss | Filter pipeline, period | `/funnel`, `/win-loss` | "No closed deals yet" | L-C | E-R | `sales_intel.read` | R-C |
| Revenue analytics | MRR, ARR, movements | Grain, currency view | `/revenue` | "No contracts yet" | L-C | Missing FX flag | `sales_intel.read` | R-C |
| Forecasts | Forward view with ranges | Run, view assumptions | `/forecasts` | "Needs 3 months of history" | L-J | E-R, previous run shown | `sales_intel.read`, `.forecast` | R-C |
| Team performance | Rep results | Choose rep, period | `/team` | "No rep activity" | L-C | E-R | `sales_intel.team` | R-C |
