# Phase 5 status

Status: built and verified locally. **Waiting for the Phase 5 gate.** The legal review points below must be settled before real use.

The deployment plan's rules still apply until legal review:
- **Outreach email:** goes only to the local mail catcher (Mailpit), through a "sink" provider. That provider is registered only when fake providers are on (`APP_ENV` local, test or testing), so it can never send through a production mailer.
- **Outreach jurisdictions:** no country has a rule, so nobody can be emailed anywhere until a platform admin enters rules from legal advice.
- **Sending domains:** checked by a fake checker. It passes reserved `.example` and `.test` domains and looks up nothing.
- **Visitor tracking:** use it only on the local test page (`/test-site/index.html`) or on sites the team controls.
- **Business identification:** done by a fake provider. It recognises only loopback and documentation addresses (RFC 5737).
- **AI and embeddings:** text comes from the fake AI provider, and chatbot embeddings are computed locally.

## What exists

### Outreach

| Area | Built | Key properties |
|---|---|---|
| Sending set-up | Sending domains with SPF, DKIM and DMARC checks. From-addresses (senders) with a daily limit | A domain is ready only if SPF and DKIM pass and DMARC passes or is `p=none`. A sender's address must be on its domain. New senders default to 50 a day |
| Templates | Versioned templates, with an optional AI draft | A version never changes, so an approved campaign keeps exactly what was approved. AI drafts are labelled "AI generated" wherever they appear. The merge fields are `{{first_name}}`, `{{company}}` and `{{sender_name}}` |
| Audiences | Static audiences built from CRM people or from a Lead Generation list | Lead Generation is reached through a new `Lists` contract, not its tables. Contacts from another workspace are rejected |
| Campaigns and sequences | Up to 10 steps with delays, a sending window in the campaign's time zone, and a postal address | Any edit returns the campaign to draft and cancels its approval |
| Send checks | A preflight report counts the audience as sendable, suppressed, blocked by country, or missing an email, and lists anything blocking the campaign | The same rules run again before every message |
| Approval | Submit, then approve or reject | The approver confirms the content hash of exactly what they reviewed: sender, purpose, postal address and every step. A changed campaign cannot be approved with an old hash. Self-approval is allowed and audited, as in Proposals |
| Sending | A one-minute dispatch tick queues one job per due enrollment on the `email_send` queue | Each job locks the enrollment, then checks every gate in order: entitlement, campaign running, enrollment active, approval hash, email, tenant and global suppression, jurisdiction rule and consent, sender and domain ready, sending window, daily limit. A message is stored before it is sent, keyed by enrollment and step, so a retry never sends twice |
| Every email | Postal address and unsubscribe link in the body, plus `List-Unsubscribe` and `List-Unsubscribe-Post` headers | The subject is kept to one line, and header values are checked for line breaks |
| Unsubscribe | Public page `/u/{token}` and a one-click POST endpoint (RFC 8058) | The token is signed and names the tenant and message. Opening the page changes nothing, so link scanners cannot unsubscribe anyone; the button or one-click POST does. It works without an account and without the tenant's entitlement. Repeating it is harmless |
| Provider events | A signed webhook for the sink provider: delivered, bounce, complaint, reply | A hard bounce suppresses the address for the tenant. A complaint also adds it to the platform's global list (D-09). A reply stops all of that person's sequences before it is recorded. Replays have no second effect |
| Replies and suppressions | Reply list with labels. A suppression list showing reason and source only | Addresses are stored as keyed hashes, so the list cannot show them. Labelling a reply "Asked to stop" opts the person out. Opt-outs, bounces and complaints cannot be removed by the tenant |
| CRM | Sent emails and replies appear on the CRM timeline of the person's company. A first email moves open deals forward to "Contacted" | Only for people who are in CRM |
| Erasure | Stops the person's sequences and blanks message and reply text | Their suppression entry stays, as a hash, so they stay opted out |

### Visitor Intelligence

