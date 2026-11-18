// Package app assembles services. The API, the worker and tests all build the same graph here.
package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/redis/go-redis/v9"

	"mspengine/internal/kernel/config"
	"mspengine/internal/kernel/crypto"
	"mspengine/internal/kernel/db"
	"mspengine/internal/kernel/events"
	"mspengine/internal/kernel/jobs"
	"mspengine/internal/kernel/mailer"
	"mspengine/internal/kernel/modulekit"
	"mspengine/internal/kernel/realtime"
	"mspengine/internal/modules/chatbot"
	cbapp "mspengine/internal/modules/chatbot/app"
	"mspengine/internal/modules/competitorintel"
	ciapp "mspengine/internal/modules/competitorintel/app"
	"mspengine/internal/modules/crm"
	crmapp "mspengine/internal/modules/crm/app"
	"mspengine/internal/modules/customerexpansion"
	ceapp "mspengine/internal/modules/customerexpansion/app"
	"mspengine/internal/modules/itassessment"
	itaapp "mspengine/internal/modules/itassessment/app"
	"mspengine/internal/modules/leadgeneration"
	leadapp "mspengine/internal/modules/leadgeneration/app"
	"mspengine/internal/modules/outreach"
	outapp "mspengine/internal/modules/outreach/app"
	"mspengine/internal/modules/proposals"
	propapp "mspengine/internal/modules/proposals/app"
	"mspengine/internal/modules/salesintel"
	siapp "mspengine/internal/modules/salesintel/app"
	"mspengine/internal/modules/visitorintel"
	viapp "mspengine/internal/modules/visitorintel/app"
	"mspengine/internal/platform/commercial"
	"mspengine/internal/platform/files"
	"mspengine/internal/platform/identity"
	"mspengine/internal/platform/integration"
	"mspengine/internal/platform/integration/aigateway"
	"mspengine/internal/platform/integration/fakes"
	"mspengine/internal/platform/notification"
	"mspengine/internal/platform/pdf"
	"mspengine/internal/platform/shared"
	"mspengine/internal/platform/trust"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Graph holds every service.
type Graph struct {
	Pools       *db.Pools
	Keys        crypto.Keys
	Hub         *realtime.Hub
	Identity    *identity.Service
	Commercial  *commercial.Service
	Shared      *shared.Service
	Files       *files.Service
	Notify      *notification.Service
	Integration *integration.Service
	AI          *aigateway.Gateway
	Ops         *jobs.Ops
	Leads       *leadapp.Service
	Competitors *ciapp.Service
	CRM         *crmapp.Service
	Proposals   *propapp.Service
	Assessments *itaapp.Service
	Outreach    *outapp.Service
	Visitors    *viapp.Service
	Chatbot     *cbapp.Service
	Expansion   *ceapp.Service
	SalesIntel  *siapp.Service
	// FakePayments is the test payment provider, when fake providers are on.
	FakePayments *fakes.FakePayments
	// Trust publishes legal documents and takes abuse reports.
	Trust *trust.Service
	// Modules are the built business modules. The others are served as skeletons.
	Modules []modulekit.Module
}

// Options are what differ between environments and tests.
type Options struct {
	Mailer  mailer.Mailer
	Store   files.Store
	Scanner files.Scanner
	Bucket  string
	BaseURL string
	Redis   *redis.Client
	// FakeProviders registers the fake company data provider. It is never set in production.
	FakeProviders bool
	// NoFakePayments keeps the fake payment provider off even with other fakes, so tests can check
	// that card payments are refused without a provider.
	NoFakePayments bool
	// PDF renders documents. Without it, reports have no PDF.
	PDF pdf.Renderer
	// Fetcher reads competitor pages. Nil means the fake fetcher, which reaches no real website.
	Fetcher ciapp.Fetcher
	// Prober runs assessment checks. Nil means the fake prober, which reaches nothing.
	Prober itaapp.Prober
	// DomainChecker reads sending domains' DNS. Nil means the fake checker, which looks nothing up.
	DomainChecker outapp.DomainChecker
	// RollupDelay gathers analytics events into one metrics rollup. Zero rolls up after every event.
	RollupDelay time.Duration
}

