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

func (uc *UseCase) GetAlertsBySeverity(ctx context.Context, severity entity.Severity) ([]*entity.Alert, error) {
	const op = "UseCase.GetAlertsBySeverity"

	alerts, err := uc.db.GetAlertsBySeverity(ctx, severity)

	return alerts, wrapErr(op, err)
}

func (uc *UseCase) GetAlertsByNotification(ctx context.Context, flag bool) ([]*entity.Alert, error) {
	const op = "UseCase.GetAlertsByNotification"

	alerts, err := uc.db.GetAlertsByNotification(ctx, flag)

	return alerts, wrapErr(op, err)
}

func (uc *UseCase) GetAllAlerts(ctx context.Context, limit uint64, offset int) ([]*entity.Alert, error) {
	const op = "UseCase.GetAllAlerts"

	alerts, err := uc.db.GetAllAlerts(ctx, limit, offset)

	return alerts, wrapErr(op, err)
}
