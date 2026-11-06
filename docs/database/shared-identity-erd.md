# Shared company and contact identity ERD

Status: **planning, pending approval.** Generated from the same specification as the [data dictionary](data-dictionary.md), which remains the fuller reference. Tenant foreign keys are omitted from the relationship lines to keep diagrams readable: assume every table with `tenant_id` belongs to `identity.tenants`. Attribute comments mark foreign keys and logical refs that point outside the diagram.

- `shared.companies` is global and holds only public or shareable-licensed facts.
- A company becomes a prospect, CRM company or competitor through a separate tenant-owned table in that module's schema. One company can be a prospect for one MSP and a competitor for another without either MSP seeing it.
- `shared.contacts` is tenant-scoped by design (ADR-009, D-06), so there is no global person record.

```mermaid
erDiagram
    shared__industries |o--o{ shared__industries : "parent_id"
    shared__industries |o--o{ shared__companies : "industry_id"
    shared__companies |o--o{ shared__companies : "merged_into_id"
    shared__companies ||--o{ shared__company_identifiers : "company_id"
    shared__companies ||--o{ shared__company_source_facts : "company_id"
    shared__companies ||--o{ shared__company_merges : "source_company_id"
    shared__companies ||--o{ shared__company_merges : "target_company_id"
    shared__companies |o--o{ shared__contacts : "company_id"
    shared__contacts ||--o{ shared__contact_sources : "contact_id"
    shared__contacts ||--o{ shared__contact_consents : "contact_id"
    shared__service_categories |o--o{ shared__service_categories : "parent_id"
    shared__service_categories |o--o{ shared__tenant_services : "category_id"
    shared__countries {
        char code PK
        text name
        text region
    }
    shared__industries {
        uuid id PK
        timestamptz created_at
        timestamptz updated_at
        text code
        text name
        uuid parent_id FK
    }
    shared__companies {
        uuid id PK
        timestamptz created_at
        timestamptz updated_at
        citext primary_domain
        text display_name
        text legal_name
        char country_code "logical ref shared.countries"
        uuid industry_id FK
        text employee_band
        text status
        uuid merged_into_id FK
    }
    shared__company_identifiers {
        uuid id PK
        timestamptz created_at
        timestamptz updated_at
        uuid company_id FK
        text kind
        text value_normalized
        text provider_key
        timestamptz verified_at
    }
    shared__company_source_facts {
        uuid id PK
        timestamptz created_at
        timestamptz updated_at
        uuid company_id FK
        text attribute
        jsonb value
        text source_key
        text source_url
        text licence_class
        numeric confidence
        timestamptz observed_at
        timestamptz expires_at
    }
    shared__company_merges {
        uuid id PK
        timestamptz created_at
        timestamptz updated_at
        uuid source_company_id FK
        uuid target_company_id FK
        text reason
        text decided_by
    }
    shared__contacts {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid company_id FK
        text full_name
        text first_name
        text last_name
        text job_title
        citext email
        bytea email_hash
        text phone
        text social_url
        char country_code "logical ref shared.countries"
        text status
        timestamptz erased_at
        uuid created_by "logical ref identity.users"
    }
    shared__contact_sources {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid contact_id FK
        text source_key
        text source_ref
        text licence_class
        timestamptz acquired_at
    }
    shared__contact_consents {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid contact_id FK
        text purpose
        text status
        text basis
        text jurisdiction
        jsonb evidence
        timestamptz captured_at
        timestamptz withdrawn_at
    }
    shared__service_categories {
        uuid id PK
        timestamptz created_at
        timestamptz updated_at
        text key
        text name
        uuid parent_id FK
    }
    shared__tenant_services {
        uuid id PK
        uuid tenant_id FK "FK identity.tenants"
        timestamptz created_at
        timestamptz updated_at
        uuid category_id FK
        text name
        text description
        text status
        timestamptz deleted_at
    }
    shared__fx_rates {
        uuid id PK
        timestamptz created_at
        timestamptz updated_at
        char base_currency
        char quote_currency
        numeric rate
        date rate_date
        text source_key
    }
```
