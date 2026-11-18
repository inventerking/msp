package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/db"
	"mspengine/internal/kernel/events"
	"mspengine/internal/modules/visitorintel/repo"
	"mspengine/internal/platform/shared"
)

// Hit is one tracker request.
type Hit struct {
	SiteKey       string `json:"site_key"`
	Type          string `json:"type"` // page_view, custom, consent
	URL           string `json:"url"`
	Referrer      string `json:"referrer"`
	AnonymousID   string `json:"anonymous_id"`
	SessionID     string `json:"session_id"`
	Name          string `json:"name"`
	Consent       string `json:"consent"` // granted, denied, or empty when not asked
	PolicyVersion string `json:"policy_version"`
}

// HitMeta is what the request itself says. The IP address is used in memory only.
type HitMeta struct {
	Origin, UserAgent string
	IP                string
}

// ErrOrigin means the request did not come from one of the site's own pages.
var ErrOrigin = apperr.New(403, "origin_not_allowed", "This site does not accept tracking from that origin")

var botUA = []string{"bot", "crawler", "spider", "headless", "preview", "monitor"}

// Collect records one hit from a tracked site. It runs as msp_ingest, which may find a site by its
// public key and write visitor rows, and nothing else. What is stored depends on the site's consent
// mode: without consent where it is needed, only an anonymous page view is kept.
func (s *Service) Collect(ctx context.Context, h Hit, m HitMeta) error {
	if h.SiteKey == "" || len(h.SiteKey) > 64 {
		return apperr.Validation(apperr.FieldError{Field: "site_key", Code: "required"})
	}
	ua := strings.ToLower(m.UserAgent)
	for _, b := range botUA {
		if strings.Contains(ua, b) {
			return nil // Automated traffic is not counted.
		}
	}
	var site repo.SiteByKeyRow
	var sessionID *uuid.UUID
	newSession, identify := false, false
	var pageURL string
	err := db.Tx(ctx, s.Pools.App, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL ROLE msp_ingest`); err != nil {
			return err
		}
		q := repo.New(tx)
		var err error
		if site, err = q.SiteByKey(ctx, h.SiteKey); err != nil {
			return notFound(err)
		}
		if site.Status != "active" {
			return apperr.NotFound()
		}
		if !originAllowed(m.Origin, site.AllowedOrigins) {
			return ErrOrigin
		}
		if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, site.TenantID.String()); err != nil {
			return err
		}
		var anon []byte
		if h.AnonymousID != "" && len(h.AnonymousID) <= 64 {
			anon = s.Keys.HMACValue("visitor:"+site.ID.String(), h.AnonymousID)
		}
		if h.Type == "consent" {
			if h.Consent != "granted" && h.Consent != "denied" && h.Consent != "withdrawn" {
				return apperr.Validation(apperr.FieldError{Field: "consent", Code: "invalid"})
			}
			return q.InsertConsentRecord(ctx, repo.InsertConsentRecordParams{TenantID: site.TenantID, SiteID: site.ID, AnonymousIDHash: anon, State: h.Consent, Mechanism: "banner", PolicyVersion: nonEmpty(h.PolicyVersion)})
		}
		if h.Type != "page_view" && h.Type != "custom" {
			return apperr.Validation(apperr.FieldError{Field: "type", Code: "invalid"})
		}
		clean, utm, ok := cleanURL(h.URL, site.Domain, site.AllowedOrigins)
		if !ok {
			return nil // Not a page of this site.
		}
		pageURL = clean
		identify = (site.ConsentMode == "opt_in" && h.Consent == "granted") || (site.ConsentMode == "opt_out" && h.Consent != "denied")
		if identify && anon != nil && h.SessionID != "" && len(h.SessionID) <= 64 {
			v, err := q.UpsertVisitor(ctx, repo.UpsertVisitorParams{TenantID: site.TenantID, SiteID: site.ID, AnonymousIDHash: anon})
			if err != nil {
				return err
			}
			sid := derivedID(s.Keys.HMACValue("session:"+site.ID.String(), h.SessionID))
			sessionID = &sid
			exists, err := q.SessionExists(ctx, repo.SessionExistsParams{TenantID: site.TenantID, SiteID: site.ID, ID: sid})
			if err != nil {
				return err
			}
			if !exists {
				state := "not_required"
				if site.ConsentMode == "opt_in" {
					state = "granted"
				}
				if err := q.InsertSession(ctx, repo.InsertSessionParams{ID: sid, TenantID: site.TenantID, SiteID: site.ID, VisitorID: &v, LandingUrl: &clean,
					ReferrerHost: referrerHost(h.Referrer, site.Domain), Utm: utm, DeviceClass: ptr(deviceClass(ua)), ConsentState: state}); err != nil {
					return err
				}
				newSession = true
			}
		}
		var props []byte
		if h.Type == "custom" {
			name := h.Name
			if len(name) > 100 {
				name = name[:100]
			}
			props, _ = json.Marshal(map[string]string{"name": name})
		}
		return q.InsertEvent(ctx, repo.InsertEventParams{ID: uuid.Must(uuid.NewV7()), TenantID: site.TenantID, SiteID: site.ID, SessionID: sessionID, EventType: h.Type, Url: &clean, Properties: props})
	})
	if err != nil || h.Type == "consent" || pageURL == "" {
		return err
	}
	// Follow-up work runs in the site's tenant as the application role, after the hit is stored.
	if active, err := s.moduleActive(ctx, site.TenantID); err != nil || !active {
		return err
	}
	if err := s.afterHit(ctx, site, h, pageURL, sessionID); err != nil {
		slog.WarnContext(ctx, "visitor follow-up failed", "error", err)
	}
	if newSession && identify && site.IpHandling == "truncate_then_discard" {
		s.identify(ctx, site, *sessionID, m.IP)
	}
	return nil
}

// moduleActive reads the tenant's entitlement, trusting a recent answer for hotTTL.
func (s *Service) moduleActive(ctx context.Context, tenant uuid.UUID) (bool, error) {
	now := time.Now()
	if v, fresh := s.hot.moduleActive(tenant, now); fresh {
		return v, nil
	}
	status, err := s.Commercial.ModuleStatus(ctx, tenant, Module)
	if err != nil {
		return false, err
	}
	s.hot.setModuleActive(tenant, status == "active", now)
	return status == "active", nil
}

// afterHit records conversions for goals the hit meets, and keeps the site's last-seen time. Goals
// come from a short-lived cache, so a hit that meets no goal, on a site touched in the last minute,
// needs no transaction here at all.
func (s *Service) afterHit(ctx context.Context, site repo.SiteByKeyRow, h Hit, pageURL string, session *uuid.UUID) error {
	now := time.Now()
	goals, fresh := s.hot.siteGoals(site.ID, now)
	if !fresh {
		if err := s.systemTx(ctx, site.TenantID, func(q *repo.Queries, _ pgx.Tx) error {
			var err error
			goals, err = q.ListGoals(ctx, repo.ListGoalsParams{TenantID: site.TenantID, SiteID: site.ID})
			return err
		}); err != nil {
			return err
		}
		s.hot.setSiteGoals(site.ID, goals, now)
	}
	var met []repo.ListGoalsRow
	for _, g := range goals {
		var rule struct{ Contains, Event string }
		_ = json.Unmarshal(g.Rule, &rule)
		if (g.Kind == "url_match" && h.Type == "page_view" && rule.Contains != "" && strings.Contains(pageURL, rule.Contains)) ||
			(g.Kind == "custom_event" && h.Type == "custom" && rule.Event != "" && h.Name == rule.Event) {
			met = append(met, g)
		}
	}
	touch := s.hot.touchDue(site.ID, now)
	if len(met) == 0 && !touch {
		return nil
	}
	return s.systemTx(ctx, site.TenantID, func(q *repo.Queries, tx pgx.Tx) error {
		if touch {
			if err := q.TouchSite(ctx, repo.TouchSiteParams{TenantID: site.TenantID, ID: site.ID}); err != nil {
				return err
			}
		}
		for _, g := range met {
			key := g.ID.String() + ":" + uuid.NewString()
			if session != nil {
				key = g.ID.String() + ":" + session.String() // Once per session.
			}
			attr, _ := json.Marshal(map[string]string{"url": pageURL})
			ids, err := q.InsertConversion(ctx, repo.InsertConversionParams{TenantID: site.TenantID, SiteID: site.ID, GoalID: g.ID, SessionID: session, Attribution: attr, DedupeKey: key})
			if err != nil {
				return err
			}
			for _, id := range ids { // Empty for a repeat within the session.
				if err := events.Append(ctx, tx, events.Event{Type: "visitor_intelligence.conversion.recorded", TenantID: &site.TenantID, SourceModule: Module, AggregateType: "conversion", AggregateID: id},
					map[string]any{"conversion_id": id, "site_id": site.ID, "goal_id": g.ID}); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// identify asks the identifier about the visitor's network. Only the truncated network address is
// passed on, and nothing about the address is stored.
func (s *Service) identify(ctx context.Context, site repo.SiteByKeyRow, session uuid.UUID, ip string) {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return
	}
	bits := 24
	if addr.Is6() && !addr.Is4In6() {
		bits = 48
	}
	prefix, err := addr.Unmap().Prefix(bits)
	if err != nil {
		return
	}
	domain, confidence, ok := s.Identifier.Identify(ctx, prefix.Addr())
	if !ok {
		return
	}
	company, err := s.Shared.ResolveOrCreate(ctx, shared.Signal{Domain: domain, Provider: s.Identifier.Key()})
	if err != nil {
		slog.WarnContext(ctx, "identified company not resolved", "error", err)
		return
	}
	if err := s.systemTx(ctx, site.TenantID, func(q *repo.Queries, _ pgx.Tx) error {
		return q.InsertIdentification(ctx, repo.InsertIdentificationParams{TenantID: site.TenantID, SiteID: site.ID, SessionID: &session, CompanyID: company, ProviderKey: s.Identifier.Key(), Confidence: confidence})
	}); err != nil {
		slog.WarnContext(ctx, "identification not stored", "error", err)
	}
}

func originAllowed(origin string, allowed []string) bool {
	origin = strings.TrimRight(strings.ToLower(origin), "/")
	if origin == "" {
		return false
	}
	for _, a := range allowed {
		if a == origin {
			return true
		}
	}
	return false
}

// cleanURL keeps a page address on the site's own domain, or on one of its allowed origins (such
// as a staging or local test host), without its fragment, and without query parameters other than
// utm_ ones, which may carry personal data such as emails or tokens.
func cleanURL(raw, domain string, origins []string) (string, []byte, bool) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || len(raw) > 2048 {
		return "", nil, false
	}
	host := strings.ToLower(u.Hostname())
	ours := host == domain || strings.HasSuffix(host, "."+domain)
	for _, o := range origins {
		if o == strings.ToLower(u.Scheme+"://"+u.Host) {
			ours = true
		}
	}
	if !ours {
		return "", nil, false
	}
	keep := url.Values{}
	utm := map[string]string{}
	for k, v := range u.Query() {
		if strings.HasPrefix(k, "utm_") && len(v) > 0 && len(k) <= 20 {
			val := v[0]
			if len(val) > 100 {
				val = val[:100]
			}
			keep.Set(k, val)
			utm[k] = val
		}
	}
	u.RawQuery, u.Fragment, u.User = keep.Encode(), "", nil
	var utmJSON []byte
	if len(utm) > 0 {
		utmJSON, _ = json.Marshal(utm)
	}
	return u.String(), utmJSON, true
}

func referrerHost(raw, domain string) *string {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return nil
	}
	h := strings.ToLower(u.Hostname())
	if h == domain || strings.HasSuffix(h, "."+domain) {
		return nil
	}
	return &h
}

func deviceClass(ua string) string {
	switch {
	case strings.Contains(ua, "ipad") || strings.Contains(ua, "tablet"):
		return "tablet"
	case strings.Contains(ua, "mobi"):
		return "mobile"
	}
	return "desktop"
}

func derivedID(mac []byte) uuid.UUID {
	var id uuid.UUID
	copy(id[:], mac[:16])
	id[6] = (id[6] & 0x0f) | 0x80 // Version 8: custom.
	id[8] = (id[8] & 0x3f) | 0x80
	return id
}

func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func ptr[T any](v T) *T { return &v }

func itoa(n int) string { return strconv.Itoa(n) }

func jsonString(s string) string { b, _ := json.Marshal(s); return string(b) }
