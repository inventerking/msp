package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/jobs"
	"mspengine/internal/modules/leadgeneration/domain"
	"mspengine/internal/modules/leadgeneration/repo"
	"mspengine/internal/platform/audit"
	"mspengine/internal/platform/integration/aigateway"
)

var errNoICP = apperr.Conflict("icp_not_configured", "Set up your ideal customer profile before scoring")

// ScoreView is a score with its factors.
type ScoreView struct {
	ID           uuid.UUID
	Value        int32
	Coverage     float64
	ModelName    string
	ModelVersion int32
	Factors      []domain.Factor
	Explanation  *string
	ExplainedBy  *uuid.UUID
	ScoredAt     time.Time
}

func (s *Service) icp(ctx context.Context, tenant uuid.UUID) (domain.ICP, bool, error) {
	icp, found, err := s.Identity.DefaultICP(ctx, tenant)
	if err != nil || !found {
		return domain.ICP{}, false, err
	}
	out := domain.ICP{Industries: icp.Industries, Countries: icp.Countries, Technologies: icp.Technologies}
	if icp.MinEmployees != nil {
		v := int(*icp.MinEmployees)
		out.MinEmployees = &v
	}
	if icp.MaxEmployees != nil {
		v := int(*icp.MaxEmployees)
		out.MaxEmployees = &v
	}
	return out, true, nil
}

func activeModel(ctx context.Context, q *repo.Queries, tenant uuid.UUID) (uuid.UUID, domain.Weights, error) {
	m, err := q.ActiveScoringModel(ctx, tenant)
	if errors.Is(err, pgx.ErrNoRows) {
		// Every tenant starts on the default model, created when first needed.
		row, err := q.InsertScoringModel(ctx, repo.InsertScoringModelParams{TenantID: tenant, Name: "Default", Weights: mustJSON(domain.DefaultWeights), Status: "active"})
		return row.ID, domain.DefaultWeights, err
	}
	if err != nil {
		return uuid.Nil, domain.Weights{}, err
	}
	var w domain.Weights
	return m.ID, w, json.Unmarshal(m.Weights, &w)
}

// facts reads the newest value of each attribute. A value that cannot be read counts as unknown.
func facts(rows []repo.LatestAttributesRow) domain.Facts {
	var f domain.Facts
	for _, r := range rows {
		switch r.Attribute {
		case attrIndustry:
			_ = json.Unmarshal(r.Value, &f.Industry)
		case attrCountry:
			_ = json.Unmarshal(r.Value, &f.Country)
		case attrEmployees:
			var n int
			if json.Unmarshal(r.Value, &n) == nil && n > 0 {
				f.Employees = &n
			}
		case attrTechnology:
			_ = json.Unmarshal(r.Value, &f.Technologies)
		}
	}
	return f
}

// score computes and stores a new score for a prospect.
func (s *Service) score(ctx context.Context, tenant uuid.UUID, prospect uuid.UUID) error {
	icp, found, err := s.icp(ctx, tenant)
	if err != nil {
		return err
	}
	if !found {
		return errNoICP
	}
	return s.systemTx(ctx, tenant, func(q *repo.Queries, tx pgx.Tx) error {
		if _, err := q.GetProspect(ctx, repo.GetProspectParams{TenantID: tenant, ID: prospect}); err != nil {
			return notFound(err)
		}
		modelID, weights, err := activeModel(ctx, q, tenant)
		if err != nil {
			return err
		}
		attrs, err := q.LatestAttributes(ctx, repo.LatestAttributesParams{TenantID: tenant, ProspectID: prospect})
		if err != nil {
			return err
		}
		sc, ok := domain.ScoreProspect(icp, weights, facts(attrs))
		if !ok {
			return errNoICP
		}
		row, err := q.InsertScore(ctx, repo.InsertScoreParams{TenantID: tenant, ProspectID: prospect, ModelID: modelID, Score: int32(sc.Value), Coverage: sc.Coverage}) //nolint:gosec // 0-100.
		if err != nil {
			return err
		}
		for _, f := range sc.Factors {
			if err := q.InsertScoreFactor(ctx, repo.InsertScoreFactorParams{TenantID: tenant, ScoreID: row.ID, FactorKey: f.Key, State: f.State, Weight: f.Weight, Earned: f.Earned, Evidence: mustJSON(f.Evidence)}); err != nil {
				return err
			}
		}
		if err := q.SetProspectScore(ctx, repo.SetProspectScoreParams{TenantID: tenant, ID: prospect, Score: ptr(int32(sc.Value)), Coverage: &sc.Coverage, DataQuality: domain.DataQuality(sc.Coverage, true)}); err != nil { //nolint:gosec // 0-100.
			return err
		}
		return emit(ctx, tx, tenant, "prospect.scored", "prospect", prospect, map[string]any{"prospect_id": prospect, "score": sc.Value, "coverage": sc.Coverage})
	})
}

