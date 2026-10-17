package config

import "testing"

func TestLoadDefaults(t *testing.T) {
	t.Setenv("APP_ENV", "")
	t.Setenv("PORT", "")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("DATABASE_URL_UNPOOLED", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Env != EnvDev {
		t.Errorf("Env = %q, want %q", cfg.Env, EnvDev)
	}
	if cfg.Port != 8080 {
		t.Errorf("Port = %d, want 8080", cfg.Port)
	}
	if cfg.Addr() != ":8080" {
		t.Errorf("Addr() = %q, want \":8080\"", cfg.Addr())
	}
}

func TestLoadRejectsBadValues(t *testing.T) {
	tests := []struct{ name, key, value string }{
		{"unknown environment", "APP_ENV", "production"},
		{"non numeric port", "PORT", "eight thousand"},
		{"port out of range", "PORT", "70000"},
		{"port zero", "PORT", "0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("APP_ENV", "")
			t.Setenv("PORT", "")
			t.Setenv(tt.key, tt.value)

			if _, err := Load(); err == nil {
				t.Errorf("%s=%q should fail but passed", tt.key, tt.value)
			}
		})
	}
}

// The direct connection must win: see the note on databaseURL.
func TestDirectConnectionIsPreferred(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://pooled/db")
	t.Setenv("DATABASE_URL_UNPOOLED", "postgres://direct/db")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DatabaseURL != "postgres://direct/db" {
		t.Errorf("DatabaseURL = %q, want the unpooled one", cfg.DatabaseURL)
	}
}

func TestFallsBackToPooled(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://pooled/db")
	t.Setenv("DATABASE_URL_UNPOOLED", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DatabaseURL != "postgres://pooled/db" {
		t.Errorf("DatabaseURL = %q, want the pooled fallback", cfg.DatabaseURL)
	}
}
