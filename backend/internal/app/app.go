// Package app assembles services. The API, the worker and tests all build the same graph here.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/redis/go-redis/v9"

	"mspengine/internal/kernel/config"
	"mspengine/internal/kernel/crypto"
	"mspengine/internal/kernel/db"
	"mspengine/internal/kernel/events"
	"mspengine/internal/kernel/jobs"
	"mspengine/internal/kernel/mailer"
	"mspengine/internal/kernel/realtime"
	"mspengine/internal/platform/commercial"
	"mspengine/internal/platform/files"
	"mspengine/internal/platform/identity"
	"mspengine/internal/platform/integration"
	"mspengine/internal/platform/integration/aigateway"
	"mspengine/internal/platform/notification"
	"mspengine/internal/platform/shared"

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
}

// Options are what differ between environments and tests.
type Options struct {
	Mailer  mailer.Mailer
	Store   files.Store
	Scanner files.Scanner
	Bucket  string
	BaseURL string
	Redis   *redis.Client
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
	return &Graph{
		Pools: pools, Keys: keys, Hub: hub, Identity: ids, Commercial: com, Integration: ints, AI: gw,
		Shared: shared.New(pools, keys), Files: files.New(pools, o.Store, o.Scanner, o.Bucket),
		Notify: notification.New(pools, o.Mailer, hub), Ops: jobs.NewOps(pools.Platform),
	}, nil
}

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
	r.Register(jobPartitions, g.housekeepPartitions)
	r.Register(jobCleanup, g.housekeepCleanup)
	bus := events.New(g.Pools.System, g.Pools.App, func(ctx context.Context, tenant uuid.UUID, module string) (bool, error) {
		status, err := g.Commercial.ModuleStatus(ctx, tenant, module)
		return status == "active", err
	})
	bus.Subscribe(g.Notify.Consumer())
	return r, bus
}

// EnsureSchedules creates the recurring work.
func (g *Graph) EnsureSchedules(ctx context.Context) error {
	for key, s := range map[string][2]string{
		"partitions":   {"@every 24h", jobPartitions},
		"cleanup":      {"@every 1h", jobCleanup},
		"credit_sweep": {"@every 5m", commercial.JobSweepReservations},
	} {
		if err := jobs.EnsureSchedule(ctx, g.Pools.System, key, s[0], s[1]); err != nil {
			return err
		}
	}
	return nil
}

func (g *Graph) housekeepPartitions(ctx context.Context, _ jobs.Job) error {
	for _, s := range []string{`SELECT audit.ensure_partitions(3)`, `SELECT jobs.ensure_partitions(3)`} {
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
	return Build(pools, crypto.Keys{HMAC: c.HMACKey, Encryption: c.EncryptionKey}, Options{
		Mailer: mailer.SMTP{Addr: c.SMTPAddr, From: c.MailFrom}, Store: store, Scanner: scanner, Bucket: c.S3Bucket, BaseURL: c.AppBaseURL, Redis: rdb,
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
