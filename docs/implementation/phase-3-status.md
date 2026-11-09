# Phase 3 status

Status: built and verified locally. **Phase 3 gate approved by the product owner on 2026-11-09.** The licensing and evidence-labelling points below still need legal review before real data is used. The sections "Licence rules to review" and "Evidence labelling to review" below cover what that review needs.

Until legal review, every rule from the deployment plan still holds. Company data comes from a fake provider, with invented companies on reserved `.example` domains. Competitor research reads canned pages instead of real websites. AI output comes from the fake provider.

## What exists

| Area | Built | Key properties |
|---|---|---|
| Ideal customer profile | `GET` and `PUT /tenant/icp`, plus a reference-data endpoint for countries and industries | Codes are checked against reference data. Each change bumps a version |
| Company search | Searches run as background jobs against the company-data provider. Results can be saved as prospects | A repeated request with the same `Idempotency-Key` returns the same search and is not charged twice. Credits are reserved first, then charged only for results found, and the rest is returned |
| Prospects | Add by domain, save from search, import, update, delete, filter, lists | One prospect per company per tenant. Saving twice returns the existing prospect |
| Shared company record | Search results and prospects point to the shared company | A name or fact licensed to one tenant never enters the shared record. Names typed or imported by a tenant never do either. Only the domain identifies the company |
| Scoring | Deterministic scores with factors, coverage and data quality. Versioned scoring models, published from drafts | Unknown data pulls a score towards 50, never down. Every score keeps the model version that produced it |
| Score explanations | Optional AI explanation, written in the background | Only the score and its factors are sent to AI, with no names, domains or contacts. The text is labelled "AI generated" |
| Enrichment | Company facts and contacts, as background jobs | "No data" is a result, not a failure, and is not charged. Contacts are created in the tenant's contact records, with their source and licence |
| CSV import | Upload, column mapping, attestation, row report | The user must attest the data was obtained lawfully, and that is recorded. Each row is reported as added, duplicate or rejected, with the reason |
| CSV export | Background export with download | Spreadsheet formulas are escaped. Data licensed only to the tenant is withheld, and the count withheld is shown |
| Convert to CRM | A contract in `crm/contract` | Returns `409 crm_unavailable` until the CRM module exists in Phase 4 |
| Competitors | Directory, discovery from company data, sources, schedules | That a company is a competitor is private to the tenant. Lead Generation warns when a prospect is also one of the tenant's competitors |
| Research | Background runs: robots.txt check, fetch, snapshot, AI extraction, change detection | URLs pass the Egress Guard on entry and again at fetch time. robots.txt is obeyed, and a server error on robots.txt disallows everything. Unchanged pages create nothing new |
| Observations and changes | Append-only facts with history, and a change feed | A fact is labelled Observed only when its quote really appears on the fetched page and contains the value. Anything else is labelled an AI estimate |
| Opportunities | Added by users, or suggested by AI reports | A user's opportunity is labelled Inferred. An AI suggestion is labelled an AI estimate |
| Comparisons | Saved sets of competitors, compared by attribute | Each value keeps its label |
| Reports | AI market reports, on screen and as PDF | Page content reaches AI only as fenced, untrusted data. Every AI statement is labelled, and the report carries a notice on screen and in the PDF |
| Monitoring | An hourly schedule queues research for due competitors | Runs only while the tenant still holds the module |
| Lifecycle | Company merges, contact erasure, loss of the module | A merge repoints prospects and competitors in every tenant. Erasure removes a contact's prospect links. Losing the module cancels open searches, enrichments and research |
| Per-module API contracts | `api/openapi/modules/lead-generation.yaml` and `competitor-intelligence.yaml`, with generated servers | The same guard enforces entitlement and permission from the contract. Startup fails if a route and its contract disagree |
| Client screens | Prospects, prospect detail, company search, lists, import and export, ideal customer profile. Competitors, competitor detail, opportunities, reports | Placeholders replaced. Scores always show coverage. Evidence labels have a key explaining them |
| Admin screen | Credit grants can now be for lead credits as well as AI credits | |

## Verification results

| Check | Result |
|---|---|
| Backend tests, with race detector, against real PostgreSQL | 491 pass, in 16 packages |
| Frontend tests | 39 pass |
| Browser end-to-end flows | 3 pass, twice in a row, against the full stack with a worker. The new flow runs from ICP to search, saved and scored prospects, an AI explanation, competitor research with labelled facts, and a labelled AI report |
| Generated route tests | Every module route rejects anonymous callers, requires the module, and enforces each declared permission for a Viewer |
| Isolation tests | Tenants share a company record but not prospects, competitors, notes or names. Tenant B cannot see tenant A's searches, prospects, lists or competitors by ID, and gets no hint that A tracks a company |
| Planted faults | Letting a tenant-licensed name into the shared record, dropping the export licence check, and ignoring robots.txt were each caught by a failing test |
| Migrations 12 and 13 | Applied, rolled back and applied again on the local database |
| Go lint, ESLint, module boundaries, OpenAPI lint, type checks | Clean |
| govulncheck and pnpm audit | No known vulnerabilities in code that is called |

