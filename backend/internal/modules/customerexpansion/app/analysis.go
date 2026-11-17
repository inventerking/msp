package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/jobs"
	"mspengine/internal/modules/customerexpansion/domain"
	"mspengine/internal/modules/customerexpansion/repo"
	"mspengine/internal/platform/audit"
	"mspengine/internal/platform/integration/aigateway"
)

// Analysis is the outcome of one run.
type Analysis struct {
	Health     string
	Created    int
	Refreshed  int
	Expired    int64
	Indicators []domain.Indicator
}

// Analyze runs the rules for one customer: health, gaps and recommendations. It works without AI;
// AI only rewords rationale afterwards, and a failure there leaves the rule wording in place.
func (s *Service) Analyze(ctx context.Context, p authctx.Principal, profile uuid.UUID) (Analysis, error) {
	var pr repo.GetProfileRow
	if err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		pr, err = q.GetProfile(ctx, repo.GetProfileParams{TenantID: p.TenantID, ID: profile})
		return notFound(err)
	}); err != nil {
		return Analysis{}, err
	}
	company, err := s.Registry.GetCompanySummary(ctx, p.TenantID, pr.CrmCompanyID)
	if err != nil {
		return Analysis{}, err
	}
	all, err := s.Shared.TenantServices(ctx, p.TenantID)
	if err != nil {
		return Analysis{}, err
	}
	in := domain.Input{Customer: company.Name, Today: s.today(), Coverage: map[uuid.UUID]domain.Covered{}, PeerPrices: map[uuid.UUID]domain.PeerPrice{}}
	for _, sv := range all {
		cat := ""
		if sv.CategoryKey != nil {
			cat = *sv.CategoryKey
		}
		in.Services = append(in.Services, domain.Service{ID: sv.ID, Name: sv.Name, Status: sv.Status, Category: cat})
	}
	var out Analysis
	err = s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		if err := s.loadFacts(ctx, q, p.TenantID, profile, &in); err != nil {
			return err
		}
		recs, gaps := domain.Analyze(in)
		indicators, health := domain.Health(in)
		out.Health, out.Indicators = health, indicators
		for _, ind := range indicators {
			value, _ := json.Marshal(ind.Value)
			evidence := []byte("{}")
			if ind.Key == "assessment_risk" && in.AssessmentID != nil {
				evidence, _ = json.Marshal(map[string]any{"from_assessment": in.AssessmentID})
			}
			if err := q.InsertIndicator(ctx, repo.InsertIndicatorParams{TenantID: p.TenantID, CustomerProfileID: profile, IndicatorKey: ind.Key, Value: value,
				Polarity: ind.Polarity, Evidence: evidence, AsOf: s.Now()}); err != nil {
				return err
			}
		}
		if err := q.SetComputedHealth(ctx, repo.SetComputedHealthParams{TenantID: p.TenantID, ID: profile, HealthStatus: health}); err != nil {
			return err
		}
		run := uuid.Must(uuid.NewV7())
		for _, g := range gaps {
			if err := q.InsertGap(ctx, repo.InsertGapParams{TenantID: p.TenantID, CustomerProfileID: profile, TenantServiceID: g.ServiceID, GapState: g.State, AnalysisRunID: run}); err != nil {
				return err
			}
		}
		open, err := q.OpenRecommendationKeys(ctx, repo.OpenRecommendationKeysParams{TenantID: p.TenantID, CustomerProfileID: profile})
		if err != nil {
			return err
		}
		openBy := map[string]uuid.UUID{}
		for _, o := range open {
			openBy[o.DedupeKey] = o.ID
		}
		dismissed, err := q.DismissedKeys(ctx, repo.DismissedKeysParams{TenantID: p.TenantID, CustomerProfileID: profile})
		if err != nil {
			return err
		}
		skip := map[string]bool{}
		for _, k := range dismissed {
			skip[k] = true
		}
		keep := []uuid.UUID{}
		for _, r := range recs {
			if skip[r.DedupeKey] {
				continue
			}
			id, exists := openBy[r.DedupeKey]
			if exists {
				if err := q.RefreshRecommendation(ctx, repo.RefreshRecommendationParams{TenantID: p.TenantID, ID: id, Rationale: r.Rationale,
					EstimatedMrrMinor: r.EstimatedMRR, Currency: r.Currency, EvidenceClass: r.EvidenceClass}); err != nil {
					return err
				}
				if err := q.DeleteEvidence(ctx, repo.DeleteEvidenceParams{TenantID: p.TenantID, RecommendationID: id}); err != nil {
					return err
				}
				out.Refreshed++
			} else {
				id, err = q.InsertRecommendation(ctx, repo.InsertRecommendationParams{TenantID: p.TenantID, CustomerProfileID: profile, TenantServiceID: r.ServiceID,
					ContractID: r.ContractID, Kind: r.Kind, Rationale: r.Rationale, EstimatedMrrMinor: r.EstimatedMRR, Currency: r.Currency,
					EvidenceClass: r.EvidenceClass, DedupeKey: r.DedupeKey})
				if err != nil {
					return err
				}
				if err := emit(ctx, tx, p.TenantID, "recommendation.created", "recommendation", id, map[string]any{"recommendation_id": id, "kind": r.Kind}); err != nil {
					return err
				}
				out.Created++
			}
			keep = append(keep, id)
			for _, e := range r.Evidence {
				detail, _ := json.Marshal(e.Detail)
				var refType *string
				if e.ReferenceType != "" {
					refType = &e.ReferenceType
				}
				if err := q.InsertEvidence(ctx, repo.InsertEvidenceParams{TenantID: p.TenantID, RecommendationID: id, Kind: e.Kind, ReferenceType: refType,
					ReferenceID: e.ReferenceID, Detail: detail}); err != nil {
					return err
				}
			}
			if s.AI != nil {
				if _, _, err := jobs.Enqueue(ctx, tx, jobs.Spec{TenantID: &p.TenantID, Kind: JobWord, Queue: jobs.QueueAI, MaxAttempts: 3,
					Payload: map[string]any{"recommendation_id": id}, IdempotencyKey: "expansion-word:" + id.String() + ":" + hash(r.Rationale)}); err != nil {
					return err
				}
			}
		}
		if out.Expired, err = q.ExpireRecommendations(ctx, repo.ExpireRecommendationsParams{TenantID: p.TenantID, CustomerProfileID: profile, Keep: keep}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "expansion.analysis_run", "customer_profile", &profile, map[string]any{
			"health": health, "created": out.Created, "refreshed": out.Refreshed, "expired": out.Expired}))
	})
	return out, err
}

