package backend

import "errors"

// ErrUnreachable wraps any error talking to the backend (network, timeout,
// non-200 response); usecase turns it into entity.ErrNotReady.
var ErrUnreachable = errors.New("argus-backend is unavailable")

// ErrNotFound means the backend answered 404: usecase turns it into entity.ErrNotFound.
var ErrNotFound = errors.New("not found")
