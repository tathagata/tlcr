package core

import "errors"

// ErrorKind is independent of any transport's status codes.
type ErrorKind string

// Service error categories are mapped to transport codes only by adapters.
const (
	Invalid        ErrorKind = "invalid"
	NotFound       ErrorKind = "not_found"
	TooLarge       ErrorKind = "too_large"
	BudgetExceeded ErrorKind = "budget_exceeded"
	ModelFailed    ErrorKind = "model_failed"
	Stale          ErrorKind = "stale"
)

// Error preserves the failure category across adapters.
type Error struct {
	Cause error
	Kind  ErrorKind
}

func (e *Error) Error() string { return e.Cause.Error() }
func (e *Error) Unwrap() error { return e.Cause }
func failure(kind ErrorKind, err error) error {
	var classified *Error
	if errors.As(err, &classified) {
		return err
	}
	return &Error{Kind: kind, Cause: err}
}
