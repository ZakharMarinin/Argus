package storage

import (
	"argus-collector/internal/domain/entity"
	"context"
	"errors"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type EventRepository struct {
	db *pgxpool.Pool
}

func (er *EventRepository) CreateEvent(ctx context.Context, event *entity.ContainerEvent) (*entity.ContainerEvent, error) {
	const op = "Storage.EventRepository.CreateEvent"

	var created entity.ContainerEvent

	err := insertReturning(ctx, er.db,
		"events",
		[]string{"target_id", "event", "details", "event_time"},
		[]any{event.TargetID, event.Event, event.Details, event.CreatedAt},
		"ON CONFLICT (target_id, event, event_time) DO NOTHING RETURNING id, target_id, event, details, event_time",
		&created.ID, &created.TargetID, &created.Event, &created.Details, &created.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("%s: %w", op, entity.ErrAlreadyExists)
		}

		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return &created, nil
}

func (er *EventRepository) DeleteEvent(ctx context.Context, id int) (int, error) {
	const op = "Storage.EventRepository.DeleteEvent"

	query, args, err := sq.
		Delete("events").
		Where(sq.Eq{"id": id}).
		PlaceholderFormat(sq.Dollar).
		ToSql()
	if err != nil {
		return 0, fmt.Errorf("%s: %w", op, err)
	}

	result, err := er.db.Exec(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", op, err)
	}

	if result.RowsAffected() == 0 {
		return 0, fmt.Errorf("%s: %w", op, entity.ErrNotFound)
	}

	return id, nil
}

func (er *EventRepository) DeleteEventsOlderThan(ctx context.Context, before time.Time) error {
	const op = "Storage.EventRepository.DeleteEventsOlderThan"

	query, args, err := sq.
		Delete("events").
		Where(sq.Lt{"event_time": before}).
		PlaceholderFormat(sq.Dollar).
		ToSql()
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	_, err = er.db.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

func (er *EventRepository) GetEvent(ctx context.Context, id int) (*entity.ContainerEvent, error) {
	const op = "Storage.EventRepository.GetEvent"

	var event entity.ContainerEvent

	err := getByID(ctx, er.db,
		"events",
		[]string{"id", "target_id", "event", "details", "event_time"},
		id,
		entity.ErrNotFound,
		&event.ID, &event.TargetID, &event.Event, &event.Details, &event.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return &event, nil
}

func (er *EventRepository) getEventsConstructor() sq.SelectBuilder {
	builder := sq.
		Select("id", "target_id", "event", "details", "event_time").
		From("events").
		OrderBy("id ASC").
		PlaceholderFormat(sq.Dollar)

	return builder
}

func (er *EventRepository) GetEventsByTarget(ctx context.Context, targetID uuid.UUID) ([]*entity.ContainerEvent, error) {
	const op = "Storage.EventRepository.GetEventsByTarget"

	query, args, err := er.getEventsConstructor().
		Where(sq.Eq{"target_id": targetID}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	events, err := er.getEventsQuery(ctx, query, args)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return events, nil
}

func (er *EventRepository) GetEventsByType(ctx context.Context, eventType entity.EventType) ([]*entity.ContainerEvent, error) {
	const op = "Storage.EventRepository.GetEventsByType"

	query, args, err := er.getEventsConstructor().
		Where(sq.Eq{"event": eventType}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	events, err := er.getEventsQuery(ctx, query, args)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return events, nil
}

func (er *EventRepository) GetAllEvents(ctx context.Context, limit uint64, offset int) ([]*entity.ContainerEvent, error) {
	const op = "Storage.EventRepository.GetAllEvents"

	query, args, err := er.getEventsConstructor().
		Where(sq.Gt{"id": offset}).
		Limit(limit).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	events, err := er.getEventsQuery(ctx, query, args)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return events, nil
}

func (er *EventRepository) getEventsQuery(ctx context.Context, query string, args []any) ([]*entity.ContainerEvent, error) {
	var events []*entity.ContainerEvent

	rows, err := er.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var event entity.ContainerEvent

		err := rows.Scan(&event.ID, &event.TargetID, &event.Event, &event.Details, &event.CreatedAt)
		if err != nil {
			return nil, err
		}

		events = append(events, &event)
	}

	err = rows.Err()
	if err != nil {
		return nil, err
	}

	return events, nil
}
