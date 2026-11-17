# Metric definitions

Status: **for review at the Phase 6 gate.** The roadmap's gate for this phase is a review of these definitions, because disputed definitions found late are a known risk.

The definitions live once, in code (`backend/internal/modules/salesintel/domain`), and every one has a test with worked numbers. The app shows the same text on the Sales Intelligence **Definitions** tab. Changing a definition means changing it there, with its test, so the app and this page cannot drift.

## Rules every metric follows

- **Value kinds.** Every number is **actual**, **estimated** or **forecast**. The kind is part of each value, every screen labels it in words, and estimates and forecasts are styled differently from actual values. No total, chart or report adds values of different kinds together.
- **Months.** Metrics are per calendar month, in UTC, for up to 24 months back.
- **Currency.** Each amount keeps its own currency, and is converted to the workspace's reporting currency at the latest approved rate on or before the event's date (D-11). If there is none, the amount is **left out** of the total and the value is flagged "some amounts left out". It is never guessed.
- **Empty divisions.** A rate with nothing to divide by, such as a win rate in a month when no deal closed, has **no value**. It is not shown as zero.
- **Deals use their current state.** A deal counts as won only if it is won now, in the month it was won. A deal that was won and then reopened no longer counts. Events that arrive out of order give the same result.
- **History.** A workspace that buys Sales Intelligence later still sees its history, because the events are recorded for every workspace (ADR-014).
- **Freshness.** Each screen shows when its numbers were computed and the latest activity they include. Metrics are recomputed a few seconds after new activity, and at least hourly.
- **Privacy.** Facts hold IDs and amounts only. Team performance shows individuals, so it needs the `sales_intel.team` permission, and every view of it is recorded in the audit log. Metrics are never compared across workspaces.

## Revenue

MRR comes only from contracts in Customer Expansion (D-03), never from a deal's estimate. Contracts are recorded for every workspace, whether or not it has Customer Expansion.

| Metric | Kind | Definition |
|---|---|---|
| MRR | Actual | Sum of active contracts' monthly recurring revenue at the end of the month. |
| ARR | Actual | MRR × 12. |
| New MRR | Actual | MRR from a customer's first contract, started in the month. |
| Expansion MRR | Actual | MRR added by existing customers: increases, and further contracts. |
| Reactivation MRR | Actual | MRR from former customers starting a contract again. |
| Contraction MRR | Actual | MRR lost from customers who stay: decreases, and a contract ending while others continue. Shown as a positive amount. |
| Churned MRR | Actual | MRR lost when a customer's last active contract ends. Shown as a positive amount. |
| Net new MRR | Actual | New + expansion + reactivation − contraction − churn. Over any period, it equals the change in MRR. |
| New customers | Actual | Customers whose first contract started in the month. |
| Churned customers | Actual | Customers whose last active contract ended in the month. |

**How a contract change is classified:**

| Change | Movement |
|---|---|
| First active contract for the customer | New |
| Another contract while one is active | Expansion |
| A contract for a customer who had one before but has none now | Reactivation |
| MRR goes up or down on an active contract | Expansion or contraction |
| A contract ends, and the customer has another active one | Contraction |
| A contract ends, and it was the customer's last | Churn |
| A won deal is reopened | Its contract returns to draft, which counts as the contract ending |

## Deals

| Metric | Kind | Definition |
|---|---|---|
| Deals won, deals lost | Actual | Deals whose current status is won (or lost), counted in the month they were won (or lost). By team member and by source. |
| Win rate | Actual | Won ÷ (won + lost), for deals closed in the month. |
| Won deal MRR | Actual | The expected MRR recorded on deals won in the month, converted at the win date. This is what the deals said, not contract revenue. |
| Meeting conversion | Actual | Of deals whose first meeting was in the month, the share now won. |
| Proposals issued | Actual | Proposals first issued in the month. |
| Proposal conversion | Actual | Of proposals first issued in the month, the share signed. |
| Deals created | Actual | Deals created in the month. |
| Deals reaching each stage | Actual | Of deals created in the month, how many reached each stage. |
| Open deals | Actual | Deals open now (current month only). |
| Weighted pipeline | Estimated | Sum of contract value × probability on open deals, now. A deal with only an MRR is valued at 12 months of it. Converted at today's rate. |

## Leads and campaigns

| Metric | Kind | Definition |
|---|---|---|
| Leads | Actual | Prospects added, chatbot leads, and website goal conversions in the month, by source. |
| Leads qualified, disqualified, converted | Actual | Prospects marked so in the month, by the source they came from. |
| Qualification rate | Actual | Of prospects added in the month, the share since qualified. |
| Emails sent, bounces, replies | Actual | Outreach events in the month, in total and by campaign. |
| Unsubscribes | Actual | People who unsubscribed in the month. Each person counts once, however often they click. |
| Reply rate, bounce rate | Actual | Replies ÷ emails sent, and bounces ÷ emails sent, in the month. |
| Campaign ROI | Estimated | For campaigns with costs entered: (attributed won MRR × 12 − cost) ÷ cost. A won deal is attributed to a campaign when its company replied to that campaign in the 90 days before the win. |

