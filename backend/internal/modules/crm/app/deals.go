package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/modules/crm/contract"
	"mspengine/internal/modules/crm/domain"
	"mspengine/internal/modules/crm/repo"
	"mspengine/internal/platform/audit"
)

func stageOf(r repo.GetStageRow) domain.Stage {
	return domain.Stage{Key: r.Key, Name: r.Name, Position: int(r.Position), Kind: r.Kind, Probability: int(r.DefaultProbability)}
}

// defaultPipeline returns the tenant's default pipeline, creating the seeded one on first use.
func defaultPipeline(ctx context.Context, q *repo.Queries, tenant uuid.UUID) (uuid.UUID, error) {
	id, err := q.DefaultPipeline(ctx, tenant)
	if err == nil || !errors.Is(err, pgx.ErrNoRows) {
		return id, err
	}
	// Serializes seeding, so two first requests cannot make two default pipelines.
	if _, err := q.LockTenantPipelines(ctx, "crm-pipeline:"+tenant.String()); err != nil {
		return uuid.Nil, err
	}
	if id, err := q.DefaultPipeline(ctx, tenant); err == nil {
		return id, nil
	}
	id, err = q.InsertPipeline(ctx, repo.InsertPipelineParams{TenantID: tenant, Name: "Sales", IsDefault: true})
	if err != nil {
		return uuid.Nil, err
	}
	for _, s := range domain.DefaultStages {
		if _, err := q.InsertStage(ctx, repo.InsertStageParams{TenantID: tenant, PipelineID: id, Key: s.Key, Name: s.Name, Position: int32(s.Position), Kind: s.Kind, DefaultProbability: int32(s.Probability)}); err != nil { //nolint:gosec // Small.
			return uuid.Nil, err
		}
	}
	return id, nil
}

// PipelineView is a pipeline with its stages.
type PipelineView struct {
	repo.ListPipelinesRow
	Stages []repo.ListStagesRow
}

// Pipelines lists pipelines with their stages, seeding the default on first use.
func (s *Service) Pipelines(ctx context.Context, p authctx.Principal) ([]PipelineView, error) {
	var out []PipelineView
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		if _, err := defaultPipeline(ctx, q, p.TenantID); err != nil {
			return err
		}
		ps, err := q.ListPipelines(ctx, p.TenantID)
		if err != nil {
			return err
		}
		for _, pl := range ps {
			st, err := q.ListStages(ctx, repo.ListStagesParams{TenantID: p.TenantID, PipelineID: pl.ID})
			if err != nil {
				return err
			}
			out = append(out, PipelineView{ListPipelinesRow: pl, Stages: st})
		}
		return nil
	})
	return out, err
}

// CreatePipeline adds a pipeline with the default stages, which can then be changed.
func (s *Service) CreatePipeline(ctx context.Context, p authctx.Principal, name string) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		if _, err := defaultPipeline(ctx, q, p.TenantID); err != nil {
			return err
		}
		var err error
		if id, err = q.InsertPipeline(ctx, repo.InsertPipelineParams{TenantID: p.TenantID, Name: name}); err != nil {
			return err
		}
		for _, st := range domain.DefaultStages {
			if _, err := q.InsertStage(ctx, repo.InsertStageParams{TenantID: p.TenantID, PipelineID: id, Key: st.Key, Name: st.Name, Position: int32(st.Position), Kind: st.Kind, DefaultProbability: int32(st.Probability)}); err != nil { //nolint:gosec // Small.
				return err
			}
		}
		return audit.Record(ctx, tx, userAudit(p, "crm.pipeline_created", "pipeline", &id, map[string]any{"name": name}))
	})
	return id, err
}

// UpdatePipeline renames or archives a pipeline. The default pipeline cannot be archived.
func (s *Service) UpdatePipeline(ctx context.Context, p authctx.Principal, id uuid.UUID, name, status *string) error {
	return s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		pl, err := q.GetPipeline(ctx, repo.GetPipelineParams{TenantID: p.TenantID, ID: id})
		if err != nil {
			return notFound(err)
		}
		if status != nil && *status == "archived" && pl.IsDefault {
			return apperr.Conflict("default_pipeline", "The default pipeline cannot be archived")
		}
		if _, err := q.UpdatePipeline(ctx, repo.UpdatePipelineParams{TenantID: p.TenantID, ID: id, Name: name, Status: status}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "crm.pipeline_updated", "pipeline", &id, map[string]any{"name": name, "status": status}))
	})
}

