# Privacy and compliance

Status: planning, pending approval.

**This is engineering design, not legal advice, and no claim of compliance is made.** The product must be reviewed by qualified counsel in target markets before launch. Statements about laws here are only prompts for that review.

## Likely roles (for legal confirmation, D-19)

| Data | MSP tenant | Platform |
|---|---|---|
| Prospects, contacts, CRM, campaigns, chats, visitor data | Controller | Processor |
| MSP's own workspace users, billing | Varies | Controller |
| Canonical company data that may include personal data | n/a | Needs careful review; designed to exclude individuals |
| Platform security logs | n/a | Controller |

The role split shapes the design: tenant-scoped contacts, tenant-held suppression lists, and tools that let tenants fulfil their duties.

## Regulations to review

| Area | Regime examples | Design hooks |
|---|---|---|
| Data protection | GDPR, UK GDPR, CCPA/CPRA, others | Rights tools, retention, DPA, sub-processors, transfers |
| Marketing email | CAN-SPAM, CASL, ePrivacy national rules, PECR | Jurisdiction rules, consent evidence, unsubscribe, suppression |
| Cookies and tracking | ePrivacy, PECR, state privacy laws | Consent modes, GPC, IP handling |
| Automated decisions | GDPR Art. 22, emerging AI rules, including the EU AI Act | Human review; recommendation-only AI; AI disclosure |
| Security scanning | Computer misuse laws by jurisdiction | Authorization evidence, tiers, non-invasive checks |
| E-signature | eIDAS, ESIGN, UETA and others | Provider choice, evidence retention |
| Payments, tax | PCI DSS, VAT, sales taxes | Hosted payment pages; D-04 |
| Employee monitoring | Local employment law | Team analytics limited and permissioned |

## Data subject rights

| Right | Mechanism |
|---|---|
| Access and portability | `GET /privacy/contacts/{id}/export`, assembled through every module's lifecycle contract |
| Rectification | Contact Identity updates |
| Erasure | `POST /privacy/contacts/{id}/erase`: tracked job through all modules |
| Objection to marketing | Unsubscribe and suppression |
| Restriction | Contact can be blocked from outreach and enrichment |

**Erasure effects.** Contact PII is nulled, and `status = erased`. Module records remove or anonymise content. A keyed hash remains in suppression, so the person isn't contacted again if re-imported; legal review should confirm this basis. Financial records follow statutory retention. Backups age out, and an erasure log is reapplied after any restore.

## Lawful basis and sources

- Contact records store source and acquisition date, supporting notices such as GDPR Art. 14 where they apply.
- CSV imports require an attestation of lawful source, recorded per import.
- Decision-maker discovery uses licensed providers. Scraping restricted platforms is not designed in.
- Enrichment of contacts can be turned off per tenant or region.

## Consent evidence

`shared.contact_consents` holds purpose, status, basis, jurisdiction, evidence and timestamps. `visitor_intelligence.consent_records` holds tracking consent. Assessment `consent` and terms versions are versioned, so what the person saw can be reconstructed.

## Proposed retention

All values are starting proposals for D-14. Each table's period is in the [data dictionary](../database/data-dictionary.md).

| Data | Proposed retention |
|---|---|
| Visitor events | Site setting, 1–730 days |
| Chat conversations | 24 months |
| Outreach message bodies | 12 months |
| Outreach metadata | 24 months |
| Suppressions | Indefinite, as hash where possible |
| Assessment authorizations | Limitation period (legal review) |
| Assessments | 36 months |
| Audit | 24 months online, then archived |
| Invoices | Statutory, varies by jurisdiction |
| Tenant data after offboarding | Export window, then purge |
| Application logs | 30 days |

## AI and privacy

- Data sent to AI is minimized per task ([ai-gateway](../architecture/ai-gateway.md)).
- No prompts or completions stored by default.
- Provider contracts must exclude training use and specify retention and region (D-05, D-07).
- Chatbot discloses that it is AI.
- AI outputs affecting people are recommendations for human decision.

## Data residency (D-13)

A worldwide market raises data location questions. The initial design has one region, with `tenants.data_region` reserved. Multi-region would likely mean separate full deployments per region, not a second database in one deployment, and needs a separate approval. EU customers may need specific transfer mechanisms or an EU region.

## Documents needed before launch

Terms of service, including acceptable use and anti-spam; privacy notice; data processing agreement; sub-processor list; cookie and tracker guidance for tenants; assessment authorization text; AI use disclosure; security overview; incident process. These require legal drafting, and are not produced here.
