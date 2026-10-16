package usecase

import (
	"argus-collector/internal/domain/entity"
	"context"
	"time"
)

func (uc *UseCase) CreateAlert(ctx context.Context, alert *entity.Alert) (*entity.Alert, error) {
	const op = "UseCase.CreateAlert"

	if !alert.Severity.Valid() {
		return nil, wrapErr(op, &entity.ValidationError{Reason: "invalid severity"})
	}

	alert.CreatedAt = time.Now()

	created, err := uc.db.CreateAlert(ctx, alert)

	return created, wrapErr(op, err)
}

func (uc *UseCase) DeleteAlert(ctx context.Context, id int) (int, error) {
	return uc.db.DeleteAlert(ctx, id)
}

func (uc *UseCase) GetAlert(ctx context.Context, id int) (*entity.Alert, error) {
	return uc.db.GetAlert(ctx, id)
}

func (uc *UseCase) GetAlertsByNotification(ctx context.Context, status entity.AlertStatus, limit uint64) ([]*entity.Alert, error) {
	return uc.db.GetAlertsByNotification(ctx, status, limit)
}

func (uc *UseCase) MarkAlertsNotified(ctx context.Context, alerts []*entity.Alert, status entity.AlertStatus) error {
	ids := make([]int, 0, len(alerts))

	for _, alert := range alerts {
		ids = append(ids, alert.ID)
	}

	return uc.db.MarkAlertsNotified(ctx, ids, status)
}

func (uc *UseCase) FindAlerts(ctx context.Context, f entity.ListFilter) ([]*entity.Alert, int, error) {
	return uc.db.FindAlerts(ctx, f)
}
