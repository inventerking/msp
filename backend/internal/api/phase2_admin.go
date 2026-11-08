package api

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/api/adminapi"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/db"
	"mspengine/internal/kernel/events"
	"mspengine/internal/kernel/jobs"
	"mspengine/internal/platform/audit"
	"mspengine/internal/platform/integration/repo"
)

func (h adminHandlers) GetTenantCredits(w http.ResponseWriter, r *http.Request, tenantId uuid.UUID) {
	balances, ledger, err := h.commercial.AdminBalances(r.Context(), tenantId)
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := adminapi.CreditDetail{Balances: []adminapi.CreditBalance{}, Ledger: []adminapi.LedgerEntry{}}
	for _, b := range balances {
		out.Balances = append(out.Balances, adminapi.CreditBalance{MeterKey: b.MeterKey, Name: b.Name, Balance: b.Balance, Reserved: b.Reserved})
	}
	for _, l := range ledger {
		out.Ledger = append(out.Ledger, adminapi.LedgerEntry{Id: l.ID, EntryType: l.EntryType, Amount: l.Amount, SourceModule: l.SourceModule, ActorType: l.ActorType, CreatedAt: l.CreatedAt})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h adminHandlers) GrantCredits(w http.ResponseWriter, r *http.Request, tenantId uuid.UUID) {
	var b adminapi.GrantCredits
	if !decode(w, r, &b) {
		return
	}
	if err := h.commercial.AdminGrant(r.Context(), authctx.AdminFrom(r.Context()), tenantId, b.MeterKey, b.Amount, b.IdempotencyKey, b.Reason); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h adminHandlers) MergeCompanies(w http.ResponseWriter, r *http.Request) {
	var b adminapi.MergeCompanies
	if !decode(w, r, &b) {
		return
	}
	if err := h.Shared.MergeCompanies(r.Context(), authctx.AdminFrom(r.Context()), b.SourceId, b.TargetId, b.Reason); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h adminHandlers) GetJobStats(w http.ResponseWriter, r *http.Request) {
	s, err := h.Ops.Stats(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := adminapi.JobStats{Queues: []adminapi.QueueStat{}, OldestUnpublishedSeconds: s.Events.OldestUnpublishedSeconds, PendingDeliveries: s.Events.PendingDeliveries, DeadDeliveries: s.Events.DeadDeliveries}
	for _, q := range s.Queues {
		out.Queues = append(out.Queues, adminapi.QueueStat{Queue: q.Queue, Status: q.Status, Jobs: q.Jobs, OldestSeconds: q.OldestSeconds})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h adminHandlers) ListDeadLetters(w http.ResponseWriter, r *http.Request) {
	rows, err := h.Ops.DeadLetters(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := adminapi.DeadLetterList{Data: []adminapi.DeadLetter{}}
	for _, d := range rows {
		out.Data = append(out.Data, adminapi.DeadLetter{Id: d.ID, TenantId: d.TenantID, Origin: d.Origin, Kind: d.Kind, Error: d.Error, Resolution: d.Resolution, CreatedAt: d.CreatedAt})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h adminHandlers) resolveDeadLetter(w http.ResponseWriter, r *http.Request, id uuid.UUID, requeue bool) {
	var b adminapi.Reason
	if !decode(w, r, &b) {
		return
	}
	a := authctx.AdminFrom(r.Context())
	action := "dead_letter.discarded"
	if requeue {
		action = "dead_letter.requeued"
	}
	err := db.Tx(r.Context(), h.Pools.Platform, func(tx pgx.Tx) error {
		tenant, err := jobs.Resolve(r.Context(), tx, id, a.AdminID, requeue)
		if err != nil {
			return err
		}
		return audit.Record(r.Context(), tx, audit.Event{TenantID: tenant, ActorType: "platform_admin", ActorID: &a.AdminID, Action: action, ResourceType: "dead_letter", ResourceID: &id, After: map[string]any{"reason": b.Reason}})
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h adminHandlers) RequeueDeadLetter(w http.ResponseWriter, r *http.Request, id adminapi.Id) {
	h.resolveDeadLetter(w, r, id, true)
}

func (h adminHandlers) DiscardDeadLetter(w http.ResponseWriter, r *http.Request, id adminapi.Id) {
	h.resolveDeadLetter(w, r, id, false)
}

func (h adminHandlers) ReplayEvents(w http.ResponseWriter, r *http.Request) {
	var b adminapi.ReplayEvents
	if !decode(w, r, &b) {
		return
	}
	a := authctx.AdminFrom(r.Context())
	n, err := events.New(h.Pools.Platform, h.Pools.App, nil).Replay(r.Context(), b.Consumer, b.EventType, b.Since)
	if err != nil {
		writeError(w, r, err)
		return
	}
	err = db.Tx(r.Context(), h.Pools.Platform, func(tx pgx.Tx) error {
		return audit.Record(r.Context(), tx, audit.Event{ActorType: "platform_admin", ActorID: &a.AdminID, Action: "events.replayed", ResourceType: "event_consumer", After: map[string]any{"consumer": b.Consumer, "event_type": b.EventType, "count": n, "reason": b.Reason}})
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, adminapi.ReplayResult{Replayed: n})
}

func (h adminHandlers) ListProviders(w http.ResponseWriter, r *http.Request) {
	rows, err := h.Integrate.ListProviders(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := adminapi.ProviderList{Data: []adminapi.IntegrationProvider{}}
	for _, p := range rows {
		out.Data = append(out.Data, adminapi.IntegrationProvider{Id: p.ID, Key: p.Key, Kind: p.Kind, Name: p.Name, Status: p.Status, HasCredential: p.HasCredential, CircuitOpen: p.Circuit, Calls24h: p.Calls24h})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h adminHandlers) SetProviderStatus(w http.ResponseWriter, r *http.Request, id adminapi.Id) {
	var b adminapi.ProviderStatus
	if !decode(w, r, &b) {
		return
	}
	if err := h.Integrate.SetProviderStatus(r.Context(), authctx.AdminFrom(r.Context()), id, string(b.Status)); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h adminHandlers) SetProviderCredential(w http.ResponseWriter, r *http.Request, id adminapi.Id) {
	var b adminapi.ProviderCredential
	if !decode(w, r, &b) {
		return
	}
	if err := h.Integrate.SetCredential(r.Context(), authctx.AdminFrom(r.Context()), id, b.Label, b.Secret); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h adminHandlers) CreateWebhookEndpoint(w http.ResponseWriter, r *http.Request) {
	var b adminapi.CreateWebhookEndpoint
	if !decode(w, r, &b) {
		return
	}
	id, token, secret, err := h.Integrate.CreateEndpoint(r.Context(), authctx.AdminFrom(r.Context()), b.TenantId, b.ProviderKey)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, adminapi.WebhookEndpoint{Id: id, PathToken: token, Secret: secret})
}

func (h adminHandlers) ListAiRoutes(w http.ResponseWriter, r *http.Request) {
	rows, err := h.AI.ListRoutes(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := adminapi.AiRouteList{Data: []adminapi.AiRoute{}}
	for _, x := range rows {
		out.Data = append(out.Data, adminapi.AiRoute{Id: x.ID, TaskKey: x.TaskKey, ProviderKey: x.ProviderKey, Model: x.Model, MaxInputTokens: int(x.MaxInputTokens), MaxOutputTokens: int(x.MaxOutputTokens), Status: x.Status})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h adminHandlers) UpdateAiRoute(w http.ResponseWriter, r *http.Request, id adminapi.Id) {
	var b adminapi.UpdateAiRoute
	if !decode(w, r, &b) {
		return
	}
	err := h.AI.UpdateRoute(r.Context(), authctx.AdminFrom(r.Context()), repo.UpdateRouteParams{ID: id, Model: b.Model, MaxInputTokens: int32(b.MaxInputTokens), MaxOutputTokens: int32(b.MaxOutputTokens), Status: string(b.Status)}) //nolint:gosec // The contract bounds these.
	if err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h adminHandlers) ListAiPrompts(w http.ResponseWriter, r *http.Request) {
	rows, err := h.AI.ListPrompts(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := adminapi.AiPromptList{Data: []adminapi.AiPrompt{}}
	for _, p := range rows {
		out.Data = append(out.Data, adminapi.AiPrompt{Id: p.ID, Key: p.Key, TaskKey: p.TaskKey, ActionClass: p.ActionClass, Status: p.Status, ActiveVersion: int(p.ActiveVersion), Versions: int(p.Versions)})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h adminHandlers) CreateAiPromptVersion(w http.ResponseWriter, r *http.Request, id adminapi.Id) {
	var b adminapi.CreateAiPromptVersion
	if !decode(w, r, &b) {
		return
	}
	v, err := h.AI.CreatePromptVersion(r.Context(), authctx.AdminFrom(r.Context()), id, b.SystemText, b.UserText)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, adminapi.CreatedId{Id: v})
}

func (h adminHandlers) ActivateAiPromptVersion(w http.ResponseWriter, r *http.Request, id adminapi.Id) {
	if err := h.AI.ActivatePromptVersion(r.Context(), authctx.AdminFrom(r.Context()), id); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
