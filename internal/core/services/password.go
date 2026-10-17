package services

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"

	"golang.org/x/crypto/argon2"
)

// argon2id parameters: OWASP's first recommendation (19 MiB, 2 passes,
// 1 lane). Memory is the constraint that matters here: the machine has
// 256 MB, and each login in flight holds this much while it hashes. The
// login rate limit keeps the number in flight small.
const (
	argonMemoryKiB = 19 * 1024
	argonTime      = 2
	argonThreads   = 1
	argonSaltLen   = 16
	argonKeyLen    = 32
)

var errMalformedHash = errors.New("malformed password hash")

// dummyHash is checked against when the email is unknown, so a wrong
// email takes as long to reject as a wrong password and cannot be told
// apart by timing. It is built on first use to keep startup fast.
var dummyHash = sync.OnceValue(func() string {
	// Ignored on purpose: crypto/rand.Read does not fail since Go 1.24,
	// and an empty hash would only make verifyPassword return an error,
	// which is a rejection all the same.
	h, _ := hashPassword("venus-oasis-dummy-password")
	return h
})

// hashPassword returns the PHC string format, which carries the
// parameters: raising them later does not invalidate existing hashes.
func hashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("couldn't generate salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemoryKiB, argonThreads, argonKeyLen)

	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemoryKiB, argonTime, argonThreads,
		b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// verifyPassword reports whether password matches encoded, using the
// parameters stored in the hash rather than the current constants.
func verifyPassword(password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return false, errMalformedHash
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, errMalformedHash
	}

	var memory, passes uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &passes, &threads); err != nil {
		return false, errMalformedHash
	}
	if memory == 0 || passes == 0 || threads == 0 {
		return false, errMalformedHash
	}

	b64 := base64.RawStdEncoding
	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return false, errMalformedHash
	}
	want, err := b64.DecodeString(parts[5])
	if err != nil || len(want) == 0 {
		return false, errMalformedHash
	}

	got := argon2.IDKey([]byte(password), salt, passes, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}
