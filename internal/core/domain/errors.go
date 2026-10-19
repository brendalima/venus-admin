package domain

// Domain errors. The repository translates infrastructure errors into
// these, and the HTTP handlers translate these into status codes, so the
// core never has to know what a 404 is.

type NotFoundError struct{ msg string }

func (e *NotFoundError) Error() string { return e.msg }

func NewNotFoundError(msg string) *NotFoundError { return &NotFoundError{msg: msg} }

// ConflictError reports a uniqueness clash, such as a slug already in use.
// Subject says what clashed, so the caller knows which field to point at.
type ConflictError struct{ Subject string }

// What a ConflictError can be about.
const (
	ConflictEmail      = "email"
	ConflictSlug       = "slug"
	ConflictColourName = "colour name"
)

func (e *ConflictError) Error() string { return e.Subject + " already in use" }

func NewConflictError(subject string) *ConflictError { return &ConflictError{Subject: subject} }

// UnauthorizedError reports a failed login. It never says whether the
// email or the password was wrong, so nobody can probe for accounts.
type UnauthorizedError struct{ msg string }

func (e *UnauthorizedError) Error() string { return e.msg }

func NewUnauthorizedError(msg string) *UnauthorizedError { return &UnauthorizedError{msg: msg} }

// ValidationError carries the field that failed and a message meant for
// the person using the admin. Error() stays in English like every other
// error; Message is user-facing, so it is in Portuguese — the people
// running this admin are Brazilian.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string { return "invalid " + e.Field }

func NewValidationError(field, message string) *ValidationError {
	return &ValidationError{Field: field, Message: message}
}

// StaleError reports a change based on data that has since changed, such
// as saving a photo list while someone else edited it on another device.
// Applying it anyway could delete what the person never saw.
type StaleError struct{ msg string }

func (e *StaleError) Error() string { return e.msg }

func NewStaleError(msg string) *StaleError { return &StaleError{msg: msg} }
