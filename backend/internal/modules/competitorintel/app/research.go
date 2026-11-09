package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/db"
	"mspengine/internal/kernel/egress"
	"mspengine/internal/kernel/jobs"
	"mspengine/internal/modules/competitorintel/domain"
	"mspengine/internal/modules/competitorintel/repo"
	"mspengine/internal/platform/audit"
	"mspengine/internal/platform/integration/aigateway"
)

type researchPayload struct {
	RunID uuid.UUID `json:"run_id"`
}

// StartRun queues research for a competitor now.
func (s *Service) StartRun(ctx context.Context, p authctx.Principal, competitorID uuid.UUID) (repo.GetRunRow, error) {
	var id uuid.UUID
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		if _, err := q.GetCompetitor(ctx, repo.GetCompetitorParams{TenantID: p.TenantID, ID: competitorID}); err != nil {
			return notFound(err)
		}
		var err error
		id, err = queueRun(ctx, q, tx, p.TenantID, competitorID, nil)
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "competitor.research_started", "competitor", &competitorID, map[string]any{"run_id": id}))
	})
	if err != nil {
		return repo.GetRunRow{}, err
	}
	return s.Run(ctx, p, id)
}

func queueRun(ctx context.Context, q *repo.Queries, tx pgx.Tx, tenant, competitorID uuid.UUID, configID *uuid.UUID) (uuid.UUID, error) {
	id, err := q.InsertRun(ctx, repo.InsertRunParams{TenantID: tenant, CompetitorID: competitorID, ConfigID: configID})
	if err != nil {
		return uuid.Nil, err
	}
	jobID, _, err := jobs.Enqueue(ctx, tx, jobs.Spec{TenantID: &tenant, Kind: JobResearch, Queue: jobs.QueueResearch, MaxAttempts: 3, Payload: researchPayload{RunID: id}, IdempotencyKey: "research:" + id.String()})
	if err != nil {
		return uuid.Nil, err
	}
	return id, q.SetRunJob(ctx, repo.SetRunJobParams{TenantID: tenant, ID: id, JobID: &jobID})
}

// Run returns one research run.
func (s *Service) Run(ctx context.Context, p authctx.Principal, id uuid.UUID) (repo.GetRunRow, error) {
	var out repo.GetRunRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.GetRun(ctx, repo.GetRunParams{TenantID: p.TenantID, ID: id})
		return notFound(err)
	})
	return out, err
}

// Runs lists a competitor's recent runs.
func (s *Service) Runs(ctx context.Context, p authctx.Principal, competitorID uuid.UUID) ([]repo.ListRunsRow, error) {
	var out []repo.ListRunsRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		if _, err := q.GetCompetitor(ctx, repo.GetCompetitorParams{TenantID: p.TenantID, ID: competitorID}); err != nil {
			return notFound(err)
		}
		var err error
		out, err = q.ListRuns(ctx, repo.ListRunsParams{TenantID: p.TenantID, CompetitorID: competitorID})
		return err
	})
	return out, err
}

