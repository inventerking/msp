# Event contracts

Status: planning, pending approval. The envelope, publishing rules and consumer duties are in [event-architecture](../architecture/event-architecture.md). All listed events are `schema_version` 1. Money fields use `_minor` plus `currency`. Payloads contain IDs, and no names, emails or free text.

## Catalog

### Identity and tenants

| Event | Key payload | Consumers |
|---|---|---|
| `identity.user.registered` | `user_id` | Notification |
| `identity.user.email_verified` | `user_id` | Tenant Management |
| `identity.tenant.created` | `tenant_id` | CRM (seed pipeline), Notification |
| `identity.tenant.suspended`, `.reactivated` | `tenant_id`, `reason_code` | All modules with running work |
| `identity.tenant.offboarding_started` | `tenant_id` | Notification, tenant purge orchestrator |
| `identity.member.joined`, `.removed`, `.role_changed` | `user_id`, `role_key` | Notification, CRM (reassign prompt) |

### Commercial

| Event | Key payload | Consumers |
|---|---|---|
| `platform.purchase.submitted` | `purchase_request_id`, `plan_id` | Notification (admin) |
| `platform.purchase.approved`, `.rejected` | `purchase_request_id` | Notification, Billing (refund) |
| `platform.payment.verified`, `.failed`, `.refunded` | `payment_id`, `amount_minor`, `currency` | Subscription, Notification |
| `platform.invoice.issued` | `invoice_id` | Notification |
| `platform.subscription.changed` | `subscription_id`, `from`, `to` | Entitlement, Notification |
| `platform.entitlement.activated` | `module_key`, `entitlement_version` | Notification, SSE, module provisioning |
| `platform.entitlement.suspended`, `.expired`, `.revoked` | `module_key`, `entitlement_version`, `reason_code` | Owning module (stop work), Notification, SSE |
| `platform.credits.low`, `.exhausted` | `meter_key`, `balance` | Notification |
| `platform.product.published`, `platform.module.status_changed` | IDs | Cache invalidation |
| `platform.support_access.granted` | `grant_id`, `scope`, `expires_at` | Notification (tenant Owner) |

### Shared identity

| Event | Key payload | Consumers |
|---|---|---|
| `shared.company.merged` | `source_company_id`, `target_company_id` | Lead Generation, Competitor Intelligence, CRM, IT Assessment, Visitor Intelligence, Customer Expansion |
| `shared.contact.erased` | `contact_id` | All modules that store `contact_id` |
| `shared.contact.consent_changed` | `contact_id`, `purpose`, `status` | Outreach |

### Lead Generation

| Event | Key payload | Consumers |
|---|---|---|
| `lead_generation.search.completed` | `search_job_id`, `result_count`, `status` | Notification |
| `lead_generation.prospect.created` | `prospect_id`, `company_id`, `source_key` | Sales Intelligence |
| `lead_generation.prospect.enriched` | `prospect_id`, `kind` | Lead Generation (rescore) |
| `lead_generation.prospect.scored` | `prospect_id`, `score`, `coverage`, `model_version` | None at launch |
| `lead_generation.prospect.qualified`, `.disqualified` | `prospect_id` | CRM, Sales Intelligence |
| `lead_generation.prospect.converted` | `prospect_id`, `crm_company_id`, `deal_id?` | Sales Intelligence |

### Competitor Intelligence

| Event | Key payload | Consumers |
|---|---|---|
| `competitor_intelligence.competitor.designated` | `competitor_id`, `company_id` | None at launch |
| `competitor_intelligence.research.completed` | `run_id`, `status` | Notification |
| `competitor_intelligence.change.detected` | `competitor_id`, `change_event_id`, `attribute_type` | Notification |
| `competitor_intelligence.opportunity.identified` | `opportunity_id` | Notification |
| `competitor_intelligence.report.ready` | `report_id` | Notification |

### Outreach

| Event | Key payload | Consumers |
|---|---|---|
| `outreach.campaign.approved`, `.started`, `.completed` | `campaign_id` | Notification |
| `outreach.message.sent` | `message_id`, `campaign_id`, `contact_id` | CRM, Sales Intelligence |
| `outreach.message.bounced` | `message_id`, `campaign_id`, `bounce_type` | Sales Intelligence |
| `outreach.reply.received` | `reply_id`, `campaign_id`, `contact_id`, `classification` | CRM, Sales Intelligence, Notification |
| `outreach.unsubscribe.recorded` | `campaign_id`, `contact_id?` | Sales Intelligence, Contact Identity (consent) |
| `outreach.suppression.added` | `suppression_id`, `reason` | None at launch |
| `outreach.sequence.stopped` | `enrollment_id`, `stop_reason` | None at launch |

### IT Assessment

| Event | Key payload | Consumers |
|---|---|---|
| `it_assessment.lead.captured` | `lead_capture_id`, `contact_id?` | CRM, Sales Intelligence, Notification |
| `it_assessment.authorization.granted` | `authorization_id`, `tier` | None at launch |
| `it_assessment.assessment.completed` | `assessment_id`, `score`, `risk_level`, `crm_company_id?` | CRM, Customer Expansion |
| `it_assessment.report.ready` | `report_id`, `assessment_id` | Notification |
| `it_assessment.consultation.requested`, `.booked` | `booking_id`, `assessment_id` | CRM, Notification |

### CRM

