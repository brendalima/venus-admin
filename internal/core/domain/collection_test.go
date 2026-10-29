package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestCollectionNormalized(t *testing.T) {
	if got := (Collection{Name: "  Verão \t 2026 "}).Normalized().Name; got != "Verão 2026" {
		t.Errorf("name = %q, want it trimmed with single spaces", got)
	}
}

func TestCollectionValidate(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		invalid bool
	}{
		{"a name", "Verão 2026", false},
		// The limit counts letters, not bytes: accents are not penalised.
		{"the longest name", strings.Repeat("ã", MaxCollectionNameLength), false},
		{"empty", "", true},
		{"blank", "   ", true},
		{"too long", strings.Repeat("a", MaxCollectionNameLength+1), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Collection{Name: tt.in}.Validate()
			var invalid *ValidationError
			if got := errors.As(err, &invalid) && invalid.Field == "name"; got != tt.invalid {
				t.Errorf("Validate(%q) = %v, want invalid %v", tt.in, err, tt.invalid)
			}
		})
	}
}
