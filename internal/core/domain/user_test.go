package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeEmail(t *testing.T) {
	if got := NormalizeEmail("  Ana@Exemplo.COM\n"); got != "ana@exemplo.com" {
		t.Errorf("NormalizeEmail = %q", got)
	}
}

func TestValidateEmail(t *testing.T) {
	tests := []struct {
		email   string
		wantErr bool
	}{
		{"ana@exemplo.com", false},
		{"ana+loja@exemplo.com.br", false},
		{"", true},
		{"ana", true},
		{"@exemplo.com", true},
		{"ana@exemplo", true},
		{"ana maria@exemplo.com", true},
	}

	for _, tt := range tests {
		t.Run(tt.email, func(t *testing.T) {
			err := ValidateEmail(tt.email)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateEmail(%q) = %v, wantErr %v", tt.email, err, tt.wantErr)
			}
			var v *ValidationError
			if err != nil && (!errors.As(err, &v) || v.Field != "email") {
				t.Errorf("err = %#v, want a ValidationError on email", err)
			}
		})
	}
}

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name     string
		password string
		wantErr  bool
	}{
		{"seven characters", "1234567", true},
		{"eight characters", "12345678", false},
		// Counted in characters, not bytes: "ção" is six bytes.
		{"accented, eight characters", "coraçãoé", false},
		{"accented, seven characters", "coração", true},
		{"at the maximum", strings.Repeat("a", MaxPasswordLength), false},
		{"over the maximum", strings.Repeat("a", MaxPasswordLength+1), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePassword(tt.password)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePassword = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
