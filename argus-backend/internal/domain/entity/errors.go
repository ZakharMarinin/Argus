package entity

import "errors"

type ValidationError struct {
	Reason string
}

func (e *ValidationError) Error() string {
	return e.Reason
}

var (
	ErrTargetNotFound      = errors.New("target not found")
	ErrTargetAlreadyExists = errors.New("target already exists")

	ErrContainerEventNotFound      = errors.New("container event not found")
	ErrContainerEventAlreadyExists = errors.New("container event already exists")
	ErrNoEvent                     = errors.New("no event")

	ErrLogMatchNotFound = errors.New("log match not found")

	ErrProbeNotFound = errors.New("probe not found")

	ErrAlertNotFound = errors.New("alert not found")

	ErrInvalidSeverity  = errors.New("invalid severity")
	ErrInvalidEventType = errors.New("invalid event type")
)