// StageInput is one stage in a replacement list. An ID keeps an existing stage.
type StageInput struct {
	ID *uuid.UUID
	domain.Stage
}

// SetStages replaces a pipeline's stages. A stage that any deal used, now or in history, cannot be
// removed, so historic deals keep their meaning.
func (s *Service) SetStages(ctx context.Context, p authctx.Principal, pipelineID uuid.UUID, in []StageInput) error {
	list := make([]domain.Stage, len(in))
	for i, st := range in {
		st.Position = i + 1
		in[i].Position = i + 1
		list[i] = st.Stage
	}
	if err := domain.ValidStages(list); err != nil {
		return apperr.Validation(apperr.FieldError{Field: "stages", Code: "invalid_stage_list"})
	}
	return s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		if _, err := q.GetPipeline(ctx, repo.GetPipelineParams{TenantID: p.TenantID, ID: pipelineID}); err != nil {
			return notFound(err)
		}
		current, err := q.ListStages(ctx, repo.ListStagesParams{TenantID: p.TenantID, PipelineID: pipelineID})
		if err != nil {
			return err
		}
		keep := map[uuid.UUID]bool{}
		for _, st := range in {
			if st.ID != nil {
				keep[*st.ID] = true
			}
		}
		for _, c := range current {
			if keep[c.ID] {
				continue
			}
			used, err := q.StageInUse(ctx, repo.StageInUseParams{TenantID: p.TenantID, StageID: c.ID})
			if err != nil {
				return err
			}
			if used {
				return apperr.Conflict("stage_in_use", "A stage that deals have used cannot be removed. Rename it instead")
			}
			if err := q.DeleteStage(ctx, repo.DeleteStageParams{TenantID: p.TenantID, ID: c.ID}); err != nil {
				return err
			}
		}
		byID := map[uuid.UUID]repo.ListStagesRow{}
		for _, c := range current {
			byID[c.ID] = c
		}
		for _, st := range in {
			if st.ID != nil {
				old, ok := byID[*st.ID]
				if !ok {
					return apperr.Validation(apperr.FieldError{Field: "stages", Code: "unknown_stage"})
				}
				if old.Kind != st.Kind {
					if used, err := q.StageInUse(ctx, repo.StageInUseParams{TenantID: p.TenantID, StageID: old.ID}); err != nil || used {
						if err != nil {
							return err
						}
						return apperr.Conflict("stage_in_use", "The kind of a stage that deals have used cannot change")
					}
				}
				if err := q.UpdateStage(ctx, repo.UpdateStageParams{TenantID: p.TenantID, ID: old.ID, Name: st.Name, Position: int32(st.Position), Kind: st.Kind, DefaultProbability: int32(st.Probability)}); err != nil { //nolint:gosec // Small.
					return err
				}
				continue
			}
			if _, err := q.InsertStage(ctx, repo.InsertStageParams{TenantID: p.TenantID, PipelineID: pipelineID, Key: st.Key, Name: st.Name, Position: int32(st.Position), Kind: st.Kind, DefaultProbability: int32(st.Probability)}); err != nil { //nolint:gosec // Small.
				if pgCode(err, "23505") {
					return apperr.Validation(apperr.FieldError{Field: "stages", Code: "duplicate_key"})
				}
				return err
			}
		}
		return audit.Record(ctx, tx, userAudit(p, "crm.pipeline_stages_changed", "pipeline", &pipelineID, map[string]any{"stages": len(in)}))
	})
}

// Board is a pipeline's deals by stage.
type Board struct {
	Stages []repo.ListStagesRow
	Deals  []repo.BoardDealsRow
}

// Board returns open deals and those closed in the last 30 days.
func (s *Service) Board(ctx context.Context, p authctx.Principal, pipelineID uuid.UUID) (Board, error) {
	var out Board
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		if _, err := q.GetPipeline(ctx, repo.GetPipelineParams{TenantID: p.TenantID, ID: pipelineID}); err != nil {
			return notFound(err)
		}
		var err error
		if out.Stages, err = q.ListStages(ctx, repo.ListStagesParams{TenantID: p.TenantID, PipelineID: pipelineID}); err != nil {
			return err
		}
		out.Deals, err = q.BoardDeals(ctx, repo.BoardDealsParams{TenantID: p.TenantID, PipelineID: pipelineID})
		return err
	})
	return out, err
}