| Area | Built | Key properties |
|---|---|---|
| Sites | Website, public key, allowed origins, consent mode, IP handling and retention | New sites default to opt-in consent, discarding IP addresses, and 395 days' retention |
| Tracker | `t.js`: no cookies, page views, single-page-app navigation, custom events, and `window.mspTrack.consent()` | It sends `text/plain` beacons, so there is no preflight request |
| Collection | Public `POST /api/v1/public/collect` | Writes run as the narrow `msp_ingest` role, which can find a site by key and write visitor rows only. The request's origin must be one of the site's own. URLs must be on the site, and only `utm_` query parameters are kept. Bots are not counted. Browser IDs are stored only as keyed hashes |
| Consent modes | Opt-in, opt-out and anonymous-only | Without the consent a mode needs, only an anonymous page view is kept, with no session or visitor. The tracker applies the same rule in the browser, so it creates no IDs without consent |
| IP addresses | Never stored | With identification on, the address is shortened to its /24 (IPv4) or /48 (IPv6) network, checked, then dropped |
| Reports | Page views, sessions, visitors, conversions, top pages, sources, daily sessions, identified companies | Computed from the raw tables over a chosen period |
| Goals | Page address contains, or custom event named | Counted once per session |
| Identified companies | "Send to CRM" adds the company through the registry and puts the visits on its timeline | A company can be sent only if this workspace's own sites identified it |
| Retention | A daily job deletes visitor data older than each site's retention period | |

### Chatbot

| Area | Built | Key properties |
|---|---|---|
| Bots | Draft, active or paused; allowed websites; business name, greeting, and a notice to visitors | A bot answers no one until it is activated, and it cannot be activated without at least one website. Visitors always see the notice that they are talking to software |
| Knowledge | FAQ, document and service catalogue text, split into passages with local embeddings | Retrieval is limited to this tenant's own bot. Removing a source stops its use at once |
| Widget | `chat.js`: renders everything as text, keeps its token in `sessionStorage`, and polls for replies from a person | |
| Answers | The fake model answers only from the passages retrieved | Knowledge and the visitor's message reach the model as fenced, untrusted data. Each message is answered without earlier turns. Messages are limited to 1,000 characters and 30 per conversation. Answers that echo instructions or contain markup are discarded. Likely injection attempts are flagged in the transcript. If there is no relevant knowledge, AI fails, or credits run out, a person takes over |
| Handoff | A person's reply silences the bot for that conversation, and a person can close it | |
| Leads | Name, email, company, preferred call time and qualification answers | Each lead becomes a contact recorded as given by the person themselves, and is added to CRM through the registry. Marketing consent is recorded only if the visitor ticks the box, with the server's exact wording as evidence. Answers are scored by question weight. A preferred time becomes a handover to a person, because no calendar is connected |
| Erasure | Blanks what the person wrote and their answers, and unlinks their conversations | |

### Platform, admin and client changes

| Change | Detail |
|---|---|
| Migration 15 | 38 tables across `outreach`, `visitor_intelligence` and `chatbot`, with forced row-level security. Also added: the `vector` extension check, monthly partitions for sessions and events, the retention purge function, the global suppression check function, narrow cross-tenant policies (due enrollments, provider message lookup, active bot lookup, ingest), AI prompt seeds, and the sink provider. Applied, rolled back and applied again locally |
| Database bootstrap | Creates the `vector` extension, and grants `msp_app` membership in `msp_ingest` without inheritance, so the API can switch to it but does not otherwise get its rights. On a database created before this phase, run both statements once as the superuser. They are already applied locally |
| Embed endpoints | `/api/v1/public/collect` and `/api/v1/public/chat/*` answer cross-origin requests without credentials. They skip the global origin check, because each checks the site's or bot's own allowed origins |
| Shared contacts | Contacts can carry a country, set in CRM's "Add person" form and checked against reference data. A contact without a country takes their company's |
| Worker | Polls the `email_send` queue |
| Admin console | New "Outreach rules" page. Super admins set each country's rule, with a note. Super admins and support can add global suppressions; the page shows only the count |
| Client screens | Outreach (campaigns, templates, audiences, replies, suppressions, sending set-up, unsubscribe page), Visitor Intelligence (sites, install snippet, reports, goals, companies), Chatbot (bots, knowledge, questions, conversations, handover). Local test site at `/test-site/index.html` |
| Configuration | `SENDING_DOMAIN_CHECK`, `fake` by default, or `live` for real DNS |

