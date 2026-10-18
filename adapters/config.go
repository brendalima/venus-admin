// Package adapters holds the infrastructure shared by every binary:
// configuration from the environment and the database connection.
package adapters

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Env string

const (
	EnvDev  Env = "dev"
	EnvProd Env = "prod"
)

// Config is read only here and handed to the other layers through their
// constructors. Everything comes from environment variables: it is what
// Fly expects (fly secrets) and it keeps credentials out of the repository.
type Config struct {
	Env         Env
	Port        int
	DatabaseURL string

	// ClientIPHeader names the header a reverse proxy puts the client's
	// IP in (Fly-Client-IP on Fly). Leave it empty when nothing sits in
	// front of the server: the header would then come from the client,
	// who could change it to dodge the login rate limit.
	ClientIPHeader string

	// R2 is the bucket the photos go to. Required in production; in
	// development, leaving it empty keeps photos in MediaDir instead,
	// served by the API under /media.
	R2       R2Config
	MediaDir string

	// ImageBaseURL is where the stored photos are publicly reachable,
	// without a trailing slash: a photo's URL is this plus "/" plus its
	// key. Keys are stored, not URLs, so moving the bucket to another
	// domain only changes this.
	ImageBaseURL string

	// DeployHookURL is the Cloudflare Pages deploy hook that rebuilds the
	// site. Optional while the site has no Pages project: without it,
	// production does not offer publishing, and development only logs.
	DeployHookURL string

	// APIToken is what the site's build sends to read /api/products.
	// Required in production; in development, leaving it empty keeps
	// the API off.
	APIToken string

	// PublicURL is the admin's address as people reach it, without a
	// trailing slash. Links in emails start with it; building them from
	// the request's Host header instead would let a client point them
	// anywhere.
	PublicURL string

	// ResendAPIKey sends email through Resend. Required in production;
	// in development, leaving it empty logs emails instead of sending.
	ResendAPIKey string
	// MailFrom is the sender, on a domain verified in Resend.
	MailFrom string
}

// minAPITokenLength keeps the token out of guessing range. 32 characters
// is what `openssl rand -hex 16` prints.
const minAPITokenLength = 32

type R2Config struct {
	AccountID       string
	AccessKeyID     string
	SecretAccessKey string
	Bucket          string
}

// Configured reports whether any R2 setting is present. Validation makes
// sure that, if one is, all are.
func (r R2Config) Configured() bool {
	return r.AccountID != "" || r.AccessKeyID != "" || r.SecretAccessKey != "" || r.Bucket != ""
}

func (c Config) IsProd() bool { return c.Env == EnvProd }

func (c Config) Addr() string { return ":" + strconv.Itoa(c.Port) }

// LoadConfig reads and validates the environment. It returns an error
// instead of exiting so the caller decides how to report it.
func LoadConfig() (Config, error) {
	if os.Getenv("APP_ENV") != string(EnvProd) {
		loadDotEnv()
	}

	cfg := Config{
		Env:            EnvDev,
		Port:           8080,
		DatabaseURL:    databaseURL(),
		ClientIPHeader: strings.TrimSpace(os.Getenv("CLIENT_IP_HEADER")),
		R2: R2Config{
			AccountID:       strings.TrimSpace(os.Getenv("R2_ACCOUNT_ID")),
			AccessKeyID:     strings.TrimSpace(os.Getenv("R2_ACCESS_KEY_ID")),
			SecretAccessKey: strings.TrimSpace(os.Getenv("R2_SECRET_ACCESS_KEY")),
			Bucket:          strings.TrimSpace(os.Getenv("R2_BUCKET")),
		},
		MediaDir:      strings.TrimSpace(os.Getenv("MEDIA_DIR")),
		DeployHookURL: strings.TrimSpace(os.Getenv("DEPLOY_HOOK_URL")),
		APIToken:      strings.TrimSpace(os.Getenv("API_TOKEN")),
		ResendAPIKey:  strings.TrimSpace(os.Getenv("RESEND_API_KEY")),
		MailFrom:      strings.TrimSpace(os.Getenv("MAIL_FROM")),
	}

	if v := strings.TrimSpace(os.Getenv("APP_ENV")); v != "" {
		switch Env(v) {
		case EnvDev, EnvProd:
			cfg.Env = Env(v)
		default:
			return Config{}, fmt.Errorf("invalid APP_ENV %q: use dev or prod", v)
		}
	}

	if v := strings.TrimSpace(os.Getenv("PORT")); v != "" {
		port, err := strconv.Atoi(v)
		if err != nil {
			return Config{}, fmt.Errorf("invalid PORT %q", v)
		}
		if port < 1 || port > 65535 {
			return Config{}, fmt.Errorf("PORT out of range: %d", port)
		}
		cfg.Port = port
	}

	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("missing DATABASE_URL_UNPOOLED or DATABASE_URL")
	}

	if err := loadImageStorage(&cfg); err != nil {
		return Config{}, err
	}

	if err := validatePublishing(cfg); err != nil {
		return Config{}, err
	}

	publicURL, err := loadPublicURL(cfg)
	if err != nil {
		return Config{}, err
	}
	cfg.PublicURL = publicURL

	// Without email, nobody can reset a forgotten password, so production
	// refuses to start rather than fail the first time someone needs it.
	if cfg.IsProd() && cfg.ResendAPIKey == "" {
		return Config{}, errors.New("missing RESEND_API_KEY")
	}
	if cfg.ResendAPIKey != "" && cfg.MailFrom == "" {
		return Config{}, errors.New("missing MAIL_FROM, needed to send email")
	}

	return cfg, nil
}

