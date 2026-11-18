-- name: ListModules :many
SELECT id, key, name, description, status FROM platform.modules ORDER BY sort_order;

-- name: GetModuleByKey :one
SELECT id, key, name, description, status FROM platform.modules WHERE key = @key;

-- name: GetEffectiveEntitlement :one
SELECT e.status, e.effective_until
FROM platform.tenant_module_entitlements e
JOIN platform.modules m ON m.id = e.module_id
WHERE e.tenant_id = @tenant_id AND m.key = @module_key;

-- name: ListEntitlements :many
SELECT m.key, COALESCE(e.status, 'inactive')::text AS status, e.effective_until,
       EXISTS (
         SELECT 1 FROM platform.entitlement_grants g
         WHERE g.tenant_id = @tenant_id AND g.module_id = m.id AND g.status = 'pending'
       ) AS has_pending
FROM platform.modules m
LEFT JOIN platform.tenant_module_entitlements e ON e.module_id = m.id AND e.tenant_id = @tenant_id
WHERE m.status <> 'retired'
ORDER BY m.sort_order;

-- name: ListProducts :many
SELECT p.id, p.key, p.kind, p.name, p.description, p.status,
       v.id AS version_id, v.version, v.status AS version_status,
       COALESCE(ARRAY(
         SELECT m.key::text FROM platform.product_version_modules pvm
         JOIN platform.modules m ON m.id = pvm.module_id
         WHERE pvm.product_version_id = v.id ORDER BY m.sort_order
       ), '{}')::text[] AS modules
FROM platform.products p
JOIN LATERAL (
  SELECT * FROM platform.product_versions pv
  WHERE pv.product_id = p.id AND (NOT @published_only::boolean OR pv.status = 'published')
  ORDER BY pv.version DESC LIMIT 1
) v ON true
WHERE (NOT @published_only::boolean OR p.status = 'active')
ORDER BY p.kind, p.name;

-- name: GetProductByVersion :one
SELECT p.id, p.key, p.kind, p.name, p.description, p.status,
       v.id AS version_id, v.version, v.status AS version_status,
       COALESCE(ARRAY(
         SELECT m.key::text FROM platform.product_version_modules pvm
         JOIN platform.modules m ON m.id = pvm.module_id
         WHERE pvm.product_version_id = v.id ORDER BY m.sort_order
       ), '{}')::text[] AS modules
FROM platform.product_versions v
JOIN platform.products p ON p.id = v.product_id
WHERE v.id = @version_id;

-- name: CreateProduct :one
INSERT INTO platform.products (key, kind, name, description, status)
VALUES (@key, @kind, @name, @description, 'active') RETURNING id;

-- name: NextProductVersion :one
SELECT COALESCE(MAX(version), 0)::int + 1 AS next FROM platform.product_versions WHERE product_id = @product_id;

-- name: CreateProductVersion :one
INSERT INTO platform.product_versions (product_id, version, status)
VALUES (@product_id, @version, 'draft') RETURNING id;

-- name: AddVersionModule :exec
INSERT INTO platform.product_version_modules (product_version_id, module_id)
SELECT @version_id, id FROM platform.modules WHERE key = @module_key;

-- name: PublishVersion :execrows
UPDATE platform.product_versions SET status = 'published', effective_from = now()
WHERE id = @version_id AND status = 'draft';

-- name: ListPlans :many
SELECT id, product_version_id, key, name, billing_period, trial_days, status FROM platform.plans ORDER BY created_at DESC;

-- name: GetPlan :one
SELECT id, product_version_id, key, name, billing_period, trial_days, status FROM platform.plans WHERE id = @id;

-- name: CreatePlan :one
INSERT INTO platform.plans (product_version_id, key, name, billing_period, trial_days, status, usage_limits, included_credits)
VALUES (@product_version_id, @key, @name, @billing_period, @trial_days, 'active', @usage_limits, @included_credits)
RETURNING id, product_version_id, key, name, billing_period, trial_days, status;

