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
	Env                                            string
	HTTPAddr                                       string
	DBHost                                         string
	DBPort                                         string
	DBName                                         string
	DBOwnerPassword                                string
	DBAppPassword                                  string
	DBPlatformPass                                 string
	DBSystemPassword                               string
	RedisAddr                                      string
	SMTPAddr                                       string
	MailFrom                                       string
	AppBaseURL                                     string
	AllowedOrigins                                 []string
	EncryptionKey                                  []byte
	HMACKey                                        []byte
	AuthRatePerMinute                              int
	S3Endpoint, S3Bucket, S3AccessKey, S3SecretKey string
	GotenbergURL                                   string
	ClamAVAddr                                     string
	// ResearchFetcher is "fake" (canned pages) or "live" (real public websites, through the Egress Guard).
	ResearchFetcher string
	// AssessmentProbe is "fake" (invented results) or "live" (real DNS and the authorized domain's
	// own web host, through the Egress Guard).
	AssessmentProbe string
	// SendingDomainCheck is "fake" (reserved domains pass, nothing is looked up) or "live" (real DNS).
	SendingDomainCheck string
	// FileScanner is clamav, or allow-all, which is refused outside local development.
	FileScanner string
	// TrustForwarded uses X-Forwarded-For. Enable only behind a trusted proxy such as Traefik.
	TrustForwarded bool
}

// IsLocal reports whether this is the local development environment.
func (c Config) IsLocal() bool { return c.Env == "local" || c.Env == "test" }

// Load reads the environment and fails if a required value is missing or invalid.
func Load() (Config, error) {
	c := Config{
		Env:                get("APP_ENV", "local"),
		HTTPAddr:           get("HTTP_ADDR", "127.0.0.1:8088"),
		DBHost:             get("DB_HOST", "127.0.0.1"),
		DBPort:             get("DB_PORT", "55432"),
		DBName:             get("DB_NAME", "msp_engine"),
		DBOwnerPassword:    os.Getenv("DB_OWNER_PASSWORD"),
		DBAppPassword:      os.Getenv("DB_APP_PASSWORD"),
		DBPlatformPass:     os.Getenv("DB_PLATFORM_PASSWORD"),
		DBSystemPassword:   os.Getenv("DB_SYSTEM_PASSWORD"),
		RedisAddr:          get("REDIS_ADDR", "127.0.0.1:56379"),
		SMTPAddr:           get("SMTP_ADDR", "127.0.0.1:51025"),
		MailFrom:           get("MAIL_FROM", "no-reply@msp-engine.test"),
		AppBaseURL:         strings.TrimRight(get("APP_BASE_URL", "http://localhost:5180"), "/"),
		AuthRatePerMinute:  authRate(),
		TrustForwarded:     os.Getenv("TRUST_FORWARDED") == "true",
		S3Endpoint:         os.Getenv("S3_ENDPOINT"),
		S3Bucket:           get("S3_BUCKET", "msp-files"),
		S3AccessKey:        os.Getenv("S3_ACCESS_KEY"),
		S3SecretKey:        os.Getenv("S3_SECRET_KEY"),
		GotenbergURL:       os.Getenv("GOTENBERG_URL"),
		ClamAVAddr:         os.Getenv("CLAMAV_ADDR"),
		ResearchFetcher:    get("RESEARCH_FETCHER", "fake"),
		AssessmentProbe:    get("ASSESSMENT_PROBE", "fake"),
		SendingDomainCheck: get("SENDING_DOMAIN_CHECK", "fake"),
		FileScanner:        get("FILE_SCANNER", "clamav"),
	}
	for _, o := range strings.Split(get("ALLOWED_ORIGINS", "http://localhost:5180,http://localhost:5181"), ",") {
		if o = strings.TrimSpace(o); o != "" {
			c.AllowedOrigins = append(c.AllowedOrigins, o)
		}
	}
	var errs []error
	var err error
	if c.EncryptionKey, err = key("ENCRYPTION_KEY"); err != nil {
		errs = append(errs, err)
	}
	if c.HMACKey, err = key("HMAC_KEY"); err != nil {
		errs = append(errs, err)
	}
	return c, errors.Join(errs...)
}

// RequireKeys checks the secrets needed to serve requests.
func (c Config) RequireKeys() error {
	if len(c.EncryptionKey) == 0 || len(c.HMACKey) == 0 {
		return errors.New("ENCRYPTION_KEY and HMAC_KEY are required")
	}
	return nil
}

// RequireScanner refuses to mark files clean without scanning them, except in local development.
func (c Config) RequireScanner() error {
	if c.FileScanner == "allow-all" && !c.IsLocal() {
		return errors.New("FILE_SCANNER=allow-all is only allowed in local development")
	}
	return nil
}

// RequireDB checks that each named role has a usable password. A process asks only for the roles
// it uses, so the running API never holds the migration role's password.
func (c Config) RequireDB(roles ...string) error {
	passwords := map[string]string{"owner": c.DBOwnerPassword, "app": c.DBAppPassword, "platform": c.DBPlatformPass, "system": c.DBSystemPassword}
	var errs []error
	for _, role := range roles {
		pw := passwords[role]
		switch {
		case pw == "":
			errs = append(errs, fmt.Errorf("the password for database role %s is required", role))
		case !c.IsLocal() && strings.HasPrefix(pw, "dev_"):
			errs = append(errs, fmt.Errorf("database role %s uses a local development password outside local development", role))
		}
	}
	return errors.Join(errs...)
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
	if os.Getenv(name) == "" {
		return nil, nil
	}
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