// Build assembles the services. Only fake providers exist until D-05 selects real ones, so no
// customer data can reach an outside AI service from this build.
func Build(pools *db.Pools, keys crypto.Keys, o Options) (*Graph, error) {
	hub := realtime.New(o.Redis)
	ids, err := identity.New(pools, keys, o.Mailer, o.BaseURL)
	if err != nil {
		return nil, err
	}
	com := commercial.New(pools, hub)
	ints := integration.New(pools, keys)
	gw := aigateway.New(pools, com, ints)
	gw.RegisterProvider(&aigateway.FakeProvider{})
	gw.RegisterTask(aigateway.Task{Key: "platform.summarize"})
	if o.FakeProviders {
		ints.RegisterCompanyData(fakes.SampleCompanies())
		// Outbound email has only the sink until D-05. It delivers through the configured mailer,
		// which is the local mail catcher outside production, so no outreach reaches a real inbox.
		ints.RegisterEmailSender(&fakes.SinkEmail{Mailer: o.Mailer})
	}
	g := &Graph{
		Pools: pools, Keys: keys, Hub: hub, Identity: ids, Commercial: com, Integration: ints, AI: gw,
		Shared: shared.New(pools, keys), Files: files.New(pools, o.Store, o.Scanner, o.Bucket),
		Notify: notification.New(pools, o.Mailer, hub), Ops: jobs.NewOps(pools.Platform),
	}
	// Payments are provider-neutral until D-04. Without fake providers there is no card provider, so
	// only bank transfers confirmed by an admin are possible.
	cardProvider := ""
	if o.FakePayments() {
		g.FakePayments = &fakes.FakePayments{Pool: pools.Platform, BaseURL: o.BaseURL}
		ints.RegisterPaymentProvider(g.FakePayments)
		cardProvider = fakes.FakePaymentsKey
	}
	ints.RegisterWebhook(fakes.FakePaymentsKey, integration.VerifyHMAC, func(ctx context.Context, _ *uuid.UUID, body []byte) error {
		var ev integration.PaymentEvent
		if err := json.Unmarshal(body, &ev); err != nil || ev.PaymentRef == "" {
			return jobs.Permanent(fmt.Errorf("bad payment event: %w", err))
		}
		return com.ApplyPaymentEvent(ctx, fakes.FakePaymentsKey, ev)
	})
	g.Trust = trust.New(pools)
	com.SetBilling(commercial.Billing{Integration: ints, Notify: g.Notify, Owner: ids.TenantOwner, CardProvider: cardProvider, BaseURL: o.BaseURL})
	ids.SetSeatLimit(com.SeatLimit)
	comps, compSvc := competitorintel.New(ciapp.Deps{Pools: pools, Identity: ids, Commercial: com, Shared: g.Shared, Integration: ints, Files: g.Files, AI: gw, PDF: o.PDF, Fetcher: o.Fetcher})
	crmMod, crmSvc := crm.New(crmapp.Deps{Pools: pools, Identity: ids, Shared: g.Shared, Files: g.Files, Notify: g.Notify})
	leads, leadSvc := leadgeneration.New(leadapp.Deps{Pools: pools, Identity: ids, Commercial: com, Shared: g.Shared, Integration: ints, Files: g.Files, AI: gw, Competitors: compSvc, CRM: crmSvc})
	g.Leads, g.Competitors, g.CRM = leadSvc, compSvc, crmSvc
	expansion, expSvc := customerexpansion.New(ceapp.Deps{Pools: pools, Registry: crmSvc, Deals: crmSvc, Shared: g.Shared, Commercial: com, Files: g.Files,
		AI: gw, Notify: g.Notify, Currency: ids.ReportingCurrency})
	g.Expansion = expSvc
	intel, intelSvc := salesintel.New(siapp.Deps{Pools: pools, ModuleStatus: com.ModuleStatus, Rates: g.Shared.Rate, Currency: ids.ReportingCurrency, AI: gw, Files: g.Files,
		RollupDelay: o.RollupDelay})
	g.SalesIntel = intelSvc
	props, propSvc := proposals.New(propapp.Deps{Pools: pools, Keys: keys, Registry: crmSvc, Deals: crmSvc, Files: g.Files, PDF: o.PDF, AI: gw, Mailer: o.Mailer, Integration: ints, BaseURL: o.BaseURL})
	assess, assessSvc := itassessment.New(itaapp.Deps{Pools: pools, Registry: crmSvc, Shared: g.Shared, Files: g.Files, PDF: o.PDF, Prober: o.Prober})
	out, outSvc := outreach.New(outapp.Deps{Pools: pools, Keys: keys, Commercial: com, Shared: g.Shared, Integration: ints, AI: gw, Lists: leadSvc, BaseURL: o.BaseURL, Checker: o.DomainChecker})
	vis, visSvc := visitorintel.New(viapp.Deps{Pools: pools, Keys: keys, Commercial: com, Shared: g.Shared, Registry: crmSvc})
	chat, chatSvc := chatbot.New(cbapp.Deps{Pools: pools, Keys: keys, Commercial: com, Shared: g.Shared, AI: gw, Registry: crmSvc})
	g.Proposals, g.Assessments, g.Outreach, g.Visitors, g.Chatbot = propSvc, assessSvc, outSvc, visSvc, chatSvc
	g.Modules = []modulekit.Module{leads, comps, crmMod, expansion, props, assess, out, vis, chat, intel}
	return g, nil
}