// validatePublishing checks the two ends of publishing: the hook that
// starts the site's build, and the token the build reads the API with.
func validatePublishing(cfg Config) error {
	if cfg.DeployHookURL != "" {
		// Plain HTTP is allowed in development, for a stand-in hook on
		// this machine.
		u, err := url.Parse(cfg.DeployHookURL)
		if err != nil || u.Host == "" || (u.Scheme != "https" && (cfg.IsProd() || u.Scheme != "http")) {
			// The URL itself is a secret, so it is not repeated here.
			return errors.New("invalid DEPLOY_HOOK_URL: use the https address Cloudflare Pages gives the hook")
		}
	}

	switch {
	case cfg.APIToken == "" && cfg.IsProd():
		return errors.New("missing API_TOKEN, which the site's build uses to read /api/products")
	case cfg.APIToken != "" && len(cfg.APIToken) < minAPITokenLength:
		return fmt.Errorf("API_TOKEN too short: use at least %d characters, such as the output of openssl rand -hex 16", minAPITokenLength)
	}
	return nil
}

// loadImageStorage checks the R2 settings and works out ImageBaseURL and
// MediaDir. Production always needs R2: the machine's disk is wiped on
// every deploy.
func loadImageStorage(cfg *Config) error {
	r2 := cfg.R2
	if cfg.IsProd() && !r2.Configured() {
		return errors.New("missing R2_ACCOUNT_ID, R2_ACCESS_KEY_ID, R2_SECRET_ACCESS_KEY and R2_BUCKET")
	}
	if r2.Configured() && (r2.AccountID == "" || r2.AccessKeyID == "" || r2.SecretAccessKey == "" || r2.Bucket == "") {
		return errors.New("incomplete R2 settings: set R2_ACCOUNT_ID, R2_ACCESS_KEY_ID, R2_SECRET_ACCESS_KEY and R2_BUCKET together")
	}

	base := strings.TrimRight(strings.TrimSpace(os.Getenv("IMAGE_BASE_URL")), "/")
	switch {
	case base == "" && r2.Configured():
		return errors.New("missing IMAGE_BASE_URL, the bucket's public address")
	case base == "":
		cfg.ImageBaseURL = "http://localhost:" + strconv.Itoa(cfg.Port) + "/media"
	default:
		u, err := url.Parse(base)
		if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.RawQuery != "" {
			return fmt.Errorf("invalid IMAGE_BASE_URL %q: use an address like https://pub-….r2.dev", base)
		}
		// The site is served over HTTPS; photos over HTTP would be blocked
		// as mixed content.
		if cfg.IsProd() && u.Scheme != "https" {
			return fmt.Errorf("invalid IMAGE_BASE_URL %q: production needs https", base)
		}
		cfg.ImageBaseURL = base
	}

	if !r2.Configured() && cfg.MediaDir == "" {
		cfg.MediaDir = ".media"
	}
	return nil
}

// loadPublicURL reads PUBLIC_URL, which development may leave out.
func loadPublicURL(cfg Config) (string, error) {
	v := strings.TrimRight(strings.TrimSpace(os.Getenv("PUBLIC_URL")), "/")
	if v == "" {
		if cfg.IsProd() {
			return "", errors.New("missing PUBLIC_URL")
		}
		return "http://localhost:" + strconv.Itoa(cfg.Port), nil
	}

	u, err := url.Parse(v)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.Path != "" || u.RawQuery != "" {
		return "", fmt.Errorf("invalid PUBLIC_URL %q: use the scheme and host only, like https://admin.venusoasis.com.br", v)
	}
	// A reset link sent over plain HTTP could be read on the way.
	if cfg.IsProd() && u.Scheme != "https" {
		return "", fmt.Errorf("invalid PUBLIC_URL %q: production needs https", v)
	}
	return v, nil
}

// loadDotEnv loads .env.local (written by `neon init`) and then .env, in
// development only. godotenv does not override what is already in the
// environment, so an exported variable still wins.
func loadDotEnv() {
	for _, f := range []string{".env.local", ".env"} {
		// A missing file is the normal case outside a developer machine.
		_ = godotenv.Load(f)
	}
}

// databaseURL picks the connection the app will use.
//
// It prefers the DIRECT one (DATABASE_URL_UNPOOLED) over the PgBouncer
// one: pgx keeps a prepared statement cache per connection, and transaction
// pooling breaks it. Neon exposes both, and for a long-lived process the
// direct connection is the right one.
func databaseURL() string {
	if v := strings.TrimSpace(os.Getenv("DATABASE_URL_UNPOOLED")); v != "" {
		return v
	}
	return strings.TrimSpace(os.Getenv("DATABASE_URL"))
}
