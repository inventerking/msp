-- +goose Up
-- Phase 7: purchases, payments, invoices, renewals and dunning, reconciliation, legal documents and
-- abuse reports. Payments are provider-neutral (D-04 is open): a fake test provider and manual bank
-- transfer exist; a real provider plugs in behind the same interface.

ALTER TABLE platform.purchase_requests
    ADD COLUMN currency char(3),
    ADD COLUMN payment_method text,
    ADD COLUMN invoice_id uuid,
    ADD COLUMN submitted_at timestamptz,
    ADD CONSTRAINT purchase_requests_ck3 CHECK (payment_method IS NULL OR payment_method IN ('card','bank_transfer','none'));

ALTER TABLE platform.payments
    ADD COLUMN idempotency_key text,
    ADD COLUMN checkout_url text,
    ADD COLUMN provider_method_ref text,
    ADD COLUMN failure_code text,
    ADD COLUMN refund_ref text,
    ADD COLUMN refund_status text,
    ADD COLUMN refunded_amount_minor bigint NOT NULL DEFAULT 0,
    ADD COLUMN refunded_at timestamptz,
    ADD COLUMN purpose text NOT NULL DEFAULT 'purchase',
    ADD CONSTRAINT payments_ck3 CHECK (refund_status IS NULL OR refund_status IN ('pending','succeeded','failed')),
    ADD CONSTRAINT payments_ck4 CHECK (refunded_amount_minor BETWEEN 0 AND amount_minor),
    ADD CONSTRAINT payments_ck5 CHECK (purpose IN ('purchase','renewal'));
CREATE UNIQUE INDEX payments_idem_uq ON platform.payments (provider, idempotency_key) WHERE idempotency_key IS NOT NULL;
CREATE UNIQUE INDEX payments_ref_uq ON platform.payments (provider, provider_payment_ref) WHERE provider_payment_ref IS NOT NULL;
CREATE INDEX payments_status_idx ON platform.payments (status, created_at);

-- Dunning state. A saved payment method is a provider's reference only: card details never reach us.
ALTER TABLE platform.subscriptions
    ADD COLUMN payment_method text,
    ADD COLUMN payment_provider text,
    ADD COLUMN provider_payment_method_ref text,
    ADD COLUMN past_due_since timestamptz,
    ADD COLUMN dunning_attempts int NOT NULL DEFAULT 0,
    ADD COLUMN next_retry_at timestamptz,
    ADD CONSTRAINT subscriptions_ck2 CHECK (payment_method IS NULL OR payment_method IN ('card','bank_transfer','none')),
    ADD CONSTRAINT subscriptions_ck3 CHECK (dunning_attempts >= 0);
CREATE INDEX subscriptions_renewal_idx ON platform.subscriptions (current_period_end) WHERE status IN ('active','trialing','past_due');

-- Invoice numbers are platform-wide and gap-tolerant; a sequence never hands out the same number twice.
CREATE SEQUENCE platform.invoice_number_seq;
GRANT USAGE ON SEQUENCE platform.invoice_number_seq TO msp_platform;

-- The fake payment provider's own records, standing in for a provider's ledger so reconciliation
-- has something to compare against. Used only when fake providers are on.
CREATE TABLE integration.fake_payment_ledger (
    ref text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    kind text NOT NULL,
    status text NOT NULL,
    amount_minor bigint NOT NULL,
    currency char(3) NOT NULL,
    refunded_minor bigint NOT NULL DEFAULT 0,
    method_ref text,
    idempotency_key text NOT NULL,
    description text,
    PRIMARY KEY (ref),
    CONSTRAINT fake_payment_ledger_ck1 CHECK (kind IN ('checkout','charge')),
    CONSTRAINT fake_payment_ledger_ck2 CHECK (status IN ('open','succeeded','failed'))
);
CREATE UNIQUE INDEX fake_payment_ledger_idem_uq ON integration.fake_payment_ledger (idempotency_key);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON integration.fake_payment_ledger FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
GRANT SELECT, INSERT, UPDATE ON integration.fake_payment_ledger TO msp_platform;