// CancelRun stops a run that has not finished.
func (s *Service) CancelRun(ctx context.Context, p authctx.Principal, id uuid.UUID) (repo.GetRunRow, error) {
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		jobID, err := q.CancelRun(ctx, repo.CancelRunParams{TenantID: p.TenantID, ID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			if _, err := q.GetRun(ctx, repo.GetRunParams{TenantID: p.TenantID, ID: id}); err != nil {
				return notFound(err)
			}
			return apperr.Conflict("run_finished", "This research run has already finished")
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
		return repo.GetRunRow{}, err
	}
	return s.Run(ctx, p, id)
}

type extracted struct {
	attr, value, quote, label string
	snapshot                  uuid.UUID
	aiRequest                 uuid.UUID
}

// HandleResearch fetches each source, honouring robots.txt, and extracts facts from pages whose
// content changed. Pages are untrusted input: they reach the AI fenced as data, and a fact is
// labelled observed only when its quote is really on the page.
func (s *Service) HandleResearch(ctx context.Context, j jobs.Job) (err error) {
	var pl researchPayload
	defer func() {
		// On the last failure the run is marked failed, so it does not look busy forever.
		if err != nil && j.Final(err) && j.TenantID != nil && pl.RunID != uuid.Nil {
			if ferr := s.systemTx(context.WithoutCancel(ctx), *j.TenantID, func(q *repo.Queries, _ pgx.Tx) error {
				return q.FinishRun(ctx, repo.FinishRunParams{TenantID: *j.TenantID, ID: pl.RunID, Status: "failed"})
			}); ferr != nil {
				slog.WarnContext(ctx, "record failed research run", "error", ferr)
			}
		}
	}()
	if err := j.Decode(&pl); err != nil {
		return err
	}
	if j.TenantID == nil {
		return jobs.Permanent(errors.New("research job without tenant"))
	}
	tenant := *j.TenantID
	var run repo.GetRunRow
	var sources []repo.ListSourcesRow
	proceed := false
	err = s.systemTx(ctx, tenant, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		if run, err = q.GetRun(ctx, repo.GetRunParams{TenantID: tenant, ID: pl.RunID}); err != nil {
			return err
		}
		if run.Status == "queued" {
			if _, err := q.StartRun(ctx, repo.StartRunParams{TenantID: tenant, ID: run.ID}); err != nil {
				return err
			}
		} else if run.Status != "running" {
			return nil
		}
		if _, err := q.GetCompetitor(ctx, repo.GetCompetitorParams{TenantID: tenant, ID: run.CompetitorID}); err != nil {
			return err
		}
		proceed = true
		sources, err = q.ListSources(ctx, repo.ListSourcesParams{TenantID: tenant, CompetitorID: run.CompetitorID})
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // The competitor was removed.
	}
	if err != nil || !proceed {
		return err
	}
	robots := map[string]domain.Robots{}
	lastHit := map[string]time.Time{}
	var facts []extracted
	fetched, failed := 0, 0
	setStatus := func(src uuid.UUID, status string, didFetch bool) {
		if err := s.systemTx(ctx, tenant, func(q *repo.Queries, _ pgx.Tx) error {
			return q.SetSourceStatus(ctx, repo.SetSourceStatusParams{TenantID: tenant, ID: src, AccessStatus: status, Fetched: didFetch})
		}); err != nil {
			slog.WarnContext(ctx, "record source status failed", "error", err)
		}
	}
	for _, src := range sources {
		if err := ctx.Err(); err != nil {
			return err // Cancelled: the run is already marked by whoever cancelled it.
		}
		u, err := s.guard.CheckURL(src.Url)
		if err != nil {
			setStatus(src.ID, "blocked", false)
			failed++
			continue
		}
		host := u.Scheme + "://" + u.Host
		if wait := s.FetchDelay - time.Since(lastHit[host]); s.FetchDelay > 0 && wait > 0 {
			time.Sleep(wait)
		}
		r, ok := robots[host]
		if !ok {
			r = s.robots(ctx, host)
			robots[host] = r
		}
		path := u.EscapedPath()
		if u.RawQuery != "" {
			path += "?" + u.RawQuery
		}
		if !r.Allowed(path) {
			setStatus(src.ID, "disallowed_by_robots", false)
			continue
		}
		page, err := s.Fetcher.Fetch(ctx, u.String())
		lastHit[host] = time.Now()
		if err != nil || page.Status >= 400 {
			status := "error"
			if errors.Is(err, egress.ErrDenied) {
				status = "blocked"
			}
			setStatus(src.ID, status, false)
			failed++
			continue
		}
		fetched++
		setStatus(src.ID, "allowed", true)
		text := domain.PageText(page.Body, page.ContentType)
		sum := sha256.Sum256([]byte(text))
		var snapshot uuid.UUID
		unchanged := false
		err = s.systemTx(ctx, tenant, func(q *repo.Queries, _ pgx.Tx) error {
			last, err := q.LatestSnapshotHash(ctx, repo.LatestSnapshotHashParams{TenantID: tenant, SourceID: src.ID})
			if err == nil && bytes.Equal(last, sum[:]) {
				unchanged = true
				return nil
			}
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
			fileID, err := s.Files.SaveGenerated(ctx, tenant, Module, "snapshot.txt", "text/plain", []byte(text))
			if err != nil {
				return err
			}
			status := int32(page.Status) //nolint:gosec // HTTP status.
			row, err := q.InsertSnapshot(ctx, repo.InsertSnapshotParams{TenantID: tenant, SourceID: src.ID, RunID: &run.ID, ContentHash: sum[:], FileID: &fileID, HttpStatus: &status})
			snapshot = row.ID
			return err
		})
		if err != nil {
			return err
		}
		if unchanged {
			continue // Unchanged content creates no new observations.
		}
		res, err := s.AI.Run(ctx, aigateway.Request{TenantID: tenant, Task: TaskExtract, IdempotencyKey: "extract:" + snapshot.String(),
			Input: aigateway.Input{Trusted: map[string]string{"url": src.Url}, Untrusted: map[string]string{"page": text}}})
		if err != nil {
			slog.WarnContext(ctx, "extraction failed; the snapshot is kept for a later run", "error", err)
			failed++
			continue
		}
		var out struct {
			Observations []struct {
				AttributeType string `json:"attribute_type"`
				Value         string `json:"value"`
				Quote         string `json:"quote"`
			} `json:"observations"`
		}
		if err := json.Unmarshal(res.Output, &out); err != nil {
			failed++
			continue
		}
		for _, o := range out.Observations {
			facts = append(facts, extracted{attr: o.AttributeType, value: o.Value, quote: o.Quote, label: domain.Label(text, o.Quote, o.Value), snapshot: snapshot, aiRequest: res.RequestID})
		}
	}
	status := "completed"
	switch {
	case failed > 0 && fetched == 0:
		status = "failed"
	case failed > 0:
		status = "partial"
	}
	return s.systemTx(ctx, tenant, func(q *repo.Queries, tx pgx.Tx) error {
		cur, err := q.GetRun(ctx, repo.GetRunParams{TenantID: tenant, ID: run.ID})
		if err != nil || cur.Status != "running" {
			return err // Cancelled while it ran.
		}
		changes, err := applyFacts(ctx, q, tx, tenant, run.CompetitorID, facts)
		if err != nil {
			return err
		}
		if err := q.FinishRun(ctx, repo.FinishRunParams{TenantID: tenant, ID: run.ID, Status: status, PagesFetched: int32(fetched)}); err != nil { //nolint:gosec // Small.
			return err
		}
		return emit(ctx, tx, tenant, "research.completed", "competitor", run.CompetitorID, map[string]any{"run_id": run.ID, "status": status, "changes": changes})
	})
}

// robots fetches and parses robots.txt. A missing file allows everything; a server error disallows
// everything, as RFC 9309 says, so an unreachable site is not crawled.
func (s *Service) robots(ctx context.Context, host string) domain.Robots {
	page, err := s.Fetcher.Fetch(ctx, host+"/robots.txt")
	switch {
	case err != nil || page.Status >= 500:
		return domain.ParseRobots([]byte("User-agent: *\nDisallow: /\n"))
	case page.Status >= 400:
		return domain.Robots{}
	}
	return domain.ParseRobots(page.Body)
}

// applyFacts records new observations where an attribute's values changed, supersedes what they
// replace, and records a change event for each. Observations are append-only.
func applyFacts(ctx context.Context, q *repo.Queries, tx pgx.Tx, tenant, competitor uuid.UUID, facts []extracted) (int, error) {
	if len(facts) == 0 {
		return 0, nil
	}
	current, err := q.CurrentObservations(ctx, repo.CurrentObservationsParams{TenantID: tenant, CompetitorID: competitor})
	if err != nil {
		return 0, err
	}
	before := map[string][]string{}
	prevID := map[string]uuid.UUID{}
	for _, o := range current {
		before[o.AttributeType] = append(before[o.AttributeType], valueText(o.Value))
		if _, ok := prevID[o.AttributeType]; !ok {
			prevID[o.AttributeType] = o.ID
		}
	}
	after := map[string][]string{}
	for _, f := range facts {
		after[f.attr] = append(after[f.attr], f.value)
	}
	changes := domain.Changes(before, after)
	for _, c := range changes {
		var ids []uuid.UUID
		seen := map[string]bool{}
		for _, f := range facts {
			if f.attr != c.Attribute || seen[f.value] {
				continue
			}
			seen[f.value] = true
			conf := 0.9
			if f.label == domain.AIEstimate {
				conf = 0.4
			}
			snap, req := f.snapshot, f.aiRequest
			id, err := q.InsertObservation(ctx, repo.InsertObservationParams{TenantID: tenant, CompetitorID: competitor, AttributeType: f.attr, Value: mustJSON(map[string]string{"text": f.value, "quote": f.quote}),
				EvidenceClass: f.label, Confidence: &conf, SourceSnapshotID: &snap, AiRequestID: &req})
			if err != nil {
				return 0, err
			}
			ids = append(ids, id)
		}
		if err := q.SupersedeObservations(ctx, repo.SupersedeObservationsParams{TenantID: tenant, CompetitorID: competitor, AttributeType: c.Attribute, NewID: &ids[0], Keep: ids}); err != nil {
			return 0, err
		}
		var prev *uuid.UUID
		if id, ok := prevID[c.Attribute]; ok {
			prev = &id
		}
		changeID, err := q.InsertChange(ctx, repo.InsertChangeParams{TenantID: tenant, CompetitorID: competitor, AttributeType: c.Attribute, PreviousObservationID: prev, NewObservationID: ids[0], Summary: ptr(c.Summary())})
		if err != nil {
			return 0, err
		}
		if err := emit(ctx, tx, tenant, "change.detected", "competitor", competitor, map[string]any{"change_id": changeID, "attribute_type": c.Attribute}); err != nil {
			return 0, err
		}
	}
	return len(changes), nil
}

func valueText(v []byte) string {
	var o struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(v, &o)
	return o.Text
}

// HandleMonitorTick queues research for every due schedule, for tenants that still hold the module.
// It reads schedules as the system role, then does each tenant's work under that tenant's RLS.
func (s *Service) HandleMonitorTick(ctx context.Context, _ jobs.Job) error {
	var due []repo.DueConfigsRow
	if err := db.Tx(ctx, s.Pools.System, func(tx pgx.Tx) error {
		var err error
		due, err = repo.New(tx).DueConfigs(ctx)
		return err
	}); err != nil {
		return err
	}
	for _, c := range due {
		if status, err := s.Commercial.ModuleStatus(ctx, c.TenantID, Module); err != nil || status != "active" {
			continue
		}
		every, ok := cadences[c.Cadence]
		if !ok {
			continue
		}
		err := s.systemTx(ctx, c.TenantID, func(q *repo.Queries, tx pgx.Tx) error {
			comp, err := q.GetCompetitor(ctx, repo.GetCompetitorParams{TenantID: c.TenantID, ID: c.CompetitorID})
			if errors.Is(err, pgx.ErrNoRows) {
				return nil // The competitor was removed.
			}
			if err != nil {
				return err
			}
			if !comp.MonitoringEnabled {
				return nil
			}
			n, err := q.AdvanceConfig(ctx, repo.AdvanceConfigParams{TenantID: c.TenantID, ID: c.ID, NextRunAt: ptr(time.Now().Add(every))})
			if err != nil || n == 0 {
				return err // Another worker took it.
			}
			_, err = queueRun(ctx, q, tx, c.TenantID, c.CompetitorID, &c.ID)
			return err
		})
		if err != nil {
			return fmt.Errorf("queue scheduled research: %w", err)
		}
	}
	return nil
}
