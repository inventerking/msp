-- Sites and goals, as the application role under the tenant's context.

-- name: InsertSite :one
INSERT INTO visitor_intelligence.sites (tenant_id, domain, site_key, allowed_origins, consent_mode, retention_days, ip_handling, status)
VALUES (@tenant_id, @domain, @site_key, @allowed_origins, @consent_mode, @retention_days, @ip_handling, 'active') RETURNING id;

-- name: GetSite :one
SELECT id, domain, site_key, allowed_origins, consent_mode, retention_days, ip_handling, status, last_event_at, created_at
FROM visitor_intelligence.sites WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: ListSites :many
SELECT id, domain, site_key, allowed_origins, consent_mode, retention_days, ip_handling, status, last_event_at, created_at
FROM visitor_intelligence.sites WHERE tenant_id = @tenant_id AND deleted_at IS NULL ORDER BY created_at;

-- name: UpdateSite :execrows
UPDATE visitor_intelligence.sites SET allowed_origins = coalesce(sqlc.narg(allowed_origins), allowed_origins), consent_mode = coalesce(sqlc.narg(consent_mode), consent_mode),
  retention_days = coalesce(sqlc.narg(retention_days), retention_days), ip_handling = coalesce(sqlc.narg(ip_handling), ip_handling), status = coalesce(sqlc.narg(status), status)
WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: DeleteSite :execrows
UPDATE visitor_intelligence.sites SET deleted_at = now(), status = 'paused', site_key = site_key || ':deleted:' || id WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: TouchSite :exec
UPDATE visitor_intelligence.sites SET last_event_at = now() WHERE tenant_id = @tenant_id AND id = @id AND (last_event_at IS NULL OR last_event_at < now() - interval '1 minute');

-- name: InsertGoal :one
INSERT INTO visitor_intelligence.conversion_goals (tenant_id, site_id, name, kind, rule) VALUES (@tenant_id, @site_id, @name, @kind, @rule) RETURNING id;

-- name: ListGoals :many
SELECT id, site_id, name, kind, rule, created_at FROM visitor_intelligence.conversion_goals WHERE tenant_id = @tenant_id AND site_id = @site_id AND deleted_at IS NULL ORDER BY created_at;

-- name: DeleteGoal :execrows
UPDATE visitor_intelligence.conversion_goals SET deleted_at = now() WHERE tenant_id = @tenant_id AND id = @id AND deleted_at IS NULL;

-- name: InsertConversion :execrows
INSERT INTO visitor_intelligence.conversions (tenant_id, site_id, goal_id, session_id, visitor_id, attribution, dedupe_key)
VALUES (@tenant_id, @site_id, @goal_id, sqlc.narg(session_id), sqlc.narg(visitor_id), @attribution, @dedupe_key) ON CONFLICT (tenant_id, dedupe_key) DO NOTHING;

-- name: InsertIdentification :exec
INSERT INTO visitor_intelligence.business_identifications (tenant_id, site_id, session_id, company_id, provider_key, confidence, observed_at)
VALUES (@tenant_id, @site_id, sqlc.narg(session_id), @company_id, @provider_key, @confidence, now());

-- Ingest, as msp_ingest. The site is found by its public key; everything after runs in its tenant.

-- name: SiteByKey :one
SELECT id, tenant_id, domain, allowed_origins, consent_mode, ip_handling, status FROM visitor_intelligence.sites WHERE site_key = @site_key AND deleted_at IS NULL;

-- name: UpsertVisitor :one
INSERT INTO visitor_intelligence.visitors (tenant_id, site_id, anonymous_id_hash, first_seen_at, last_seen_at) VALUES (@tenant_id, @site_id, @anonymous_id_hash, now(), now())
ON CONFLICT (tenant_id, site_id, anonymous_id_hash) DO UPDATE SET last_seen_at = now() RETURNING id;

-- name: SessionExists :one
SELECT EXISTS (SELECT 1 FROM visitor_intelligence.sessions WHERE tenant_id = @tenant_id AND site_id = @site_id AND id = @id AND started_at > now() - interval '1 day');