// scoreQuietly scores new or changed prospects. A tenant without an ICP simply has no scores yet.
func (s *Service) scoreQuietly(ctx context.Context, tenant uuid.UUID, ids []uuid.UUID) {
	for _, id := range ids {
		if err := s.score(ctx, tenant, id); err != nil && !errors.Is(err, errNoICP) {
			slog.WarnContext(ctx, "score prospect failed", "error", err)
		}
	}
}

// Rescore computes a fresh score now.
func (s *Service) Rescore(ctx context.Context, p authctx.Principal, id uuid.UUID) (ScoreView, error) {
	if _, err := s.GetProspect(ctx, p, id); err != nil {
		return ScoreView{}, err
	}
	if err := s.score(ctx, p.TenantID, id); err != nil {
		return ScoreView{}, err
	}
	return s.Score(ctx, p, id)
}

// Score returns the latest score with its factors.
func (s *Service) Score(ctx context.Context, p authctx.Principal, id uuid.UUID) (ScoreView, error) {
	var out ScoreView
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		if _, err := q.GetProspect(ctx, repo.GetProspectParams{TenantID: p.TenantID, ID: id}); err != nil {
			return notFound(err)
		}
		sc, err := q.LatestScore(ctx, repo.LatestScoreParams{TenantID: p.TenantID, ProspectID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.New(http.StatusNotFound, "not_scored", "This prospect has no score yet")
		}
		if err != nil {
			return err
		}
		out = ScoreView{ID: sc.ID, Value: sc.Score, Coverage: sc.Coverage, ModelName: sc.ModelName, ModelVersion: sc.ModelVersion, Explanation: sc.Explanation, ExplainedBy: sc.AiRequestID, ScoredAt: sc.CreatedAt}
		fs, err := q.ScoreFactors(ctx, repo.ScoreFactorsParams{TenantID: p.TenantID, ScoreID: sc.ID})
		if err != nil {
			return err
		}
		for _, f := range fs {
			ev := map[string]any{}
			_ = json.Unmarshal(f.Evidence, &ev)
			out.Factors = append(out.Factors, domain.Factor{Key: f.FactorKey, State: f.State, Weight: f.Weight, Earned: f.Earned, Evidence: ev})
		}
		return nil
	})
	return out, err
}

type explainPayload struct {
	ScoreID uuid.UUID `json:"score_id"`
}

// Explain queues an AI explanation of the latest score. It is informational, and labelled as AI.
func (s *Service) Explain(ctx context.Context, p authctx.Principal, id uuid.UUID) (uuid.UUID, error) {
	sc, err := s.Score(ctx, p, id)
	if err != nil {
		return uuid.Nil, err
	}
	var jobID uuid.UUID
	err = s.tx(ctx, p, func(_ *repo.Queries, tx pgx.Tx) error {
		var err error
		jobID, _, err = jobs.Enqueue(ctx, tx, jobs.Spec{TenantID: &p.TenantID, Kind: JobExplain, Queue: jobs.QueueAI, MaxAttempts: 3, Payload: explainPayload{ScoreID: sc.ID}, IdempotencyKey: "explain:" + sc.ID.String()})
		return err
	})
	return jobID, err
}

