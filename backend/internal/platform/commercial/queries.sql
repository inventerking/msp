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
INSERT INTO platform.plans (product_version_id, key, name, billing_period, trial_days, status)
VALUES (@product_version_id, @key, @name, @billing_period, @trial_days, 'active')
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
