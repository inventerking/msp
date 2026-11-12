// Package app is IT Assessment's application service. Checks run only on a domain someone with
// authority has authorized, only from the allowlist for that authorization's tier.
package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/db"
	"mspengine/internal/kernel/events"
	"mspengine/internal/kernel/jobs"
	crm "mspengine/internal/modules/crm/contract"
	"mspengine/internal/modules/itassessment/domain"
	"mspengine/internal/modules/itassessment/repo"
	"mspengine/internal/platform/audit"
	"mspengine/internal/platform/files"
	"mspengine/internal/platform/pdf"
	"mspengine/internal/platform/shared"
)

// Module is the entitlement key.
const Module = "it_assessment"

// Job kinds.
const (
	JobRun    = "assessment.run"
	JobReport = "assessment.report"
)

// TermsVersion is the authorization wording users accept. Its text needs legal review (D-12).
const TermsVersion = "2026-11-draft"

// AuthorizationDays is how long an authorization lasts (proposed: 30 days).
const AuthorizationDays = 30

// DKIMSelectors are the common selector names checked. DKIM selectors cannot be listed publicly.
var DKIMSelectors = []string{"default", "selector1", "selector2", "google", "k1", "s1", "s2"}

// Deps are the services IT Assessment uses.
type Deps struct {
	Pools    *db.Pools
	Registry crm.Registry
	Shared   *shared.Service
	Files    *files.Service
	PDF      pdf.Renderer
	Prober   Prober
}

// Service implements IT Assessment.
type Service struct{ Deps }

// New creates the service. Without a prober it uses the fake one, which reaches nothing.
func New(d Deps) *Service {
	if d.Prober == nil {
		d.Prober = NewFakeProber()
	}
	return &Service{Deps: d}
}

func (s *Service) tx(ctx context.Context, p authctx.Principal, fn func(q *repo.Queries, tx pgx.Tx) error) error {
	return db.TenantTx(ctx, s.Pools.App, p.TenantID, p.UserID, func(tx pgx.Tx) error { return fn(repo.New(tx), tx) })
}

func (s *Service) systemTx(ctx context.Context, tenantID uuid.UUID, fn func(q *repo.Queries, tx pgx.Tx) error) error {
	return db.TenantTx(ctx, s.Pools.App, tenantID, uuid.Nil, func(tx pgx.Tx) error { return fn(repo.New(tx), tx) })
}

func notFoundErr(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.NotFound()
	}
	return err
}

func userAudit(p authctx.Principal, action, resource string, id *uuid.UUID, after any) audit.Event {
	return audit.Event{TenantID: &p.TenantID, ActorType: "user", ActorID: &p.UserID, Action: action, ResourceType: resource, ResourceID: id, After: after}
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func uid(p authctx.Principal) *uuid.UUID {
	if p.UserID == uuid.Nil {
		return nil
	}
	id := p.UserID
	return &id
}

func normalize(d string) (string, error) {
	n, err := shared.NormalizeDomain(d)
	if err != nil {
		return "", apperr.Validation(apperr.FieldError{Field: "domain", Code: "invalid_company_domain"})
	}
	return n, nil
}

// Templates.

var defaultTemplates = []struct {
	name, tier string
	checks     []string
}{
	{"Email and website basics", domain.Passive, domain.Allowed[domain.Passive]},
	{"Extended public exposure", domain.Exposure, domain.Allowed[domain.Exposure]},
}

func seedTemplates(ctx context.Context, q *repo.Queries, tenant uuid.UUID) error {
	if n, err := q.CountTemplates(ctx, tenant); err != nil || n > 0 {
		return err
	}
	if _, err := q.LockSeed(ctx, "assessment-templates:"+tenant.String()); err != nil {
		return err
	}
	if n, err := q.CountTemplates(ctx, tenant); err != nil || n > 0 {
		return err
	}
	for _, t := range defaultTemplates {
		if _, err := q.InsertTemplate(ctx, repo.InsertTemplateParams{TenantID: tenant, Name: t.name, Tier: t.tier, CheckKeys: t.checks, ScoringVersion: "1"}); err != nil {
			return err
		}
	}
	return nil
}

// Templates lists templates, seeding the defaults on first use.
func (s *Service) Templates(ctx context.Context, p authctx.Principal) ([]repo.ListTemplatesRow, error) {
	var out []repo.ListTemplatesRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		if err := seedTemplates(ctx, q, p.TenantID); err != nil {
			return err
		}
		var err error
		out, err = q.ListTemplates(ctx, p.TenantID)
		return err
	})
	return out, err
}