## Defects found and fixed during the phase

| Defect | How it was found |
|---|---|
| The worker did not take jobs from the `provider_io` and `research_fetch` queues, so searches and research waited forever outside tests. A test now checks every module queue is polled | Browser test |
| A job that failed for the last time left its search, enrichment, import, export, research run or report showing as running. Handlers now mark their own record failed on the final attempt | Browser test |
| Module pages beyond the first did not render, and tab links resolved against the current sub-page | Browser test, then a routing unit test |
| A score's AI explanation, and new facts after research, did not appear until the page was reloaded | Browser test |
| The e2e environment loader skipped variable names containing digits, such as `S3_ENDPOINT` | Browser test |
| A Phase 2 test edited whichever AI prompt sorted first. With the new prompts, that broke competitor extraction for other tests | API test |
| The monitoring schedule swallowed database errors | Go lint |
| An MFA end-to-end step could fail when a code expired between typing and submitting | Browser test, fixed by waiting for a fresh code window |

## Decisions made during the phase

| Decision | Choice | Reason |
|---|---|---|
| Module routes | Each built module has its own OpenAPI document and generated server, mounted behind the shared guard | Keeps each module's contract separate, as the API plan intended |
| Prospect and competitor names | Stored on the tenant's own rows | The shared record must not hold tenant-licensed or tenant-entered names. Migration 12 was changed for this before it was ever committed |
| Credit costs | One lead credit per search result and per successful enrichment | Internal metering only, not a provider price. Easy to change |
| Reserved domains | `.example` and `.test` are accepted outside production only | Fake data can never create records for real companies' domains |
| Fake providers | Registered only when `APP_ENV` is `local`, `test` or `testing` | An allowlist, so a misspelt production setting never gets fake data |
| Research fetcher | `RESEARCH_FETCHER=fake` by default. `live` uses the Egress Guard and logs a warning | No real website is fetched until that is approved |
| Robots.txt | Our agent's group, or `*`; longest match wins; a 5xx response disallows everything | Follows RFC 9309 |
| Numeric columns | Generated as `float64` | Simpler code than `pgtype.Numeric` for scores and confidence |

## Licence rules to review

These are design choices made without knowing any provider's terms. They need legal review before real data is used.

1. A fact or name licensed only to one tenant (`licensed_tenant`) never enters the shared company record. Only the domain and provider ID do.
2. Exports leave out `licensed_tenant` values. Public, shareable-licensed and tenant-provided values are exported. The export reports how many values it withheld.
3. Contacts from a provider are stored with their source and licence class, and can be erased on request.
4. CSV imports require an attestation of lawful source, which is recorded in the audit log.
5. Which sources research may read, and whether reading them is allowed at all, is not decided. Research is limited to canned pages until it is.

## Evidence labelling to review

1. **Observed**: the AI's quote appears word for word on the fetched page, and contains the value. The page snapshot is kept, with its date and URL.
2. **AI estimate**: anything the AI produced that fails that check, and all AI summaries and suggested opportunities.
3. **Inferred**: an opportunity a user records.
4. **Verified**: reserved for confirmed public records. Nothing produces it yet, and no automated process can upgrade an estimate to it.
5. Reports show a notice on screen and in the PDF: "Statements marked observed were copied from the competitor's public page on the date shown. AI estimates are generated and may be wrong: check them before relying on or sharing them."

## Known gaps

**Waiting on decisions or later phases**
- Real company-data and AI providers (D-05). Only fakes exist, so search results and research pages are invented.
- Conversion to CRM, in Phase 4.
- Whole-tenant purge, as noted in Phase 2.

**Smaller gaps**
- No screens yet for scoring-model weights, saved searches or comparisons. Their APIs exist and are tested.
- Contacts on a prospect show role and source, not name or email. The names stay in the contact records, for CRM to show.
- Saving an ideal customer profile does not rescore existing prospects. Each prospect is rescored on demand or when enriched.
- `/files/{id}/download` lets any member of a workspace download any of its files by ID. Exports and reports have their own permission-checked downloads, but the generic route should check the file's purpose. This was introduced in Phase 2 and should be fixed before real data.
- Change summaries are written in English on the server, with attribute keys such as `pricing`.
- The testing server stack has still not been run end to end since Phase 1. Its worker will need the `RESEARCH_FETCHER` setting left at `fake`.
