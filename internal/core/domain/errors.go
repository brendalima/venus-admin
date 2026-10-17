package domain

// Domain errors. The repository translates infrastructure errors into
// these, and the HTTP handlers translate these into status codes, so the
// core never has to know what a 404 is.

type NotFoundError struct{ msg string }

func (e *NotFoundError) Error() string { return e.msg }

func NewNotFoundError(msg string) *NotFoundError { return &NotFoundError{msg: msg} }

// ConflictError reports a uniqueness clash, such as a slug already in use.
type ConflictError struct{ msg string }

func (e *ConflictError) Error() string { return e.msg }

func NewConflictError(msg string) *ConflictError { return &ConflictError{msg: msg} }

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
