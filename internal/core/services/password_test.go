package services

import (
	"errors"
	"strings"
	"testing"
)

func TestPasswordHashRoundTrip(t *testing.T) {
	hash, err := hashPassword("biquíni aurora")
	if err != nil {
		t.Fatalf("hashing: %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Errorf("hash = %q, want PHC format with the current parameters", hash)
	}

	tests := []struct {
		password string
		want     bool
	}{
		{"biquíni aurora", true},
		{"biquini aurora", false},
		{"Biquíni aurora", false},
		{"", false},
	}
	for _, tt := range tests {
		got, err := verifyPassword(tt.password, hash)
		if err != nil {
			t.Fatalf("verifying %q: %v", tt.password, err)
		}
		if got != tt.want {
			t.Errorf("verifyPassword(%q) = %v, want %v", tt.password, got, tt.want)
		}
	}
}

func TestPasswordHashesAreSalted(t *testing.T) {
	a, _ := hashPassword("mesma senha")
	b, _ := hashPassword("mesma senha")
	if a == b {
		t.Error("two hashes of the same password are equal; the salt is not random")
	}
}

// The parameters are read from the hash, so raising the constants later
// keeps old passwords working.
func TestVerifyUsesParametersFromHash(t *testing.T) {
	// Built with m=8192,t=1,p=1, not the current constants.
	const hash = "$argon2id$v=19$m=8192,t=1,p=1$c2FsdHNhbHRzYWx0c2FsdA$m/30inbqmdOruyAzZnP36XbDtTKukfASd02cq552WWU"
	ok, err := verifyPassword("senha antiga", hash)
	if err != nil {
		t.Fatalf("verifying: %v", err)
	}
	if !ok {
		t.Error("a hash made with other parameters was rejected")
	}
}

func TestVerifyRejectsMalformedHashes(t *testing.T) {
	for _, hash := range []string{
		"",
		"plaintext",
		"$2a$10$bcrypthashbcrypthashbcrypthashbcrypthashbcrypthashbcr",
		"$argon2i$v=19$m=19456,t=2,p=1$c2FsdA$a2V5",
		"$argon2id$v=18$m=19456,t=2,p=1$c2FsdA$a2V5",
		"$argon2id$v=19$m=0,t=2,p=1$c2FsdA$a2V5",
		"$argon2id$v=19$m=19456,t=2,p=1$not base64!$a2V5",
		"$argon2id$v=19$m=19456,t=2,p=1$c2FsdA$",
	} {
		if _, err := verifyPassword("x", hash); !errors.Is(err, errMalformedHash) {
			t.Errorf("verifyPassword(%q) err = %v, want errMalformedHash", hash, err)
		}
	}
}