func (s *Service) loadFacts(ctx context.Context, q *repo.Queries, tenant, profile uuid.UUID, in *domain.Input) error {
	cov, err := q.ListCoverage(ctx, repo.ListCoverageParams{TenantID: tenant, CustomerProfileID: profile})
	if err != nil {
		return err
	}
	for _, c := range cov {
		in.Coverage[c.TenantServiceID] = domain.Covered{Coverage: c.Coverage, MRR: c.MrrMinor, Currency: c.Currency}
	}
	contracts, err := q.ListContracts(ctx, repo.ListContractsParams{TenantID: tenant, CustomerProfileID: profile})
	if err != nil {
		return err
	}
	for _, c := range contracts {
		in.Contracts = append(in.Contracts, domain.Contract{ID: c.ID, Status: c.Status, Start: c.StartDate, End: c.EndDate, TermMonths: c.TermMonths,
			AutoRenew: boolOf(c.AutoRenew), NoticeDays: c.NoticeDays, Currency: c.Currency, MRR: c.MrrMinor})
	}
	peers, err := q.PeerPrices(ctx, repo.PeerPricesParams{TenantID: tenant, ExcludeProfile: profile})
	if err != nil {
		return err
	}
	for _, pp := range peers {
		// With several currencies for one service, the most common one is used.
		if cur, ok := in.PeerPrices[pp.TenantServiceID]; !ok || int(pp.Customers) > cur.Customers {
			in.PeerPrices[pp.TenantServiceID] = domain.PeerPrice{Customers: int(pp.Customers), Median: pp.MedianMrrMinor, Currency: pp.Currency}
		}
	}
	a, err := q.LatestAssessment(ctx, repo.LatestAssessmentParams{TenantID: tenant, CustomerProfileID: profile})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var value struct {
		Risk string `json:"risk_level"`
	}
	var ev struct {
		ID uuid.UUID `json:"assessment_id"`
	}
	_ = json.Unmarshal(a.Value, &value)
	_ = json.Unmarshal(a.Evidence, &ev)
	if value.Risk != "" {
		in.AssessmentRisk, in.AssessmentID, in.AssessmentAt = &value.Risk, &ev.ID, &a.AsOf
	}
	return nil
}

