// Package config loads configuration from the environment.
//
// Everything comes from environment variables, with no config file: it
// is what Fly expects (fly secrets) and it keeps credentials out of the
// repository.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Env string

const (
	EnvDev  Env = "dev"
	EnvProd Env = "prod"
)

type Config struct {
	Env  Env
	Port int

	// May be empty: the server still starts and /readyz says so, which is
	// handy for working on screens without a database around.
	DatabaseURL string
}

func (c Config) IsProd() bool { return c.Env == EnvProd }

func (c Config) Addr() string { return ":" + strconv.Itoa(c.Port) }

// Load reads and validates the environment. It returns an error instead
// of exiting so the caller decides how to report it.
func Load() (Config, error) {
	cfg := Config{
		Env:         EnvDev,
		Port:        8080,
		DatabaseURL: databaseURL(),
	}

	if v := strings.TrimSpace(os.Getenv("APP_ENV")); v != "" {
		switch Env(v) {
		case EnvDev, EnvProd:
			cfg.Env = Env(v)
		default:
			return Config{}, fmt.Errorf("invalid APP_ENV: %q (use dev or prod)", v)
		}
	}

	if v := strings.TrimSpace(os.Getenv("PORT")); v != "" {
		port, err := strconv.Atoi(v)
		if err != nil {
			return Config{}, fmt.Errorf("invalid PORT: %q", v)
		}
		if port < 1 || port > 65535 {
			return Config{}, fmt.Errorf("PORT out of range: %d", port)
		}
		cfg.Port = port
	}

	return cfg, nil
}

// databaseURL picks the connection the pool will use.
//
// It prefers the DIRECT one (DATABASE_URL_UNPOOLED) over the PgBouncer
// one: pgx keeps its own pool, and stacking that on top of transaction
// pooling breaks the prepared statement cache. Neon exposes both, and
// for a long-lived process the direct connection is the right one.
func databaseURL() string {
	if v := strings.TrimSpace(os.Getenv("DATABASE_URL_UNPOOLED")); v != "" {
		return v
	}
	return strings.TrimSpace(os.Getenv("DATABASE_URL"))
}
