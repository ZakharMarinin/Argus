package usecase

import (
	"context"
	"time"

	"argus-frontend/internal/domain/entity"
)

func (uc *UseCase) Recent(ctx context.Context) (entity.Recent, error) {
	byID, err := uc.targetsByID(ctx)
	if err != nil {
		return entity.Recent{}, err
	}

	now := time.Now()

	page, err := uc.backend.Alerts(ctx, entity.ListQuery{Since: now.Add(-24 * time.Hour)})
	if err != nil {
		return entity.Recent{}, notReady(err)
	}

	alerts := make([]entity.Alert, 0, len(page.Items))
	for _, a := range page.Items {
		if !a.CreatedAt.After(now) {
			alerts = append(alerts, enrichAlert(a, byID))
		}
	}

	logs, err := uc.LogMatches(ctx, "")
	if err != nil {
		return entity.Recent{}, err
	}

	incidents, _, err := uc.Incidents(ctx)
	if err != nil {
		return entity.Recent{}, err
	}

	return entity.Recent{Alerts: alerts, LogMatches: logs.Matches, Incidents: incidents}, nil
}
