package handlers

import (
	"argus-collector/internal/domain/entity"
	"context"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/google/uuid"
)

const defaultLimit = 50

type UseCase interface {
	CreateTarget(ctx context.Context, target *entity.Target) (*entity.Target, error)
	UpdateTargetStatus(ctx context.Context, id uuid.UUID, status string) (*entity.Target, error)
	DeleteTarget(ctx context.Context, id uuid.UUID) error
	GetTarget(ctx context.Context, id uuid.UUID) (*entity.Target, error)
	GetTargetsByType(ctx context.Context, targetType string) ([]*entity.Target, error)
	GetTargetsByStatus(ctx context.Context, status string) ([]*entity.Target, error)
	GetAllTargets(ctx context.Context, limit, offset uint64) ([]*entity.Target, error)

	GetProbe(ctx context.Context, id int) (*entity.Probe, error)
	DeleteProbe(ctx context.Context, id int) (int, error)
	GetProbesByTarget(ctx context.Context, targetID uuid.UUID) ([]*entity.Probe, error)
	GetProbesBySuccess(ctx context.Context, success bool) ([]*entity.Probe, error)
	GetAllProbes(ctx context.Context, limit uint64, offset int) ([]*entity.Probe, error)

	GetEvent(ctx context.Context, id int) (*entity.ContainerEvent, error)
	DeleteEvent(ctx context.Context, id int) (int, error)
	GetEventsByTarget(ctx context.Context, targetID uuid.UUID) ([]*entity.ContainerEvent, error)
	GetEventsByType(ctx context.Context, eventType entity.EventType) ([]*entity.ContainerEvent, error)
	GetAllEvents(ctx context.Context, limit uint64, offset int) ([]*entity.ContainerEvent, error)

	CreateAlert(ctx context.Context, alert *entity.Alert) (*entity.Alert, error)
	GetAlert(ctx context.Context, id int) (*entity.Alert, error)
	DeleteAlert(ctx context.Context, id int) (int, error)
	GetAlertsBySeverity(ctx context.Context, severity entity.Severity) ([]*entity.Alert, error)
	GetAlertsByNotification(ctx context.Context, flag bool) ([]*entity.Alert, error)
	GetAllAlerts(ctx context.Context, limit uint64, offset int) ([]*entity.Alert, error)
}

type Handler struct {
	log *slog.Logger
	uc  UseCase
}

func NewHandler(log *slog.Logger, uc UseCase) *Handler {
	return &Handler{log: log, uc: uc}
}

func parsePagination(r *http.Request) (limit uint64, offset int) {
	limit = parseLimit(r)

	offsetParam := r.URL.Query().Get("offset")
	if offsetParam != "" {
		parsed, err := strconv.Atoi(offsetParam)
		if err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	return limit, offset
}

func parsePaginationUint64(r *http.Request) (limit, offset uint64) {
	limit = parseLimit(r)

	offsetParam := r.URL.Query().Get("offset")
	if offsetParam != "" {
		parsed, err := strconv.ParseUint(offsetParam, 10, 64)
		if err == nil {
			offset = parsed
		}
	}

	return limit, offset
}

func parseLimit(r *http.Request) uint64 {
	limit := uint64(defaultLimit)

	limitParam := r.URL.Query().Get("limit")
	if limitParam != "" {
		parsed, err := strconv.ParseUint(limitParam, 10, 64)
		if err == nil {
			limit = parsed
		}
	}

	return limit
}
