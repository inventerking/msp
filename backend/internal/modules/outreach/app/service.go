// Package app is Outreach's application service. Nothing is sent unless every send gate passes,
// checked again immediately before each message.
package app

import (
	"context"
	"errors"
	"net"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/crypto"
	"mspengine/internal/kernel/db"
	"mspengine/internal/kernel/events"
	lead "mspengine/internal/modules/leadgeneration/contract"
	"mspengine/internal/modules/outreach/repo"
	"mspengine/internal/platform/audit"
	"mspengine/internal/platform/commercial"
	"mspengine/internal/platform/integration"
	"mspengine/internal/platform/integration/aigateway"
	"mspengine/internal/platform/shared"
)

// Module is the entitlement key.
const Module = "outreach"

// Job kinds.
const (
	JobDispatch = "outreach.dispatch"
	JobSend     = "outreach.send"
)

// TaskDraft is the AI task that drafts a template.
const TaskDraft = "outreach.draft_template"

// SenderProvider is the email provider used for sending. Only the sink exists until D-05.
const SenderProvider = "sink_email"

// Deps are the services Outreach uses.
type Deps struct {
	Pools       *db.Pools
	Keys        crypto.Keys
	Commercial  *commercial.Service
	Shared      *shared.Service
	Integration *integration.Service
	AI          *aigateway.Gateway
	Lists       lead.Lists
	// BaseURL is the public address of the API, for unsubscribe links.
	BaseURL string
	// Checker checks a sending domain's DNS. The fake one passes reserved domains only.
	Checker DomainChecker
}

// Service implements Outreach.
type Service struct{ Deps }

// New creates the service and registers its AI task and email webhook.
func New(d Deps) *Service {
	if d.Checker == nil {
		d.Checker = FakeChecker{}
	}
	s := &Service{Deps: d}
	if d.AI != nil {
		d.AI.RegisterTask(aigateway.Task{Key: TaskDraft, Module: Module, ReserveCredits: 1, Timeout: 60 * time.Second})
	}
	if d.Integration != nil {
		d.Integration.RegisterWebhook(SenderProvider, integration.VerifyHMAC, s.HandleEmailWebhook)
	}
	return s
}

func (s *Service) tx(ctx context.Context, p authctx.Principal, fn func(q *repo.Queries, tx pgx.Tx) error) error {
	return db.TenantTx(ctx, s.Pools.App, p.TenantID, p.UserID, func(tx pgx.Tx) error { return fn(repo.New(tx), tx) })
}

func (s *Service) systemTx(ctx context.Context, tenantID uuid.UUID, fn func(q *repo.Queries, tx pgx.Tx) error) error {
	return db.TenantTx(ctx, s.Pools.App, tenantID, uuid.Nil, func(tx pgx.Tx) error { return fn(repo.New(tx), tx) })
}

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.NotFound()
	}
	return err
}

func userAudit(p authctx.Principal, action, resource string, id *uuid.UUID, after any) audit.Event {
	return audit.Event{TenantID: &p.TenantID, ActorType: "user", ActorID: &p.UserID, Action: action, ResourceType: resource, ResourceID: id, After: after}
}

func emit(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, typ, aggregate string, id uuid.UUID, payload any) error {
	return events.Append(ctx, tx, events.Event{Type: "outreach." + typ, TenantID: &tenantID, SourceModule: Module, AggregateType: aggregate, AggregateID: id}, payload)
}

func uid(p authctx.Principal) *uuid.UUID {
	if p.UserID == uuid.Nil {
		return nil
	}
	id := p.UserID
	return &id
}

func ptr[T any](v T) *T { return &v }

// DomainChecker reads a sending domain's email authentication records.
type DomainChecker interface {
	Check(ctx context.Context, domain string) (spf, dkim, dmarc string)
}

// FakeChecker passes reserved .example and .test domains and nothing else, so no real domain is
// looked up until real sending is approved.
type FakeChecker struct{}

// Check returns pass for reserved domains.
func (FakeChecker) Check(_ context.Context, d string) (string, string, string) {
	if strings.HasSuffix(d, ".example") || strings.HasSuffix(d, ".test") {
		return "pass", "pass", "pass"
	}
	return "unknown", "unknown", "unknown"
}

// LiveChecker reads real DNS.
type LiveChecker struct{ Resolver *net.Resolver }

// Check looks up SPF, a DKIM key on the default selector, and DMARC.
func (c LiveChecker) Check(ctx context.Context, d string) (string, string, string) {
	r := c.Resolver
	if r == nil {
		r = net.DefaultResolver
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	has := func(name, prefix string) bool {
		recs, err := r.LookupTXT(ctx, name)
		if err != nil {
			return false
		}
		for _, x := range recs {
			if strings.HasPrefix(strings.ToLower(x), prefix) {
				return true
			}
		}
		return false
	}
	spf, dkim, dmarc := "fail", "not_detected", "fail"
	if has(d, "v=spf1") {
		spf = "pass"
	}
	if has("default._domainkey."+d, "v=dkim1") {
		dkim = "pass"
	}
	if recs, err := r.LookupTXT(ctx, "_dmarc."+d); err == nil {
		for _, x := range recs {
			if strings.HasPrefix(strings.ToLower(x), "v=dmarc1") {
				dmarc = "pass"
				if strings.Contains(strings.ReplaceAll(strings.ToLower(x), " ", ""), "p=none") {
					dmarc = "none_policy"
				}
			}
		}
	}
	return spf, dkim, dmarc
}