// AddTemplate adds a template. Its checks must come from the tier's allowlist.
func (s *Service) AddTemplate(ctx context.Context, p authctx.Principal, name, tier string, checks []string) (uuid.UUID, error) {
	scoped := domain.Scope(tier, checks)
	if len(scoped) == 0 || len(scoped) != len(checks) {
		return uuid.Nil, apperr.Validation(apperr.FieldError{Field: "check_keys", Code: "not_allowed_for_tier"})
	}
	var id uuid.UUID
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		var err error
		if id, err = q.InsertTemplate(ctx, repo.InsertTemplateParams{TenantID: p.TenantID, Name: name, Tier: tier, CheckKeys: scoped, ScoringVersion: "1"}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "assessment.template_added", "assessment_template", &id, map[string]any{"tier": tier, "checks": scoped}))
	})
	return id, err
}

// Domain verification.

// Verification is a started verification with what the user must publish.
type Verification struct {
	ID           uuid.UUID
	Domain       string
	Method       string
	Token        string
	Instructions string
	ExpiresAt    time.Time
}

// StartVerification creates a challenge. The token is shown once and stored only as a hash.
func (s *Service) StartVerification(ctx context.Context, p authctx.Principal, rawDomain, method string) (Verification, error) {
	d, err := normalize(rawDomain)
	if err != nil {
		return Verification{}, err
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return Verification{}, err
	}
	token := hex.EncodeToString(b)
	sum := sha256.Sum256([]byte(token))
	out := Verification{Domain: d, Method: method, Token: token, ExpiresAt: time.Now().Add(7 * 24 * time.Hour)}
	switch method {
	case "dns_txt":
		out.Instructions = fmt.Sprintf("Add a TXT record at _msp-verify.%s with the value msp-verify=%s", d, token)
	case "http_file":
		out.Instructions = fmt.Sprintf("Serve the text %s at https://%s/.well-known/msp-verify.txt", token, d)
	default:
		return Verification{}, apperr.Validation(apperr.FieldError{Field: "method", Code: "invalid"})
	}
	err = s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		var err error
		if out.ID, err = q.InsertVerification(ctx, repo.InsertVerificationParams{TenantID: p.TenantID, Domain: d, Method: method, TokenHash: sum[:], ExpiresAt: out.ExpiresAt}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "assessment.verification_started", "domain_verification", &out.ID, map[string]any{"domain": d, "method": method}))
	})
	return out, err
}

// CheckVerification looks for the published token. With the fake prober, reserved .example and
// .test domains verify automatically, because nothing real is reached.
func (s *Service) CheckVerification(ctx context.Context, p authctx.Principal, id uuid.UUID) (repo.GetVerificationRow, error) {
	var v repo.GetVerificationRow
	if err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		v, err = q.GetVerification(ctx, repo.GetVerificationParams{TenantID: p.TenantID, ID: id})
		return notFoundErr(err)
	}); err != nil {
		return v, err
	}
	if v.Status == "verified" {
		return v, nil
	}
	status := "pending"
	switch {
	case v.ExpiresAt.Before(time.Now()):
		status = "expired"
	case s.found(ctx, v):
		status = "verified"
	}
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		if err := q.SetVerification(ctx, repo.SetVerificationParams{TenantID: p.TenantID, ID: id, Status: status}); err != nil {
			return err
		}
		if status == "verified" {
			return audit.Record(ctx, tx, userAudit(p, "assessment.domain_verified", "domain_verification", &id, map[string]any{"domain": v.Domain, "method": v.Method, "fake_prober": !s.Prober.Live()}))
		}
		return nil
	})
	if err != nil {
		return v, err
	}
	v.Status = status
	return v, nil
}

func (s *Service) found(ctx context.Context, v repo.GetVerificationRow) bool {
	if f, ok := s.Prober.(*FakeProber); ok && f.AutoVerify && (strings.HasSuffix(v.Domain, ".example") || strings.HasSuffix(v.Domain, ".test")) {
		return true
	}
	var candidates []string
	switch v.Method {
	case "dns_txt":
		recs, err := s.Prober.TXT(ctx, "_msp-verify."+v.Domain)
		if err != nil {
			return false
		}
		for _, r := range recs {
			candidates = append(candidates, strings.TrimPrefix(strings.TrimSpace(r), "msp-verify="))
		}
	case "http_file":
		if lp, ok := s.Prober.(*LiveProber); ok {
			r, err := lp.Client.Get(ctx, "https://"+v.Domain+"/.well-known/msp-verify.txt")
			if err == nil && r.Status == http.StatusOK {
				candidates = append(candidates, strings.TrimSpace(string(r.Body)))
			}
		}
	}
	for _, c := range candidates {
		sum := sha256.Sum256([]byte(c))
		if hex.EncodeToString(sum[:]) == hex.EncodeToString(v.TokenHash) {
			return true
		}
	}
	return false
}

