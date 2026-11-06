-- +goose Up
CREATE TABLE audit.events (
    id uuid NOT NULL,
    tenant_id uuid,
    actor_type text NOT NULL,
    actor_id uuid,
    action text NOT NULL,
    resource_type text NOT NULL,
    resource_id uuid,
    outcome text NOT NULL,
    ip_hash bytea,
    user_agent text,
    correlation_id uuid,
    before jsonb,
    after jsonb,
    chain_seq bigint NOT NULL DEFAULT 0,
    prev_hash bytea,
    row_hash bytea NOT NULL,
    occurred_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id, occurred_at),
    CONSTRAINT events_ck1 CHECK (actor_type IN ('user','platform_admin','system','public')),
    CONSTRAINT events_ck2 CHECK (outcome IN ('success','denied','failed'))
) PARTITION BY RANGE (occurred_at);

CREATE TABLE audit.security_events (
    id uuid NOT NULL,
    tenant_id uuid,
    event_type text NOT NULL,
    severity text NOT NULL,
    actor_ref text,
    ip_hash bytea,
    detail jsonb NOT NULL,
    correlation_id uuid,
    occurred_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id, occurred_at),
    CONSTRAINT security_events_ck1 CHECK (severity IN ('info','warning','high','critical'))
) PARTITION BY RANGE (occurred_at);

CREATE TABLE audit.support_access_grants (
    id uuid NOT NULL DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    admin_id uuid NOT NULL,
    reason text NOT NULL,
    scope text NOT NULL,
    ticket_ref text,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, id),
    CONSTRAINT support_access_grants_ck1 CHECK (scope IN ('metadata','read_only'))
);

ALTER TABLE audit.support_access_grants ADD CONSTRAINT support_access_grants_tenant_id_fk FOREIGN KEY (tenant_id) REFERENCES identity.tenants (id) ON DELETE RESTRICT;

CREATE INDEX events_ix1 ON audit.events (tenant_id, occurred_at DESC);
CREATE INDEX events_ix2 ON audit.events (resource_type, resource_id, occurred_at DESC);
CREATE INDEX security_events_ix1 ON audit.security_events (event_type, occurred_at DESC);

-- +goose Down
DROP TABLE IF EXISTS audit.support_access_grants CASCADE;
DROP TABLE IF EXISTS audit.security_events CASCADE;
DROP TABLE IF EXISTS audit.events CASCADE;