// HandleExplain asks the AI gateway to explain a score. Only the score and its factors are sent:
// no names, domains or contacts.
func (s *Service) HandleExplain(ctx context.Context, j jobs.Job) error {
	var pl explainPayload
	if err := j.Decode(&pl); err != nil {
		return err
	}
	if j.TenantID == nil {
		return jobs.Permanent(errors.New("explain job without tenant"))
	}
	tenant := *j.TenantID
	var sc repo.LatestScoreRow
	var factors []repo.ScoreFactorsRow
	err := s.systemTx(ctx, tenant, func(q *repo.Queries, _ pgx.Tx) error {
		row, err := q.GetScoreByID(ctx, repo.GetScoreByIDParams{TenantID: tenant, ID: pl.ScoreID})
		if err != nil {
			return err
		}
		sc = repo.LatestScoreRow(row)
		factors, err = q.ScoreFactors(ctx, repo.ScoreFactorsParams{TenantID: tenant, ScoreID: pl.ScoreID})
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if sc.Explanation != nil {
		return nil // Already explained.
	}
	type factorIn struct {
		Key    string   `json:"key"`
		State  string   `json:"state"`
		Weight float64  `json:"weight"`
		Earned *float64 `json:"earned"`
	}
	fin := make([]factorIn, len(factors))
	for i, f := range factors {
		fin[i] = factorIn{f.FactorKey, f.State, f.Weight, f.Earned}
	}
	res, err := s.AI.Run(ctx, aigateway.Request{TenantID: tenant, Task: TaskExplain, IdempotencyKey: "explain:" + pl.ScoreID.String(), Input: aigateway.Input{Trusted: map[string]string{
		"score": fmt.Sprint(sc.Score), "coverage": fmt.Sprintf("%.2f", sc.Coverage), "factors": string(mustJSON(fin)),
	}}})
	if err != nil {
		var ae *apperr.Error
		if errors.As(err, &ae) && ae.Status < 500 {
			return jobs.Permanent(err) // Out of credits or not entitled: retrying will not help.
		}
		return err
	}
	var out struct {
		Explanation string `json:"explanation"`
	}
	if err := json.Unmarshal(res.Output, &out); err != nil || out.Explanation == "" {
		return jobs.Permanent(errors.New("explanation output had no text"))
	}
	return s.systemTx(ctx, tenant, func(q *repo.Queries, _ pgx.Tx) error {
		return q.SetExplanation(ctx, repo.SetExplanationParams{TenantID: tenant, ID: pl.ScoreID, Explanation: &out.Explanation, AiRequestID: &res.RequestID})
	})
}

// Scoring models.

// ListModels returns every model version.
func (s *Service) ListModels(ctx context.Context, p authctx.Principal) ([]repo.ListScoringModelsRow, error) {
	var out []repo.ListScoringModelsRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		if _, _, err := activeModel(ctx, q, p.TenantID); err != nil {
			return err
		}
		var err error
		out, err = q.ListScoringModels(ctx, p.TenantID)
		return err
	})
	return out, err
}

// CreateModel adds a draft model version. Drafts do not score anything until published.
func (s *Service) CreateModel(ctx context.Context, p authctx.Principal, name string, w domain.Weights) (repo.InsertScoringModelRow, error) {
	if w.Industry+w.Geography+w.Employees+w.Technology <= 0 {
		return repo.InsertScoringModelRow{}, apperr.Validation(apperr.FieldError{Field: "weights", Code: "all_zero"})
	}
	var out repo.InsertScoringModelRow
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		var err error
		out, err = q.InsertScoringModel(ctx, repo.InsertScoringModelParams{TenantID: p.TenantID, Name: name, Weights: mustJSON(w), Status: "draft", CreatedBy: uid(p)})
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "lead.scoring_model_created", "scoring_model", &out.ID, map[string]any{"name": name, "version": out.Version, "weights": w}))
	})
	return out, err
}

// PublishModel makes a draft the active model. Existing scores keep the version that produced them.
func (s *Service) PublishModel(ctx context.Context, p authctx.Principal, id uuid.UUID) (repo.GetScoringModelRow, error) {
	var out repo.GetScoringModelRow
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		m, err := q.GetScoringModel(ctx, repo.GetScoringModelParams{TenantID: p.TenantID, ID: id})
		if err != nil {
			return notFound(err)
		}
		if m.Status != "draft" {
			return apperr.Conflict("model_not_draft", "Only a draft model can be published")
		}
		if err := q.RetireActiveModels(ctx, p.TenantID); err != nil {
			return err
		}
		if _, err := q.ActivateModel(ctx, repo.ActivateModelParams{TenantID: p.TenantID, ID: id}); err != nil {
			return err
		}
		if out, err = q.GetScoringModel(ctx, repo.GetScoringModelParams{TenantID: p.TenantID, ID: id}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "lead.scoring_model_published", "scoring_model", &id, map[string]any{"name": m.Name, "version": m.Version}))
	})
	return out, err
}