// FakePayments reports whether the fake payment provider is registered.
func (o Options) FakePayments() bool { return o.FakeProviders && !o.NoFakePayments }

// Job kinds for housekeeping.
const (
	jobPartitions = "housekeeping.partitions"
	jobCleanup    = "housekeeping.cleanup"
)

// Worker builds the job runner and event bus, with every handler registered.
func (g *Graph) Worker(workerID string) (*jobs.Runner, *events.Bus) {
	r := jobs.NewRunner(g.Pools.System, workerID)
	r.Register(shared.JobEraseContact, g.Shared.HandleErase)
	r.Register(files.JobScan, g.Files.HandleScan)
	r.Register(notification.JobDeliver, g.Notify.HandleDeliver)
	r.Register(integration.JobWebhookDispatch, g.Integration.HandleWebhookDispatch)
	r.Register(commercial.JobSweepReservations, g.Commercial.HandleSweepReservations)
	for kind, h := range g.Commercial.BillingJobs() {
		r.Register(kind, h)
	}
	r.Register(jobPartitions, g.housekeepPartitions)
	r.Register(jobCleanup, g.housekeepCleanup)
	bus := events.New(g.Pools.System, g.Pools.App, func(ctx context.Context, tenant uuid.UUID, module string) (bool, error) {
		status, err := g.Commercial.ModuleStatus(ctx, tenant, module)
		return status == "active", err
	})
	bus.Subscribe(g.Notify.Consumer())
	for _, m := range g.Modules {
		for kind, h := range m.Jobs {
			r.Register(kind, h)
		}
		for _, c := range m.Consumers {
			bus.Subscribe(c)
		}
	}
	return r, bus
}

// EnsureSchedules creates the recurring work.
func (g *Graph) EnsureSchedules(ctx context.Context) error {
	for key, s := range map[string][2]string{
		"partitions":   {"@every 24h", jobPartitions},
		"cleanup":      {"@every 1h", jobCleanup},
		"credit_sweep": {"@every 5m", commercial.JobSweepReservations},
		"billing_tick": {"@every 15m", commercial.JobBillingTick},
		"reconcile":    {"@every 24h", commercial.JobReconcile},
	} {
		if err := jobs.EnsureSchedule(ctx, g.Pools.System, key, s[0], s[1]); err != nil {
			return err
		}
	}
	for _, m := range g.Modules {
		for _, s := range m.Schedules {
			if err := jobs.EnsureSchedule(ctx, g.Pools.System, s.Key, s.Every, s.Kind); err != nil {
				return err
			}
		}
	}
	return nil
}

func (g *Graph) housekeepPartitions(ctx context.Context, _ jobs.Job) error {
	for _, s := range []string{`SELECT audit.ensure_partitions(3)`, `SELECT jobs.ensure_partitions(3)`, `SELECT visitor_intelligence.ensure_partitions(3)`, `SELECT visitor_intelligence.purge_expired()`} {
		if _, err := g.Pools.System.Exec(ctx, s); err != nil {
			return fmt.Errorf("%s: %w", s, err)
		}
	}
	return nil
}

func (g *Graph) housekeepCleanup(ctx context.Context, _ jobs.Job) error {
	return pgx.BeginFunc(ctx, g.Pools.System, func(tx pgx.Tx) error {
		for _, s := range []string{
			`DELETE FROM identity.sessions WHERE expires_at < now() - interval '30 days'`,
			`DELETE FROM identity.platform_admin_sessions WHERE expires_at < now() - interval '30 days'`,
			`DELETE FROM identity.verification_tokens WHERE expires_at < now() - interval '7 days'`,
			`DELETE FROM identity.login_attempts WHERE created_at < now() - interval '90 days'`,
			`DELETE FROM jobs.jobs WHERE status IN ('succeeded', 'failed', 'cancelled') AND finished_at < now() - interval '30 days'`,
			`DELETE FROM jobs.event_deliveries WHERE status = 'processed' AND processed_at < now() - interval '30 days'`,
		} {
			if _, err := tx.Exec(ctx, s); err != nil {
				return fmt.Errorf("%s: %w", s, err)
			}
		}
		return nil
	})
}

