# ERD overview

Status: planning, pending approval. Mermaid ER diagrams, written for GitHub-style rendering. They were checked structurally by the generator but not rendered, because the Mermaid CLI is not installed and installing packages is out of scope for this exercise.

## ERD coverage

| # | ERD | Location |
|---|---|---|
| 1 | Overall platform | Below |
| 2 | Identity and tenant | [identity-erd](identity-erd.md) |
| 3 | Product, bundle, subscription, entitlement | [commercial-erd](commercial-erd.md) |
| 4 | Shared company and contact identity | [shared-identity-erd](shared-identity-erd.md) |
| 5–14 | The ten modules | [module-er-ds](module-er-ds.md) |
| 15 | Integrations, jobs, files, audit, notifications | [module-er-ds](module-er-ds.md) |

## ERD 1: Overall platform

Only the anchor tables are shown. Solid relationships are real foreign keys. Relationship labels beginning `ref` are logical references between modules, with no database foreign key.

```mermaid
erDiagram
    identity_tenants ||--o{ identity_memberships : "tenant_id"
    identity_users ||--o{ identity_memberships : "user_id"
    identity_tenants ||--o{ platform_subscriptions : "tenant_id"
    platform_subscriptions ||--o{ platform_entitlement_grants : "subscription_id"
    platform_modules ||--o{ platform_entitlement_grants : "module_id"
    platform_modules ||--o{ platform_tenant_module_entitlements : "module_id"
    identity_tenants ||--o{ platform_tenant_module_entitlements : "tenant_id"

    shared_companies ||--o{ shared_company_identifiers : "company_id"
    identity_tenants ||--o{ shared_contacts : "tenant_id"
    shared_companies |o--o{ shared_contacts : "company_id"

    shared_companies ||--o{ lead_generation_prospects : "company_id"
    shared_companies ||--o{ competitor_intelligence_competitors : "company_id"
    shared_companies |o--o{ crm_companies : "company_id"
    shared_companies ||--o{ visitor_intelligence_business_identifications : "company_id"
    shared_contacts ||--o{ crm_contacts : "contact_id"
    shared_contacts ||--o{ lead_generation_prospect_contacts : "contact_id"
    shared_contacts ||--o{ outreach_enrollments : "contact_id"

    lead_generation_prospects ||--o{ lead_generation_prospect_contacts : "prospect_id"
    crm_companies ||--o{ crm_contacts : "crm_company_id"
    crm_companies ||--o{ crm_deals : "crm_company_id"
    outreach_campaigns ||--o{ outreach_enrollments : "campaign_id"
    it_assessment_authorizations ||--o{ it_assessment_assessments : "authorization_id"
    proposals_proposals ||--o{ proposals_proposal_versions : "proposal_id"
    chatbot_bots ||--o{ chatbot_conversations : "bot_id"
    customer_expansion_customer_profiles ||--o{ customer_expansion_contracts : "customer_profile_id"

    lead_generation_prospects }o..o| crm_companies : "ref crm_company_id"
    crm_deals |o..o{ proposals_proposals : "ref deal_id"
    crm_companies ||..o{ customer_expansion_customer_profiles : "ref crm_company_id"
    crm_deals |o..o{ customer_expansion_contracts : "ref deal_id"
    proposals_proposal_versions |o..o{ customer_expansion_contracts : "ref proposal_version_id"
    crm_companies |o..o{ it_assessment_assessments : "ref crm_company_id"
    crm_companies |o..o{ chatbot_qualifications : "ref crm_company_id"
```

## Diagram conventions

- In the detailed ERDs, entity names use `schema__table`, because a dot is not valid in an unquoted Mermaid entity name.
- Types with precision are shown without it (`numeric`, `char`) because of Mermaid syntax limits. The data dictionary has exact types.
- `tenant_id` foreign keys to `identity.tenants` are omitted as lines, since they would dominate every diagram.
- Attribute comments such as `"logical ref crm.companies"` mark references outside the module.

## Tenant ownership at a glance

| Scope | Schemas |
|---|---|
| All tables tenant-owned | `lead_generation`, `competitor_intelligence`, `it_assessment`, `crm`, `proposals`, `visitor_intelligence`, `chatbot`, `customer_expansion`, `sales_intelligence` |
| Mostly tenant-owned | `outreach` (one global table: `global_suppressions`, D-09) |
| Mixed | `identity`, `platform`, `shared`, `integration`, `audit`, `notification`, `jobs`, `files` |
