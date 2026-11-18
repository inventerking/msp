package shared

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/crypto"
	"mspengine/internal/kernel/db"
	"mspengine/internal/kernel/events"
	"mspengine/internal/kernel/jobs"
	"mspengine/internal/platform/audit"
	"mspengine/internal/platform/shared/repo"
)

// Licence classes. Only the first two may enter global tables.
const (
	LicencePublic          = "public"
	LicenceShareable       = "licensed_shareable"
	LicenceTenantOnly      = "licensed_tenant"
	LicenceTenantProvided  = "tenant_provided"
	LicenceSubjectProvided = "data_subject_provided"
)

// Service implements company and contact identity.
type Service struct {
	pools   *db.Pools
	keys    crypto.Keys
	erasers []Eraser
	// erasureLog keeps a record of each erasure outside the database. Nil means none.
	erasureLog ObjectLog
}

// New creates the service.
func New(pools *db.Pools, keys crypto.Keys) *Service { return &Service{pools: pools, keys: keys} }

func badDomain() *apperr.Error {
	return apperr.Validation(apperr.FieldError{Field: "domain", Code: "invalid_company_domain"})
}

// Fact is a piece of company data with its source. Licence must allow reuse across tenants.
type Fact struct {
	Attribute  string
	Value      any
	SourceKey  string
	SourceURL  *string
	Licence    string
	Confidence float64
	ObservedAt time.Time
	ExpiresAt  *time.Time
}

// Signal identifies a company from a source. It has no field for tenant-entered text, so private
// data has no path into the canonical record.
type Signal struct {
	Domain string
	// SourceName is the name reported by a public or licensed source, if any.
	SourceName string
	ProviderID string
	Provider   string
	Facts      []Fact
}

// ResolveByDomain finds a company by domain, following merges, without creating one.
func (s *Service) ResolveByDomain(ctx context.Context, domain string) (uuid.UUID, bool, error) {
	norm, err := NormalizeDomain(domain)
	if err != nil {
		return uuid.Nil, false, badDomain()
	}
	var id uuid.UUID
	found := false
	err = db.Tx(ctx, s.pools.App, func(tx pgx.Tx) error {
		var err error
		id, found, err = findByIdentifier(ctx, repo.New(tx), "domain", norm, nil)
		return err
	})
	return id, found, err
}

func findByIdentifier(ctx context.Context, q *repo.Queries, kind, value string, provider *string) (uuid.UUID, bool, error) {
	row, err := q.FindCompanyByIdentifier(ctx, repo.FindCompanyByIdentifierParams{Kind: kind, Value: value, ProviderKey: provider})
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, false, err
	}
	id := row.ID
	for hops := 0; row.MergedIntoID != nil && hops < 10; hops++ {
		id = *row.MergedIntoID
		next, err := q.GetCompany(ctx, id)
		if err != nil {
			return uuid.Nil, false, err
		}
		row.MergedIntoID = next.MergedIntoID
	}
	return id, true, nil
}

