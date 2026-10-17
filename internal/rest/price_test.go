package rest

import (
	"errors"
	"testing"
)

func TestParseReais(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"189", 18900},
		{"189,90", 18990},
		{"189,9", 18990},
		{"0,99", 99},
		{",50", 50},
		{"R$ 189,90", 18990},
		{"R$189", 18900},
		{" 1.234,56 ", 123456},
		{"1.234", 123400},
		{"12.345.678", 1234567800},
		{"189.90", 18990},
		{"189.9", 18990},
		{"189.", 18900},
		{"", 0},
		{"R$ ", 0},
	}
	for _, tt := range tests {
		got, err := parseReais(tt.in)
		if err != nil || got != tt.want {
			t.Errorf("parseReais(%q) = %d, %v; want %d", tt.in, got, err, tt.want)
		}
	}

	for _, in := range []string{
		"abc", "18x", "-10", "189,999", "1,2,3", "1.23.4,00", "1234.567", ".123", "R$ R$ 10",
		"1234567890", // more than the cap
	} {
		if got, err := parseReais(in); !errors.Is(err, errInvalidPrice) {
			t.Errorf("parseReais(%q) = %d, %v; want errInvalidPrice", in, got, err)
		}
	}
}

func TestFormatReais(t *testing.T) {
	tests := []struct {
		cents int64
		want  string
	}{
		{0, "0,00"},
		{5, "0,05"},
		{18990, "189,90"},
		{123456, "1.234,56"},
		{100000000, "1.000.000,00"},
		{-1050, "-10,50"},
	}
	for _, tt := range tests {
		if got := formatReais(tt.cents); got != tt.want {
			t.Errorf("formatReais(%d) = %q, want %q", tt.cents, got, tt.want)
		}
		if tt.cents < 0 {
			continue
		}
		// What is shown in the form must read back to the same price.
		if back, err := parseReais(tt.want); err != nil || back != tt.cents {
			t.Errorf("parseReais(formatReais(%d)) = %d, %v", tt.cents, back, err)
		}
	}
}