## Verification results

| Check | Result |
|---|---|
| Backend tests, with race detector, against real PostgreSQL | 708 pass, in 22 packages |
| Frontend tests | 43 pass. New: the unsubscribe page sends nothing until the button is pressed |
| Browser end-to-end flows | 5 pass, three full runs in a row. The new flow: an admin allows the UK; the owner sets up a domain, sender, template and audience; a campaign is approved and started; the worker sends the email to the mail catcher; the recipient unsubscribes from the link; the test page is tracked and a company identified and sent to CRM; the widget answers from the bot's knowledge |
| Send gates | Of six contacts, the two allowed (UK, and Germany with consent) receive both steps, exactly twice each. The other four are stopped for the right reason: no consent, no country rule, suppressed, no email. More ticks send nothing more |
| Approval | A wrong hash and a stale hash are rejected; an edited campaign returns to draft and cannot start |
| Unsubscribe | GET changes nothing. A tampered token is rejected. One-click POST works twice without harm. No further step is sent, and the opt-out applies to the tenant's later campaigns |
| Provider events | Reply, bounce and complaint each stop sending. A replayed reply is recorded once. A complaint suppresses the address in another workspace too, without showing where it came from |
| Pause, cancel, limits | Nothing is sent while paused or after cancelling; a daily limit of one sends one; a window excluding today holds the message |
| Visitor privacy | No IP address or raw browser or session ID appears in any visitor table. Query strings and fragments are removed except `utm_` parameters. Other origins get 403. Bots, other domains, paused sites and unknown keys are not counted. Anonymous-only sites never record sessions |
| Chatbot | Bot B cannot answer from bot A's knowledge. A's conversation token does nothing through B's key. Injection attempts are flagged and leak nothing. Long messages and the 31st message are refused |
| Generated route tests | Every Outreach, Visitor Intelligence and Chatbot route rejects anonymous callers, requires its module, and enforces each declared permission for a Viewer. Public routes are declared public |
| Isolation tests | Workspace B cannot read or change A's campaigns, messages, senders, sites, reports, goals, bots, knowledge or conversations by ID. B's lists show none of A's data, and B cannot add A's contacts to an audience or send A's identified company to CRM |
| Admin | Support can read jurisdiction rules but not change them; tenants cannot reach them |
| Generated code | `make gen` reproduces every generated file exactly |
| Go lint, ESLint, module boundaries, OpenAPI lint, type checks | Clean |
| govulncheck | No known vulnerabilities in called code |
| pnpm audit | One moderate advisory: `minimatch` (GHSA-g3hv-75wg-m249). It is reached only through ESLint, a development tool, and is not in any shipped code |

## Defects found and fixed during the phase

| Defect | How it was found |
|---|---|
| The sink email provider was registered in every environment, so in production it would have sent through the real mailer | Code review |
| Several stop, suppression, reply and delivery reason codes did not match the migration's allowed values | Code review against the migration |
| The public bot lookup policy checked for status `published`, which bots never have | Code review |
| New provider connections were stored with a status the table does not allow | API test |
| People added in CRM had no country, so Outreach would block every one of them | While writing the browser test |
| CRM people have only a full name, so `{{first_name}}` came out empty | API test |
| A new bot with no websites failed on a not-null column | API test |
| A count of a company's visits failed when it read a missing date | API test |
| The chat widget was refused on same-origin pages, because browsers send no `Origin` header on same-origin GET requests | Browser test |
| The first activation browser test assumed Outreach was still a placeholder | Browser test |
| Two lint findings | Go lint |

## Decisions made during the phase