// ResolveOrCreate returns the canonical company for a signal, creating it if it is new. Concurrent
// callers end up with one company: the unique identifier decides, and the loser reads the winner's.
func (s *Service) ResolveOrCreate(ctx context.Context, sig Signal) (uuid.UUID, error) {
	var domain *string
	if sig.Domain != "" {
		norm, err := NormalizeDomain(sig.Domain)
		if err != nil {
			return uuid.Nil, badDomain()
		}
		domain = &norm
	}
	if domain == nil && sig.ProviderID == "" {
		return uuid.Nil, badDomain()
	}
	for _, f := range sig.Facts {
		if f.Licence != LicencePublic && f.Licence != LicenceShareable {
			return uuid.Nil, fmt.Errorf("fact %s has licence %q, which may not enter canonical data", f.Attribute, f.Licence)
		}
	}
	name := sig.SourceName
	if name == "" && domain != nil {
		name = *domain
	}
	if name == "" {
		return uuid.Nil, errors.New("a company needs a source name or a domain")
	}
	var id uuid.UUID
	err := pgx.BeginFunc(ctx, s.pools.App, func(tx pgx.Tx) error {
		q := repo.New(tx)
		// Serializes creation per identity, so concurrent callers cannot make two companies.
		lockKey := sig.Provider + "|" + sig.ProviderID
		if domain != nil {
			lockKey = "domain|" + *domain
		}
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "company:"+lockKey); err != nil {
			return err
		}
		var err error
		found := false
		if domain != nil {
			id, found, err = findByIdentifier(ctx, q, "domain", *domain, nil)
			if err != nil {
				return err
			}
		}
		if !found && sig.ProviderID != "" {
			id, found, err = findByIdentifier(ctx, q, "provider_id", sig.ProviderID, &sig.Provider)
			if err != nil {
				return err
			}
		}
		if !found {
			id, err = q.InsertCompany(ctx, repo.InsertCompanyParams{PrimaryDomain: domain, DisplayName: name})
			if err != nil {
				return err
			}
		}
		if domain != nil {
			if err := q.InsertIdentifier(ctx, repo.InsertIdentifierParams{CompanyID: id, Kind: "domain", Value: *domain}); err != nil {
				return err
			}
		}
		if sig.ProviderID != "" {
			if err := q.InsertIdentifier(ctx, repo.InsertIdentifierParams{CompanyID: id, Kind: "provider_id", Value: sig.ProviderID, ProviderKey: &sig.Provider}); err != nil {
				return err
			}
		}
		for _, f := range sig.Facts {
			v, err := json.Marshal(f.Value)
			if err != nil {
				return err
			}
			if err := q.InsertFact(ctx, repo.InsertFactParams{CompanyID: id, Attribute: f.Attribute, Value: v, SourceKey: f.SourceKey, SourceUrl: f.SourceURL, LicenceClass: f.Licence, Confidence: f.Confidence, ObservedAt: f.ObservedAt, ExpiresAt: f.ExpiresAt}); err != nil {
				return err
			}
		}
		return nil
	})
	return id, err
}

// GetCompany returns a canonical company.
func (s *Service) GetCompany(ctx context.Context, id uuid.UUID) (repo.GetCompanyRow, error) {
	var out repo.GetCompanyRow
	err := db.Tx(ctx, s.pools.App, func(tx pgx.Tx) error {
		var err error
		out, err = repo.New(tx).GetCompany(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound()
		}
		return err
	})
	return out, err
}

// SearchCompanies finds canonical companies. Results come only from global data, so they carry no
// sign of whether any other tenant has a relationship with a company.
func (s *Service) SearchCompanies(ctx context.Context, query string) ([]repo.SearchCompaniesRow, error) {
	if len(query) < 2 {
		return nil, apperr.Validation(apperr.FieldError{Field: "q", Code: "too_short"})
	}
	var out []repo.SearchCompaniesRow
	err := db.Tx(ctx, s.pools.App, func(tx pgx.Tx) error {
		var err error
		out, err = repo.New(tx).SearchCompanies(ctx, repo.SearchCompaniesParams{Query: &query, RowLimit: 25})
		return err
	})
	return out, err
}

// MergeCompanies folds one canonical company into another, and tells modules to repoint. Merges are
// made by a person, because a wrong automatic merge would be hard to detect and undo.
func (s *Service) MergeCompanies(ctx context.Context, a authctx.AdminPrincipal, source, target uuid.UUID, reason string) error {
	if source == target {
		return apperr.Conflict("invalid_transition", "A company cannot merge into itself")
	}
	return db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		q := repo.New(tx)
		n, err := q.CompanyActive(ctx, target)
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.NotFound()
		}
		changed, err := q.MergeSetStatus(ctx, repo.MergeSetStatusParams{SourceID: source, TargetID: &target})
		if err != nil {
			return err
		}
		if changed == 0 {
			return apperr.NotFound()
		}
		if err := q.MergeMoveIdentifiers(ctx, repo.MergeMoveIdentifiersParams{TargetID: target, SourceID: source}); err != nil {
			return err
		}
		if err := q.MergeMoveFacts(ctx, repo.MergeMoveFactsParams{TargetID: target, SourceID: source}); err != nil {
			return err
		}
		if err := q.InsertMerge(ctx, repo.InsertMergeParams{SourceID: source, TargetID: target, Reason: reason, DecidedBy: a.AdminID.String()}); err != nil {
			return err
		}
		if err := audit.Record(ctx, tx, audit.Event{ActorType: "platform_admin", ActorID: &a.AdminID, Action: "company.merged", ResourceType: "company", ResourceID: &target, After: map[string]any{"source": source, "reason": reason}}); err != nil {
			return err
		}
		return events.Append(ctx, tx, events.Event{Type: "shared.company.merged", SourceModule: "shared", AggregateType: "company", AggregateID: target},
			map[string]any{"source_company_id": source, "target_company_id": target})
	})
}

