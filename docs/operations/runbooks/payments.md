# Payments, refunds and reconciliation

Payments are provider-neutral (D-04 still open): card payments go through the payment provider interface, which on the testing server is the **fake** provider, and bank transfers are confirmed by an admin. Charging happens at submission, and a rejected purchase is refunded (D-01).

Admin → **Billing** holds every queue below. Only Super Admin and Billing Admin can act; Platform Analyst can view.

## Purchase review queue

Each paid or free purchase waits for review. `PurchaseReviewOverdue` fires when the oldest waits longer than the target in the dunning policy.

- **Approve** activates the plan's modules and grants its included credits. The customer is notified.
- **Reject** needs a reason (shown to the customer). A card payment is refunded through the provider at once; a bank transfer becomes a manual refund in the Refunds list.

## Bank transfers

Mark a transfer paid **only after the money is in the bank account**, and record the bank's reference. Invoices past their due date move the subscription to past due, and then follow the dunning policy.

## Refunds

| Status | Meaning | Action |
|---|---|---|
| Pending | Requested from the provider | Wait; the provider confirms |
| Failed | The provider refused | Retry; if it keeps failing, refund outside the system and mark refunded with the reference |
| Manual refund needed | A bank transfer | Pay it back from the bank, then mark refunded with the bank reference |

`RefundNotCompleted` fires if one stays open for a day.

## Failed renewals (dunning)

Settings → Failed payment rules sets the card retry days, when a past-due subscription is suspended, and when it ends. The customer is notified at each step and can pay the open invoice from Subscription → Invoices. Paying recovers a suspended subscription.

## Reconciliation

Runs daily, comparing our payments with the provider's for the last period. Each difference is listed once:

| Difference | Usual cause | Action |
|---|---|---|
| Missing in our records | A webhook never arrived | Check the provider's dashboard; if real, apply it (contact engineering) |
| Missing at provider | A checkout created here was never completed or was deleted there | Usually harmless if still pending; check before resolving |
| Amount, status or refund differs | A partial refund or a manual change at the provider | Find which side is right, correct it, then resolve with a note |

Resolve each one with a note saying what was found and done. `ReconciliationNotRunning` fires if it has not completed in two days.

## Webhooks

The provider's webhook URL contains a secret path token, and each request is signed. A webhook that fails to verify is rejected and logged. If the provider reports failed deliveries, check that the endpoint in Admin → Integrations is active and that its secret matches.

## Before real payments

A real provider is chosen at D-04, after legal review. Until then, `fake_payments` is registered only when `APP_ENV` is local, test or testing, and the bank instructions are a placeholder. Put real bank details in only on a server cleared for real payments.