## Costs

Costs are the only figures a workspace types in, because the platform cannot know marketing spend.

| Metric | Kind | Definition |
|---|---|---|
| Costs | Actual | Costs entered, spread evenly over each entry's days, and converted at its start date. |
| Customer acquisition cost (CAC) | Estimated | Costs in the month ÷ new customers in the month. Shown only for months with costs entered. It is an estimate because costs are entered by hand. |

## Forecasts

Forecasts are stored apart from metrics and are always labelled **forecast**. Each run records its method version (`v1`) and its assumptions, and the Forecasts screen shows both. AI never produces a forecast number.

**Revenue forecast (MRR).** Needs at least **3 complete months** of contract history; with less, it is refused and the screen says so. It starts from actual MRR now, then for each month ahead:

> expected = last month × (1 + average monthly expansion rate − average monthly churn rate) − MRR of contracts due to end that month without auto-renewal + weighted MRR of open deals expected to close that month

- The rates are averages over the complete months of history, as a share of MRR at the start of each month.
- An open deal counts at its MRR × its chance of winning (see below). Deals with no expected close date are left out and counted in the assumptions. Overdue deals count in the first month.
- The range is an 80% band from the month-to-month spread of net new MRR, widening with the square root of the months ahead.
- **It leans low.** Historical churn can include contracts that ended on schedule, and scheduled ends are subtracted as well.

**Pipeline forecast (expected won value).** For each month ahead: the sum over open deals expected to close that month of value × chance of winning. Value is the contract value, or 12 × MRR when only MRR is known. The range is an 80% band from the uncertainty of each deal closing.

**A deal's chance of winning** is its stage's historical win rate (of closed deals that reached that stage, the share won), once at least **10** closed deals reached the stage. Below that, it is the deal's own probability from CRM.

## AI commentary

AI may write commentary on the metrics for a screen. It receives the metric names, months and values only: no names, contacts or deals. It must cite the metrics it uses, and every number in its text must appear in a metric it cites. If not, the commentary is rejected and nothing is kept. Commentary is labelled as AI-written.

## Customer Expansion rules

These are not Sales Intelligence metrics, but they produce figures and labels that people act on.

| Rule | Definition |
|---|---|
| Renewal alerts | At 90, 60 and 30 days before a contract's end, and before its notice deadline (end minus notice days). Only the tightest window reached is raised, so a contract first seen 20 days out gets the 30-day alert. Each alert is raised once and goes to the contract's owner. |
| Health status | From four indicators about the business relationship only (never about people): active MRR, how soon a contract without auto-renewal ends (within 60 days is a concern), share of known services the customer has, and the latest IT assessment's risk. No known indicator: unknown. One concern: watch. Two or more: at risk. A status set by hand is kept until cleared. |
| Service gaps | "Does not have" only when coverage records say so. **Unknown is never treated as absent**: it becomes a request to confirm, with no revenue estimate. Declined services are not suggested. |
| Renewal suggestions | For contracts ending within 90 days. |
| Estimated MRR on a suggestion | The median that the workspace's other customers pay for that service. Labelled as an estimate, and never counted as revenue. |
| Repeated suggestions | Running analysis again updates the same suggestion instead of adding one. A dismissed suggestion is not repeated for 90 days. One no longer supported by the facts is marked "no longer supported". |
| AI wording | AI may reword a suggestion's rationale, but the wording is kept only if it has exactly the same numbers and dates, and, for unknown coverage, makes no claim that the customer lacks the service. Otherwise the rule wording stays. |

## Questions for the review

Each of these is a choice made to build the phase. They are easy to change now and harder later.

1. **Contraction or churn.** Is a contract ending while the customer has others contraction, as built, or churn of that contract?
2. **Won deal MRR.** Is it useful next to contract MRR, or confusing? It is the deal's figure, not revenue.
3. **Conversion cohorts.** Meeting and proposal conversion are counted by the month of the first meeting or first issue, so recent months rise as deals close. Is that acceptable, or should they count by the month the outcome happened?
4. **Weighted pipeline value.** For deals with only an MRR, is 12 months the right value?
5. **CAC.** New customers only, not reactivations. Spread costs by day, converted at the start date. Correct?
6. **Campaign ROI attribution.** A reply within 90 days before the win, and 12 months of MRR as the return. Are the model, the window and the horizon acceptable? It is labelled as an estimate.
7. **Forecast minimum.** Three months of history before a revenue forecast. Enough?
8. **Forecast caution.** The revenue forecast leans low (see above). Acceptable, or should historical churn exclude scheduled ends?
9. **Stage win rates.** Ten closed deals per stage before using the stage's own rate. Enough?
10. **Health thresholds.** Within 60 days of a non-renewing end is a concern; one concern is watch, two at risk. Right for MSPs?
11. **Reporting currency and rates.** New workspaces report in USD until changed in Settings. Rates come from the reference rate table, which nothing fills yet: until an exchange-rate source is chosen (D-05, D-11), amounts in other currencies are left out and flagged. Is "latest rate on or before the date" acceptable when a day has no rate?
