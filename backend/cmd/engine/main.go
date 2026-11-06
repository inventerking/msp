// Command engine is the single backend binary. One image runs as different process roles.
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log/slog"
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
	"mspengine/internal/kernel/config"
	"mspengine/internal/kernel/crypto"
	"mspengine/internal/kernel/db"
	"mspengine/internal/kernel/mailer"
	"mspengine/internal/kernel/ratelimit"
	"mspengine/internal/kernel/realtime"
	"mspengine/internal/platform/commercial"
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
	switch os.Args[1] {
	case "api":
		err = runAPI(ctx, cfg)
	case "worker":
		err = runWorker(ctx, cfg)
	case "ingest":
		err = errors.New("the ingest role arrives with Visitor Intelligence in Phase 5")
	case "migrate":
		err = runMigrate(ctx, cfg)
	case "create-admin":
		err = runCreateAdmin(ctx, cfg, os.Args[2:])
	default:
		usage()
	}
	if err != nil {
		fatal(err)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: engine api | worker | migrate | create-admin --email E --name N")
	os.Exit(2)
}

func fatal(err error) {
	slog.Error("fatal", "error", err)
	os.Exit(1)
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
	rdb := redis.NewClient(&redis.Options{Addr: c.RedisAddr})
	defer func() { _ = rdb.Close() }()
	keys := crypto.Keys{HMAC: c.HMACKey, Encryption: c.EncryptionKey}
	hub := realtime.New(rdb)
	ids, err := identity.New(pools, keys, mailer.SMTP{Addr: c.SMTPAddr, From: c.MailFrom}, c.AppBaseURL)
	if err != nil {
		return err
	}
	handler, err := api.New(api.Deps{Config: c, Identity: ids, Commercial: commercial.New(pools, hub), Keys: keys, Limiter: ratelimit.New(rdb), Hub: hub, DB: pools.App, Redis: rdb})
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

// runWorker runs Phase 1 housekeeping. The full job system arrives in Phase 2.
func runWorker(ctx context.Context, c config.Config) error {
	pool, err := db.Open(ctx, db.DSN(c.DBHost, c.DBPort, c.DBName, "msp_system", c.DBSystemPassword), 2)
	if err != nil {
		return err
	}
	defer pool.Close()
	tick := func() {
		statements := []string{
			`SELECT audit.ensure_partitions(3)`,
			`DELETE FROM identity.sessions WHERE expires_at < now() - interval '30 days'`,
			`DELETE FROM identity.platform_admin_sessions WHERE expires_at < now() - interval '30 days'`,
			`DELETE FROM identity.verification_tokens WHERE expires_at < now() - interval '7 days'`,
			`DELETE FROM identity.login_attempts WHERE created_at < now() - interval '90 days'`,
		}
		for _, s := range statements {
			if _, err := pool.Exec(ctx, s); err != nil && ctx.Err() == nil {
				slog.Error("housekeeping failed", "statement", strings.Fields(s)[0], "error", err)
			}
		}
	}
	tick()
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	slog.Info("worker running")
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			tick()
		}
	}
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
