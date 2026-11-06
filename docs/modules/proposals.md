# Module 6: Proposals

Status: planning, pending approval. Common rules are in [module-overview](module-overview.md).

## 1. Purpose
Generate professional MSP service proposals and quotations.

## 2. Business responsibilities
Catalog, pricing, templates, commercial calculations, versioning, approval, sending, viewing and signature tracking.

## 3. Features
Catalog and descriptions; per-user, per-device and fixed pricing; setup and recurring charges; contract duration; SLA templates; terms; proposal templates; AI-assisted content; branded PDFs; versions; approval; sending; viewing status; e-signature; signature status; calculations.

## 4. Data ownership
Proposal documents, commercial snapshots, signature evidence. The deal stays in CRM, and the resulting contract in Customer Expansion.

## 5. Database tables
`service_catalog_items`, `sla_templates`, `terms_templates`, `proposal_templates`, `proposals`, `proposal_versions`, `proposal_line_items`, `approvals`, `share_links`, `view_events`, `signature_requests`, `signature_events`.

## 6. REST API (`/api/v1/proposals`)

| Method and path | Purpose | Permission |
|---|---|---|
| `GET, POST /catalog`, `PATCH /catalog/{id}` | Catalog | `proposal.configure` |
| `GET, POST /sla-templates`, `/terms-templates`, `/templates` and `PATCH` | Templates | `proposal.configure` |
| `GET, POST /proposals`, `GET, DELETE /proposals/{id}` | Proposals | `proposal.read`, `proposal.write` |
| `POST /proposals/{id}/versions` | New draft version | `proposal.write` |
| `GET, PATCH /proposals/{id}/versions/{v}` | Edit draft | `proposal.write` |
| `POST /proposals/{id}/versions/{v}/calculate` | Calculate totals | `proposal.read` |
| `POST /proposals/{id}/versions/{v}/ai-draft` | AI content | `proposal.write` |
| `POST /proposals/{id}/versions/{v}/submit` | Request approval | `proposal.write` |
| `POST /proposals/{id}/versions/{v}/approve`, `/reject` | Decide | `proposal.approve` |
| `POST /proposals/{id}/versions/{v}/issue` | Freeze, PDF, send | `proposal.send` |
| `POST /share-links/{id}/revoke` | Revoke link | `proposal.send` |
| `GET /proposals/{id}/activity` | Views and signature events | `proposal.read` |
| `POST /proposals/{id}/versions/{v}/signature-requests` | Send for signature | `proposal.send` |
| Public: `GET /api/v1/public/proposals/{token}` | View issued version | Token |
| Public: `GET /api/v1/public/proposals/{token}/pdf` | Download | Token |
| Webhooks: `/api/v1/webhooks/esign/{pathToken}` | Signature events | Signature |

## 7. Request structures
**Create proposal:** `{ title, crm_company_id or new_company: { name, domain }, deal_id?, template_id? }`.
**Edit version:** `{ content, term_months, currency, valid_until, lines: [{ catalog_item_id?, name, pricing_model, quantity, unit_price_minor, setup_fee_minor, discount_bps }] }`.
**Issue:** `{ recipients: [{ name, email }], message?, content_hash }`.

## 8. Response structures
**Calculate:** `{ currency, lines: [{ name, recurring_total_minor, setup_fee_minor }], setup_total_minor, mrr_total_minor, term_months, tcv_minor, tax_note }`.
**Public view:** only issued content and commercial snapshot, with no internal notes, approvals or margin data.

## 9. Internal interfaces
`GetIssuedSummary(version_id)` returning totals, term, currency and signature status, for Customer Expansion.

## 10. Required dependencies
CRM registry (customer name), CRM deals (optional), shared `tenant_services`, PDF, Files, Email Provider (transactional stream), Jobs, AI Gateway, Integration (e-sign).

## 11. Published events
`proposals.version.issued`, `.proposal.viewed`, `.proposal.signed`, `.proposal.declined`, `.proposal.expired`, `.proposal.withdrawn`.

## 12. Consumed events
`crm.deal.lost` (offer withdrawal, no automatic change), `shared.contact.erased`, `platform.entitlement.revoked` (stop new sends; existing links remain readable until retention, because a customer already holds that offer).

## 13. Background jobs
`proposal.pdf`, `proposal.send`, `proposal.expire`, signature status reconcile.

## 14. Permissions
`proposal.read`, `proposal.write`, `proposal.approve`, `proposal.send`, `proposal.configure`.

## 15. Validation rules

### Commercial calculation
All in one tested package. The server calculates: client values are never trusted.

| Item | Formula |
|---|---|
| Line recurring | `quantity × unit_price`, less discount, for `per_user` and `per_device`; `unit_price`, less discount, for `fixed` |
| Setup total | Sum of line setup fees |
| MRR total | Sum of line recurring totals |
| TCV | `setup total + MRR total × term months` |

**Example.** 50 users at 2,000 minor units, a 10% discount and a 50,000 setup fee on a 36-month term: recurring 90,000; setup 50,000; TCV 3,290,000. Rounding uses half-up at line level, in a currency's minor unit.

All lines share one currency. Tax is out of scope unless D-11 adds it: the document displays "excludes tax". Annual billing or non-monthly recurring lines would need a normalization rule, and are not included at first.

### Immutability
On `issue`: the version gets a full commercial snapshot (prices, service descriptions, SLA text, terms text) and a SHA-256 hash, and a database trigger then rejects any update to its content columns, `commercial_snapshot`, or line items. A correction is a new version, and old links show a "superseded" notice.

### Other rules
Issuing requires approval when tenant policy requires it (default on above a threshold; proposed). `valid_until` is in the future. AI content is draft and has to be human-approved. Discount limits per role can be configured later.

## 16. Failure handling
Issue happens in two steps: freeze the snapshot in a transaction, then run PDF and email as jobs. A PDF failure shows "issued, delivery pending" and can retry, using the same frozen content. E-sign failure leaves the proposal issued and viewable.

## 17. Idempotency
`issue` with `Idempotency-Key`, and re-issuing an already issued version returns it. Signature request has a unique key. E-sign events dedupe on provider event ID. View events are throttled to one per link per short period.

## 18. Audit
Version create, approve, reject, issue, withdraw; share-link creation and revocation; signature events; catalog price changes.

## 19. Security considerations
- Share links: 256-bit random tokens stored hashed, expiring, revocable; `noindex`; `Referrer-Policy: no-referrer`.
- A public view exposes only the snapshot.
- AI never sets prices (prohibited task), and never sends.
- Terms and SLA text belong to the tenant; the product doesn't warrant them.
- PDF renderer sandbox with no network.
- E-signature legal validity differs by jurisdiction and provider; counsel must review before claims are made.

## 20. Unit tests
Formula table across pricing models and currencies including zero-decimal; TCV check; rounding; discount limits; snapshot hashing; state machine.

## 21. Integration tests
Trigger blocks update after issue, even with direct SQL through the app role. Editing a catalog price doesn't alter issued versions. Replay of a signing webhook gives one `proposals.proposal.signed`. Revoked and expired links are rejected. A tenant's token can't reveal another tenant's proposal.

## 22. Acceptance criteria
- Setup, MRR and TCV are shown separately and match the formulas.
- An issued version can never change, and any correction produces a new version.
- A customer can view and sign through a link without an account, and the tenant sees viewed and signed statuses.
- AI-drafted text cannot be issued without human approval.