// Verifications lists verifications.
func (s *Service) Verifications(ctx context.Context, p authctx.Principal) ([]repo.ListVerificationsRow, error) {
	var out []repo.ListVerificationsRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.ListVerifications(ctx, p.TenantID)
		return err
	})
	return out, err
}

// Authorizations.

// AuthorizeInput records who authorized assessing a domain.
type AuthorizeInput struct {
	Domain, Tier, Name, Email, Role string
	VerificationID                  *uuid.UUID
	Attested                        bool
}

// Authorize records an authorization with its full evidence. The exposure tier also needs verified
// control of the domain (D-12).
func (s *Service) Authorize(ctx context.Context, p authctx.Principal, in AuthorizeInput) (repo.GetAuthorizationRow, error) {
	d, err := normalize(in.Domain)
	if err != nil {
		return repo.GetAuthorizationRow{}, err
	}
	if !in.Attested {
		return repo.GetAuthorizationRow{}, apperr.Validation(apperr.FieldError{Field: "attested", Code: "required"})
	}
	method := "attestation"
	var id uuid.UUID
	err = s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		if in.Tier == domain.Exposure || in.VerificationID != nil {
			if in.VerificationID == nil {
				return apperr.Validation(apperr.FieldError{Field: "domain_verification_id", Code: "required_for_exposure"})
			}
			v, err := q.GetVerification(ctx, repo.GetVerificationParams{TenantID: p.TenantID, ID: *in.VerificationID})
			if err != nil || v.Status != "verified" || v.Domain != d {
				return apperr.Validation(apperr.FieldError{Field: "domain_verification_id", Code: "not_verified_for_domain"})
			}
			method = "verified_domain_control"
		}
		var role *string
		if in.Role != "" {
			role = &in.Role
		}
		var err error
		id, err = q.InsertAuthorization(ctx, repo.InsertAuthorizationParams{TenantID: p.TenantID, Domain: d, Tier: in.Tier, Method: method, AuthorizerName: in.Name, AuthorizerEmail: strings.ToLower(in.Email),
			AuthorizerRole: role, TermsVersion: TermsVersion, DomainVerificationID: in.VerificationID, IpHash: authctx.MetaFrom(ctx).IPHash, ExpiresAt: time.Now().AddDate(0, 0, AuthorizationDays)})
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "assessment.authorized", "authorization", &id, map[string]any{"domain": d, "tier": in.Tier, "method": method,
			"authorizer_name": in.Name, "authorizer_email": in.Email, "authorizer_role": in.Role, "terms_version": TermsVersion}))
	})
	if err != nil {
		return repo.GetAuthorizationRow{}, err
	}
	var out repo.GetAuthorizationRow
	err = s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		out, err = q.GetAuthorization(ctx, repo.GetAuthorizationParams{TenantID: p.TenantID, ID: id})
		return err
	})
	return out, err
}

// Authorizations lists authorizations.
func (s *Service) Authorizations(ctx context.Context, p authctx.Principal) ([]repo.ListAuthorizationsRow, error) {
	var out []repo.ListAuthorizationsRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.ListAuthorizations(ctx, p.TenantID)
		return err
	})
	return out, err
}

// Revoke ends an authorization. Queued runs under it will not start.
func (s *Service) Revoke(ctx context.Context, p authctx.Principal, id uuid.UUID) error {
	return s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		n, err := q.RevokeAuthorization(ctx, repo.RevokeAuthorizationParams{TenantID: p.TenantID, ID: id})
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.Conflict("not_active", "This authorization is not active")
		}
		return audit.Record(ctx, tx, userAudit(p, "assessment.authorization_revoked", "authorization", &id, nil))
	})
}

func active(a repo.GetAuthorizationRow) bool {
	return a.Status == "active" && a.ExpiresAt.After(time.Now())
}

// Assessments.