-- name: ListPrices :many
SELECT id, plan_id, currency, amount_minor, valid_from, valid_to FROM platform.prices ORDER BY valid_from DESC;

-- name: CreatePrice :one
INSERT INTO platform.prices (plan_id, currency, amount_minor, valid_from)
VALUES (@plan_id, @currency, @amount_minor, now())
RETURNING id, plan_id, currency, amount_minor, valid_from, valid_to;

-- name: EndCurrentPrice :exec
UPDATE platform.prices SET valid_to = now()
WHERE plan_id = @plan_id AND currency = @currency AND valid_to IS NULL;

-- name: CurrentPrice :one
SELECT id, amount_minor FROM platform.prices
WHERE plan_id = @plan_id AND currency = @currency AND valid_to IS NULL;

-- name: PlanModuleIDs :many
SELECT pvm.module_id FROM platform.plans pl
JOIN platform.product_version_modules pvm ON pvm.product_version_id = pl.product_version_id
WHERE pl.id = @plan_id;

-- name: CreateSubscription :one
INSERT INTO platform.subscriptions (tenant_id, plan_id, status, price_snapshot, currency)
VALUES (@tenant_id, @plan_id, 'pending_activation', @price_snapshot, @currency) RETURNING id;

-- name: GetSubscription :one
SELECT s.id, s.tenant_id, s.plan_id, pl.name AS plan_name, s.status, s.currency, s.current_period_end, s.created_at
FROM platform.subscriptions s JOIN platform.plans pl ON pl.id = s.plan_id
WHERE s.id = @id;

-- name: ListTenantSubscriptions :many
SELECT s.id, s.tenant_id, s.plan_id, pl.name AS plan_name, s.status, s.currency, s.current_period_end, s.created_at
FROM platform.subscriptions s JOIN platform.plans pl ON pl.id = s.plan_id
WHERE s.tenant_id = @tenant_id ORDER BY s.created_at DESC;

-- name: ListAllSubscriptions :many
SELECT s.id, s.tenant_id, s.plan_id, pl.name AS plan_name, s.status, s.currency, s.current_period_end, s.created_at
FROM platform.subscriptions s JOIN platform.plans pl ON pl.id = s.plan_id
ORDER BY s.created_at DESC LIMIT 200;

-- name: SetSubscriptionStatus :execrows
UPDATE platform.subscriptions SET status = @status WHERE id = @id AND status = @from_status;

-- name: CreateGrant :one
INSERT INTO platform.entitlement_grants (tenant_id, module_id, subscription_id, source, status, starts_at, ends_at, activated_by, status_reason)
VALUES (@tenant_id, @module_id, sqlc.narg(subscription_id), @source, @status, sqlc.narg(starts_at), sqlc.narg(ends_at), sqlc.narg(activated_by), sqlc.narg(status_reason))
RETURNING id;

-- name: ListGrantsForModule :many
SELECT id, source, status, starts_at, ends_at, subscription_id
FROM platform.entitlement_grants
WHERE tenant_id = @tenant_id AND module_id = @module_id
ORDER BY created_at
FOR UPDATE;

-- name: ListSubscriptionGrants :many
SELECT id, module_id, status FROM platform.entitlement_grants
WHERE tenant_id = @tenant_id AND subscription_id = @subscription_id
FOR UPDATE;

-- name: SetGrantStatus :exec
UPDATE platform.entitlement_grants
SET status = @status, status_reason = @reason, activated_by = COALESCE(sqlc.narg(activated_by), activated_by),
    starts_at = COALESCE(sqlc.narg(starts_at), starts_at), ends_at = sqlc.narg(ends_at)
WHERE id = @id;

-- name: UpsertEffective :one
INSERT INTO platform.tenant_module_entitlements (tenant_id, module_id, status, effective_from, effective_until, entitlement_version)
VALUES (@tenant_id, @module_id, @status, sqlc.narg(effective_from), sqlc.narg(effective_until), 1)
ON CONFLICT (tenant_id, module_id) DO UPDATE
SET status = EXCLUDED.status, effective_from = EXCLUDED.effective_from, effective_until = EXCLUDED.effective_until,
    entitlement_version = platform.tenant_module_entitlements.entitlement_version + 1
