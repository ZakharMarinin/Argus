package usecase

import (
	"argus-collector/internal/domain/entity"
	"context"
	"time"
)

func (uc *UseCase) CreateAlert(ctx context.Context, alert *entity.Alert) (*entity.Alert, error) {
	const op = "UseCase.CreateAlert"

	if !alert.Severity.Valid() {
		return nil, wrapErr(op, entity.ErrInvalidSeverity)
	}

	alert.CreatedAt = time.Now()

	created, err := uc.db.CreateAlert(ctx, alert)

	return created, wrapErr(op, err)
}

func (uc *UseCase) DeleteAlert(ctx context.Context, id int) (int, error) {
	const op = "UseCase.DeleteAlert"

	deletedID, err := uc.db.DeleteAlert(ctx, id)

	return deletedID, wrapErr(op, err)
}

func (uc *UseCase) GetAlert(ctx context.Context, id int) (*entity.Alert, error) {
	const op = "UseCase.GetAlert"

	alert, err := uc.db.GetAlert(ctx, id)

	return alert, wrapErr(op, err)
}

func (uc *UseCase) GetAlertsByNotification(ctx context.Context, status entity.AlertStatus, limit uint64) ([]*entity.Alert, error) {
	const op = "UseCase.GetAlertsByNotification"

	alerts, err := uc.db.GetAlertsByNotification(ctx, status, limit)

	return alerts, wrapErr(op, err)
}

func (uc *UseCase) MarkAlertsNotified(ctx context.Context, alerts []*entity.Alert, status entity.AlertStatus) error {
	const op = "UseCase.MarkAlertsNotified"

	ids := make([]int, 0, len(alerts))

	for _, alert := range alerts {
		ids = append(ids, alert.ID)
	}

	err := uc.db.MarkAlertsNotified(ctx, ids, status)

	return wrapErr(op, err)
}

func (uc *UseCase) FindAlerts(ctx context.Context, f entity.ListFilter) ([]*entity.Alert, int, error) {
	const op = "UseCase.FindAlerts"

	alerts, total, err := uc.db.FindAlerts(ctx, f)

	return alerts, total, wrapErr(op, err)
}
