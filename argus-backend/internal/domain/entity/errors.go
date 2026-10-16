package entity

import "errors"

type ValidationError struct {
	Reason string
}

func (e *ValidationError) Error() string {
	return e.Reason
}

var (
	ErrNotFound      = errors.New("not found")
	ErrAlreadyExists = errors.New("already exists")

	ErrNoEvent = errors.New("no event")

	ErrInvalidSeverity  = errors.New("invalid severity")
	ErrInvalidEventType = errors.New("invalid event type")
)