// Start queues an assessment. Domain and tier come from the authorization, never from the request.
func (s *Service) Start(ctx context.Context, p authctx.Principal, authorizationID, templateID uuid.UUID, crmCompanyID *uuid.UUID) (repo.GetAssessmentRow, error) {
	var auth repo.GetAuthorizationRow
	var tmpl repo.GetTemplateRow
	if err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		if auth, err = q.GetAuthorization(ctx, repo.GetAuthorizationParams{TenantID: p.TenantID, ID: authorizationID}); err != nil {
			return apperr.Validation(apperr.FieldError{Field: "authorization_id", Code: "unknown_authorization"})
		}
		if tmpl, err = q.GetTemplate(ctx, repo.GetTemplateParams{TenantID: p.TenantID, ID: templateID}); err != nil {
			return apperr.Validation(apperr.FieldError{Field: "template_id", Code: "unknown_template"})
		}
		return nil
	}); err != nil {
		return repo.GetAssessmentRow{}, err
	}
	if !active(auth) {
		return repo.GetAssessmentRow{}, apperr.Conflict("authorization_inactive", "This authorization has expired or been revoked")
	}
	if !domain.TierAllows(auth.Tier, tmpl.Tier) {
		return repo.GetAssessmentRow{}, apperr.Validation(apperr.FieldError{Field: "template_id", Code: "tier_not_authorized"})
	}
	if crmCompanyID != nil {
		if _, err := s.Registry.GetCompanySummary(ctx, p.TenantID, *crmCompanyID); err != nil {
			return repo.GetAssessmentRow{}, apperr.Validation(apperr.FieldError{Field: "crm_company_id", Code: "unknown_company"})
		}
	}
	company, err := s.Shared.ResolveOrCreate(ctx, shared.Signal{Domain: auth.Domain})
	if err != nil {
		return repo.GetAssessmentRow{}, err
	}
	if crmCompanyID == nil {
		id, err := s.Registry.EnsureCompany(ctx, crm.CompanyInput{TenantID: p.TenantID, CompanyID: &company, Name: auth.Domain, Domain: &auth.Domain, SourceModule: Module, ActorID: uid(p)})
		if err != nil {
			return repo.GetAssessmentRow{}, err
		}
		crmCompanyID = &id
	}
	var id uuid.UUID
	err = s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		recent, err := q.RecentAssessment(ctx, repo.RecentAssessmentParams{TenantID: p.TenantID, Domain: auth.Domain})
		if err != nil {
			return err
		}
		if recent {
			return apperr.New(http.StatusTooManyRequests, "assessment_rate_limited", "This domain was assessed in the last ten minutes. Try again later")
		}
		if id, err = q.InsertAssessment(ctx, repo.InsertAssessmentParams{TenantID: p.TenantID, AuthorizationID: auth.ID, TemplateID: tmpl.ID, Domain: auth.Domain, CompanyID: &company, CrmCompanyID: crmCompanyID, CreatedBy: uid(p)}); err != nil {
			return err
		}
		jobID, _, err := jobs.Enqueue(ctx, tx, jobs.Spec{TenantID: &p.TenantID, Kind: JobRun, Queue: jobs.QueueAssessment, MaxAttempts: 2, Payload: runPayload{AssessmentID: id}, IdempotencyKey: "assessment:" + id.String()})
		if err != nil {
			return err
		}
		if err := q.SetAssessmentJob(ctx, repo.SetAssessmentJobParams{TenantID: p.TenantID, ID: id, JobID: &jobID}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "assessment.started", "assessment", &id, map[string]any{"domain": auth.Domain, "tier": tmpl.Tier, "template": tmpl.Name}))
	})
	if err != nil {
		return repo.GetAssessmentRow{}, err
	}
	return s.Assessment(ctx, p, id)
}

// Assessment returns one assessment.
func (s *Service) Assessment(ctx context.Context, p authctx.Principal, id uuid.UUID) (repo.GetAssessmentRow, error) {
	var out repo.GetAssessmentRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.GetAssessment(ctx, repo.GetAssessmentParams{TenantID: p.TenantID, ID: id})
		return notFoundErr(err)
	})
	return out, err
}

// Assessments lists assessments.
func (s *Service) Assessments(ctx context.Context, p authctx.Principal) ([]repo.ListAssessmentsRow, error) {
	var out []repo.ListAssessmentsRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.ListAssessments(ctx, p.TenantID)
		return err
	})
	return out, err
}

// Detail is an assessment's checks and findings.
type Detail struct {
	Checks   []repo.ListChecksRow
	Findings []repo.ListFindingsRow
	Coverage float64
}