// ContactInput identifies a contact in a tenant. A contact belongs to one tenant only: the same
// person at two MSPs is two rows, so no global person record exists (ADR-009).
type ContactInput struct {
	Email     string
	FullName  string
	JobTitle  string
	// CountryCode is where the person is, which decides the outreach rules that apply to them.
	CountryCode string
	CompanyID *uuid.UUID
	SourceKey string
	SourceRef string
	Licence   string
	CreatedBy *uuid.UUID
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ResolveOrCreateContact returns the tenant's contact for an email, creating it if it is new.
func (s *Service) ResolveOrCreateContact(ctx context.Context, tenantID uuid.UUID, in ContactInput) (uuid.UUID, error) {
	if in.SourceKey == "" || in.Licence == "" {
		return uuid.Nil, errors.New("a contact needs a recorded source")
	}
	var id uuid.UUID
	err := db.TenantTx(ctx, s.pools.App, tenantID, uuid.Nil, func(tx pgx.Tx) error {
		q := repo.New(tx)
		var err error
		if in.CountryCode != "" {
			var known bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM shared.countries WHERE code = $1)`, strings.ToUpper(in.CountryCode)).Scan(&known); err != nil {
				return err
			}
			if !known {
				return apperr.Validation(apperr.FieldError{Field: "country_code", Code: "unknown_country"})
			}
		}
		if email := NormalizeEmail(in.Email); email != "" {
			id, err = q.UpsertContact(ctx, repo.UpsertContactParams{TenantID: tenantID, CompanyID: in.CompanyID, FullName: optional(in.FullName), JobTitle: optional(in.JobTitle),
				Email: &email, EmailHash: s.keys.HMACValue("contact-email:"+tenantID.String(), email), CountryCode: optional(strings.ToUpper(in.CountryCode)), CreatedBy: in.CreatedBy})
		} else {
			id, err = q.InsertContactNoEmail(ctx, repo.InsertContactNoEmailParams{TenantID: tenantID, CompanyID: in.CompanyID, FullName: optional(in.FullName), JobTitle: optional(in.JobTitle), CountryCode: optional(strings.ToUpper(in.CountryCode)), CreatedBy: in.CreatedBy})
		}
		if err != nil {
			return err
		}
		return q.InsertContactSource(ctx, repo.InsertContactSourceParams{TenantID: tenantID, ContactID: id, SourceKey: in.SourceKey, SourceRef: optional(in.SourceRef), LicenceClass: in.Licence})
	})
	return id, err
}

// ConsentInput records a consent decision and its evidence.
type ConsentInput struct {
	Purpose      string
	Status       string
	Basis        string
	Jurisdiction string
	Evidence     map[string]any
}

// RecordConsent adds a consent record, and tells interested modules that it changed.
func (s *Service) RecordConsent(ctx context.Context, tenantID, contactID uuid.UUID, in ConsentInput) error {
	ev, err := json.Marshal(in.Evidence)
	if err != nil {
		return err
	}
	var withdrawn *time.Time
	if in.Status == "withdrawn" {
		now := time.Now()
		withdrawn = &now
	}
	return db.TenantTx(ctx, s.pools.App, tenantID, uuid.Nil, func(tx pgx.Tx) error {
		q := repo.New(tx)
		if _, err := q.GetContact(ctx, repo.GetContactParams{TenantID: tenantID, ID: contactID}); errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound()
		} else if err != nil {
			return err
		}
		if err := q.InsertConsent(ctx, repo.InsertConsentParams{TenantID: tenantID, ContactID: contactID, Purpose: in.Purpose, Status: in.Status, Basis: optional(in.Basis), Jurisdiction: optional(in.Jurisdiction), Evidence: ev, WithdrawnAt: withdrawn}); err != nil {
			return err
		}
		return events.Append(ctx, tx, events.Event{Type: "shared.contact.consent_changed", TenantID: &tenantID, SourceModule: "shared", AggregateType: "contact", AggregateID: contactID},
			map[string]any{"contact_id": contactID, "purpose": in.Purpose, "status": in.Status})
	})
}

// Eraser is implemented by each module that stores data about a contact. It must be idempotent.
type Eraser interface {
	Name() string
	EraseContact(ctx context.Context, tenantID, contactID uuid.UUID) error
	ExportContact(ctx context.Context, tenantID, contactID uuid.UUID) (any, error)
}

// RegisterEraser adds a module to erasure and export. Modules register at startup.
func (s *Service) RegisterEraser(e Eraser) { s.erasers = append(s.erasers, e) }

// JobEraseContact is the job kind for erasure.
const JobEraseContact = "privacy.erase_contact"

// RequestErasure starts tracked erasure of a contact, and returns the job that follows it.
func (s *Service) RequestErasure(ctx context.Context, p authctx.Principal, contactID uuid.UUID) (uuid.UUID, error) {
	var jobID uuid.UUID
	err := db.TenantTx(ctx, s.pools.App, p.TenantID, p.UserID, func(tx pgx.Tx) error {
		if _, err := repo.New(tx).GetContact(ctx, repo.GetContactParams{TenantID: p.TenantID, ID: contactID}); errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound()
		} else if err != nil {
			return err
		}
		var err error
		jobID, _, err = jobs.Enqueue(ctx, tx, jobs.Spec{TenantID: &p.TenantID, Kind: JobEraseContact, Queue: jobs.QueueDefault, MaxAttempts: 25, Payload: map[string]any{"contact_id": contactID}, IdempotencyKey: "erase:" + contactID.String()})
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{TenantID: &p.TenantID, ActorType: "user", ActorID: &p.UserID, Action: "privacy.erasure_requested", ResourceType: "contact", ResourceID: &contactID})
	})
	return jobID, err
}

// ObjectLog is append-only storage outside the database, such as an object store bucket.
type ObjectLog interface {
	Put(ctx context.Context, key string, body []byte, contentType string) error
	Get(ctx context.Context, key string) ([]byte, error)
	List(ctx context.Context, prefix string) ([]string, error)
}

// erasureLogPrefix holds one object per erased contact, by the UTC day it was erased.
const erasureLogPrefix = "erasure-log/"

// SetErasureLog records erasures outside the database, so they can be repeated after a restore.
func (s *Service) SetErasureLog(l ObjectLog) { s.erasureLog = l }

type erasureRecord struct {
	TenantID  uuid.UUID `json:"tenant_id"`
	ContactID uuid.UUID `json:"contact_id"`
	LoggedAt  time.Time `json:"logged_at"`
}

// logErasure writes the record before anything is erased, so an erasure that took effect is always
// in the log. The key is fixed per contact and day, so a retried job writes the same object.
func (s *Service) logErasure(ctx context.Context, tenantID, contactID uuid.UUID) error {
	if s.erasureLog == nil {
		return nil
	}
	now := time.Now().UTC()
	body, _ := json.Marshal(erasureRecord{TenantID: tenantID, ContactID: contactID, LoggedAt: now})
	return s.erasureLog.Put(ctx, erasureLogPrefix+now.Format("2006-01-02")+"/"+tenantID.String()+"_"+contactID.String()+".json", body, "application/json")
}

// ReplayResult counts what ReplayErasures found.
type ReplayResult struct {
	Logged, Queued, Absent int
}

// ReplayErasures queues erasure again for every contact logged as erased since the given day. It is
// for after a database restore, which can bring back data erased after the backup was taken.
// Contacts the restored database never held are counted as absent. Erasure is idempotent, so a
// contact that is already erased is harmless to queue.
func (s *Service) ReplayErasures(ctx context.Context, since time.Time) (ReplayResult, error) {
	var r ReplayResult
	if s.erasureLog == nil {
		return r, errors.New("no erasure log is configured")
	}
	for day := since.UTC().Truncate(24 * time.Hour); !day.After(time.Now().UTC()); day = day.Add(24 * time.Hour) {
		keys, err := s.erasureLog.List(ctx, erasureLogPrefix+day.Format("2006-01-02")+"/")
		if err != nil {
			return r, err
		}
		for _, k := range keys {
			raw, err := s.erasureLog.Get(ctx, k)
			if err != nil {
				return r, err
			}
			var rec erasureRecord
			if err := json.Unmarshal(raw, &rec); err != nil {
				return r, fmt.Errorf("%s: %w", k, err)
			}
			r.Logged++
			err = db.TenantTx(ctx, s.pools.App, rec.TenantID, uuid.Nil, func(tx pgx.Tx) error {
				if _, err := repo.New(tx).GetContact(ctx, repo.GetContactParams{TenantID: rec.TenantID, ID: rec.ContactID}); errors.Is(err, pgx.ErrNoRows) {
					r.Absent++
					return nil
				} else if err != nil {
					return err
				}
				if _, _, err := jobs.Enqueue(ctx, tx, jobs.Spec{TenantID: &rec.TenantID, Kind: JobEraseContact, Queue: jobs.QueueDefault, MaxAttempts: 25,
					Payload: map[string]any{"contact_id": rec.ContactID}, IdempotencyKey: "erase-replay:" + rec.ContactID.String() + ":" + time.Now().UTC().Format(time.RFC3339)}); err != nil {
					return err
				}
				r.Queued++
				return audit.Record(ctx, tx, audit.Event{TenantID: &rec.TenantID, ActorType: "system", Action: "privacy.erasure_replayed", ResourceType: "contact", ResourceID: &rec.ContactID})
			})
			if err != nil {
				return r, err
			}
		}
	}
	return r, nil
}

// HandleErase is the erasure job. Modules erase first, so that if one fails, the identity survives
// and the job retries until every module has finished.
func (s *Service) HandleErase(ctx context.Context, j jobs.Job) error {
	var p struct {
		ContactID uuid.UUID `json:"contact_id"`
	}
	if err := j.Decode(&p); err != nil {
		return err
	}
	if j.TenantID == nil {
		return jobs.Permanent(errors.New("erasure job has no tenant"))
	}
	tenantID := *j.TenantID
	if err := s.logErasure(ctx, tenantID, p.ContactID); err != nil {
		return fmt.Errorf("erasure log: %w", err)
	}
	for _, e := range s.erasers {
		if err := e.EraseContact(ctx, tenantID, p.ContactID); err != nil {
			return fmt.Errorf("erase in %s: %w", e.Name(), err)
		}
	}
	return db.TenantTx(ctx, s.pools.App, tenantID, uuid.Nil, func(tx pgx.Tx) error {
		q := repo.New(tx)
		n, err := q.EraseContact(ctx, repo.EraseContactParams{TenantID: tenantID, ID: p.ContactID})
		if err != nil {
			return err
		}
		if err := q.DeleteContactSources(ctx, repo.DeleteContactSourcesParams{TenantID: tenantID, ContactID: p.ContactID}); err != nil {
			return err
		}
		if err := q.ScrubConsentEvidence(ctx, repo.ScrubConsentEvidenceParams{TenantID: tenantID, ContactID: p.ContactID}); err != nil {
			return err
		}
		if n == 0 {
			return nil // Already erased by an earlier attempt.
		}
		if err := audit.Record(ctx, tx, audit.Event{TenantID: &tenantID, ActorType: "system", Action: "privacy.contact_erased", ResourceType: "contact", ResourceID: &p.ContactID}); err != nil {
			return err
		}
		return events.Append(ctx, tx, events.Event{Type: "shared.contact.erased", TenantID: &tenantID, SourceModule: "shared", AggregateType: "contact", AggregateID: p.ContactID}, map[string]any{"contact_id": p.ContactID})
	})
}

// ContactExport is everything held about a contact.
type ContactExport struct {
	Contact  repo.GetContactRow           `json:"contact"`
	Sources  []repo.ListContactSourcesRow `json:"sources"`
	Consents []repo.ListConsentsRow       `json:"consents"`
	Modules  map[string]any               `json:"modules"`
}

// ExportContact assembles a contact's data from identity and every registered module.
func (s *Service) ExportContact(ctx context.Context, p authctx.Principal, contactID uuid.UUID) (ContactExport, error) {
	out := ContactExport{Modules: map[string]any{}}
	err := db.TenantTx(ctx, s.pools.App, p.TenantID, p.UserID, func(tx pgx.Tx) error {
		q := repo.New(tx)
		var err error
		out.Contact, err = q.GetContact(ctx, repo.GetContactParams{TenantID: p.TenantID, ID: contactID})
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		if out.Sources, err = q.ListContactSources(ctx, repo.ListContactSourcesParams{TenantID: p.TenantID, ContactID: contactID}); err != nil {
			return err
		}
		if out.Consents, err = q.ListConsents(ctx, repo.ListConsentsParams{TenantID: p.TenantID, ContactID: contactID}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{TenantID: &p.TenantID, ActorType: "user", ActorID: &p.UserID, Action: "privacy.contact_exported", ResourceType: "contact", ResourceID: &contactID})
	})
	if err != nil {
		return out, err
	}
	for _, e := range s.erasers {
		v, err := e.ExportContact(ctx, p.TenantID, contactID)
		if err != nil {
			return out, fmt.Errorf("export from %s: %w", e.Name(), err)
		}
		out.Modules[e.Name()] = v
	}
	return out, nil
}

// Rate returns the exchange rate on or before a date, from approved reference data.
func (s *Service) Rate(ctx context.Context, base, quote string, on time.Time) (float64, bool, error) {
	if base == quote {
		return 1, true, nil
	}
	var rate float64
	found := false
	err := db.Tx(ctx, s.pools.App, func(tx pgx.Tx) error {
		v, err := repo.New(tx).GetRate(ctx, repo.GetRateParams{Base: base, Quote: quote, OnDate: on})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		rate, found = v, true
		return nil
	})
	return rate, found, err
}

// CheckCodes rejects country or industry codes that are not in the reference data.
func CheckCodes(ctx context.Context, tx pgx.Tx, countries, industries []string) error {
	q := repo.New(tx)
	if n, err := q.CountUnknownCountries(ctx, nonNil(countries)); err != nil {
		return err
	} else if n > 0 {
		return apperr.Validation(apperr.FieldError{Field: "countries", Code: "unknown_country"})
	}
	if n, err := q.CountUnknownIndustries(ctx, nonNil(industries)); err != nil {
		return err
	} else if n > 0 {
		return apperr.Validation(apperr.FieldError{Field: "industries", Code: "unknown_industry"})
	}
	return nil
}

// Reference returns the country and industry lists.
func (s *Service) Reference(ctx context.Context) ([]repo.ListCountriesRow, []repo.ListIndustriesRow, error) {
	var cs []repo.ListCountriesRow
	var is []repo.ListIndustriesRow
	err := pgx.BeginFunc(ctx, s.pools.App, func(tx pgx.Tx) error {
		var err error
		q := repo.New(tx)
		if cs, err = q.ListCountries(ctx); err != nil {
			return err
		}
		is, err = q.ListIndustries(ctx)
		return err
	})
	return cs, is, err
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// ContactView is what other modules may show about a contact.
type ContactView struct {
	ID                        uuid.UUID
	CompanyID                 *uuid.UUID
	FullName, JobTitle, Email *string
	FirstName, Phone          *string
	CountryCode               *string
	Erased                    bool
}

// ContactsByID returns the tenant's contacts by ID. Erased contacts come back with no details.
func (s *Service) ContactsByID(ctx context.Context, tenantID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]ContactView, error) {
	out := map[uuid.UUID]ContactView{}
	if len(ids) == 0 {
		return out, nil
	}
	err := db.TenantTx(ctx, s.pools.App, tenantID, uuid.Nil, func(tx pgx.Tx) error {
		rows, err := repo.New(tx).ContactsByIDs(ctx, repo.ContactsByIDsParams{TenantID: tenantID, Ids: ids})
		if err != nil {
			return err
		}
		for _, r := range rows {
			v := ContactView{ID: r.ID, CompanyID: r.CompanyID, Erased: r.ErasedAt != nil}
			if !v.Erased {
				v.FullName, v.JobTitle, v.Email, v.Phone, v.FirstName = r.FullName, r.JobTitle, r.Email, r.Phone, r.FirstName
				if r.CountryCode != "" {
					v.CountryCode = &r.CountryCode
				}
			}
			out[r.ID] = v
		}
		return nil
	})
	return out, err
}

// HasConsent reports whether a contact has a current, granted consent for a purpose.
func (s *Service) HasConsent(ctx context.Context, tenantID, contactID uuid.UUID, purpose string) (bool, error) {
	var out bool
	err := db.TenantTx(ctx, s.pools.App, tenantID, uuid.Nil, func(tx pgx.Tx) error {
		var err error
		out, err = repo.New(tx).HasConsent(ctx, repo.HasConsentParams{TenantID: tenantID, ContactID: contactID, Purpose: purpose})
		return err
	})
	return out, err
}

// EmailKey is the tenant-scoped keyed hash of an email address, used for suppressions.
func (s *Service) EmailKey(tenantID uuid.UUID, email string) []byte {
	return s.keys.HMACValue("suppression:"+tenantID.String(), NormalizeEmail(email))
}

// GlobalEmailKey is the platform-wide keyed hash used for the narrow global suppression list (D-09).
// It is a different key from any tenant's, so it links no tenant's records.
func (s *Service) GlobalEmailKey(email string) []byte {
	return s.keys.HMACValue("global-suppression", NormalizeEmail(email))
}
