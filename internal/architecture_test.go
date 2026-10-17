package catalog_test

import (
	"os/exec"
	"strings"
	"testing"
)

const modulePath = "github.com/brendalima/venus-admin"

// The hexagon only holds if dependencies point inward: adapters know the
// core, never the reverse. That rule is easy to break by accident with a
// single convenient import, and the compiler will not complain — so it is
// asserted here.
func TestCoreDependsOnNothingInternal(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", modulePath+"/internal/catalog").Output()
	if err != nil {
		t.Skipf("could not run go list: %v", err)
	}

	for _, dep := range strings.Fields(string(out)) {
		if !strings.HasPrefix(dep, modulePath) {
			continue // standard library or third party, fine
		}
		if dep == modulePath+"/internal/catalog" {
			continue // itself
		}
		t.Errorf("the core imports %s; it must depend on nothing inside the project", dep)
	}
}

// The core must not reach for a driver, a framework or an HTTP package
// either. Those belong behind a port.
func TestCoreAvoidsInfrastructure(t *testing.T) {
	forbidden := []string{
		"net/http",
		"database/sql",
		"github.com/jackc/pgx",
		"github.com/pressly/goose",
		"github.com/aws/aws-sdk-go",
	}

	out, err := exec.Command("go", "list", "-deps", modulePath+"/internal/catalog").Output()
	if err != nil {
		t.Skipf("could not run go list: %v", err)
	}

	deps := strings.Fields(string(out))
	for _, bad := range forbidden {
		for _, dep := range deps {
			if strings.HasPrefix(dep, bad) {
				t.Errorf("the core imports %s; that belongs in an adapter", dep)
			}
		}
	}
}