// DealInput creates a deal.
type DealInput struct {
	CRMCompanyID, PipelineID, StageID, PrimaryContactID, OwnerUserID *uuid.UUID
	Title, Currency                                                  string
	ContractValue, ExpectedMRR                                       *int64
	Probability                                                      *int32
	ExpectedClose                                                    *time.Time
	SourceModule                                                     string
	SourceRef                                                        *uuid.UUID
}

// CreateDeal opens a deal in a pipeline's first open stage, unless another open stage is given.
func (s *Service) CreateDeal(ctx context.Context, p authctx.Principal, in DealInput) (repo.GetDealRow, error) {
	if !domain.ValidCurrency(in.Currency) {
		return repo.GetDealRow{}, apperr.Validation(apperr.FieldError{Field: "currency", Code: "invalid_currency"})
	}
	if in.CRMCompanyID == nil {
		return repo.GetDealRow{}, apperr.Validation(apperr.FieldError{Field: "crm_company_id", Code: "required"})
	}
	if err := s.checkMember(ctx, p.TenantID, in.OwnerUserID, "owner_user_id"); err != nil {
		return repo.GetDealRow{}, err
	}
	var id uuid.UUID
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		var err error
		id, err = createDeal(ctx, q, tx, p.TenantID, uid(p), in)
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "crm.deal_created", "deal", &id, map[string]any{"title": in.Title, "currency": in.Currency, "contract_value_minor": in.ContractValue, "expected_mrr_minor": in.ExpectedMRR}))
	})
	if err != nil {
		return repo.GetDealRow{}, err
	}
	return s.Deal(ctx, p, id)
}

func createDeal(ctx context.Context, q *repo.Queries, tx pgx.Tx, tenant uuid.UUID, actor *uuid.UUID, in DealInput) (uuid.UUID, error) {
	if _, err := q.GetCompany(ctx, repo.GetCompanyParams{TenantID: tenant, ID: *in.CRMCompanyID}); err != nil {
		return uuid.Nil, apperr.Validation(apperr.FieldError{Field: "crm_company_id", Code: "unknown_company"})
	}
	if in.PrimaryContactID != nil {
		if _, err := q.GetContact(ctx, repo.GetContactParams{TenantID: tenant, ID: *in.PrimaryContactID}); err != nil {
			return uuid.Nil, apperr.Validation(apperr.FieldError{Field: "primary_contact_id", Code: "unknown_contact"})
		}
	}
	var pipeline uuid.UUID
	if in.PipelineID != nil {
		pl, err := q.GetPipeline(ctx, repo.GetPipelineParams{TenantID: tenant, ID: *in.PipelineID})
		if err != nil || pl.Status != "active" {
			return uuid.Nil, apperr.Validation(apperr.FieldError{Field: "pipeline_id", Code: "unknown_pipeline"})
		}
		pipeline = pl.ID
	} else {
		var err error
		if pipeline, err = defaultPipeline(ctx, q, tenant); err != nil {
			return uuid.Nil, err
		}
	}
	stages, err := q.ListStages(ctx, repo.ListStagesParams{TenantID: tenant, PipelineID: pipeline})
	if err != nil {
		return uuid.Nil, err
	}
	var stage *repo.ListStagesRow
	for i := range stages {
		st := stages[i]
		if in.StageID != nil && st.ID == *in.StageID || in.StageID == nil && st.Kind == domain.Open && stage == nil {
			stage = &stages[i]
		}
	}
	if stage == nil || stage.Kind != domain.Open {
		return uuid.Nil, apperr.Validation(apperr.FieldError{Field: "stage_id", Code: "not_an_open_stage"})
	}
	prob := stage.DefaultProbability
	if in.Probability != nil {
		prob = *in.Probability
	}
	var src *string
	if in.SourceModule != "" {
		src = &in.SourceModule
	}
	id, err := q.InsertDeal(ctx, repo.InsertDealParams{TenantID: tenant, PipelineID: pipeline, StageID: stage.ID, CrmCompanyID: *in.CRMCompanyID, PrimaryContactID: in.PrimaryContactID,
		Title: strings.TrimSpace(in.Title), OwnerUserID: in.OwnerUserID, Currency: in.Currency, ContractValueMinor: in.ContractValue, ExpectedMrrMinor: in.ExpectedMRR,
		Probability: prob, ExpectedCloseDate: in.ExpectedClose, SourceModule: src, SourceRef: in.SourceRef, CreatedBy: actor})
	if err != nil {
		return uuid.Nil, err
	}
	if err := q.InsertStageHistory(ctx, repo.InsertStageHistoryParams{TenantID: tenant, DealID: id, ToStageID: stage.ID, ChangedBy: actor}); err != nil {
		return uuid.Nil, err
	}
	return id, emit(ctx, tx, tenant, "deal.created", "deal", id, map[string]any{"deal_id": id, "crm_company_id": in.CRMCompanyID, "stage_key": stage.Key})
}