CREATE TABLE platform.reconciliation_runs (
    id uuid NOT NULL DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    provider text NOT NULL,
    window_start timestamptz NOT NULL,
    window_end timestamptz NOT NULL,
    checked int NOT NULL DEFAULT 0,
    issues int NOT NULL DEFAULT 0,
    status text NOT NULL,
    error text,
    PRIMARY KEY (id),
    CONSTRAINT reconciliation_runs_ck1 CHECK (status IN ('running','completed','failed'))
);
CREATE TABLE platform.reconciliation_issues (
    id uuid NOT NULL DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    run_id uuid NOT NULL REFERENCES platform.reconciliation_runs (id) ON DELETE CASCADE,
    provider text NOT NULL,
    provider_ref text NOT NULL,
    payment_id uuid,
    kind text NOT NULL,
    detail jsonb NOT NULL DEFAULT '{}',
    status text NOT NULL DEFAULT 'open',
    resolved_by uuid,
    resolved_at timestamptz,
    note text,
    PRIMARY KEY (id),
    CONSTRAINT reconciliation_issues_ck1 CHECK (kind IN ('missing_locally','missing_at_provider','amount_mismatch','status_mismatch','refund_mismatch')),
    CONSTRAINT reconciliation_issues_ck2 CHECK (status IN ('open','resolved'))
);
-- An issue is reported once while open, however often reconciliation runs.
CREATE UNIQUE INDEX reconciliation_issues_open_uq ON platform.reconciliation_issues (provider, provider_ref, kind) WHERE status = 'open';
CREATE INDEX reconciliation_issues_run_idx ON platform.reconciliation_issues (run_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON platform.reconciliation_runs FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON platform.reconciliation_issues FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();

-- Legal documents. The text is supplied by counsel (D-19); the platform only publishes versions and
-- records who accepted which. Nothing here claims the text is reviewed unless an admin says so.
CREATE TABLE platform.legal_documents (
    id uuid NOT NULL DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    kind text NOT NULL,
    version int NOT NULL,
    title text NOT NULL,
    body text NOT NULL,
    status text NOT NULL,
    requires_acceptance boolean NOT NULL,
    counsel_reviewed boolean NOT NULL DEFAULT false,
    published_at timestamptz,
    published_by uuid,
    created_by uuid,
    PRIMARY KEY (id),
    CONSTRAINT legal_documents_ck1 CHECK (kind IN ('terms','privacy','dpa','acceptable_use','subprocessors')),
    CONSTRAINT legal_documents_ck2 CHECK (status IN ('draft','published','retired')),
    CONSTRAINT legal_documents_ck3 CHECK (length(title) BETWEEN 1 AND 200 AND length(body) BETWEEN 1 AND 200000),
    CONSTRAINT legal_documents_ck4 CHECK (version > 0)
);
CREATE UNIQUE INDEX legal_documents_uq1 ON platform.legal_documents (kind, version);
-- At most one published version of each kind.
CREATE UNIQUE INDEX legal_documents_published_uq ON platform.legal_documents (kind) WHERE status = 'published';
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON platform.legal_documents FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();
GRANT SELECT ON platform.legal_documents TO msp_app;

-- Who accepted which version. A DPA is accepted for a workspace; other documents by a person.
CREATE TABLE identity.document_acceptances (
    id uuid NOT NULL DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE CASCADE,
    workspace_id uuid REFERENCES identity.tenants (id) ON DELETE CASCADE,
    document_id uuid NOT NULL REFERENCES platform.legal_documents (id) ON DELETE RESTRICT,
    PRIMARY KEY (id)
);
CREATE UNIQUE INDEX document_acceptances_uq1 ON identity.document_acceptances (user_id, document_id, COALESCE(workspace_id, '00000000-0000-0000-0000-000000000000'::uuid));
CREATE INDEX document_acceptances_document_idx ON identity.document_acceptances (document_id);
ALTER TABLE identity.document_acceptances ENABLE ROW LEVEL SECURITY;
ALTER TABLE identity.document_acceptances FORCE ROW LEVEL SECURITY;
CREATE POLICY own_acceptances ON identity.document_acceptances TO msp_app
    USING (user_id = NULLIF(current_setting('app.user_id', true), '')::uuid)
    WITH CHECK (user_id = NULLIF(current_setting('app.user_id', true), '')::uuid);
CREATE POLICY platform_read ON identity.document_acceptances FOR SELECT TO msp_platform USING (true);
GRANT SELECT, INSERT ON identity.document_acceptances TO msp_app;
GRANT SELECT ON identity.document_acceptances TO msp_platform;

-- Abuse reports from anyone, reviewed by platform admins. The reporter's contact is optional.
CREATE TABLE platform.abuse_reports (
    id uuid NOT NULL DEFAULT uuidv7(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    category text NOT NULL,
    subject_url text,
    details text NOT NULL,
    reporter_email text,
    target_tenant_id uuid REFERENCES identity.tenants (id) ON DELETE SET NULL,
    status text NOT NULL DEFAULT 'new',
    decision text,
    reviewed_by uuid,
    reviewed_at timestamptz,
    PRIMARY KEY (id),
    CONSTRAINT abuse_reports_ck1 CHECK (category IN ('spam','phishing','malware','privacy','harassment','unauthorized_scanning','other')),
    CONSTRAINT abuse_reports_ck2 CHECK (status IN ('new','reviewing','actioned','dismissed')),
    CONSTRAINT abuse_reports_ck3 CHECK (length(details) BETWEEN 1 AND 5000 AND (subject_url IS NULL OR length(subject_url) <= 2000)),
    CONSTRAINT abuse_reports_ck4 CHECK (decision IS NULL OR length(decision) <= 2000)
);
CREATE INDEX abuse_reports_status_idx ON platform.abuse_reports (status, created_at);
CREATE INDEX abuse_reports_target_idx ON platform.abuse_reports (target_tenant_id);
CREATE TRIGGER touch_updated_at BEFORE UPDATE ON platform.abuse_reports FOR EACH ROW EXECUTE FUNCTION shared.touch_updated_at();

-- Tenants read their own billing records; every change is made by platform code.
GRANT SELECT ON platform.purchase_requests, platform.invoices, platform.invoice_lines, platform.payments TO msp_app;
GRANT SELECT, INSERT, UPDATE ON platform.reconciliation_runs, platform.reconciliation_issues, platform.legal_documents, platform.abuse_reports TO msp_platform;

-- The renewal and dunning policy, editable by admins (D-18). Days are counted from the due date.
INSERT INTO platform.settings (key, value) VALUES
    ('dunning_policy', '{"retry_days": [1, 3, 7], "suspend_after_days": 14, "expire_after_days": 30, "bank_transfer_due_days": 14, "review_target_hours": 24}');

INSERT INTO integration.providers (key, kind, name, capabilities, tenant_connectable, status) VALUES
    ('fake_payments', 'payment', 'Test payments (no real money)', '{"checkout": true, "saved_methods": true, "refunds": true}', false, 'enabled'),
    ('manual_payments', 'payment', 'Bank transfer, confirmed by an admin', '{"refunds": false}', false, 'enabled');

INSERT INTO notification.templates (key, channel, locale, subject, body) VALUES
    ('billing.purchase_approved', 'in_app', 'en', NULL, 'Your purchase of {{.plan}} was approved. The modules are active.'),
    ('billing.purchase_approved', 'email', 'en', 'Your purchase was approved', 'Your purchase of {{.plan}} was approved. The modules are active.'),
    ('billing.purchase_rejected', 'in_app', 'en', NULL, 'Your purchase of {{.plan}} was not approved. {{.refund}}'),
    ('billing.purchase_rejected', 'email', 'en', 'Your purchase was not approved', 'Your purchase of {{.plan}} was not approved. {{.refund}}'),
    ('billing.payment_failed', 'in_app', 'en', NULL, 'A payment of {{.amount}} for {{.plan}} failed. We will try again on {{.retry}}.'),
    ('billing.payment_failed', 'email', 'en', 'A payment failed', 'A payment of {{.amount}} for {{.plan}} failed. We will try again on {{.retry}}. You can update the payment method in your workspace.'),
    ('billing.subscription_suspended', 'in_app', 'en', NULL, 'Your {{.plan}} subscription is suspended for non-payment. Pay the open invoice to restore access.'),
    ('billing.subscription_suspended', 'email', 'en', 'Your subscription is suspended', 'Your {{.plan}} subscription is suspended for non-payment. Pay the open invoice to restore access.');

-- +goose Down
DELETE FROM notification.templates WHERE key LIKE 'billing.%';
DELETE FROM integration.providers WHERE key IN ('fake_payments','manual_payments');
DELETE FROM platform.settings WHERE key = 'dunning_policy';
REVOKE SELECT ON platform.purchase_requests, platform.invoices, platform.invoice_lines, platform.payments FROM msp_app;
DROP TABLE IF EXISTS platform.abuse_reports CASCADE;
DROP TABLE IF EXISTS identity.document_acceptances CASCADE;
DROP TABLE IF EXISTS platform.legal_documents CASCADE;
DROP TABLE IF EXISTS platform.reconciliation_issues CASCADE;
DROP TABLE IF EXISTS platform.reconciliation_runs CASCADE;
DROP TABLE IF EXISTS integration.fake_payment_ledger CASCADE;
DROP SEQUENCE IF EXISTS platform.invoice_number_seq;
DROP INDEX IF EXISTS platform.subscriptions_renewal_idx;
ALTER TABLE platform.subscriptions DROP CONSTRAINT IF EXISTS subscriptions_ck3, DROP CONSTRAINT IF EXISTS subscriptions_ck2,
    DROP COLUMN IF EXISTS next_retry_at, DROP COLUMN IF EXISTS dunning_attempts, DROP COLUMN IF EXISTS past_due_since,
    DROP COLUMN IF EXISTS provider_payment_method_ref, DROP COLUMN IF EXISTS payment_provider, DROP COLUMN IF EXISTS payment_method;
DROP INDEX IF EXISTS platform.payments_status_idx;
DROP INDEX IF EXISTS platform.payments_ref_uq;
DROP INDEX IF EXISTS platform.payments_idem_uq;
ALTER TABLE platform.payments DROP CONSTRAINT IF EXISTS payments_ck5, DROP CONSTRAINT IF EXISTS payments_ck4, DROP CONSTRAINT IF EXISTS payments_ck3,
    DROP COLUMN IF EXISTS purpose, DROP COLUMN IF EXISTS refunded_at, DROP COLUMN IF EXISTS refunded_amount_minor, DROP COLUMN IF EXISTS refund_status,
    DROP COLUMN IF EXISTS refund_ref, DROP COLUMN IF EXISTS failure_code, DROP COLUMN IF EXISTS provider_method_ref, DROP COLUMN IF EXISTS checkout_url, DROP COLUMN IF EXISTS idempotency_key;
ALTER TABLE platform.purchase_requests DROP CONSTRAINT IF EXISTS purchase_requests_ck3, DROP COLUMN IF EXISTS submitted_at,
    DROP COLUMN IF EXISTS invoice_id, DROP COLUMN IF EXISTS payment_method, DROP COLUMN IF EXISTS currency;