-- name: InsertSession :exec
INSERT INTO visitor_intelligence.sessions (id, tenant_id, site_id, visitor_id, started_at, landing_url, referrer_host, utm, device_class, consent_state)
VALUES (@id, @tenant_id, @site_id, sqlc.narg(visitor_id), now(), sqlc.narg(landing_url), sqlc.narg(referrer_host), sqlc.narg(utm), sqlc.narg(device_class), @consent_state);

-- name: InsertEvent :exec
INSERT INTO visitor_intelligence.events (id, tenant_id, site_id, session_id, event_type, url, properties) VALUES (@id, @tenant_id, @site_id, sqlc.narg(session_id), @event_type, sqlc.narg(url), sqlc.narg(properties));

-- name: InsertConsentRecord :exec
INSERT INTO visitor_intelligence.consent_records (tenant_id, site_id, anonymous_id_hash, state, mechanism, policy_version) VALUES (@tenant_id, @site_id, sqlc.narg(anonymous_id_hash), @state, @mechanism, sqlc.narg(policy_version));

-- Reports, as the application role.

-- name: SiteSummary :one
SELECT
  (SELECT count(*) FROM visitor_intelligence.sessions s WHERE s.tenant_id = @tenant_id AND s.site_id = @site_id AND s.started_at >= @since)::int AS sessions,
  (SELECT count(DISTINCT s.visitor_id) FROM visitor_intelligence.sessions s WHERE s.tenant_id = @tenant_id AND s.site_id = @site_id AND s.started_at >= @since)::int AS visitors,
  (SELECT count(*) FROM visitor_intelligence.events e WHERE e.tenant_id = @tenant_id AND e.site_id = @site_id AND e.occurred_at >= @since AND e.event_type = 'page_view')::int AS page_views,
  (SELECT count(*) FROM visitor_intelligence.conversions c WHERE c.tenant_id = @tenant_id AND c.site_id = @site_id AND c.occurred_at >= @since)::int AS conversions;

-- name: TopPages :many
SELECT coalesce(url, '') AS url, count(*)::int AS views FROM visitor_intelligence.events
WHERE tenant_id = @tenant_id AND site_id = @site_id AND occurred_at >= @since AND event_type = 'page_view' GROUP BY 1 ORDER BY 2 DESC, 1 LIMIT 20;

-- name: TopSources :many
SELECT coalesce(referrer_host, coalesce(utm->>'utm_source', 'direct')) AS source, count(*)::int AS sessions FROM visitor_intelligence.sessions
WHERE tenant_id = @tenant_id AND site_id = @site_id AND started_at >= @since GROUP BY 1 ORDER BY 2 DESC, 1 LIMIT 20;

-- name: DailyCounts :many
SELECT date_trunc('day', started_at)::date AS day, count(*)::int AS sessions FROM visitor_intelligence.sessions
WHERE tenant_id = @tenant_id AND site_id = @site_id AND started_at >= @since GROUP BY 1 ORDER BY 1;

-- name: IdentifiedCompanies :many
SELECT b.company_id, count(*)::int AS visits, max(b.observed_at)::timestamptz AS last_seen, max(b.confidence)::float8 AS confidence
FROM visitor_intelligence.business_identifications b
WHERE b.tenant_id = @tenant_id AND b.site_id = @site_id AND b.observed_at >= @since GROUP BY b.company_id ORDER BY 3 DESC LIMIT 100;

-- name: CompanyVisits :one
SELECT count(*)::int AS visits FROM visitor_intelligence.business_identifications
WHERE tenant_id = @tenant_id AND company_id = @company_id;

-- name: VisitorsForContact :many
SELECT id FROM visitor_intelligence.visitors WHERE tenant_id = @tenant_id AND contact_id = @contact_id;

-- name: UnlinkContact :exec
UPDATE visitor_intelligence.visitors SET contact_id = NULL WHERE tenant_id = @tenant_id AND contact_id = @contact_id;