func hash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:8])
}

// HandleWord asks AI to reword one recommendation's rationale. The wording is kept only if it keeps
// every fact; otherwise, or if AI is unavailable, the rule wording stays. Either way the job ends.
func (s *Service) HandleWord(ctx context.Context, j jobs.Job) error {
	if j.TenantID == nil {
		return nil
	}
	tenant := *j.TenantID
	var in struct {
		ID uuid.UUID `json:"recommendation_id"`
	}
	if err := j.Decode(&in); err != nil {
		return jobs.Permanent(err)
	}
	var r repo.GetRecommendationRow
	var facts []string
	if err := s.systemTx(ctx, tenant, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		r, err = q.GetRecommendation(ctx, repo.GetRecommendationParams{TenantID: tenant, ID: in.ID})
		if err != nil {
			return err
		}
		ev, err := q.EvidenceFor(ctx, repo.EvidenceForParams{TenantID: tenant, Ids: []uuid.UUID{in.ID}})
		for _, e := range ev {
			facts = append(facts, e.Kind+": "+string(e.Detail))
		}
		return err
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	if r.Status != "open" {
		return nil
	}
	// The rationale and facts come from the tenant's own records and this module's rules. Service and
	// customer names are tenant-entered, so the whole text is passed as data, not instructions.
	res, err := s.AI.Run(ctx, aigateway.Request{TenantID: tenant, Task: TaskWord, IdempotencyKey: "word:" + r.ID.String() + ":" + hash(r.RuleRationale),
		Input: aigateway.Input{Untrusted: map[string]string{"rationale": r.RuleRationale, "facts": strings.Join(facts, "\n")}}})
	if err != nil {
		var ae *apperr.Error
		if errors.As(err, &ae) {
			slog.InfoContext(ctx, "recommendation wording skipped", "code", ae.Code)
			return nil // No credits, no entitlement or no route: the rule wording is complete.
		}
		return err
	}
	var out struct {
		Rationale string `json:"rationale"`
	}
	// Wording that cannot be read, or that changes a fact, is dropped; the rule wording stays.
	if kept := json.Unmarshal(res.Output, &out) == nil && domain.KeepsFacts(r.Kind, r.RuleRationale, out.Rationale); !kept {
		slog.InfoContext(ctx, "recommendation wording rejected", "recommendation_id", r.ID)
		return nil
	}
	return s.systemTx(ctx, tenant, func(q *repo.Queries, tx pgx.Tx) error {
		n, err := q.SetAIWording(ctx, repo.SetAIWordingParams{TenantID: tenant, ID: r.ID, Rationale: strings.TrimSpace(out.Rationale), AiRequestID: &res.RequestID, RuleRationale: r.RuleRationale})
		if err != nil || n == 0 {
			return err
		}
		return audit.Record(ctx, tx, systemAudit(tenant, "expansion.recommendation_worded", "recommendation", &r.ID, map[string]any{"ai_request_id": res.RequestID}))
	})
}
