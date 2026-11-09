package api_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"mspengine/db/migrations"
	"mspengine/internal/api"
	"mspengine/internal/app"
	"mspengine/internal/kernel/config"
	"mspengine/internal/kernel/crypto"
	"mspengine/internal/kernel/db"
	"mspengine/internal/kernel/mailer"
	"mspengine/internal/kernel/ratelimit"
	"mspengine/internal/platform/files"
	"mspengine/internal/platform/identity"
	"mspengine/internal/platform/shared"
)

// fakeMailer captures email. Tests never send real messages.
type fakeMailer struct {
	mu   sync.Mutex
	sent []mailer.Message
}

func (f *fakeMailer) Send(_ context.Context, m mailer.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, m)
	return nil
}

// waitFor returns the newest message to an address whose subject contains text.
func (f *fakeMailer) waitFor(t *testing.T, to, subject string) mailer.Message {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		for i := len(f.sent) - 1; i >= 0; i-- {
			if f.sent[i].To == to && strings.Contains(f.sent[i].Subject, subject) {
				m := f.sent[i]
				f.mu.Unlock()
				return m
			}
		}
		f.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no email to %s with subject containing %q", to, subject)
	return mailer.Message{}
}

// fakePDF stands in for Gotenberg, which has its own live test.
type fakePDF struct{}

func (fakePDF) Render(_ context.Context, html []byte) ([]byte, error) {
	return append([]byte("%PDF-1.7 fake\n"), html...), nil
}

// Env is one running application on a real, migrated PostgreSQL.
type Env struct {
	URL   string
	Mail  *fakeMailer
	IDs   *identity.Service
	Owner *pgxpool.Pool // The migration role, for arranging test state only.
	DSN   func(role, password string) string
}

var (
	env   *Env
	graph *app.Graph
)

func TestMain(m *testing.M) {
	code, err := run(m)
	if err != nil {
		fmt.Fprintln(os.Stderr, "test environment failed:", err)
		os.Exit(1)
	}
	os.Exit(code)
}

func run(m *testing.M) (int, error) {
	ctx := context.Background()
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "pgvector/pgvector:0.8.7-pg18-trixie",
			Env:          map[string]string{"POSTGRES_PASSWORD": "test_superuser"},
			ExposedPorts: []string{"5432/tcp"},
			Files: []testcontainers.ContainerFile{{
				HostFilePath: "../../../deploy/local/postgres/00-roles.sql", ContainerFilePath: "/docker-entrypoint-initdb.d/00-roles.sql", FileMode: 0o644,
			}},
			WaitingFor: wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(90 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		return 1, err
	}
	defer func() { _ = c.Terminate(ctx) }()
	host, err := c.Host(ctx)
	if err != nil {
		return 1, err
	}
	port, err := c.MappedPort(ctx, "5432/tcp")
	if err != nil {
		return 1, err
	}
	dsn := func(role, pw string) string { return db.DSN(host, port.Port(), "msp_engine", role, pw) }

	conn, err := sql.Open("pgx", dsn("msp_owner", "dev_owner"))
	if err != nil {
		return 1, err
	}
	defer conn.Close()
	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		return 1, err
	}
	if err := goose.UpContext(ctx, conn, "."); err != nil {
		return 1, fmt.Errorf("migrate: %w", err)
	}

	appPool, err := db.Open(ctx, dsn("msp_app", "dev_app"), 20)
	if err != nil {
		return 1, err
	}
	platform, err := db.Open(ctx, dsn("msp_platform", "dev_platform"), 5)
	if err != nil {
		return 1, err
	}
	system, err := db.Open(ctx, dsn("msp_system", "dev_system"), 2)
	if err != nil {
		return 1, err
	}
	owner, err := db.Open(ctx, dsn("msp_owner", "dev_owner"), 3)
	if err != nil {
		return 1, err
	}
	pools := &db.Pools{App: appPool, Platform: platform, System: system}
	defer pools.Close()
	defer owner.Close()

	keys := crypto.Keys{HMAC: bytes.Repeat([]byte{7}, 32), Encryption: bytes.Repeat([]byte{9}, 32)}
	mail := &fakeMailer{}
	shared.AllowReservedTLDs = true // Fake company data uses reserved .example domains.
	g, err := app.Build(pools, keys, app.Options{FakeProviders: true, PDF: fakePDF{}, Mailer: mail, Store: &files.MemoryStore{}, Scanner: files.AllowAllScanner{}, Bucket: "test", BaseURL: "http://app.test"})
	if err != nil {
		return 1, err
	}
	ids := g.Identity
	cfg := config.Config{Env: "test", AllowedOrigins: []string{"http://app.test"}, AuthRatePerMinute: 1_000_000}
	h, err := api.New(api.Deps{Config: cfg, Identity: ids, Commercial: g.Commercial, Keys: keys, Limiter: ratelimit.New(nil), Hub: g.Hub, DB: pools.App,
		Shared: g.Shared, Files: g.Files, Notify: g.Notify, Integrate: g.Integration, AI: g.AI, Pools: pools, Ops: g.Ops, Modules: g.Modules})
	if err != nil {
		return 1, err
	}
	graph = g
	srv := httptest.NewServer(h)
	defer srv.Close()
	env = &Env{URL: srv.URL, Mail: mail, IDs: ids, Owner: owner, DSN: dsn}
	return m.Run(), nil
}

