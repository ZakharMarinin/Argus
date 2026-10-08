// Package usecase computes everything the dashboard shows.
//
// The backend returns only raw lists (targets, probes, events, alerts), so the
// aggregates (24-hour uptime, restarts per hour, alert counters, hourly p95)
// live here. The package knows nothing about SQL, HTTP or HTML: from the
// outside it sees only the Backend port and the entity domain types.
package usecase

import (
	"context"
	"errors"
	"fmt"

	"argus-frontend/internal/domain/entity"
)

// Backend is everything usecase asks of argus-backend.
//
// The port is declared here and implemented in internal/gateway/backend: the
// dependency points inward, toward the domain, not outward toward HTTP.
type Backend interface {
	// Targets returns all targets, both uptime targets and containers.
	Targets(ctx context.Context) ([]entity.Target, error)

	// Probes returns the check history of one target.
	Probes(ctx context.Context, targetID string) ([]entity.ProbeOutcome, error)

	// Events returns the Docker events of one container target.
	Events(ctx context.Context, targetID string) ([]entity.ContainerEvent, error)

	Alerts(ctx context.Context, q entity.ListQuery) (entity.Page[entity.Alert], error)
	LogMatches(ctx context.Context, q entity.ListQuery) (entity.Page[entity.LogMatch], error)

	Host(ctx context.Context) (entity.Host, error)
	CreateTarget(ctx context.Context, t entity.Target) error
	DeleteTarget(ctx context.Context, id string) error

	// Ping checks that the backend is responding.
	Ping(ctx context.Context) error
}

// UseCase reads and aggregates the monitoring state.
type UseCase struct {
	backend Backend
}

// NewUseCase builds the use cases on top of the backend port.
func NewUseCase(b Backend) *UseCase {
	return &UseCase{backend: b}
}

// Ping checks that the backend is responding.
func (uc *UseCase) Ping(ctx context.Context) error {
	return notReady(uc.backend.Ping(ctx))
}

// notReady turns any backend error into entity.ErrNotReady: for the dashboard
// an unavailable backend and a backend that is still starting are the same
// waiting state.
func notReady(err error) error {
	if err == nil || errors.Is(err, entity.ErrBackendTooOld) {
		return err
	}

	return fmt.Errorf("%w: %w", entity.ErrNotReady, err)
}
