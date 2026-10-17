package internal_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const modulePath = "github.com/brendalima/venus-admin"

// forbidden lists, per layer, the direct imports AGENTS.md §2 rules out.
// A prefix match is used, so "github.com/jackc/pgx" covers every subpackage.
// The compiler will not catch a convenient but wrong import, so this does.
var forbidden = map[string][]string{
	"internal/core/domain": {
		modulePath + "/",
		"net/http", "database/sql", "github.com/jackc/pgx", "github.com/pressly/goose",
	},
	"internal/core/services": {
		modulePath + "/adapters", modulePath + "/internal/rest", modulePath + "/internal/subscriber",
		"net/http", "database/sql", "github.com/jackc/pgx",
	},
	"internal/repository": {
		modulePath + "/internal/core/services", modulePath + "/internal/rest", modulePath + "/internal/subscriber",
		"net/http",
	},
	"internal/storage": {
		modulePath + "/internal/core/services", modulePath + "/internal/rest", modulePath + "/internal/repository",
	},
	"internal/rest": {
		modulePath + "/internal/repository", modulePath + "/internal/storage", modulePath + "/adapters",
		"database/sql", "github.com/jackc/pgx",
	},
}

func TestLayersRespectDependencyRules(t *testing.T) {
	for layer, banned := range forbidden {
		t.Run(layer, func(t *testing.T) {
			out, err := exec.Command("go", "list", "-f", `{{join .Imports "\n"}}`, modulePath+"/"+layer).Output()
			if err != nil {
				t.Skipf("couldn't run go list: %v", err)
			}

			for _, imp := range strings.Fields(string(out)) {
				for _, bad := range banned {
					if strings.HasPrefix(imp, bad) {
						t.Errorf("%s imports %s, which AGENTS.md forbids", layer, imp)
					}
				}
			}
		})
	}
}

// Configuration is read only in adapters/config.go and handed down
// through constructors.
func TestNoEnvironmentReadsInsideInternal(t *testing.T) {
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, call := range []string{"os.Getenv", "os.LookupEnv", "os.Environ"} {
			if strings.Contains(string(src), call) {
				t.Errorf("%s calls %s; read configuration in adapters/config.go", path, call)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