// DealView is a deal with its stage history.
type DealView struct {
	repo.GetDealRow
	History []repo.StageHistoryRow
}

// Deal returns one deal.
func (s *Service) Deal(ctx context.Context, p authctx.Principal, id uuid.UUID) (repo.GetDealRow, error) {
	var out repo.GetDealRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.GetDeal(ctx, repo.GetDealParams{TenantID: p.TenantID, ID: id})
		return notFound(err)
	})
	return out, err
}

// DealDetail returns a deal with its stage history.
func (s *Service) DealDetail(ctx context.Context, p authctx.Principal, id uuid.UUID) (DealView, error) {
	var out DealView
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		if out.GetDealRow, err = q.GetDeal(ctx, repo.GetDealParams{TenantID: p.TenantID, ID: id}); err != nil {
			return notFound(err)
		}
		out.History, err = q.StageHistory(ctx, repo.StageHistoryParams{TenantID: p.TenantID, DealID: id})
		return err
	})
	return out, err
}

// DealFilter narrows the deal list.
type DealFilter struct {
	Status                   *string
	PipelineID, CRMCompanyID *uuid.UUID
}

// Deals lists deals.
func (s *Service) Deals(ctx context.Context, p authctx.Principal, f DealFilter) ([]repo.ListDealsRow, error) {
	var out []repo.ListDealsRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.ListDeals(ctx, repo.ListDealsParams{TenantID: p.TenantID, Status: f.Status, PipelineID: f.PipelineID, CrmCompanyID: f.CRMCompanyID})
		return err
	})
	return out, err
}

// DealPatch changes a deal. Set flags say which nullable fields to change.
type DealPatch struct {
	Title, Currency            *string
	ContractValue, ExpectedMRR *int64
	SetValue, SetMRR           bool
	Probability                *int32
	ExpectedClose              *time.Time
	SetClose                   bool
	Owner, PrimaryContact      *uuid.UUID
	SetOwner, SetContact       bool
	RowVersion                 int32
}

// stale returns the conflict error with the deal's current state, so a board can reconcile.
func (s *Service) stale(ctx context.Context, p authctx.Principal, id uuid.UUID) error {
	if _, err := s.Deal(ctx, p, id); err != nil {
		return err
	}
	return errConflict
}

