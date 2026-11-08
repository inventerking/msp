package config

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestRequireDB(t *testing.T) {
	local := Config{Env: "local", DBAppPassword: "dev_app"}
	if err := local.RequireDB("app"); err != nil {
		t.Errorf("local may use development passwords: %v", err)
	}
	if err := local.RequireDB("owner"); err == nil {
		t.Error("a missing password was accepted")
	}
	shared := Config{Env: "testing", DBAppPassword: "dev_app", DBPlatformPass: "a-generated-value"}
	if err := shared.RequireDB("app"); err == nil || !strings.Contains(err.Error(), "local development password") {
		t.Errorf("development password accepted outside local: %v", err)
	}
	if err := shared.RequireDB("platform"); err != nil {
		t.Error(err)
	}
}

func TestLoadDoesNotNeedOwnerPassword(t *testing.T) {
	t.Setenv("DB_OWNER_PASSWORD", "")
	t.Setenv("ENCRYPTION_KEY", "")
	t.Setenv("HMAC_KEY", "")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := c.RequireKeys(); err == nil {
		t.Error("missing keys were accepted")
	}
}

func TestLoadRejectsMalformedKeys(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "REPLACE_ME")
	if _, err := Load(); err == nil {
		t.Error("placeholder key accepted")
	}
	t.Setenv("ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(make([]byte, 16)))
	if _, err := Load(); err == nil {
		t.Error("short key accepted")
	}
}

func TestAllowAllScannerIsLocalOnly(t *testing.T) {
	if err := (Config{Env: "local", FileScanner: "allow-all"}).RequireScanner(); err != nil {
		t.Error(err)
	}
	if err := (Config{Env: "testing", FileScanner: "allow-all"}).RequireScanner(); err == nil {
		t.Error("files could be marked clean without a scan on a shared server")
	}
}

func TestCookieEnvironment(t *testing.T) {
	if (Config{Env: "testing"}).IsLocal() {
		t.Error("a testing server must use secure cookies")
	}
}
