# Risk register

Status: planning, pending approval. Likelihood and impact are judgement estimates (L, M, H). Security-specific threats are in the [threat model](../security/threat-model.md).

| ID | Risk | L | I | Mitigation | Owner role |
|---|---|---|---|---|---|
| R1 | **Scope.** Ten modules at once is very large, even with a solid foundation | H | H | Phase gates; consider launching in stages by module; resist unrequested features | Product owner |
| R2 | **Company data** licensing or quality doesn't support the promised lead generation | H | H | Evaluate providers before Phase 3; check reuse, export and retention terms; keep a CSV path | Product, Legal |
| R3 | **Outreach law**: cold email restrictions limit value in key markets | H | H | Early legal input; jurisdiction rules; market prioritization | Legal, Product |
| R4 | **Sender reputation**: tenants' behaviour harms shared infrastructure | H | H | Tenants use own domains; separate streams; thresholds; review | Engineering, Ops |
| R5 | **Assessment legal exposure**: unauthorized scanning claims | M | H | Tiers, evidence, non-invasive scope, legal review | Legal, Security |
| R6 | **Cross-tenant leak** would be severe for trust | M | H | Layered isolation, generated tests | Engineering |
| R7 | **Payment and tax** complexity for worldwide sales | H | M | Evaluate merchant of record (D-04) | Product, Finance |
| R8 | **Admin activation bottleneck**: payers wait for manual review | M | M | Queue target, alerts, optional auto-approval policy later (a change to a confirmed decision, so it would need your approval) | Product, Ops |
| R9 | **Prompt injection** causing brand-damaging chatbot responses | H | M | Scope limits, test suite, disclaimers, monitoring | Engineering |
| R10 | **AI cost** exceeds pricing assumptions | M | H | Budgets, per-task limits, cost reports before finalizing credit prices | Product, Engineering |
| R11 | **AI provider** terms or availability change | M | M | Abstraction, approved fallback | Engineering |
| R12 | **Single database** contention, from visitor and analytics load | M | H | Ingest role, partitions, pools, rollups; read replica if justified | Engineering |
| R13 | **Module boundary erosion** under schedule pressure | H | M | Lint in CI from day one; exceptions need review | Engineering |
| R14 | **Standalone purchases** multiply the test matrix (1,024 possible module combinations) | H | M | Test each module alone, all together, and seed bundles; contract fakes rather than all combinations | Engineering |
| R15 | **Eventual consistency** confuses users, such as a delay on a timeline | M | L | "As of" labels, UI refetch, low-latency dispatch | Engineering |
| R16 | **RLS performance** on large tables | L | M | Tenant-leading indexes, load comparison in Phase 6 | Engineering |
| R17 | **99.9% target** costs more than expected | M | M | D-23; revise targets openly | Product, Ops |
| R18 | **Deliverability** undermines Outreach value, regardless of compliance | M | H | Domain guidance, limits, monitoring; set honest expectations | Product |
| R19 | **Business identification** accuracy is low or legally limited in some regions | H | M | Clear wording with confidence levels; can be disabled by region; never promise identification | Product, Legal |
| R20 | **Reply detection** varies by mailbox provider | M | M | Adapter capabilities; mailbox polling; test early | Engineering |
| R21 | **Global requirements** (language, currency, tax, time zones) touch everything | M | M | Designed in from the start; launch market order to be set | Product |
| R22 | **Team capacity unknown**, which makes phase sizes hard to translate into time | H | M | Confirm team size, then re-estimate | Product owner |
| R23 | **Late legal review** causes rework, as review now happens at handover | H | H | Build to conservative defaults; keep jurisdiction rules as data; real sending, scanning and tracking off until reviewed; offer an early look at D-09, D-10 and D-12 | Product owner, boss |
| R24 | **Canonical company matching** errors merge unrelated companies | M | M | Conservative auto-match; review queue; corrective merge | Engineering |
| R25 | **Data retention conflicts**: retention, erasure and audit needs may clash | M | M | Per-table design; legal review; anonymize rather than delete where needed | Legal |
| R26 | **Plan assumptions not validated**: the documents haven't been tested by implementation | M | M | Expect revisions; update docs at each gate | Architecture |
| R27 | **No git repository yet** in the project, so these documents have no version history | H | L | Initialize at the start of Phase 1, or sooner if you wish | Product owner |

## Top five, in order

1. **R1 scope:** consider launching in waves of modules.
2. **R2 data providers:** evaluate before committing to Phase 3 detail.
3. **R3 outreach law:** get legal input early, because it may change which markets you target first.
4. **R6 tenant isolation:** the most damaging technical failure, and it's tested from day one.
5. **R4 sending reputation:** one tenant's abuse could harm all tenants.