RETURNING status, entitlement_version;

-- name: GetEffectiveStatus :one
SELECT status FROM platform.tenant_module_entitlements WHERE tenant_id = @tenant_id AND module_id = @module_id;

-- name: InsertHistory :exec
INSERT INTO platform.entitlement_status_history (tenant_id, module_id, grant_id, from_status, to_status, actor_type, actor_id, reason)
VALUES (@tenant_id, @module_id, sqlc.narg(grant_id), sqlc.narg(from_status), @to_status, @actor_type, sqlc.narg(actor_id), sqlc.narg(reason));

-- name: GetMeterByKey :one
SELECT id, key FROM platform.usage_meters WHERE key = @key AND status = 'active';

-- name: EnsureCreditAccount :one
INSERT INTO platform.credit_accounts (tenant_id, meter_id) VALUES (@tenant_id, @meter_id)
ON CONFLICT (tenant_id, meter_id) DO UPDATE SET meter_id = EXCLUDED.meter_id RETURNING id;

-- name: AddBalance :one
UPDATE platform.credit_accounts SET balance = balance + @amount WHERE id = @id AND balance + @amount >= reserved RETURNING balance;

-- name: ReserveCredits :execrows
-- Atomic, so concurrent reservations can never overspend.
UPDATE platform.credit_accounts SET reserved = reserved + @amount WHERE id = @id AND balance - reserved >= @amount;

-- name: SettleReservation :one
UPDATE platform.credit_accounts SET reserved = reserved - @reserved_amount, balance = balance - @spent WHERE id = @id RETURNING balance;

-- name: InsertLedger :one
INSERT INTO platform.credit_ledger (tenant_id, account_id, entry_type, amount, source_module, reference_type, reference_id, idempotency_key, expires_at, actor_type, actor_id)
VALUES (@tenant_id, @account_id, @entry_type, @amount, sqlc.narg(source_module), sqlc.narg(reference_type), sqlc.narg(reference_id), @idempotency_key, sqlc.narg(expires_at), @actor_type, sqlc.narg(actor_id))
ON CONFLICT (tenant_id, idempotency_key) DO NOTHING RETURNING id;

-- name: GetReservation :one
SELECT account_id, amount, source_module FROM platform.credit_ledger
WHERE tenant_id = @tenant_id AND idempotency_key = @idempotency_key AND entry_type = 'reservation';

-- name: ListExpiredReservations :many
SELECT l.tenant_id, l.reference_id FROM platform.credit_ledger l
WHERE l.entry_type = 'reservation' AND l.expires_at < now()
  AND NOT EXISTS (SELECT 1 FROM platform.credit_ledger s WHERE s.tenant_id = l.tenant_id AND s.reference_id = l.reference_id AND s.entry_type IN ('commit', 'release'))
LIMIT 200;

-- name: ListCreditAccounts :many
SELECT m.key AS meter_key, m.name, a.balance, a.reserved FROM platform.credit_accounts a
JOIN platform.usage_meters m ON m.id = a.meter_id WHERE a.tenant_id = @tenant_id ORDER BY m.key;

-- name: ListLedger :many
SELECT id, entry_type, amount, source_module, actor_type, created_at FROM platform.credit_ledger
WHERE tenant_id = @tenant_id ORDER BY id DESC LIMIT 100;

-- name: LedgerTotals :one
SELECT COALESCE(SUM(CASE entry_type WHEN 'grant' THEN amount WHEN 'adjustment' THEN amount WHEN 'refund' THEN amount WHEN 'commit' THEN -amount WHEN 'expiry' THEN -amount ELSE 0 END), 0)::bigint AS net
FROM platform.credit_ledger WHERE account_id = @account_id;

-- name: GetModuleKey :one
SELECT key FROM platform.modules WHERE id = @id;

-- Billing: purchases, invoices, payments, renewals, dunning and reconciliation (Phase 7).