// UpdateDeal changes a deal's fields, if nobody else changed it first.
func (s *Service) UpdateDeal(ctx context.Context, p authctx.Principal, id uuid.UUID, in DealPatch) (repo.GetDealRow, error) {
	if in.Currency != nil && !domain.ValidCurrency(*in.Currency) {
		return repo.GetDealRow{}, apperr.Validation(apperr.FieldError{Field: "currency", Code: "invalid_currency"})
	}
	if err := s.checkMember(ctx, p.TenantID, in.Owner, "owner_user_id"); err != nil {
		return repo.GetDealRow{}, err
	}
	conflict := false
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		before, err := q.GetDeal(ctx, repo.GetDealParams{TenantID: p.TenantID, ID: id})
		if err != nil {
			return notFound(err)
		}
		if in.SetContact && in.PrimaryContact != nil {
			if _, err := q.GetContact(ctx, repo.GetContactParams{TenantID: p.TenantID, ID: *in.PrimaryContact}); err != nil {
				return apperr.Validation(apperr.FieldError{Field: "primary_contact_id", Code: "unknown_contact"})
			}
		}
		_, err = q.UpdateDeal(ctx, repo.UpdateDealParams{TenantID: p.TenantID, ID: id, RowVersion: in.RowVersion, Title: in.Title, Currency: in.Currency,
			SetValue: in.SetValue, ContractValueMinor: in.ContractValue, SetMrr: in.SetMRR, ExpectedMrrMinor: in.ExpectedMRR, Probability: in.Probability,
			SetClose: in.SetClose, ExpectedCloseDate: in.ExpectedClose, SetOwner: in.SetOwner, OwnerUserID: in.Owner, SetContact: in.SetContact, PrimaryContactID: in.PrimaryContact})
		if errors.Is(err, pgx.ErrNoRows) {
			conflict = true
			return nil
		}
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "crm.deal_updated", "deal", &id, map[string]any{
			"before": map[string]any{"contract_value_minor": before.ContractValueMinor, "expected_mrr_minor": before.ExpectedMrrMinor, "owner_user_id": before.OwnerUserID},
			"title":  in.Title, "contract_value_minor": in.ContractValue, "expected_mrr_minor": in.ExpectedMRR, "owner_changed": in.SetOwner}))
	})
	if err == nil && conflict {
		err = s.stale(ctx, p, id)
	}
	if err != nil {
		return repo.GetDealRow{}, err
	}
	return s.Deal(ctx, p, id)
}

// MoveDeal changes an open deal's stage.
func (s *Service) MoveDeal(ctx context.Context, p authctx.Principal, id, stageID uuid.UUID, rowVersion int32, reason *string) (repo.GetDealRow, error) {
	conflict := false
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		d, err := q.GetDeal(ctx, repo.GetDealParams{TenantID: p.TenantID, ID: id})
		if err != nil {
			return notFound(err)
		}
		if d.Status != domain.Open {
			return apperr.Conflict("deal_closed", "This deal is closed. Reopen it first")
		}
		target, err := q.GetStage(ctx, repo.GetStageParams{TenantID: p.TenantID, ID: stageID})
		if err != nil || target.PipelineID != d.PipelineID {
			return apperr.Validation(apperr.FieldError{Field: "stage_id", Code: "not_in_pipeline"})
		}
		if err := domain.CanMoveTo(stageOf(target)); err != nil {
			return apperr.Validation(apperr.FieldError{Field: "stage_id", Code: "use_win_or_lose"})
		}
		if target.ID == d.StageID {
			if d.RowVersion != rowVersion {
				conflict = true
			}
			return nil
		}
		if _, err := q.MoveDeal(ctx, repo.MoveDealParams{TenantID: p.TenantID, ID: id, StageID: stageID, Probability: target.DefaultProbability, RowVersion: rowVersion}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				conflict = true
				return nil
			}
			return err
		}
		if err := q.InsertStageHistory(ctx, repo.InsertStageHistoryParams{TenantID: p.TenantID, DealID: id, FromStageID: &d.StageID, ToStageID: stageID, ChangedBy: uid(p), Reason: reason}); err != nil {
			return err
		}
		if err := stageActivity(ctx, q, p.TenantID, d, uid(p), "Moved to "+target.Name); err != nil {
			return err
		}
		if err := emit(ctx, tx, p.TenantID, "deal.stage_changed", "deal", id, map[string]any{"deal_id": id, "from_stage_id": d.StageID, "to_stage_id": stageID, "to_stage_key": target.Key}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "crm.deal_moved", "deal", &id, map[string]any{"from": d.StageName, "to": target.Name, "reason": reason}))
	})
	if err == nil && conflict {
		err = s.stale(ctx, p, id)
	}
	if err != nil {
		return repo.GetDealRow{}, err
	}
	return s.Deal(ctx, p, id)
}

func stageActivity(ctx context.Context, q *repo.Queries, tenant uuid.UUID, d repo.GetDealRow, actor *uuid.UUID, summary string) error {
	_, err := q.InsertActivity(ctx, repo.InsertActivityParams{TenantID: tenant, ActivityType: "stage_change", CrmCompanyID: &d.CrmCompanyID, DealID: &d.ID, ActorUserID: actor, Summary: summary, SourceModule: ptr(Module)})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	return err
}

