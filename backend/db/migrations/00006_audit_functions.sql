-- +goose Up
CREATE TABLE audit.events_default PARTITION OF audit.events DEFAULT;
CREATE TABLE audit.security_events_default PARTITION OF audit.security_events DEFAULT;

-- +goose StatementBegin
-- The fields covered by an audit row's hash. JSON keeps the encoding unambiguous, and the epoch
-- keeps it independent of the session's time zone.
CREATE FUNCTION audit.event_hash(prev bytea, e audit.events) RETURNS bytea
LANGUAGE sql IMMUTABLE SET search_path = pg_catalog AS $$
    SELECT sha256(convert_to(jsonb_build_array(
        encode(COALESCE(prev, ''::bytea), 'hex'), e.id, e.tenant_id, e.chain_seq, e.actor_type, e.actor_id,
        e.action, e.resource_type, e.resource_id, e.outcome, extract(epoch FROM e.occurred_at)::text, e.before, e.after
    )::text, 'UTF8'));
$$;
-- +goose StatementEnd

-- +goose StatementBegin
-- Links each audit row to the previous one for its tenant, so later tampering is detectable.
-- The advisory lock serializes audit writes per tenant until commit, which makes chain_seq a
-- true order. Timestamps cannot do this, because they come from when each transaction started.
CREATE FUNCTION audit.chain_event() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, audit AS $$
DECLARE
    last_seq bigint;
    last_hash bytea;
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended('audit:' || COALESCE(NEW.tenant_id::text, 'platform'), 0));
    IF NEW.tenant_id IS NULL THEN
        SELECT chain_seq, row_hash INTO last_seq, last_hash FROM audit.events
        WHERE tenant_id IS NULL ORDER BY chain_seq DESC LIMIT 1;
    ELSE
        SELECT chain_seq, row_hash INTO last_seq, last_hash FROM audit.events
        WHERE tenant_id = NEW.tenant_id ORDER BY chain_seq DESC LIMIT 1;
    END IF;
    NEW.chain_seq := COALESCE(last_seq, 0) + 1;
    NEW.prev_hash := last_hash;
    NEW.row_hash := audit.event_hash(last_hash, NEW);
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
-- Reports the first problem in a tenant's chain: an altered row, a missing row or a broken link.
-- No rows returned means the chain is intact. A null tenant checks the platform-level chain.
CREATE FUNCTION audit.verify_chain(p_tenant uuid) RETURNS TABLE (event_id uuid, chain_seq bigint, problem text)
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path = pg_catalog, audit AS $$
DECLARE
    e audit.events;
    expected_seq bigint := 1;
    expected_prev bytea := NULL;
BEGIN
    FOR e IN SELECT * FROM audit.events a WHERE a.tenant_id IS NOT DISTINCT FROM p_tenant ORDER BY a.chain_seq LOOP
        IF e.chain_seq <> expected_seq THEN
            RETURN QUERY SELECT e.id, e.chain_seq, 'missing_row'::text;
            RETURN;
        END IF;
        IF e.prev_hash IS DISTINCT FROM expected_prev THEN
            RETURN QUERY SELECT e.id, e.chain_seq, 'broken_link'::text;
            RETURN;
        END IF;
        IF e.row_hash IS DISTINCT FROM audit.event_hash(e.prev_hash, e) THEN
            RETURN QUERY SELECT e.id, e.chain_seq, 'altered_row'::text;
            RETURN;
        END IF;
        expected_seq := expected_seq + 1;
        expected_prev := e.row_hash;
    END LOOP;
END;
$$;
-- +goose StatementEnd

CREATE INDEX events_chain_idx ON audit.events (tenant_id, chain_seq DESC);

CREATE TRIGGER chain_event BEFORE INSERT ON audit.events
    FOR EACH ROW EXECUTE FUNCTION audit.chain_event();

ALTER TABLE audit.events ALTER COLUMN id SET DEFAULT uuidv7();
ALTER TABLE audit.security_events ALTER COLUMN id SET DEFAULT uuidv7();
ALTER TABLE audit.events ALTER COLUMN row_hash DROP NOT NULL;

-- +goose StatementBegin
-- Creates monthly partitions. Runs from the worker; the default partition is the safety net.
CREATE FUNCTION audit.ensure_partitions(months_ahead int) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, audit AS $$
DECLARE
    t text;
    start_month date;
    i int;
BEGIN
    FOREACH t IN ARRAY ARRAY['events', 'security_events'] LOOP
        FOR i IN 0..months_ahead LOOP
            start_month := (date_trunc('month', now()) + make_interval(months => i))::date;
            EXECUTE format(
                'CREATE TABLE IF NOT EXISTS audit.%I PARTITION OF audit.%I FOR VALUES FROM (%L) TO (%L)',
                t || '_' || to_char(start_month, 'YYYY_MM'), t, start_month, (start_month + interval '1 month')::date
            );
        END LOOP;
    END LOOP;
END;
$$;
-- +goose StatementEnd

-- PostgreSQL lets everyone execute a new function unless this is revoked.
REVOKE ALL ON FUNCTION audit.ensure_partitions(int) FROM PUBLIC;
REVOKE ALL ON FUNCTION audit.verify_chain(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION audit.event_hash(bytea, audit.events) FROM PUBLIC;
REVOKE ALL ON FUNCTION audit.chain_event() FROM PUBLIC;
GRANT USAGE ON SCHEMA audit TO msp_system;
GRANT EXECUTE ON FUNCTION audit.ensure_partitions(int) TO msp_system;
GRANT EXECUTE ON FUNCTION audit.verify_chain(uuid) TO msp_platform;
SELECT audit.ensure_partitions(3);

-- +goose Down
DROP FUNCTION audit.verify_chain(uuid);
DROP FUNCTION audit.ensure_partitions(int);
DROP TRIGGER chain_event ON audit.events;
DROP FUNCTION audit.chain_event();
DROP FUNCTION audit.event_hash(bytea, audit.events);
DROP INDEX audit.events_chain_idx;
