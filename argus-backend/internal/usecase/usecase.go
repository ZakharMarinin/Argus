package usecase

import (
	"argus-collector/internal/domain/entity"
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type TargetRepository interface {
	CreateTarget(ctx context.Context, target *entity.Target) (*entity.Target, error)
	UpdateTargetStatus(ctx context.Context, id uuid.UUID, status string) (*entity.Target, error)
	DeleteTarget(ctx context.Context, id uuid.UUID) error
	GetTarget(ctx context.Context, id uuid.UUID) (*entity.Target, error)
	GetTargetsByIDs(ctx context.Context, ids []uuid.UUID) ([]*entity.Target, error)
	GetTargetsByType(ctx context.Context, targetType string) ([]*entity.Target, error)
	GetTargetsByStatus(ctx context.Context, status string) ([]*entity.Target, error)
	GetAllTargets(ctx context.Context, limit, offset uint64) ([]*entity.Target, error)
}

type ProberRepository interface {
	SaveProbe(ctx context.Context, probe *entity.Probe) (*entity.Probe, error)
	SaveProbes(ctx context.Context, probes []*entity.Probe) ([]*entity.Probe, error)
	DeleteProbe(ctx context.Context, id int) (int, error)
	DeleteProbesOlderThan(ctx context.Context, before time.Time) error
	GetProbe(ctx context.Context, id int) (*entity.Probe, error)
	GetProbesByTarget(ctx context.Context, targetID uuid.UUID) ([]*entity.Probe, error)
	GetProbesBySuccess(ctx context.Context, success bool) ([]*entity.Probe, error)
	GetAllProbes(ctx context.Context, limit uint64, offset int) ([]*entity.Probe, error)
}

type AlertRepository interface {
	CreateAlert(ctx context.Context, alert *entity.Alert) (*entity.Alert, error)
	DeleteAlert(ctx context.Context, id int) (int, error)
	GetAlert(ctx context.Context, id int) (*entity.Alert, error)
	GetAlertsBySeverity(ctx context.Context, severity entity.Severity) ([]*entity.Alert, error)
	GetAlertsByNotification(ctx context.Context, flag bool) ([]*entity.Alert, error)
	GetAllAlerts(ctx context.Context, limit uint64, offset int) ([]*entity.Alert, error)
	MarkAlertNotified(ctx context.Context, id int) error
}

//nolint:dupl
type EventRepository interface {
	CreateEvent(ctx context.Context, event *entity.ContainerEvent) (*entity.ContainerEvent, error)
	DeleteEvent(ctx context.Context, id int) (int, error)
	DeleteEventsOlderThan(ctx context.Context, before time.Time) error
	GetEvent(ctx context.Context, id int) (*entity.ContainerEvent, error)
	GetEventsByTarget(ctx context.Context, targetID uuid.UUID) ([]*entity.ContainerEvent, error)
	GetEventsByType(ctx context.Context, eventType entity.EventType) ([]*entity.ContainerEvent, error)
	GetAllEvents(ctx context.Context, limit uint64, offset int) ([]*entity.ContainerEvent, error)
}

//nolint:dupl
type LogMatchRepository interface {
	SaveLogMatch(ctx context.Context, logMatch *entity.LogMatch) (*entity.LogMatch, error)
	DeleteLogMatch(ctx context.Context, id int) (int, error)
	DeleteLogMatchesOlderThan(ctx context.Context, before time.Time) error
	GetLogMatch(ctx context.Context, id int) (*entity.LogMatch, error)
	GetLogMatchesByTarget(ctx context.Context, targetID uuid.UUID) ([]*entity.LogMatch, error)
	GetLogMatchesBySeverity(ctx context.Context, severity entity.Severity) ([]*entity.LogMatch, error)
	GetAllLogMatches(ctx context.Context, limit uint64, offset int) ([]*entity.LogMatch, error)
}

type Storage interface {
	TargetRepository
	ProberRepository
	AlertRepository
	EventRepository
	LogMatchRepository
}

type UseCase struct {
	db Storage
}

func NewUseCase(db Storage) *UseCase {
	return &UseCase{
		db: db,
	}
}

func wrapErr(op string, err error) error {
	if err == nil {
		return nil
	}

	return fmt.Errorf("%s: %w", op, err)
}