| Decision | Choice | Reason |
|---|---|---|
| Countries without a rule | Blocked | Nothing goes anywhere until counsel sets rules (D-10). Rules are data, so review can change them without a release |
| Contact country | The contact's own, else their company's | Jurisdiction depends on where the person is. Company country is a fallback, not proof |
| Consent purpose | `marketing_email` | Shared by Outreach (checked) and the Chatbot (recorded) |
| Who a reply stops | All of that person's sequences in the workspace | A reply to one campaign should not be followed by another |
| Complaints | Global suppression. Bounces stay with the tenant | D-09. A complaint is about the sending practice; a bounce is about one tenant's data |
| Domain readiness | SPF pass, DKIM pass, DMARC pass or `p=none` | Proposed minimum |
| Visitor defaults | Opt-in, IP discarded, 395 days | The most cautious settings. Tenants choose otherwise knowingly |
| Ingest role | `SET ROLE msp_ingest` inside the API, rather than a separate service | Least privilege without adding a service. A separate ingest process can come later if load needs it |
| Daily rollups | Not used yet; reports read raw tables over a period | Accurate and simple at current volumes |
| Chat history | Not sent to the model | Limits instructions built up over several turns. Answers are less conversational |
| Embeddings | Local 64-dimension word vectors, with no approximate index | No embedding provider yet (D-05). Search is limited to one bot's passages, so a full scan is cheap. Real embeddings need re-indexing |
| Booking | A preferred call time becomes a handover to a person | No calendar provider yet (D-05) |
| Embed endpoints and CORS | Origin echoed, credentials never allowed | Customers' pages must call them; with no credentials, a page cannot act as anyone |

## For legal review

1. **Jurisdiction rules (D-10).** There are none. Which countries allow opt-out B2B email, which need consent, and which are blocked? Is the company's country an acceptable fallback for a contact's?
2. **Email content.** The footer gives the tenant's postal address and an unsubscribe link. Is anything else needed in target jurisdictions, such as sender identification or a reason the person is being contacted?
3. **Unsubscribe.** The page wording: "Stop emails from {sender}?" and "You are unsubscribed. {sender} will not email you again." Opt-out applies to the whole workspace at once.
4. **Suppression (D-09).** Hashes are kept indefinitely, including after erasure, so the person stays opted out. Complaints become global. Admins can add global entries for legal requests or abuse.
5. **Visitor tracking.** Are the three consent modes, and the opt-in default, right? Does `sessionStorage` and `localStorage` use by the tracker, after consent, need anything more? Is the 395-day default retention acceptable?
6. **Business identification.** It uses a shortened network address in memory only. Is this allowed without consent, or only with it, as built? The UI says a match is an estimate.
7. **Chatbot.** The notice to visitors, the marketing consent wording ("I agree to receive emails from {business} about their services. I can unsubscribe at any time."), and recording chat leads as data the person gave.
8. **AI text in emails.** AI-drafted templates are labelled, and campaigns using them still need approval. Is labelling to recipients required anywhere?

## For the network review

- **Live domain checks** (`SENDING_DOMAIN_CHECK=live`) make DNS TXT lookups only, for SPF, the default DKIM selector, and DMARC. They are off by default.
- **Embed endpoints** accept cross-origin requests without credentials, rate-limited like other public routes. A per-site or per-bot rate limit is not built yet.

## Known gaps

**Waiting on decisions or later phases**
- Real email, calendar, AI, embedding and business identification providers (D-05).
- Inbound email: replies arrive only as provider webhook events.
- Booking through a calendar.
- AI labelling of replies.
- Customer Expansion and Sales Intelligence (Phase 6).

**Smaller gaps**
- **Sender reputation:** `reputation_state` is read by the send gates but nothing sets it yet. It should be set from bounce and complaint rates.
- **Per-message approval** mode exists in the schema only; every campaign uses campaign-level approval.
- **Dynamic audiences:** none yet; audiences are static.
- **Chatbot sources:** URLs and uploaded documents are not supported yet, only pasted text.
- **Visitors and contacts:** visitor records are never linked to contacts yet.
- **Rate limits:** no per-site or per-bot limit on the embed endpoints.
- **One unreproduced e2e failure:** the new browser flow timed out once in a full-suite run and passed in the next seven runs (four alone, three full suites). Playwright now has a 30-second step timeout, so a repeat will name the step.
- **Carried over:**
  - Proposal expiry marking.
  - The generic file download route that does not check purpose.
  - The testing server has not been run end to end since Phase 1. It will need the two bootstrap statements above run once, `SENDING_DOMAIN_CHECK`, `ASSESSMENT_PROBE` and `RESEARCH_FETCHER` left at `fake`, and the images rebuilt.
