package domain

import (
	"strings"
	"time"
	"unicode/utf8"
)

// User is a person allowed into the admin. There is no public signup.
type User struct {
	ID    string
	Email string

	// PasswordHash is argon2id in PHC string format. The password itself
	// is never stored.
	PasswordHash string

	CreatedAt time.Time
	UpdatedAt time.Time
}

const (
	// NIST SP 800-63B asks for at least 8 characters and no composition
	// rules; a long phrase is easier to remember than "Biqu1n!".
	MinPasswordLength = 8
	// The upper bound only keeps a pasted novel from reaching the hasher.
	MaxPasswordLength = 256
)

// NormalizeEmail makes "Ana@Exemplo.com " and "ana@exemplo.com" the same
// account.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// ValidateEmail expects an already normalised address. It only catches
// typos; whether the address receives mail is up to the password reset.
func ValidateEmail(email string) error {
	local, host, ok := strings.Cut(email, "@")
	switch {
	case email == "":
		return NewValidationError("email", "Falta o e-mail")
	case !ok || local == "" || !strings.Contains(host, ".") || strings.ContainsAny(email, " \t\r\n"):
		return NewValidationError("email", "Esse e-mail não parece certo")
	}
	return nil
}

func ValidatePassword(password string) error {
	n := utf8.RuneCountInString(password)
	switch {
	case n < MinPasswordLength:
		return NewValidationError("password", "A senha precisa ter pelo menos 8 caracteres")
	case n > MaxPasswordLength:
		return NewValidationError("password", "A senha pode ter no máximo 256 caracteres")
	}
	return nil
}
