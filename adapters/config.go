// Package adapters holds the infrastructure shared by every binary:
// configuration from the environment and the database connection.
package adapters

import (
	"errors"
	"fmt"
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

	return cfg, nil
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
