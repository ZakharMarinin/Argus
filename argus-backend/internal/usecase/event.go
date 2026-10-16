//nolint:dupl
package usecase

import (
	"argus-collector/internal/domain/entity"
	"context"
	"time"

	"github.com/google/uuid"
)

func (uc *UseCase) CreateEvent(ctx context.Context, event *entity.ContainerEvent) (*entity.ContainerEvent, error) {
	const op = "UseCase.CreateEvent"

	if !event.Event.Valid() {
		return nil, wrapErr(op, &entity.ValidationError{Reason: "invalid event type"})
	}

	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now()
	}

	created, err := uc.db.CreateEvent(ctx, event)

	return created, wrapErr(op, err)
}

func (uc *UseCase) DeleteEvent(ctx context.Context, id int) (int, error) {
	return uc.db.DeleteEvent(ctx, id)
}

func (uc *UseCase) DeleteEventsOlderThan(ctx context.Context, before time.Time) error {
	return uc.db.DeleteEventsOlderThan(ctx, before)
}

func (uc *UseCase) GetEvent(ctx context.Context, id int) (*entity.ContainerEvent, error) {
	return uc.db.GetEvent(ctx, id)
}

func (uc *UseCase) GetEventsByTarget(ctx context.Context, targetID uuid.UUID) ([]*entity.ContainerEvent, error) {
	return uc.db.GetEventsByTarget(ctx, targetID)
}

func (uc *UseCase) GetEventsByType(ctx context.Context, eventType entity.EventType) ([]*entity.ContainerEvent, error) {
	return uc.db.GetEventsByType(ctx, eventType)
}

func (uc *UseCase) GetAllEvents(ctx context.Context, limit uint64, offset int) ([]*entity.ContainerEvent, error) {
	return uc.db.GetAllEvents(ctx, limit, offset)
}
