package adapters

import "testing"

func setEnv(t *testing.T, env map[string]string) {
	t.Helper()
	for _, k := range []string{"APP_ENV", "PORT", "DATABASE_URL", "DATABASE_URL_UNPOOLED", "CLIENT_IP_HEADER",
		"R2_ACCOUNT_ID", "R2_ACCESS_KEY_ID", "R2_SECRET_ACCESS_KEY", "R2_BUCKET", "IMAGE_BASE_URL", "MEDIA_DIR"} {
		t.Setenv(k, env[k])
	}
}

// devImages is what development gets without R2: photos on disk, served
// by the API itself.
func devImages(port string) (string, string) { return "http://localhost:" + port + "/media", ".media" }

// withR2 adds a complete R2 configuration to env.
func withR2(env map[string]string) map[string]string {
	for k, v := range map[string]string{
		"R2_ACCOUNT_ID": "acc", "R2_ACCESS_KEY_ID": "id", "R2_SECRET_ACCESS_KEY": "secret", "R2_BUCKET": "fotos",
		"IMAGE_BASE_URL": "https://pub-x.r2.dev/",
	} {
		if _, set := env[k]; !set {
			env[k] = v
		}
	}
	return env
}

var testR2 = R2Config{AccountID: "acc", AccessKeyID: "id", SecretAccessKey: "secret", Bucket: "fotos"}

func TestLoadConfig(t *testing.T) {
	devBase, devDir := devImages("8080")

	tests := []struct {
		name    string
		env     map[string]string
		want    Config
		wantErr bool
	}{
		{
			name: "defaults",
			env:  map[string]string{"DATABASE_URL": "postgres://pooled/db"},
			want: Config{Env: EnvDev, Port: 8080, DatabaseURL: "postgres://pooled/db", ImageBaseURL: devBase, MediaDir: devDir},
		},
		{
			// The direct connection must win: see the note on databaseURL.
			name: "direct connection is preferred",
			env: withR2(map[string]string{
				"APP_ENV":               "prod",
				"PORT":                  "9000",
				"DATABASE_URL":          "postgres://pooled/db",
				"DATABASE_URL_UNPOOLED": "postgres://direct/db",
			}),
			want: Config{
				Env: EnvProd, Port: 9000, DatabaseURL: "postgres://direct/db",
				R2: testR2, ImageBaseURL: "https://pub-x.r2.dev",
			},
		},
		{
			name: "client IP header behind a proxy",
			env:  map[string]string{"DATABASE_URL": "x", "CLIENT_IP_HEADER": " Fly-Client-IP "},
			want: Config{Env: EnvDev, Port: 8080, DatabaseURL: "x", ClientIPHeader: "Fly-Client-IP", ImageBaseURL: devBase, MediaDir: devDir},
		},
		{
			name: "development with R2",
			env:  withR2(map[string]string{"DATABASE_URL": "x"}),
			want: Config{Env: EnvDev, Port: 8080, DatabaseURL: "x", R2: testR2, ImageBaseURL: "https://pub-x.r2.dev"},
		},
		{
			name: "development media elsewhere",
			env:  map[string]string{"DATABASE_URL": "x", "PORT": "3000", "MEDIA_DIR": "/tmp/fotos"},
			want: Config{Env: EnvDev, Port: 3000, DatabaseURL: "x", ImageBaseURL: "http://localhost:3000/media", MediaDir: "/tmp/fotos"},
		},
		{name: "production without R2", env: map[string]string{"APP_ENV": "prod", "DATABASE_URL": "x"}, wantErr: true},
		{name: "incomplete R2", env: withR2(map[string]string{"DATABASE_URL": "x", "R2_BUCKET": ""}), wantErr: true},
		{name: "R2 without its address", env: withR2(map[string]string{"DATABASE_URL": "x", "IMAGE_BASE_URL": ""}), wantErr: true},
		{
			name:    "production photos over plain HTTP",
			env:     withR2(map[string]string{"APP_ENV": "prod", "DATABASE_URL": "x", "IMAGE_BASE_URL": "http://pub-x.r2.dev"}),
			wantErr: true,
		},
		{name: "image address without scheme", env: map[string]string{"DATABASE_URL": "x", "IMAGE_BASE_URL": "pub-x.r2.dev"}, wantErr: true},
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