-- name: PlanTerms :one
SELECT pl.id, pl.key, pl.name, pl.billing_period, pl.trial_days, pl.usage_limits, pl.included_credits, pl.status, p.name AS product_name, p.status AS product_status
FROM platform.plans pl
JOIN platform.product_versions pv ON pv.id = pl.product_version_id
JOIN platform.products p ON p.id = pv.product_id
WHERE pl.id = @id;

-- name: PurchasablePlans :many
-- Plans a tenant may buy: active plans of active products, with a current price.
SELECT pl.id, pl.key, pl.name, pl.billing_period, pl.trial_days, pl.usage_limits, pl.included_credits, p.key AS product_key, p.name AS product_name,
  p.description AS product_description, pr.currency, pr.amount_minor
FROM platform.plans pl
JOIN platform.product_versions pv ON pv.id = pl.product_version_id
JOIN platform.products p ON p.id = pv.product_id
JOIN platform.prices pr ON pr.plan_id = pl.id AND pr.valid_to IS NULL
WHERE pl.status = 'active' AND p.status = 'active' AND pv.status = 'published'
ORDER BY p.name, pl.name, pr.currency;

-- name: GetPromotionByCode :one
SELECT id, kind, percent_off, amount_off_minor, currency, max_redemptions, valid_from, valid_to, status,
  (SELECT count(*) FROM platform.promotion_redemptions r WHERE r.promotion_id = p.id)::int AS redemptions
FROM platform.promotions p WHERE code = @code;

-- name: InsertRedemption :exec
INSERT INTO platform.promotion_redemptions (tenant_id, promotion_id, purchase_request_id) VALUES (@tenant_id, @promotion_id, @purchase_request_id);

-- name: PurchaseByKey :one
SELECT id FROM platform.purchase_requests WHERE tenant_id = @tenant_id AND idempotency_key = @idempotency_key;

-- name: InsertPurchase :one
INSERT INTO platform.purchase_requests (tenant_id, plan_id, promotion_id, quantity, quote_snapshot, status, idempotency_key, requested_by, currency, payment_method, submitted_at)
VALUES (@tenant_id, @plan_id, sqlc.narg(promotion_id), @quantity, @quote_snapshot, @status, @idempotency_key, @requested_by, @currency, @payment_method, now())
RETURNING id;

-- name: SetPurchaseInvoice :exec
UPDATE platform.purchase_requests SET invoice_id = @invoice_id WHERE id = @id;

-- name: GetPurchaseForUpdate :one
SELECT id, tenant_id, plan_id, quantity, quote_snapshot, status, requested_by, currency, payment_method, invoice_id, rejection_reason, created_at, submitted_at
FROM platform.purchase_requests WHERE id = @id FOR UPDATE;

-- name: SetPurchaseStatus :execrows
UPDATE platform.purchase_requests SET status = @status WHERE id = @id AND status = ANY(@from_statuses::text[]);

-- name: DecidePurchase :execrows
UPDATE platform.purchase_requests SET status = @status, reviewed_by = sqlc.narg(reviewed_by), reviewed_at = now(), rejection_reason = sqlc.narg(rejection_reason)
WHERE id = @id AND status = ANY(@from_statuses::text[]);

-- name: ListTenantPurchases :many
SELECT pr.id, pr.plan_id, pl.name AS plan_name, pr.quantity, pr.quote_snapshot, pr.status, pr.currency, pr.payment_method, pr.invoice_id,
  pr.rejection_reason, pr.created_at, pr.reviewed_at
FROM platform.purchase_requests pr JOIN platform.plans pl ON pl.id = pr.plan_id
WHERE pr.tenant_id = @tenant_id ORDER BY pr.created_at DESC LIMIT 100;

-- name: ReviewQueue :many
SELECT pr.id, pr.tenant_id, pr.plan_id, pl.name AS plan_name, pr.quantity, pr.quote_snapshot, pr.status, pr.currency,
  pr.payment_method, pr.invoice_id, pr.created_at, pr.submitted_at, pr.reviewed_at, pr.rejection_reason