// Client is a browser-like client with cookies and CSRF handling.
type Client struct {
	t    *testing.T
	c    *http.Client
	csrf string
	// log records every response body, for the canary scan.
	log *[]string
	// headers are sent with every request.
	headers map[string]string
}

func newClient(t *testing.T) *Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &Client{t: t, c: &http.Client{Jar: jar, Timeout: 20 * time.Second}}
}

// Do sends a request and returns the status and decoded body.
func (c *Client) Do(method, path string, body any) (int, map[string]any) {
	c.t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			c.t.Fatal(err)
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, env.URL+path, r)
	if err != nil {
		c.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.csrf != "" {
		req.Header.Set("X-CSRF-Token", c.csrf)
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	resp, err := c.c.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		c.t.Fatal(err)
	}
	if c.log != nil {
		*c.log = append(*c.log, string(raw))
	}
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

func (c *Client) must(want int, method, path string, body any) map[string]any {
	c.t.Helper()
	got, out := c.Do(method, path, body)
	if got != want {
		c.t.Fatalf("%s %s: status %d, want %d: %v", method, path, got, want, out)
	}
	return out
}

func (c *Client) wantCode(status int, code, method, path string, body any) {
	c.t.Helper()
	got, out := c.Do(method, path, body)
	if got != status || out["code"] != code {
		c.t.Fatalf("%s %s: got %d %v, want %d %s", method, path, got, out["code"], status, code)
	}
}

func (c *Client) refreshCSRF(ctxPath string) map[string]any {
	c.t.Helper()
	out := c.must(200, http.MethodGet, ctxPath, nil)
	c.csrf, _ = out["csrf_token"].(string)
	return out
}

var (
	seq      int
	seqMu    sync.Mutex
	tokenRe  = regexp.MustCompile(`token=([A-Za-z0-9_\-]+)`)
	inviteRe = regexp.MustCompile(`/invite/([A-Za-z0-9_\-.]+)`)
)

const testPassword = "a-long-test-passphrase"

func unique(prefix string) string {
	seqMu.Lock()
	defer seqMu.Unlock()
	seq++
	return fmt.Sprintf("%s%d@example.test", prefix, seq)
}

// signUp registers, verifies and signs in a new user.
func signUp(t *testing.T, prefix string) (*Client, string) {
	t.Helper()
	email := unique(prefix)
	c := newClient(t)
	c.must(202, http.MethodPost, "/api/v1/auth/register", map[string]any{"email": email, "password": testPassword, "display_name": "Test " + prefix})
	m := env.Mail.waitFor(t, email, "Verify")
	c.must(204, http.MethodPost, "/api/v1/auth/verify-email", map[string]any{"token": tokenRe.FindStringSubmatch(m.Body)[1]})
	c.must(200, http.MethodPost, "/api/v1/auth/login", map[string]any{"email": email, "password": testPassword})
	c.refreshCSRF("/api/v1/me/context")
	return c, email
}

// newTenant signs up an owner with a workspace and returns the client, email and tenant ID.
func newTenant(t *testing.T, name string) (*Client, string, string) {
	t.Helper()
	c, email := signUp(t, strings.ToLower(strings.ReplaceAll(name, " ", "")))
	out := c.must(201, http.MethodPost, "/api/v1/tenants", map[string]any{"name": name})
	return c, email, out["id"].(string)
}

func asJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
