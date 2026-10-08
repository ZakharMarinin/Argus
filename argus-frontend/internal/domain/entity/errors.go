package entity

import "errors"

// ErrNotReady means there is no data yet: the backend hasn't started, isn't
// answering, or its schema hasn't been created yet.
//
// The start order of collector and web isn't guaranteed (spec §11), so this
// is a normal situation. The dashboard must show a clear waiting screen, not a 500.
var ErrNotReady = errors.New("argus-backend is not ready yet or unavailable")

// ErrNotFound means the backend doesn't track a target with this name.
var ErrNotFound = errors.New("target is not tracked")

type ValidationError struct {
	Reason string
}

func (e *ValidationError) Error() string {
	return e.Reason
}

var ErrBackendTooOld = errors.New("argus-backend is outdated: no order=desc and X-Total-Count, please upgrade it")