FROM platform.purchase_requests pr
JOIN platform.plans pl ON pl.id = pr.plan_id
WHERE (sqlc.narg(status)::text IS NULL OR pr.status = sqlc.narg(status))
ORDER BY pr.submitted_at NULLS LAST, pr.created_at LIMIT 200;

-- name: OldestUnderReview :one
SELECT COALESCE(EXTRACT(EPOCH FROM now() - min(submitted_at)), 0)::float8 FROM platform.purchase_requests WHERE status = 'under_review';

-- name: NextInvoiceNumber :one
SELECT nextval('platform.invoice_number_seq')::bigint;

-- name: InsertInvoice :one
INSERT INTO platform.invoices (tenant_id, subscription_id, number, status, currency, subtotal_minor, tax_minor, total_minor, issued_at, due_at)
VALUES (@tenant_id, sqlc.narg(subscription_id), @number, 'issued', @currency, @subtotal_minor, 0, @subtotal_minor, now(), @due_at)
RETURNING id;

-- name: InsertInvoiceLine :exec
INSERT INTO platform.invoice_lines (tenant_id, invoice_id, description, quantity, unit_amount_minor, amount_minor, period_start, period_end)
VALUES (@tenant_id, @invoice_id, @description, @quantity, @unit_amount_minor, @amount_minor, sqlc.narg(period_start), sqlc.narg(period_end));

-- name: GetInvoice :one
SELECT id, tenant_id, subscription_id, number, status, currency, subtotal_minor, tax_minor, total_minor, issued_at, due_at, paid_at, created_at
FROM platform.invoices WHERE id = @id;

-- name: SetInvoiceStatus :execrows
UPDATE platform.invoices SET status = @status, paid_at = CASE WHEN @status = 'paid' THEN now() ELSE paid_at END
WHERE id = @id AND status = ANY(@from_statuses::text[]);

-- name: ListTenantInvoices :many
SELECT id, subscription_id, number, status, currency, subtotal_minor, tax_minor, total_minor, issued_at, due_at, paid_at
FROM platform.invoices WHERE tenant_id = @tenant_id ORDER BY created_at DESC LIMIT 200;

-- name: InvoiceLines :many
SELECT description, quantity, unit_amount_minor, amount_minor, period_start, period_end FROM platform.invoice_lines
WHERE invoice_id = @invoice_id ORDER BY created_at, id;

-- name: InsertPayment :one
INSERT INTO platform.payments (tenant_id, invoice_id, purchase_request_id, provider, method, status, amount_minor, currency, idempotency_key, purpose)
VALUES (@tenant_id, sqlc.narg(invoice_id), sqlc.narg(purchase_request_id), @provider, @method, 'pending', @amount_minor, @currency, @idempotency_key, @purpose)
ON CONFLICT (provider, idempotency_key) WHERE idempotency_key IS NOT NULL DO UPDATE SET updated_at = now()
RETURNING id;

-- name: SetPaymentCheckout :exec
UPDATE platform.payments SET provider_payment_ref = @ref, checkout_url = @checkout_url WHERE id = @id;

-- name: GetPaymentForUpdate :one
SELECT id, tenant_id, invoice_id, purchase_request_id, provider, provider_payment_ref, method, status, amount_minor, currency, checkout_url,
  provider_method_ref, refund_status, refunded_amount_minor, purpose
FROM platform.payments WHERE id = @id FOR UPDATE;

-- name: PaymentByRef :one
SELECT id FROM platform.payments WHERE provider = @provider AND provider_payment_ref = @ref;

-- name: SettlePayment :execrows
UPDATE platform.payments SET status = @status, failure_code = sqlc.narg(failure_code), provider_method_ref = COALESCE(sqlc.narg(method_ref), provider_method_ref),
  provider_payment_ref = COALESCE(sqlc.narg(ref), provider_payment_ref), verified_by = sqlc.narg(verified_by),
  verified_at = CASE WHEN @status = 'succeeded' THEN now() ELSE verified_at END
WHERE id = @id AND status = 'pending';

