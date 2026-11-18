# Abuse reports

Anyone can report misuse from the public **Report abuse** page (5 reports per hour per address). Reports appear in Admin → **Abuse reports** for Super Admin and Platform Support. `AbuseReportsWaiting` fires when one has waited a day.

## Triage

1. **Start review** so others know it is taken.
2. Identify the workspace if you can, from the reported URL (for example a chatbot or tracking script ID) or from the details. Record its ID on the report.
3. Decide, using the acceptable use policy that counsel provides. When unsure, ask the product owner. For anything that may be illegal, also ask counsel before acting.

| Finding | Action |
|---|---|
| Not abuse, or not ours | **Dismiss**, with the decision recorded |
| Abuse, minor or first time | Contact the workspace owner; **Actioned**, with what was done |
| Abuse, serious or repeated | Suspend the workspace (Admin → Tenants → Suspend, with the reason); **Actioned** |
| Spam sent through Outreach | Also add global suppressions in Outreach rules |
| Possible exposure of data | Follow [data-breach](data-breach.md) |

The reporter is not told anything about the workspace. If they gave an email address and need a reply, reply only with whether it was handled, after counsel has agreed the wording for these replies.

## Records

Each decision is stored on the report with who made it and when, and suspensions are in the audit log. How long reports are kept is in the [retention table](../legal/review-pack.md#retention), and is for counsel to confirm.