// BuildFromConfig assembles real adapters for a running process.
func BuildFromConfig(ctx context.Context, c config.Config, pools *db.Pools, rdb *redis.Client) (*Graph, error) {
	if err := c.RequireScanner(); err != nil {
		return nil, err
	}
	var scanner files.Scanner = files.NoScanner{} // Without a scanner, uploads stay blocked.
	switch {
	case c.FileScanner == "allow-all":
		slog.Warn("files are marked clean without a malware scan; local development only")
		scanner = files.AllowAllScanner{}
	case c.ClamAVAddr != "":
		scanner = files.ClamAV{Addr: c.ClamAVAddr}
	}
	var store files.Store = unavailableStore{}
	if c.S3Endpoint != "" {
		s3 := files.NewS3Store(c.S3Endpoint, c.S3AccessKey, c.S3SecretKey, c.S3Bucket)
		if err := s3.EnsureBucket(ctx); err != nil {
			return nil, fmt.Errorf("prepare file storage: %w", err)
		}
		store = s3
	}
	// Until legal review and D-05, only fake data exists outside production, on reserved domains.
	// An allowlist, so a misspelt production environment never gets fake data.
	fakes := c.IsLocal() || c.Env == "testing"
	shared.AllowReservedTLDs = fakes
	var renderer pdf.Renderer
	if c.GotenbergURL != "" {
		renderer = pdf.NewGotenberg(c.GotenbergURL)
	}
	var fetcher ciapp.Fetcher // Nil is the fake fetcher.
	switch c.ResearchFetcher {
	case "fake", "":
	case "live":
		slog.Warn("competitor research fetches real public websites; check this is allowed before enabling it")
		fetcher = ciapp.NewLiveFetcher()
	default:
		return nil, fmt.Errorf("RESEARCH_FETCHER must be fake or live, not %q", c.ResearchFetcher)
	}
	var prober itaapp.Prober // Nil is the fake prober.
	switch c.AssessmentProbe {
	case "fake", "":
	case "live":
		slog.Warn("IT assessments probe real domains; run them only on domains whose owners authorized it")
		prober = itaapp.NewLiveProber()
	default:
		return nil, fmt.Errorf("ASSESSMENT_PROBE must be fake or live, not %q", c.AssessmentProbe)
	}
	var checker outapp.DomainChecker // Nil is the fake checker.
	switch c.SendingDomainCheck {
	case "fake", "":
	case "live":
		checker = outapp.LiveChecker{}
	default:
		return nil, fmt.Errorf("SENDING_DOMAIN_CHECK must be fake or live, not %q", c.SendingDomainCheck)
	}
	return Build(pools, crypto.Keys{HMAC: c.HMACKey, Encryption: c.EncryptionKey}, Options{
		Mailer: mailer.SMTP{Addr: c.SMTPAddr, From: c.MailFrom}, Store: store, Scanner: scanner, Bucket: c.S3Bucket, BaseURL: c.AppBaseURL, Redis: rdb, FakeProviders: fakes,
		PDF: renderer, Fetcher: fetcher, Prober: prober, DomainChecker: checker, RollupDelay: siapp.DefaultRollupDelay,
	})
}

type unavailableStore struct{}

func (unavailableStore) Put(context.Context, string, []byte, string) error {
	return fmt.Errorf("file storage is not configured")
}

func (unavailableStore) Get(context.Context, string) ([]byte, error) {
	return nil, fmt.Errorf("file storage is not configured")
}

// WorkerID names this process in job records.
func WorkerID() string {
	host, err := os.Hostname()
	if err != nil {
		host = "worker"
	}
	return host + "-" + uuid.NewString()[:8]
}

// FakeCheckoutPage is what the API's fake checkout routes use.
type FakeCheckoutPage interface {
	Info(ctx context.Context, ref string) (fakes.CheckoutInfo, error)
	Complete(ctx context.Context, ref, outcome string) (integration.PaymentEvent, error)
}

// FakeCheckout returns the test checkout, or nil where fake providers are off.
func (g *Graph) FakeCheckout() FakeCheckoutPage {
	if g.FakePayments == nil {
		return nil // A typed nil would look present; the routes must answer not found.
	}
	return g.FakePayments
}