-- name: RecordRefund :exec
UPDATE platform.payments SET refund_status = @refund_status, refund_ref = sqlc.narg(refund_ref),
  refunded_amount_minor = CASE WHEN @refund_status = 'succeeded' THEN amount_minor ELSE refunded_amount_minor END,
  refunded_at = CASE WHEN @refund_status = 'succeeded' THEN now() ELSE refunded_at END,
  status = CASE WHEN @refund_status = 'succeeded' THEN 'refunded' ELSE status END
WHERE id = @id;

-- name: PaymentsForPurchase :many
SELECT id, provider, method, status, amount_minor, currency, checkout_url, refund_status, failure_code, created_at
FROM platform.payments WHERE purchase_request_id = @purchase_request_id ORDER BY created_at;

-- name: PaymentsForInvoice :many
SELECT id, provider, method, status, amount_minor, currency, checkout_url, refund_status, failure_code, created_at
FROM platform.payments WHERE invoice_id = @invoice_id ORDER BY created_at;

-- name: PendingBankTransfers :many
SELECT p.id, p.tenant_id, p.invoice_id, i.number AS invoice_number, p.amount_minor, p.currency, p.purpose, p.created_at
FROM platform.payments p LEFT JOIN platform.invoices i ON i.id = p.invoice_id
WHERE p.method = 'bank_transfer' AND p.status = 'pending' ORDER BY p.created_at;

-- name: RefundsToMake :many
SELECT p.id, p.tenant_id, p.amount_minor, p.currency, p.method, p.refund_status, p.created_at
FROM platform.payments p
WHERE p.refund_status IN ('pending','failed') ORDER BY p.created_at;

-- name: CreateSubscriptionFull :one
INSERT INTO platform.subscriptions (tenant_id, plan_id, purchase_request_id, status, price_snapshot, currency, current_period_start, current_period_end,
  trial_ends_at, payment_method, payment_provider, provider_payment_method_ref)
VALUES (@tenant_id, @plan_id, @purchase_request_id, 'pending_activation', @price_snapshot, @currency, @current_period_start, @current_period_end,
  sqlc.narg(trial_ends_at), @payment_method, sqlc.narg(payment_provider), sqlc.narg(provider_payment_method_ref))
RETURNING id;

-- name: SubscriptionForPurchase :one
SELECT id FROM platform.subscriptions WHERE purchase_request_id = @purchase_request_id;

-- name: GetSubscriptionBilling :one
SELECT s.id, s.tenant_id, s.plan_id, pl.name AS plan_name, pl.billing_period, s.status, s.currency, s.price_snapshot, s.current_period_start,
  s.current_period_end, s.trial_ends_at, s.cancel_at, s.payment_method, s.payment_provider, s.provider_payment_method_ref, s.past_due_since,
  s.dunning_attempts, s.next_retry_at, s.purchase_request_id
FROM platform.subscriptions s JOIN platform.plans pl ON pl.id = s.plan_id WHERE s.id = @id FOR UPDATE OF s;

-- name: DueSubscriptions :many
-- Renewals, trial ends, cancellations and dunning steps that are due, across tenants.
SELECT id FROM platform.subscriptions
WHERE (status IN ('active','trialing') AND current_period_end <= now())
   OR (status IN ('past_due','suspended') AND (next_retry_at <= now() OR past_due_since <= now() - interval '1 day'))
ORDER BY current_period_end LIMIT 500;

-- name: SetSubscriptionBilling :exec
UPDATE platform.subscriptions SET status = @status, current_period_start = @current_period_start, current_period_end = @current_period_end,
  past_due_since = sqlc.narg(past_due_since), dunning_attempts = @dunning_attempts, next_retry_at = sqlc.narg(next_retry_at),
  provider_payment_method_ref = COALESCE(sqlc.narg(method_ref), provider_payment_method_ref)
WHERE id = @id;

-- name: OpenInvoiceForSubscription :one
SELECT id, total_minor, currency, due_at, status FROM platform.invoices
WHERE subscription_id = @subscription_id AND status = 'issued' ORDER BY created_at DESC LIMIT 1;

