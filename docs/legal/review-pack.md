# Legal review pack

**For:** legal counsel reviewing MSP Growth Engine before any real use (D-19). Prepared 2026-11-18.

**What this is.** Facts about what the system stores, where it sends data, how long it keeps it, and what it shows people, followed by the questions the build team needs answered. It is not legal advice, and it does not claim that the system complies with any law. Where the team made a design choice without legal input, it is marked as a question.

**Current state.** Nothing has been used with real data. The local and testing environments use synthetic data only, send no email to real recipients, run no assessments against domains the team does not control, and track no real visitors. Every external provider is a fake that makes no external calls.

**How decisions come back.** Most rules that may need to change after review are stored as data, so counsel's answers can be applied by an administrator without a code release. Examples: outreach jurisdiction rules, global suppressions, dunning policy, bank transfer instructions, legal document texts, and visitor tracking consent mode. The questions note where a change would need engineering work instead.

## Contents

1. [The product in brief](#the-product-in-brief)
2. [Roles of the parties](#roles-of-the-parties)
3. [Data map](#data-map)
4. [Sub-processors and external services](#sub-processors-and-external-services)
5. [Retention](#retention)
6. [Rights requests](#rights-requests)
7. [Security measures](#security-measures)
8. [Legal documents in the product](#legal-documents-in-the-product)
9. [Payments](#payments)
10. [Open questions](#open-questions)

## The product in brief

A multi-tenant web application for managed service providers (MSPs), the **tenants**. Each tenant buys modules that help it find, contact and sell to businesses, the tenant's **prospects and customers**. A platform operator, the **operator**, runs the service, sells the modules, and reviews purchases.

| Module | What it does with data about people |
|---|---|
| Lead Generation | Finds companies and decision-makers through company-data providers, or by CSV import |
| Competitor Intelligence | Reads competitors' public web pages and summarizes them with AI |
| Outreach | Sends email sequences to contacts, records replies, unsubscribes and complaints |
| IT Assessment | Runs passive checks (DNS, public web headers) on a prospect's domain, with their authorization |
| CRM | Stores companies, contacts, deals, notes, tasks, meetings and activities |
| Proposals | Builds proposals, shares them by link, records views and e-signatures |
| Visitor Intelligence | A tracking script on the tenant's website records page views and may identify the visiting business |
| Chatbot | An AI chat on the tenant's website, which may collect contact details |
| Customer Expansion | Records contracts and suggests further services |
| Sales Intelligence | Reports on the tenant's own sales and revenue |

## Roles of the parties

These are the team's working assumptions. Counsel should confirm or correct them (question L-01).

| Data | Assumed controller | Assumed processor |
|---|---|---|
| Tenant user accounts, billing, platform audit | Operator | — |
| Tenant's contacts, CRM records, outreach, visitors, chats | Tenant | Operator |
| Shared company records (company names, domains, public facts) | Unclear: shared across tenants | — |
| Platform-wide suppression list | Unclear: shared across tenants (D-09) | — |

## Data map

The database has one schema per area. Every table holding tenant data is protected by row-level security in the database, keyed on the tenant, and tested to stay that way. "Hash" means a keyed hash (HMAC) that cannot be reversed without the server's key.

### Accounts and access (`identity`)

| Data | Whose | Fields | Notes |
|---|---|---|---|
| Users | Tenant staff | Email, display name, locale, time zone, last sign-in | Password stored as an Argon2id hash |
| Two-step verification | Tenant staff, operator staff | TOTP secret, recovery code hashes | Secret encrypted (AES-256-GCM) |
| Sessions | Tenant staff, operator staff | Token hash, IP address hash, user agent | |
| Sign-in attempts | Anyone signing in | Email hash, IP address hash, success or reason | For lockout and abuse detection |
| Invitations, memberships, roles | Tenant staff | Email, role | |
| Workspace profile | Tenant | Business name, address, ideal customer profile | Postal address appears in outreach footers |
| Legal document acceptances | Tenant staff | User, document version, workspace (for DPA), time | |
| Operator administrators | Operator staff | Email, name, roles, sessions | |

### Shared records (`shared`)

| Data | Whose | Fields | Notes |
|---|---|---|---|
| Companies | Businesses | Name, domain, country, industry, size, public facts with source and licence class | Shared across tenants, except facts licensed to one tenant |
| Contacts | Prospects' and customers' staff | Full name, job title, email, phone, social URL, country, source, acquisition date | Per tenant (ADR-009, D-06). Erasable |
| Contact consents | Same | Purpose, status, basis, jurisdiction, evidence, time | |
| Contact sources | Same | Where each contact came from, and when | Supports notices to people whose data was not collected from them |

### Modules

| Schema | Personal data held | Notes |
|---|---|---|
| `lead_generation` | Links prospects to contacts, with role and confidence; import and export jobs | CSV imports record an attestation of lawful source |
| `competitor_intelligence` | Mostly business data. Snapshots of public web pages, which may name individuals | AI summaries are labelled as estimates |
| `outreach` | Message subjects and bodies, send times, delivery events, reply snippets, suppression hashes | Reply text arrives through the email provider |
| `it_assessment` | Authorizer's name, email, role and IP hash; lead captures with consent | Results are about domains, not people |
| `crm` | Contacts linked to companies, notes, meetings, attendees, activities, tasks | Free text may contain anything a user types |
| `proposals` | Recipient details, share-link views with IP hash, signature events | E-signature through a fake provider today |
| `visitor_intelligence` | Hashed anonymous visitor ID, sessions (landing page, referrer host, UTM tags, device class), page URLs with query strings stripped except `utm_`, consent records, business matches | IP address used in memory only, never stored. No identification of individuals |
| `chatbot` | Conversation messages, contact details the visitor gives, consent | Visitors are told they are talking to an AI |
| `customer_expansion` | Mostly business data: contracts, services, health indicators | |
| `sales_intelligence` | Sales facts by user ID, for team performance reports | Viewing team performance is permission-controlled and audited |

### Platform (`platform`, `integration`, `notification`, `files`, `jobs`, `audit`)

| Data | Personal data held | Notes |
|---|---|---|
| Billing (`platform`) | Purchases, invoices and payments per tenant; the payment provider's references | No card numbers are stored or seen: card entry is on the provider's page |
| Abuse reports | Reporter's email (optional), reported URL and details | |
| Legal documents | The texts counsel supplies | |
| Notifications | Email address and message content, delivery attempts | |
| Files | Uploaded files: proposals, attachments, exports | Scanned for malware before use |
| AI requests (`integration.ai_requests`) | Metadata only: model, tokens, cost, outcome. No prompts or completions by default (D-07) | |
| Provider call logs | Request metadata, not bodies | |
| Jobs and events | Payloads may include IDs and some data, briefly | Finished jobs deleted after 30 days |
| Audit (`audit`) | Who did what, when, with IP hash and user agent; before and after values for some changes | Hash-chained to detect tampering |

### Outside the database

| Where | What | Notes |
|---|---|---|
| Application logs | Route template, status, duration, request ID | Paths, IP addresses and bodies are deliberately left out |
| Proxy logs (Traefik) | Method, status, timing | Client address, path and headers are dropped |
| Metrics | Counts and timings by route template | No tenant or user identifiers |
| Backups | A full encrypted copy of the database, and its change log | See [retention](#retention) |
| Redis | Rate-limit counters, cache | Rebuildable; not backed up |
| Browser storage | Visitor tracking: anonymous ID and session ID, only as the site's consent setting allows | Question L-12 |

## Sub-processors and external services

**Today there are none.** Every provider is a fake that makes no external calls, and the testing server's email goes to a local sink that cannot deliver to real inboxes.

The table lists what a real deployment would need. None has been chosen (D-05). Choosing each needs a review of its data terms, location, retention and training use (D-05, D-07).

| Capability | Data it would receive | Chosen? |
|---|---|---|
| Hosting and database | Everything | No (D-23) |
| Object storage | Uploaded files | No |
| Email sending | Recipient address, message content | No |
| AI models | Text from the task, minimized per task | No |
| Company and contact data | Search criteria; returns company and contact data | No |
| Business identification (from IP address) | Shortened IP address | No |
| E-signature | Signer name and email, the document | No |
| Calendar | Meeting attendees and times | No |
| Payments | Amount, currency and a description from us; card details are entered on the provider's page, not ours | No (D-04) |

The admin **Integrations** page lists every provider configured, with its status. The legal document kind "Sub-processor list" exists so counsel can publish the list in the app once providers are chosen.

## Retention

"Enforced" means the system deletes the data automatically today. "Proposed" values are from [privacy-and-compliance](../security/privacy-and-compliance.md) and are not enforced yet: they need counsel's decision (D-14) and then a deletion job.

| Data | Period | Status |
|---|---|---|
| Visitor events, sessions, visitors, business matches | Per site, 1–730 days; default 395 | **Enforced** daily |
| Expired sessions | 30 days after expiry | **Enforced** hourly |
| Verification tokens | 7 days after expiry | **Enforced** |
| Sign-in attempts | 90 days | **Enforced** |
| Finished jobs and event deliveries | 30 days | **Enforced** |
| Erased contacts | Personal fields cleared at once; a keyed hash stays in suppression lists indefinitely | **Enforced** on request |
| Chat conversations | 24 months | Proposed |
| Outreach message bodies | 12 months | Proposed |
| Outreach metadata | 24 months | Proposed |
| Assessments | 36 months | Proposed |
| Assessment authorizations | The limitation period | Proposed, needs counsel |
| Audit log | 24 months online, then archived | Proposed |
| Invoices and payments | Statutory period | Proposed, varies by country |
| Abuse reports | Not set | Question L-21 |
| Legal document acceptances | Not set; kept as evidence | Question L-21 |
| Tenant data after offboarding | Read-only 30 days, retained 90 days, then purged after notice (D-14) | Proposed; purge not built |
| Application logs | 30 days | Proposed; the testing server rotates by size |
| Backups | 7 daily full backups and the change log between them; older ones expire | **Enforced** by pgBackRest |

**Erasure and backups.** An erased person's data remains in backups until they expire. If a backup is restored, erasures made after it must be repeated; see the [restore runbook](../operations/runbooks/restore.md). The record of erasure requests is currently kept in the database itself, which the team considers a gap for production.

## Rights requests

| Request | How it is handled today |
|---|---|
| Access or export of a contact's data | Each module provides an export for the contact; collected through the shared contact service |
| Erasure of a contact | A tenant user requests it; a job erases the contact in every module, then the shared record. Audited. The email hash stays in suppression lists so the person is not contacted again |
| Objection to email | Unsubscribe link in every message; applies to the whole tenant. Complaints apply across all tenants (D-09) |
| Visitor tracking | Follows the site's consent mode; withdrawal is recorded |
| Tenant user's own account | Not self-service yet; handled by support |

## Security measures

In summary, for the DPA's description of measures. Details are in [security-architecture](../security/security-architecture.md), [tenant-isolation](../security/tenant-isolation.md) and [threat-model](../security/threat-model.md).

- Tenant isolation is enforced in the database with forced row-level security, and checked by automated tests, including a check on a restored backup.
- Passwords use Argon2id; secrets in the database use AES-256-GCM; backups use AES-256.
- Operator administrators must use two-step verification; their actions are audited, and support access to tenant content needs a time-limited grant visible to the tenant (D-22).
- Uploaded files are scanned for malware; outbound requests to user-supplied addresses go through a guard that refuses private networks.
- Logs and metrics leave out IP addresses, paths and bodies.
- Gaps the team knows of are listed in [phase-7-status](../implementation/phase-7-status.md). They include rotating the application's encryption key, and an independent penetration test, which has not been done.

## Legal documents in the product

The team has written no legal text. Counsel supplies it, and an operator's Super Admin publishes it in Admin → **Legal documents**:

- Kinds: terms of service, privacy notice, data processing agreement, acceptable use policy, sub-processor list.
- Each has versions. Publishing a version retires the previous one. A checkbox records whether counsel reviewed that text, as the admin states it.
- Published documents are public at `/legal`.
- When a version requires acceptance, each signed-in user is asked to accept it before continuing. The DPA is asked only of workspace owners, once per workspace. Acceptance records the user, the version, the workspace for a DPA, and the time.

## Payments

The payment provider is not chosen (D-04). The build supports any provider through one interface, a test provider for development, and bank transfers that an operator confirms by hand.

- **Charging (D-01).** A card is charged when the purchase is submitted. An operator then reviews it, normally within 24 hours (a setting). Rejection refunds a card automatically. A bank transfer is refunded by hand.
- **Tax.** Not calculated. Quotes and invoices say so. Question L-24.
- **Invoices.** Numbered in one sequence. Whether their content meets invoicing rules has not been checked. Question L-25.
- **Failed renewals (D-18).** Card retries on set days, then suspension, then the subscription ends. Each step notifies the customer. The days are a setting.
- **Abuse.** Anyone can report abuse. Operators review reports and may suspend a workspace.

## Open questions

Grouped by area. "Applied by" says what is needed to apply counsel's answer: **data** (an admin can change it), **text** (a wording change), or **build** (engineering work).

### Roles and documents

| ID | Question | Applied by |
|---|---|---|
| L-01 | Are the roles in [roles of the parties](#roles-of-the-parties) right? In particular, who controls the shared company records and the cross-tenant suppression list? | Text and build |
| L-02 | Which documents are needed, and which need acceptance by whom? Is accepting the DPA in the app, per workspace, by the owner, acceptable? | Data |
| L-03 | Must re-acceptance be required on every new version, or only for material changes? Today it is per version, and the admin chooses whether a version requires acceptance | Data |
| L-04 | Data residency and transfers: which region (D-13), and what transfer mechanism for any provider outside it? | Build |

### Prospecting data (Phase 3)

| ID | Question | Applied by |
|---|---|---|
| L-05 | The licence rules in [phase-3-status](../implementation/phase-3-status.md#licence-rules-to-review): facts licensed to one tenant never enter shared records, exports leave them out, and CSV imports need an attestation of lawful source. Are these enough? | Build |
| L-06 | Which sources may competitor research read, if any? Research is limited to canned pages until this is decided | Build |
| L-07 | The labels "observed", "AI estimate" and "inferred", and the report notice. Are they adequate? | Text |
| L-08 | Notices to people whose data comes from providers or imports (for example under GDPR Article 14): who gives them, and how? | Build |

### Assessments and proposals (Phase 4)

| ID | Question | Applied by |
|---|---|---|
| L-09 | Is attestation enough for passive checks (D-12)? The attestation wording and the result notice are in [phase-4-status](../implementation/phase-4-status.md#for-legal-review) | Text |
| L-10 | Public assessment landing pages are not built, because they would let anyone who attests trigger checks. May they be built, and on what terms? | Build |
| L-11 | Proposals: the approval rules, the "All prices exclude tax" note, and whether e-signatures through a future provider are valid in target countries | Text |

### Engagement (Phase 5)

| ID | Question | Applied by |
|---|---|---|
| L-12 | Visitor tracking: are the three consent modes and the opt-in default right? Does the use of browser storage after consent need anything more? Is 395 days an acceptable default retention? | Data |
| L-13 | Business identification uses a shortened IP address in memory only, with consent. Is it allowed, and is consent required? | Data |
| L-14 | Outreach jurisdiction rules (D-10): which countries allow opt-out business email, which need consent, which are blocked? Is the company's country an acceptable fallback for a contact's? No country has a rule today, so nothing can be sent | Data |
| L-15 | Email content: the footer has the tenant's postal address and an unsubscribe link. Is anything else needed? | Text |
| L-16 | Unsubscribe applies to the whole tenant; complaints apply across tenants (D-09); suppression hashes are kept indefinitely, including after erasure. Are these right, and on what basis? | Data |
| L-17 | The chatbot's AI notice and marketing consent wording | Text |
| L-18 | Must AI-drafted email be labelled to recipients anywhere? It is labelled to the tenant today | Build |

### Retention and rights (D-14)

| ID | Question | Applied by |
|---|---|---|
| L-19 | Confirm or change each proposed period in [retention](#retention) | Build |
| L-20 | Tenant offboarding: is read-only for 30 days, retained for 90, then purged, acceptable? | Build |
| L-21 | How long to keep abuse reports and document acceptances | Build |
| L-22 | Do erased people's data in backups need anything beyond backups expiring, and reapplying erasures after a restore? | Build |
| L-23 | Is a breach notification procedure needed in the product, for example notifying tenants in the app? The [data breach runbook](../operations/runbooks/data-breach.md) leaves every notification decision to counsel | Build |

### Commercial (Phase 7)

| ID | Question | Applied by |
|---|---|---|
| L-24 | Tax: which taxes apply where the operator sells, and does that favour a merchant of record (D-04)? | Build |
| L-25 | What must an invoice contain, and in which countries? | Build |
| L-26 | Charging at submission, before review, with a 24-hour review target and automatic refund on rejection (D-01). Are any notices or cancellation rights needed, for example for consumers or in particular countries? | Text and data |
| L-27 | Suspension and expiry after non-payment (D-18): are the notices and timings acceptable? | Data |
| L-28 | Abuse: what may the operator do on a report, and what should reporters be told? | Text |
| L-29 | Is a refund by bank transfer, recorded by hand, acceptable evidence? | Data |
