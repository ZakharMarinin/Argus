package handlers

import (
	"argus-collector/internal/domain/entity"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

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

	GetLogMatch(ctx context.Context, id int) (*entity.LogMatch, error)
	DeleteLogMatch(ctx context.Context, id int) (int, error)
	FindLogMatches(ctx context.Context, f entity.ListFilter) ([]*entity.LogMatch, int, error)

	CreateAlert(ctx context.Context, alert *entity.Alert) (*entity.Alert, error)
	GetAlert(ctx context.Context, id int) (*entity.Alert, error)
	DeleteAlert(ctx context.Context, id int) (int, error)
	FindAlerts(ctx context.Context, f entity.ListFilter) ([]*entity.Alert, int, error)
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

var errInvalidOrder = errors.New("invalid order")

func parseListFilter(r *http.Request) (entity.ListFilter, error) {
	q := r.URL.Query()
	limit, offset := parsePagination(r)
	f := entity.ListFilter{Limit: limit, After: offset}

	for _, raw := range q["target_id"] {
		id, err := uuid.Parse(raw)
		if err != nil {
			return entity.ListFilter{}, err
		}

		f.TargetIDs = append(f.TargetIDs, id)
	}

	if raw := q.Get("severity"); raw != "" {
		level, err := strconv.Atoi(raw)
		if err != nil {
			return entity.ListFilter{}, err
		}

		severity := entity.Severity(level)
		f.Severity = &severity
	}

	if raw := q.Get("since"); raw != "" {
		since, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return entity.ListFilter{}, err
		}

		f.Since = &since
	}

	if raw := q.Get("skip"); raw != "" {
		skip, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return entity.ListFilter{}, err
		}

		f.Skip = skip
	}

	switch q.Get("order") {
	case "", "asc":
	case "desc":
		f.Desc = true
	default:
		return entity.ListFilter{}, errInvalidOrder
	}

	return f, nil
}

func setTotal(w http.ResponseWriter, total int) {
	w.Header().Set("X-Total-Count", strconv.Itoa(total))
}
