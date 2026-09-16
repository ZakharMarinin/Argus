package storage

import (
	"argus-collector/internal/domain/entity"
	"context"
	"fmt"

	sq "github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5/pgxpool"
)

type AlertRepository struct {
	db *pgxpool.Pool
}

func (ar *AlertRepository) CreateAlert(ctx context.Context, alert *entity.Alert) (*entity.Alert, error) {
	const op = "Storage.AlertRepository.CreateAlert"

	var created entity.Alert

	err := insertReturning(ctx, ar.db,
		"alerts",
		[]string{"target_id", "severity", "details", "notificated", "created_at"},
		[]any{alert.TargetID, alert.Severity, alert.Details, alert.Notified, alert.CreatedAt},
		"RETURNING id, target_id, severity, details, notificated, created_at",
		&created.ID, &created.TargetID, &created.Severity,
		&created.Details, &created.Notified, &created.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return &created, nil
}

func (ar *AlertRepository) DeleteAlert(ctx context.Context, id int) (int, error) {
	const op = "Storage.AlertRepository.DeleteAlert"

	query, args, err := sq.
		Delete("alerts").
		Where(sq.Eq{"id": id}).
		PlaceholderFormat(sq.Dollar).
		ToSql()
	if err != nil {
		return 0, fmt.Errorf("%s: %w", op, err)
	}

	result, err := ar.db.Exec(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", op, err)
	}

	if result.RowsAffected() == 0 {
		return 0, fmt.Errorf("%s: %w", op, entity.ErrAlertNotFound)
	}

	return id, nil
}

func (ar *AlertRepository) MarkAlertNotified(ctx context.Context, id int) error {
	const op = "Storage.AlertRepository.MarkAlertNotified"

	query, args, err := sq.
		Update("alerts").
		Set("notificated", true).
		Where(sq.Eq{"id": id}).
		PlaceholderFormat(sq.Dollar).
		ToSql()
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	result, err := ar.db.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("%s: %w", op, entity.ErrAlertNotFound)
	}

	return nil
}

func (ar *AlertRepository) GetAlert(ctx context.Context, id int) (*entity.Alert, error) {
	const op = "Storage.AlertRepository.GetAlert"

	var alert entity.Alert

	err := getByID(ctx, ar.db,
		"alerts",
		[]string{"id", "target_id", "severity", "details", "notificated", "created_at"},
		id,
		entity.ErrAlertNotFound,
		&alert.ID, &alert.TargetID, &alert.Severity, &alert.Details, &alert.Notified, &alert.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return &alert, nil
}

func (ar *AlertRepository) getAlertsConstructor() sq.SelectBuilder {
	builder := sq.
		Select("id", "target_id", "severity", "details", "notificated", "created_at").
		From("alerts").
		OrderBy("id ASC").
		PlaceholderFormat(sq.Dollar)

	return builder
}

func (ar *AlertRepository) GetAlertsBySeverity(ctx context.Context, severity entity.Severity) ([]*entity.Alert, error) {
	const op = "Storage.AlertRepository.GetAlertsBySeverity"

	query, args, err := ar.
		getAlertsConstructor().
		Where(sq.Eq{"severity": severity}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	alerts, err := ar.getAlertsQuery(ctx, query, args)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return alerts, nil
}

func (ar *AlertRepository) GetAlertsByNotification(ctx context.Context, flag bool) ([]*entity.Alert, error) {
	const op = "Storage.AlertRepository.GetAlertsByNotification"

	query, args, err := ar.
		getAlertsConstructor().
		Where(sq.Eq{"notificated": flag}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	alerts, err := ar.getAlertsQuery(ctx, query, args)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return alerts, nil
}

func (ar *AlertRepository) GetAllAlerts(ctx context.Context, limit uint64, offset int) ([]*entity.Alert, error) {
	const op = "Storage.AlertRepository.GetAllAlerts"

	query, args, err := ar.
		getAlertsConstructor().
		Where(sq.Gt{"id": offset}).
		Limit(limit).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	alerts, err := ar.getAlertsQuery(ctx, query, args)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return alerts, nil
}

func (ar *AlertRepository) getAlertsQuery(ctx context.Context, query string, args []any) ([]*entity.Alert, error) {
	var alerts []*entity.Alert

	rows, err := ar.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var alert entity.Alert

		err := rows.Scan(
			&alert.ID, &alert.TargetID, &alert.Severity,
			&alert.Details, &alert.Notified, &alert.CreatedAt,
		)
		if err != nil {
			return nil, err
		}

		alerts = append(alerts, &alert)
	}

	err = rows.Err()
	if err != nil {
		return nil, err
	}

	return alerts, nil
}
