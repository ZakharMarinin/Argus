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
		return nil, wrapErr(op, entity.ErrInvalidEventType)
	}

	created, err := uc.db.CreateEvent(ctx, event)

	return created, wrapErr(op, err)
}

func (uc *UseCase) DeleteEvent(ctx context.Context, id int) (int, error) {
	const op = "UseCase.DeleteEvent"

	deletedID, err := uc.db.DeleteEvent(ctx, id)

	return deletedID, wrapErr(op, err)
}

func (uc *UseCase) DeleteEventsOlderThan(ctx context.Context, before time.Time) error {
	const op = "UseCase.DeleteEventsOlderThan"

	err := uc.db.DeleteEventsOlderThan(ctx, before)

	return wrapErr(op, err)
}

func (uc *UseCase) GetEvent(ctx context.Context, id int) (*entity.ContainerEvent, error) {
	const op = "UseCase.GetEvent"

	event, err := uc.db.GetEvent(ctx, id)

	return event, wrapErr(op, err)
}

func (uc *UseCase) GetEventsByTarget(ctx context.Context, targetID uuid.UUID) ([]*entity.ContainerEvent, error) {
	const op = "UseCase.GetEventsByTarget"

	events, err := uc.db.GetEventsByTarget(ctx, targetID)

	return events, wrapErr(op, err)
}

func (uc *UseCase) GetEventsByType(ctx context.Context, eventType entity.EventType) ([]*entity.ContainerEvent, error) {
	const op = "UseCase.GetEventsByType"

	events, err := uc.db.GetEventsByType(ctx, eventType)

	return events, wrapErr(op, err)
}

func (uc *UseCase) GetAllEvents(ctx context.Context, limit uint64, offset int) ([]*entity.ContainerEvent, error) {
	const op = "UseCase.GetAllEvents"

	events, err := uc.db.GetAllEvents(ctx, limit, offset)

	return events, wrapErr(op, err)
}