// CloseDeal wins or loses a deal. Closing a deal already in that state returns it unchanged, so a
// retried request publishes one event.
func (s *Service) CloseDeal(ctx context.Context, p authctx.Principal, id uuid.UUID, won bool, rowVersion int32, lossReason *string) (repo.GetDealRow, error) {
	kind := domain.Lost
	if won {
		kind = domain.Won
	}
	conflict := false
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		d, err := q.GetDeal(ctx, repo.GetDealParams{TenantID: p.TenantID, ID: id})
		if err != nil {
			return notFound(err)
		}
		if d.Status == kind {
			return nil // Already done.
		}
		if d.Status != domain.Open {
			return apperr.Conflict("deal_closed", "This deal is already closed. Reopen it first")
		}
		if won {
			if err := domain.CanWin(d.ContractValueMinor, d.ExpectedMrrMinor); err != nil {
				return apperr.Validation(apperr.FieldError{Field: "contract_value_minor", Code: "value_required_to_win"})
			}
		} else if lossReason == nil || strings.TrimSpace(*lossReason) == "" {
			return apperr.Validation(apperr.FieldError{Field: "loss_reason", Code: "required"})
		}
		stages, err := q.ListStages(ctx, repo.ListStagesParams{TenantID: p.TenantID, PipelineID: d.PipelineID})
		if err != nil {
			return err
		}
		var target *repo.ListStagesRow
		for i := range stages {
			if stages[i].Kind == kind {
				target = &stages[i]
			}
		}
		if target == nil {
			return apperr.Conflict("pipeline_incomplete", "This pipeline has no stage for that outcome")
		}
		if _, err := q.CloseDeal(ctx, repo.CloseDealParams{TenantID: p.TenantID, ID: id, StageID: target.ID, Status: kind, LossReason: lossReason, Probability: target.DefaultProbability, RowVersion: rowVersion}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				conflict = true
				return nil
			}
			return err
		}
		if err := q.InsertStageHistory(ctx, repo.InsertStageHistoryParams{TenantID: p.TenantID, DealID: id, FromStageID: &d.StageID, ToStageID: target.ID, ChangedBy: uid(p), Reason: lossReason}); err != nil {
			return err
		}
		summary := "Deal won"
		if !won {
			summary = "Deal lost: " + *lossReason
		}
		if err := stageActivity(ctx, q, p.TenantID, d, uid(p), summary); err != nil {
			return err
		}
		if won {
			if err := setLifecycle(ctx, q, tx, p.TenantID, d.CrmCompanyID, "customer"); err != nil {
				return err
			}
		}
		payload := map[string]any{"deal_id": id, "crm_company_id": d.CrmCompanyID, "currency": d.Currency, "contract_value_minor": d.ContractValueMinor,
			"expected_mrr_minor": d.ExpectedMrrMinor, "closed_on": time.Now().UTC().Format(time.DateOnly)}
		if !won {
			payload = map[string]any{"deal_id": id, "crm_company_id": d.CrmCompanyID}
		}
		if err := emit(ctx, tx, p.TenantID, "deal."+kind, "deal", id, payload); err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "crm.deal_"+kind, "deal", &id, map[string]any{"loss_reason": lossReason, "contract_value_minor": d.ContractValueMinor, "expected_mrr_minor": d.ExpectedMrrMinor}))
	})
	if err == nil && conflict {
		err = s.stale(ctx, p, id)
	}
	if err != nil {
		return repo.GetDealRow{}, err
	}
	return s.Deal(ctx, p, id)
}

// ReopenDeal returns a closed deal to the open stage it was last in.
func (s *Service) ReopenDeal(ctx context.Context, p authctx.Principal, id uuid.UUID) (repo.GetDealRow, error) {
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		d, err := q.GetDeal(ctx, repo.GetDealParams{TenantID: p.TenantID, ID: id})
		if err != nil {
			return notFound(err)
		}
		if d.Status == domain.Open {
			return nil
		}
		stageID, err := q.LastOpenStage(ctx, repo.LastOpenStageParams{TenantID: p.TenantID, DealID: id})
		if err != nil {
			return err
		}
		st, err := q.GetStage(ctx, repo.GetStageParams{TenantID: p.TenantID, ID: stageID})
		if err != nil {
			return err
		}
		if _, err := q.ReopenDeal(ctx, repo.ReopenDealParams{TenantID: p.TenantID, ID: id, StageID: stageID, Probability: st.DefaultProbability}); err != nil {
			return err
		}
		if err := q.InsertStageHistory(ctx, repo.InsertStageHistoryParams{TenantID: p.TenantID, DealID: id, FromStageID: &d.StageID, ToStageID: stageID, ChangedBy: uid(p), Reason: ptr("reopened")}); err != nil {
			return err
		}
		if err := emit(ctx, tx, p.TenantID, "deal.reopened", "deal", id, map[string]any{"deal_id": id, "previous_status": d.Status}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "crm.deal_reopened", "deal", &id, map[string]any{"previous_status": d.Status}))
	})
	if err != nil {
		return repo.GetDealRow{}, err
	}
	return s.Deal(ctx, p, id)
}

