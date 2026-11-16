// Command engine is the single backend binary. One image runs as different process roles.
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/redis/go-redis/v9"

	"mspengine/db/migrations"
	"mspengine/internal/api"
	"mspengine/internal/app"
	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/config"
	"mspengine/internal/kernel/crypto"
	"mspengine/internal/kernel/db"
	"mspengine/internal/kernel/mailer"
	"mspengine/internal/kernel/ratelimit"
	"mspengine/internal/platform/identity"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if len(os.Args) < 2 {
		usage()
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg, err := config.Load()
	if err != nil {
		fatal(err)
	}
	// Each command checks only what it uses, so a process holds no extra credentials.
	needRuntime := func() error { return errors.Join(cfg.RequireKeys(), cfg.RequireDB("app", "platform", "system")) }
	switch os.Args[1] {
	case "api":
		if err = needRuntime(); err == nil {
			err = runAPI(ctx, cfg)
		}
	case "worker":
		if err = needRuntime(); err == nil {
			err = runWorker(ctx, cfg)
		}
	case "ingest":
		err = errors.New("the ingest role arrives with Visitor Intelligence in Phase 5")
	case "migrate":
		if err = cfg.RequireDB("owner"); err == nil {
			err = runMigrate(ctx, cfg)
		}
	case "create-admin":
		if err = needRuntime(); err == nil {
			err = runCreateAdmin(ctx, cfg, os.Args[2:])
		}
	case "recover-admin":
		if err = needRuntime(); err == nil {
			err = runRecoverAdmin(ctx, cfg, os.Args[2:])
		}
	case "healthcheck":
		err = runHealthcheck(ctx, cfg)
	default:
		usage()
	}
	if err != nil {
		fatal(err)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: engine api | worker | migrate | healthcheck | create-admin --email E --name N | recover-admin --email E --reason R")
	os.Exit(2)
}

func fatal(err error) {
	// Name the fields that failed, so an operator running create-admin sees what to fix.
	var ae *apperr.Error
	if errors.As(err, &ae) && len(ae.Fields) > 0 {
		slog.Error("fatal", "error", err, "fields", ae.Fields)
		if hint := fieldHints(ae.Fields); hint != "" {
			fmt.Fprintln(os.Stderr, hint)
		}
		os.Exit(1)
	}
	slog.Error("fatal", "error", err)
	os.Exit(1)
}

// fieldHints turns validation codes from the admin commands into plain sentences.
func fieldHints(fields []apperr.FieldError) string {
	hints := map[string]string{
		"invalid_email":       "The email address is not valid.",
		"password_length":     "The password must be 12 to 256 characters long.",
		"password_too_common": "The password is too common or easy to guess. Choose a different one.",
	}
	var out []string
	for _, f := range fields {
		if h, ok := hints[f.Code]; ok {
			out = append(out, h)
		} else {
			out = append(out, f.Field+": "+f.Code)
		}
	}
	return strings.Join(out, "\n")
}

func openPools(ctx context.Context, c config.Config) (*db.Pools, error) {
	app, err := db.Open(ctx, db.DSN(c.DBHost, c.DBPort, c.DBName, "msp_app", c.DBAppPassword), 20)
	if err != nil {
		return nil, err
	}
	platform, err := db.Open(ctx, db.DSN(c.DBHost, c.DBPort, c.DBName, "msp_platform", c.DBPlatformPass), 5)
	if err != nil {
		return nil, err
	}
	system, err := db.Open(ctx, db.DSN(c.DBHost, c.DBPort, c.DBName, "msp_system", c.DBSystemPassword), 2)
	if err != nil {
		return nil, err
	}
	return &db.Pools{App: app, Platform: platform, System: system}, nil
}

func runMigrate(ctx context.Context, c config.Config) error {
	conn, err := sql.Open("pgx", db.DSN(c.DBHost, c.DBPort, c.DBName, "msp_owner", c.DBOwnerPassword))
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	return goose.UpContext(ctx, conn, ".")
}

func runAPI(ctx context.Context, c config.Config) error {
	pools, err := openPools(ctx, c)
	if err != nil {
		return err
	}
	defer pools.Close()
	if err := migrations.CheckCurrent(ctx, pools.App); err != nil {
		return err
	}
	rdb := redis.NewClient(&redis.Options{Addr: c.RedisAddr})
	defer func() { _ = rdb.Close() }()
	g, err := app.BuildFromConfig(ctx, c, pools, rdb)
	if err != nil {
		return err
	}
	handler, err := api.New(api.Deps{Config: c, Identity: g.Identity, Commercial: g.Commercial, Keys: g.Keys, Limiter: ratelimit.New(rdb), Hub: g.Hub, DB: pools.App, Redis: rdb,
		Shared: g.Shared, Files: g.Files, Notify: g.Notify, Integrate: g.Integration, AI: g.AI, Pools: pools, Ops: g.Ops, Modules: g.Modules, OutreachPolicy: g.Outreach})
	if err != nil {
		return err
	}
	srv := &http.Server{Addr: c.HTTPAddr, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 2 * time.Minute}
	errs := make(chan error, 1)
	go func() { errs <- srv.ListenAndServe() }()
	slog.Info("api listening", "addr", c.HTTPAddr)
	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
	}
	shut, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return srv.Shutdown(shut)
}

