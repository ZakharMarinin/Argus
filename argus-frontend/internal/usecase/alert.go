package usecase

import (
	"context"
	"time"

	"argus-frontend/internal/domain/entity"
)

const historyPageSize = 50

func (uc *UseCase) AlertCounts(ctx context.Context) (entity.AlertCounts, error) {
	now := time.Now()

	day, err := uc.backend.Alerts(ctx, entity.ListQuery{Since: now.Add(-24 * time.Hour)})
	if err != nil {
		return entity.AlertCounts{}, notReady(err)
	}

	var c entity.AlertCounts

	for _, a := range day.Items {
		if a.CreatedAt.After(now) {
			continue
		}

		c.Day++

		switch a.Severity {
		case entity.SeverityCritical, entity.SeverityError:
			c.Critical++
		case entity.SeverityWarning:
			c.Warning++
		}
	}

	return c, nil
}

func (uc *UseCase) HistoryAlerts(ctx context.Context, q entity.HistoryQuery) (entity.Page[entity.Alert], error) {
	byID, err := uc.targetsByID(ctx)
	if err != nil {
		return entity.Page[entity.Alert]{}, err
	}

	lq := entity.ListQuery{Severity: q.Severity, Skip: skipFor(q.Page), Limit: historyPageSize}

	if q.Kind != "" {
		lq.TargetIDs = idsOfKind(byID, q.Kind)
		if len(lq.TargetIDs) == 0 {
			return entity.Page[entity.Alert]{Skip: lq.Skip}, nil
		}
	}

	page, err := uc.backend.Alerts(ctx, lq)
	if err != nil {
		return entity.Page[entity.Alert]{}, notReady(err)
	}

	for i := range page.Items {
		page.Items[i] = enrichAlert(page.Items[i], byID)
	}

	return page, nil
}

func skipFor(page int) int {
	if page < 1 {
		return 0
	}

	return (page - 1) * historyPageSize
}

func idsOfKind(byID map[string]entity.Target, kind entity.Kind) []string {
	var ids []string

	for id, t := range byID {
		if t.Kind == kind {
			ids = append(ids, id)
		}
	}

	return ids
}

func (uc *UseCase) targetsByID(ctx context.Context) (map[string]entity.Target, error) {
	targets, err := uc.backend.Targets(ctx)
	if err != nil {
		return nil, notReady(err)
	}

	byID := make(map[string]entity.Target, len(targets))
	for _, t := range targets {
		byID[t.ID] = t
	}

	return byID, nil
}

// enrichAlert подшивает к алерту цель. Цели может не быть - тогда вместо
// имени остаётся её id, а не выдуманная подпись.
func enrichAlert(a entity.Alert, targetByID map[string]entity.Target) entity.Alert {
	a.TargetLabel = a.TargetID

	t, ok := targetByID[a.TargetID]
	if ok {
		a.TargetLabel = t.Label
		a.TargetKind = t.Kind
	}

	return a
}