| Event | Key payload | Consumers |
|---|---|---|
| `crm.company.created` | `crm_company_id`, `company_id?` | Lead Generation |
| `crm.company.lifecycle_changed` | `crm_company_id`, `from`, `to` | Customer Expansion |
| `crm.contact.created` | `crm_contact_id`, `contact_id` | None at launch |
| `crm.deal.created` | `deal_id`, `crm_company_id`, `source_module?`, `owner_user_id?` | Sales Intelligence |
| `crm.deal.stage_changed` | `deal_id`, `from_stage_key`, `to_stage_key` | Sales Intelligence |
| `crm.deal.won` | `deal_id`, `crm_company_id`, `currency`, `contract_value_minor?`, `expected_mrr_minor?`, `source_module?`, `owner_user_id?` | Customer Expansion, Sales Intelligence, Outreach |
| `crm.deal.lost` | `deal_id`, `loss_reason_code?` | Customer Expansion, Sales Intelligence, Outreach, Proposals |
| `crm.deal.reopened` | `deal_id` | Sales Intelligence, Customer Expansion |
| `crm.meeting.scheduled`, `.completed` | `meeting_id`, `deal_id?` | Sales Intelligence |
| `crm.task.overdue` | `task_id`, `assignee_user_id` | Notification |

### Proposals

| Event | Key payload | Consumers |
|---|---|---|
| `proposals.version.issued` | `proposal_id`, `version_id`, `deal_id?`, totals | CRM, Sales Intelligence |
| `proposals.proposal.viewed` | `proposal_id`, `version_id` | CRM, Notification |
| `proposals.proposal.signed` | `proposal_id`, `version_id`, `deal_id?`, totals | CRM, Customer Expansion, Sales Intelligence, Notification |
| `proposals.proposal.declined`, `.expired`, `.withdrawn` | `proposal_id`, `version_id` | CRM, Sales Intelligence |

### Visitor Intelligence

| Event | Key payload | Consumers |
|---|---|---|
| `visitor_intelligence.conversion.recorded` | `conversion_id`, `site_id`, `goal_id`, `contact_id?` | CRM, Sales Intelligence |
| `visitor_intelligence.business.identified` | `site_id`, `company_id`, `day` | Notification (optional) |
| `visitor_intelligence.consent.withdrawn` | `site_id` | Chatbot |
| `visitor_intelligence.site.first_event` | `site_id` | Notification |

### Chatbot

| Event | Key payload | Consumers |
|---|---|---|
| `chatbot.contact.captured` | `conversation_id`, `contact_id`, `visitor_ref?` | Visitor Intelligence |
| `chatbot.prospect.qualified` | `conversation_id`, `qualification_id`, `crm_company_id?` | CRM, Sales Intelligence |
| `chatbot.handoff.requested` | `conversation_id`, `handoff_id` | Notification |
| `chatbot.booking.confirmed`, `.cancelled` | `booking_id`, `crm_meeting_id?` | CRM, Notification |
| `chatbot.conversation.closed` | `conversation_id` | None at launch |

### Customer Expansion

| Event | Key payload | Consumers |
|---|---|---|
| `customer_expansion.contract.activated` | `contract_id`, `crm_company_id`, `mrr_minor`, `currency`, `movement`, `source_deal_id?` | Sales Intelligence |
| `customer_expansion.contract.changed` | `contract_id`, `mrr_delta_minor`, `currency`, `movement` | Sales Intelligence |
| `customer_expansion.contract.ended` | `contract_id`, `mrr_delta_minor`, `currency`, `movement` | Sales Intelligence, CRM |
| `customer_expansion.renewal.due` | `contract_id`, `due_date`, `window_days` | Notification, CRM (task) |
| `customer_expansion.recommendation.created`, `.accepted`, `.dismissed` | `recommendation_id`, `kind` | Notification |
| `customer_expansion.opportunity.won`, `.lost` | `opportunity_id`, `deal_id`, `realised_mrr_minor?`, `currency?` | None at launch |

### Sales Intelligence, integrations and files

| Event | Key payload | Consumers |
|---|---|---|
| `sales_intelligence.forecast.completed` | `run_id`, `kind` | Notification |
| `integration.connection.connected`, `.needs_reauth`, `.disconnected` | `connection_id`, `provider_key` | Outreach, Chatbot, Notification |
| `integration.provider.degraded`, `.recovered` | `provider_key` | Admin alerts |
| `integration.calendar_event.changed` | `connection_id`, `provider_event_id` | Chatbot, CRM |
| `files.object.ready`, `.quarantined` | `object_id`, `owner_module` | Owning module, Notification |

## Example

```json
{
  "event_id": "0194a1b2-...",
  "event_type": "crm.deal.won",
  "schema_version": 1,
  "tenant_id": "0194a000-...",
  "source_module": "crm",
  "aggregate_type": "deal",
  "aggregate_id": "0194a0ff-...",
  "occurred_at": "2026-11-06T12:00:00Z",
  "correlation_id": "0194a0aa-...",
  "causation_id": "0194a1a0-...",
  "payload": {
    "deal_id": "0194a0ff-...",
    "crm_company_id": "0194a0cc-...",
    "currency": "USD",
    "contract_value_minor": 1440000,
    "expected_mrr_minor": 40000,
    "source_module": "lead_generation",
    "owner_user_id": "0194a0dd-..."
  }
}
```

## Consumer classes

- **Passive** (run regardless of the consumer module's entitlement, ADR-014): all Sales Intelligence fact consumers, and Customer Expansion's `crm.deal.won`, `crm.deal.reopened` and `proposals.proposal.signed` consumers.
- **Lifecycle** (run always, as they enforce safety or privacy): `shared.contact.erased`, `shared.company.merged`, suspension and revocation consumers, `outreach` consent handling.
- **Active** (skip when the module isn't entitled): everything else, including CRM's timeline consumers.

**Consequence.** A deal won under an expired Customer Expansion subscription still creates the contract, so the MRR record stays whole. By contrast, CRM timeline entries are not created while CRM is not entitled. Whether they should be is covered by D-02.