// Results returns checks and findings.
func (s *Service) Results(ctx context.Context, p authctx.Principal, id uuid.UUID) (Detail, error) {
	var out Detail
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		if _, err := q.GetAssessment(ctx, repo.GetAssessmentParams{TenantID: p.TenantID, ID: id}); err != nil {
			return notFoundErr(err)
		}
		var err error
		if out.Checks, err = q.ListChecks(ctx, repo.ListChecksParams{TenantID: p.TenantID, AssessmentID: id}); err != nil {
			return err
		}
		out.Findings, err = q.ListFindings(ctx, repo.ListFindingsParams{TenantID: p.TenantID, AssessmentID: id})
		return err
	})
	if err == nil {
		res := make([]domain.Result, len(out.Checks))
		for i, c := range out.Checks {
			res[i] = domain.Result{Key: c.CheckKey, Status: c.Status}
		}
		_, out.Coverage, _, _ = domain.Score(res)
	}
	return out, err
}

// Cancel stops an assessment that has not finished.
func (s *Service) Cancel(ctx context.Context, p authctx.Principal, id uuid.UUID) (repo.GetAssessmentRow, error) {
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		jobID, err := q.CancelAssessment(ctx, repo.CancelAssessmentParams{TenantID: p.TenantID, ID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			if _, err := q.GetAssessment(ctx, repo.GetAssessmentParams{TenantID: p.TenantID, ID: id}); err != nil {
				return notFoundErr(err)
			}
			return apperr.Conflict("assessment_finished", "This assessment has already finished")
		}
		if err != nil {
			return err
		}
		if jobID != nil {
			if _, err := jobs.RequestCancel(ctx, tx, *jobID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return repo.GetAssessmentRow{}, err
	}
	return s.Assessment(ctx, p, id)
}

type runPayload struct {
	AssessmentID uuid.UUID `json:"assessment_id"`
}

// HandleRun performs the checks in scope, records each result and its evidence, and scores the
// conclusive ones. Every check is independent: a timeout makes one inconclusive, not the run fail.
func (s *Service) HandleRun(ctx context.Context, j jobs.Job) (err error) {
	var pl runPayload
	if err := j.Decode(&pl); err != nil {
		return err
	}
	if j.TenantID == nil {
		return jobs.Permanent(errors.New("assessment without tenant"))
	}
	tenant := *j.TenantID
	defer func() {
		if err != nil && j.Final(err) {
			_ = s.systemTx(context.WithoutCancel(ctx), tenant, func(q *repo.Queries, _ pgx.Tx) error {
				return q.FinishAssessment(ctx, repo.FinishAssessmentParams{TenantID: tenant, ID: pl.AssessmentID, Status: "failed"})
			})
		}
	}()
	var a repo.GetAssessmentRow
	var auth repo.GetAuthorizationRow
	var tmpl repo.GetTemplateRow
	proceed := false
	err = s.systemTx(ctx, tenant, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		if a, err = q.GetAssessment(ctx, repo.GetAssessmentParams{TenantID: tenant, ID: pl.AssessmentID}); err != nil {
			return err
		}
		if a.Status != "queued" && a.Status != "running" {
			return nil
		}
		if auth, err = q.GetAuthorization(ctx, repo.GetAuthorizationParams{TenantID: tenant, ID: a.AuthorizationID}); err != nil {
			return err
		}
		if tmpl, err = q.GetTemplate(ctx, repo.GetTemplateParams{TenantID: tenant, ID: a.TemplateID}); err != nil {
			return err
		}
		if !active(auth) {
			return q.FinishAssessment(ctx, repo.FinishAssessmentParams{TenantID: tenant, ID: a.ID, Status: "cancelled"})
		}
		proceed = true
		_, err = q.StartAssessment(ctx, repo.StartAssessmentParams{TenantID: tenant, ID: a.ID})
		return err
	})
	if err != nil || !proceed {
		return err
	}
	// The scope is recomputed here from the authorization's tier, whatever the template says.
	scope := domain.Scope(auth.Tier, tmpl.CheckKeys)
	facts := s.probe(ctx, tenant, a.ID, a.Domain, scope)
	results := domain.Evaluate(scope, facts, time.Now())
	score, _, risk, scored := domain.Score(results)
	status := "completed"
	for _, r := range results {
		if r.Status == domain.Inconclusive {
			status = "partial"
		}
	}
	return s.systemTx(ctx, tenant, func(q *repo.Queries, tx pgx.Tx) error {
		cur, err := q.GetAssessment(ctx, repo.GetAssessmentParams{TenantID: tenant, ID: a.ID})
		if err != nil || cur.Status != "running" {
			return err // Cancelled while running.
		}
		if err := q.DeleteFindings(ctx, repo.DeleteFindingsParams{TenantID: tenant, AssessmentID: a.ID}); err != nil {
			return err
		}
		for _, r := range results {
			checkID, err := q.UpsertCheck(ctx, repo.UpsertCheckParams{TenantID: tenant, AssessmentID: a.ID, CheckKey: r.Key, Status: r.Status, Target: r.Target, RawResult: mustJSON(r.Evidence)})
			if err != nil {
				return err
			}
			if f, ok := domain.FindingFor(r); ok {
				if err := q.InsertFinding(ctx, repo.InsertFindingParams{TenantID: tenant, AssessmentID: a.ID, CheckID: checkID, Severity: f.Severity, Classification: f.Classification,
					Title: f.Title, Description: f.Description, Recommendation: f.Recommendation, Evidence: mustJSON(r.Evidence)}); err != nil {
					return err
				}
			}
		}
		params := repo.FinishAssessmentParams{TenantID: tenant, ID: a.ID, Status: status}
		if scored {
			sc := int32(score) //nolint:gosec // 0-100.
			params.Score, params.RiskLevel = &sc, &risk
		}
		if err := q.FinishAssessment(ctx, params); err != nil {
			return err
		}
		payload := map[string]any{"assessment_id": a.ID, "crm_company_id": a.CrmCompanyID, "domain": a.Domain}
		if scored {
			payload["score"] = score
		}
		return events.Append(ctx, tx, events.Event{Type: "it_assessment.assessment.completed", TenantID: &tenant, SourceModule: Module, AggregateType: "assessment", AggregateID: a.ID}, payload)
	})
}

// probe gathers the facts the scope needs. A lookup that errors is recorded as inconclusive.
func (s *Service) probe(ctx context.Context, tenant, assessment uuid.UUID, d string, scope []string) domain.Facts {
	f := domain.Facts{Domain: d}
	need := map[string]bool{}
	for _, k := range scope {
		need[k] = true
	}
	prober := s.Prober
	if lp, ok := s.Prober.(*LiveProber); ok {
		run := *lp // Each run gets its own copy, so concurrent runs record their own decisions.
		prober = &run
		run.OnDecision = func(host string, ip netip.Addr, allowed bool, reason string) {
			decision := "denied"
			if allowed {
				decision = "allowed"
			}
			ipCopy := ip
			var why *string
			if reason != "" {
				why = &reason
			}
			if err := s.systemTx(context.WithoutCancel(ctx), tenant, func(q *repo.Queries, _ pgx.Tx) error {
				return q.InsertEgressDecision(ctx, repo.InsertEgressDecisionParams{TenantID: tenant, AssessmentID: assessment, CheckKey: domain.CheckTLS, RequestedHost: host, ResolvedIp: &ipCopy, Decision: decision, Reason: why})
			}); err != nil {
				slog.WarnContext(ctx, "record egress decision", "error", err)
			}
		}
	}
	lookup := func(name string) ([]string, bool) {
		r, err := prober.TXT(ctx, name)
		if errors.Is(err, ErrNoRecord) {
			return nil, false
		}
		return r, err != nil
	}
	if need[domain.CheckSPF] {
		f.RootTXT, f.RootTXTErr = lookup(d)
	}
	if need[domain.CheckDMARC] {
		f.DMARC, f.DMARCErr = lookup("_dmarc." + d)
	}
	if need[domain.CheckDKIM] {
		failures := 0
		for _, sel := range DKIMSelectors {
			r, failed := lookup(sel + "._domainkey." + d)
			if failed {
				failures++
			}
			if len(r) > 0 {
				f.DKIMFound = append(f.DKIMFound, sel)
			}
		}
		f.DKIMErr = failures == len(DKIMSelectors)
	}
	if need[domain.CheckMX] {
		mx, err := prober.MX(ctx, d)
		f.MX, f.MXErr = mx, err != nil && !errors.Is(err, ErrNoRecord)
	}
	if need[domain.CheckTLS] {
		info, err := prober.TLS(ctx, d)
		f.TLS, f.TLSErr = info, err != nil
	}
	if need[domain.CheckHeaders] {
		h, err := prober.Headers(ctx, "https://"+d+"/")
		f.Headers, f.HeadersErr = h, err != nil
	}
	return f
}