// runWorker runs background jobs, event dispatch and schedules until told to stop.
func runWorker(ctx context.Context, c config.Config) error {
	pools, err := openPools(ctx, c)
	if err != nil {
		return err
	}
	defer pools.Close()
	if err := migrations.CheckCurrent(ctx, pools.App); err != nil {
		return err
	}
	rdb := redis.NewClient(&redis.Options{Addr: c.RedisAddr})
	defer func() { _ = rdb.Close() }()
	g, err := app.BuildFromConfig(ctx, c, pools, rdb)
	if err != nil {
		return err
	}
	if err := g.EnsureSchedules(ctx); err != nil {
		return err
	}
	runner, bus := g.Worker(app.WorkerID())
	done := make(chan struct{})
	go func() {
		defer close(done)
		bus.Run(ctx)
	}()
	slog.Info("worker running")
	runner.Run(ctx)
	<-done
	return nil
}

// runHealthcheck lets a minimal container image, with no shell or curl, check its own API.
func runHealthcheck(ctx context.Context, c config.Config) error {
	_, port, err := net.SplitHostPort(c.HTTPAddr)
	if err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, "http://127.0.0.1:"+port+"/readyz", nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("not ready: %d", resp.StatusCode)
	}
	return nil
}

// runRecoverAdmin is the break-glass path for when no Super Admin can sign in. It needs access to
// the server and its secrets, and the action is recorded in the audit log.
func runRecoverAdmin(ctx context.Context, c config.Config, args []string) error {
	fs := flag.NewFlagSet("recover-admin", flag.ExitOnError)
	email := fs.String("email", "", "admin email")
	reason := fs.String("reason", "", "why this is needed")
	if err := fs.Parse(args); err != nil {
		return err
	}
	password := os.Getenv("ADMIN_PASSWORD")
	if *email == "" || *reason == "" || password == "" {
		return errors.New("--email, --reason and the ADMIN_PASSWORD environment variable are required")
	}
	pools, err := openPools(ctx, c)
	if err != nil {
		return err
	}
	defer pools.Close()
	ids, err := identity.New(pools, crypto.Keys{HMAC: c.HMACKey, Encryption: c.EncryptionKey}, mailer.SMTP{Addr: c.SMTPAddr, From: c.MailFrom}, c.AppBaseURL)
	if err != nil {
		return err
	}
	id, err := ids.AdminIDByEmail(ctx, *email)
	if err != nil {
		return err
	}
	if err := ids.RecoverAdmin(ctx, nil, id, password, *reason); err != nil {
		return err
	}
	time.Sleep(time.Second) // Gives the notice email time to leave before the process exits.
	slog.Info("recovered admin; they must set up two-step verification at next sign-in", "id", id)
	return nil
}

// runCreateAdmin bootstraps a Super Admin. The password comes from the environment, so it never
// appears in shell history or process listings.
func runCreateAdmin(ctx context.Context, c config.Config, args []string) error {
	fs := flag.NewFlagSet("create-admin", flag.ExitOnError)
	email := fs.String("email", "", "admin email")
	name := fs.String("name", "", "display name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	password := os.Getenv("ADMIN_PASSWORD")
	if *email == "" || *name == "" || password == "" {
		return errors.New("--email, --name and the ADMIN_PASSWORD environment variable are required")
	}
	pools, err := openPools(ctx, c)
	if err != nil {
		return err
	}
	defer pools.Close()
	keys := crypto.Keys{HMAC: c.HMACKey, Encryption: c.EncryptionKey}
	ids, err := identity.New(pools, keys, mailer.SMTP{Addr: c.SMTPAddr, From: c.MailFrom}, c.AppBaseURL)
	if err != nil {
		return err
	}
	id, err := ids.CreateAdmin(ctx, nil, *email, *name, password, []string{"super_admin"})
	if err != nil {
		return err
	}
	slog.Info("created super admin", "id", id)
	return nil
}
