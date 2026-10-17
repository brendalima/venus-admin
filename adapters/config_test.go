package adapters

import "testing"

func setEnv(t *testing.T, env map[string]string) {
	t.Helper()
	for _, k := range []string{"APP_ENV", "PORT", "DATABASE_URL", "DATABASE_URL_UNPOOLED", "CLIENT_IP_HEADER"} {
		t.Setenv(k, env[k])
	}
}

func TestLoadConfig(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		want    Config
		wantErr bool
	}{
		{
			name: "defaults",
			env:  map[string]string{"DATABASE_URL": "postgres://pooled/db"},
			want: Config{Env: EnvDev, Port: 8080, DatabaseURL: "postgres://pooled/db"},
		},
		{
			// The direct connection must win: see the note on databaseURL.
			name: "direct connection is preferred",
			env: map[string]string{
				"APP_ENV":               "prod",
				"PORT":                  "9000",
				"DATABASE_URL":          "postgres://pooled/db",
				"DATABASE_URL_UNPOOLED": "postgres://direct/db",
			},
			want: Config{Env: EnvProd, Port: 9000, DatabaseURL: "postgres://direct/db"},
		},
		{
			name: "client IP header behind a proxy",
			env:  map[string]string{"DATABASE_URL": "x", "CLIENT_IP_HEADER": " Fly-Client-IP "},
			want: Config{Env: EnvDev, Port: 8080, DatabaseURL: "x", ClientIPHeader: "Fly-Client-IP"},
		},
		{name: "missing database", env: map[string]string{}, wantErr: true},
		{name: "unknown environment", env: map[string]string{"APP_ENV": "production", "DATABASE_URL": "x"}, wantErr: true},
		{name: "non numeric port", env: map[string]string{"PORT": "eight thousand", "DATABASE_URL": "x"}, wantErr: true},
		{name: "port out of range", env: map[string]string{"PORT": "70000", "DATABASE_URL": "x"}, wantErr: true},
		{name: "port zero", env: map[string]string{"PORT": "0", "DATABASE_URL": "x"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setEnv(t, tt.env)

			got, err := LoadConfig()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("LoadConfig() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestAddr(t *testing.T) {
	if got := (Config{Port: 8080}).Addr(); got != ":8080" {
		t.Errorf("Addr() = %q, want \":8080\"", got)
	}
}