-- name: OverdueBankInvoices :many
SELECT i.id, i.subscription_id FROM platform.invoices i JOIN platform.subscriptions s ON s.id = i.subscription_id
WHERE i.status = 'issued' AND i.due_at < now() AND s.status = 'active' AND s.payment_method = 'bank_transfer';

-- name: ActiveSeatLimit :one
-- The most seats any active subscription allows: plan seats per unit times quantity. Zero means no limit.
SELECT COALESCE(max(((pl.usage_limits->>'seats')::int) * COALESCE((s.price_snapshot->>'quantity')::int, 1)), 0)::int
FROM platform.subscriptions s JOIN platform.plans pl ON pl.id = s.plan_id
WHERE s.tenant_id = @tenant_id AND s.status IN ('active','trialing','past_due') AND pl.usage_limits ? 'seats';

-- name: GetSetting :one
SELECT value FROM platform.settings WHERE key = @key;

-- name: PutSetting :exec
INSERT INTO platform.settings (key, value, updated_by, updated_at) VALUES (@key, @value, sqlc.narg(updated_by), now())
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_by = EXCLUDED.updated_by, updated_at = now();

-- name: PaymentsSince :many
SELECT id, provider, provider_payment_ref, status, amount_minor, refunded_amount_minor, currency, created_at FROM platform.payments
WHERE provider = @provider AND provider_payment_ref IS NOT NULL AND created_at >= @since;

-- name: InsertReconciliationRun :one
INSERT INTO platform.reconciliation_runs (provider, window_start, window_end, status) VALUES (@provider, @window_start, @window_end, 'running') RETURNING id;

-- name: FinishReconciliationRun :exec
UPDATE platform.reconciliation_runs SET status = @status, checked = @checked, issues = @issues, error = sqlc.narg(error) WHERE id = @id;

-- name: InsertReconciliationIssue :execrows
INSERT INTO platform.reconciliation_issues (run_id, provider, provider_ref, payment_id, kind, detail)
VALUES (@run_id, @provider, @provider_ref, sqlc.narg(payment_id), @kind, @detail)
ON CONFLICT (provider, provider_ref, kind) WHERE status = 'open' DO NOTHING;

-- name: ListReconciliationRuns :many
SELECT id, provider, window_start, window_end, checked, issues, status, error, created_at FROM platform.reconciliation_runs ORDER BY created_at DESC LIMIT 30;

-- name: ListReconciliationIssues :many
SELECT id, run_id, provider, provider_ref, payment_id, kind, detail, status, resolved_by, resolved_at, note, created_at
FROM platform.reconciliation_issues WHERE (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status)) ORDER BY created_at DESC LIMIT 200;

-- name: ResolveReconciliationIssue :execrows
UPDATE platform.reconciliation_issues SET status = 'resolved', resolved_by = @resolved_by, resolved_at = now(), note = @note WHERE id = @id AND status = 'open';

-- name: BillingHealth :one
-- Counts for monitoring. Nothing here identifies a tenant.
SELECT
  (SELECT count(*) FROM platform.purchase_requests WHERE status = 'under_review')::bigint AS review_waiting,
  (SELECT COALESCE(EXTRACT(EPOCH FROM now() - min(submitted_at)), 0) FROM platform.purchase_requests WHERE status = 'under_review')::float8 AS review_oldest_seconds,
  (SELECT count(*) FROM platform.payments WHERE refund_status IN ('pending','failed'))::bigint AS refunds_pending,
  (SELECT count(*) FROM platform.payments WHERE method = 'bank_transfer' AND status = 'pending')::bigint AS bank_transfers_pending,
  (SELECT count(*) FROM platform.subscriptions WHERE status = 'past_due')::bigint AS subscriptions_past_due,
  (SELECT count(*) FROM platform.reconciliation_issues WHERE status = 'open')::bigint AS reconciliation_open,
  (SELECT COALESCE(EXTRACT(EPOCH FROM now() - max(created_at)), -1) FROM platform.reconciliation_runs WHERE status = 'completed')::float8 AS reconciliation_age_seconds;
