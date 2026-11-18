# Suspected data breach

Use this whenever personal or tenant data **may** have been seen by someone who should not have seen it: another tenant, an outsider, or a provider outside its role. Act on suspicion; do not wait for certainty.

This runbook covers the technical response. **Whether, when and whom to notify is decided with legal counsel.** Some laws and contracts set deadlines counted in hours from when you become aware, so involve counsel at step 1, not at the end.

## 1. First hour

1. Name an incident lead and open a timeline (see [incident](incident.md)). Record when the issue was first noticed and by whom: notification deadlines may run from that time.
2. Tell counsel and the product owner. Until handover this means the user's boss (see [deployment plan](../../implementation/implementation-roadmap.md)).
3. **Preserve evidence before changing anything:**
   ```bash
   cd deploy/testing
   mkdir -p ~/incident-$(date -u +%Y%m%d) && cd ~/incident-$(date -u +%Y%m%d)
   docker compose -f /path/to/deploy/testing/compose.yaml logs --since 72h --timestamps > logs.txt
   ```
   Also export the audit log for the period from Admin → Audit, and take a base backup (`./backup.sh`), which keeps the database state as it was.
4. **Contain** with the narrowest action that stops it:
   - a feature or route: disable the provider or the module entitlement, or deploy a fix that blocks the route;
   - an account: suspend the tenant, or reset the user;
   - a leaked secret: rotate it ([key-rotation](key-rotation.md)).

## 2. Scope (first day)

Answer, with evidence, and record the sources used:

| Question | Where to look |
|---|---|
| What data, of which tenants? | Audit log (`audit.events`), request logs (route templates and request IDs), the code path |
| Whose personal data, and what kinds? | The [legal review pack's data map](../legal/review-pack.md#data-map) for what each table holds |
| From when to when? | First and last occurrences in logs and audit |
| Who received it? | Session and actor IDs in audit; provider logs if a provider was involved |
| Is it still happening? | Re-test after containment |

Request logs omit paths, IP addresses and bodies by design, so the audit log is the main record of who accessed what. If it is not enough, say so in the findings rather than guess.

## 3. Decide and notify

Counsel decides whether the incident must be notified to authorities, to the affected tenants or to individuals, and by when. Prepare for them: the facts from step 2, the containment, and the timeline. Do not contact affected people or authorities, and do not publish anything, until counsel has approved the wording.

## 4. Fix and learn

- Fix the cause, and add a test that would have caught it. For any cross-tenant exposure, add a case to the tenant isolation suite ([isolation_test.go](../../../backend/internal/api/isolation_test.go)).
- Write the review, as in [incident](incident.md). Keep the evidence for as long as counsel advises.