// DeleteDeal removes a deal.
func (s *Service) DeleteDeal(ctx context.Context, p authctx.Principal, id uuid.UUID) error {
	return s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		n, err := q.DeleteDeal(ctx, repo.DeleteDealParams{TenantID: p.TenantID, ID: id})
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.NotFound()
		}
		return audit.Record(ctx, tx, userAudit(p, "crm.deal_deleted", "deal", &id, nil))
	})
}

// Deals contract.

// GetDealSummary returns what another module may know about a deal.
func (s *Service) GetDealSummary(ctx context.Context, tenantID, dealID uuid.UUID) (contract.DealSummary, error) {
	var out contract.DealSummary
	err := s.systemTx(ctx, tenantID, func(q *repo.Queries, _ pgx.Tx) error {
		d, err := q.GetDeal(ctx, repo.GetDealParams{TenantID: tenantID, ID: dealID})
		if err != nil {
			return notFound(err)
		}
		st, err := q.GetStage(ctx, repo.GetStageParams{TenantID: tenantID, ID: d.StageID})
		if err != nil {
			return err
		}
		out = contract.DealSummary{ID: d.ID, CRMCompanyID: d.CrmCompanyID, Title: d.Title, Status: d.Status, StageKey: st.Key, Currency: d.Currency}
		return nil
	})
	return out, err
}

// RequestStage applies another module's stage suggestion, forward only.
func (s *Service) RequestStage(ctx context.Context, tenantID, dealID uuid.UUID, stageKey, reason string) (bool, error) {
	moved := false
	err := s.systemTx(ctx, tenantID, func(q *repo.Queries, tx pgx.Tx) error {
		var err error
		moved, err = requestStage(ctx, q, tx, tenantID, dealID, stageKey, reason)
		return err
	})
	return moved, err
}

func requestStage(ctx context.Context, q *repo.Queries, tx pgx.Tx, tenantID, dealID uuid.UUID, stageKey, reason string) (bool, error) {
	d, err := q.GetDeal(ctx, repo.GetDealParams{TenantID: tenantID, ID: dealID})
	if err != nil {
		return false, notFound(err)
	}
	stages, err := q.ListStages(ctx, repo.ListStagesParams{TenantID: tenantID, PipelineID: d.PipelineID})
	if err != nil {
		return false, err
	}
	var current, target *repo.ListStagesRow
	for i := range stages {
		if stages[i].ID == d.StageID {
			current = &stages[i]
		}
		if stages[i].Key == stageKey {
			target = &stages[i]
		}
	}
	if current == nil || target == nil || !domain.AcceptsRequest(stageOf(repo.GetStageRow(*current)), stageOf(repo.GetStageRow(*target)), d.Status == domain.Open) {
		return false, nil
	}
	if _, err := q.MoveDeal(ctx, repo.MoveDealParams{TenantID: tenantID, ID: dealID, StageID: target.ID, Probability: target.DefaultProbability, RowVersion: d.RowVersion}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	if err := q.InsertStageHistory(ctx, repo.InsertStageHistoryParams{TenantID: tenantID, DealID: dealID, FromStageID: &d.StageID, ToStageID: target.ID, Reason: &reason}); err != nil {
		return false, err
	}
	return true, emit(ctx, tx, tenantID, "deal.stage_changed", "deal", dealID, map[string]any{"deal_id": dealID, "from_stage_id": d.StageID, "to_stage_id": target.ID, "to_stage_key": target.Key, "requested": true})
}
