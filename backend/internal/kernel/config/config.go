// Package config loads process configuration from the environment.
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config holds all process settings. Required values are checked at startup.
type Config struct {
	Env               string
	HTTPAddr          string
	DBHost            string
	DBPort            string
	DBName            string
	DBOwnerPassword   string
	DBAppPassword     string
	DBPlatformPass    string
	DBSystemPassword  string
	RedisAddr         string
	SMTPAddr          string
	MailFrom          string
	AppBaseURL        string
	AllowedOrigins    []string
	EncryptionKey     []byte
	HMACKey           []byte
	AuthRatePerMinute int
	// TrustForwarded uses X-Forwarded-For. Enable only behind a trusted proxy such as Traefik.
	TrustForwarded bool
}

// IsLocal reports whether this is the local development environment.
func (c Config) IsLocal() bool { return c.Env == "local" || c.Env == "test" }

// Load reads the environment and fails if a required value is missing or invalid.
func Load() (Config, error) {
	c := Config{
		Env:               get("APP_ENV", "local"),
		HTTPAddr:          get("HTTP_ADDR", "127.0.0.1:8088"),
		DBHost:            get("DB_HOST", "127.0.0.1"),
		DBPort:            get("DB_PORT", "55432"),
		DBName:            get("DB_NAME", "msp_engine"),
		DBOwnerPassword:   os.Getenv("DB_OWNER_PASSWORD"),
		DBAppPassword:     os.Getenv("DB_APP_PASSWORD"),
		DBPlatformPass:    os.Getenv("DB_PLATFORM_PASSWORD"),
		DBSystemPassword:  os.Getenv("DB_SYSTEM_PASSWORD"),
		RedisAddr:         get("REDIS_ADDR", "127.0.0.1:56379"),
		SMTPAddr:          get("SMTP_ADDR", "127.0.0.1:51025"),
		MailFrom:          get("MAIL_FROM", "no-reply@msp-engine.test"),
		AppBaseURL:        strings.TrimRight(get("APP_BASE_URL", "http://localhost:5180"), "/"),
		AuthRatePerMinute: authRate(),
		TrustForwarded:    os.Getenv("TRUST_FORWARDED") == "true",
	}
	for _, o := range strings.Split(get("ALLOWED_ORIGINS", "http://localhost:5180,http://localhost:5181"), ",") {
		if o = strings.TrimSpace(o); o != "" {
			c.AllowedOrigins = append(c.AllowedOrigins, o)
		}
	}
	var errs []error
	for name, v := range map[string]string{
		"DB_OWNER_PASSWORD": c.DBOwnerPassword, "DB_APP_PASSWORD": c.DBAppPassword,
		"DB_PLATFORM_PASSWORD": c.DBPlatformPass, "DB_SYSTEM_PASSWORD": c.DBSystemPassword,
	} {
		if v == "" {
			errs = append(errs, fmt.Errorf("%s is required", name))
		}
	}
	var err error
	if c.EncryptionKey, err = key("ENCRYPTION_KEY"); err != nil {
		errs = append(errs, err)
	}
	if c.HMACKey, err = key("HMAC_KEY"); err != nil {
		errs = append(errs, err)
	}
	return c, errors.Join(errs...)
}

// authRate is the limit on authentication requests per IP. Test runs raise it through the
// environment. Invalid or missing values use the production default.
func authRate() int {
	if n, err := strconv.Atoi(os.Getenv("AUTH_RATE_PER_MINUTE")); err == nil && n > 0 {
		return n
	}
	return 30
}

func key(name string) ([]byte, error) {
	k, err := base64.StdEncoding.DecodeString(os.Getenv(name))
	if err != nil || len(k) != 32 {
		return nil, fmt.Errorf("%s must be 32 bytes of base64", name)
	}
	return k, nil
}

func get(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
