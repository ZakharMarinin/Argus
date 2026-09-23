package usecase

import (
	"context"
	"sort"
	"time"

	"argus-frontend/internal/domain/entity"
)

// detailEventLimit - сколько последних событий показывает страница контейнера.
const detailEventLimit = 20

// ContainerDetail собирает страницу одного контейнера по его имени.
//
// Поиск идёт по имени, а не по id цели: страница маршрутизируется по имени
// (/containers/{name}), и стабильного "container:<имя>" идентификатора, как в
// старой Postgres-версии, больше нет - только настоящий UUID цели, который
// снаружи не виден. Запрос цели по id здесь не подходит: имя контейнера - не
// UUID, и бэкенд ответит на него 400, а не 404 (что мы приняли бы за «бэкенд
// не готов», а не «не найдено»).
func (uc *UseCase) ContainerDetail(ctx context.Context, name string) (entity.ContainerDetail, error) {
	targets, err := uc.backend.Targets(ctx)
	if err != nil {
		return entity.ContainerDetail{}, notReady(err)
	}

	target, ok := findContainer(targets, name)
	if !ok {
		return entity.ContainerDetail{}, entity.ErrNotFound
	}

	events, err := uc.backend.Events(ctx, target.ID)
	if err != nil {
		return entity.ContainerDetail{}, notReady(err)
	}

	page, err := uc.backend.LogMatches(ctx, entity.ListQuery{
		TargetIDs: []string{target.ID},
		Since:     time.Now().Add(-24 * time.Hour),
	})
	if err != nil {
		return entity.ContainerDetail{}, notReady(err)
	}

	matches, _ := lastDay(page.Items, time.Now())

	for i := range matches {
		matches[i].TargetLabel = target.Label
		matches[i].TargetKind = target.Kind
	}

	sort.Slice(events, func(i, j int) bool { return events[i].OccurredAt.After(events[j].OccurredAt) })

	summary := summaryOf(target)
	summary.Container = containerStatsFromEvents(events)

	limited := events
	if len(limited) > detailEventLimit {
		limited = limited[:detailEventLimit]
	}

	return entity.ContainerDetail{
		Target:     summary,
		Events:     limited,
		Activity:   containerActivity(events, matches),
		LogMatches: matches,
		Incidents:  withinWindow(containerIncidents(target, events), time.Now().Add(-incidentWindow)),
	}, nil
}

// findContainer ищет контейнер-цель по имени среди всех целей.
func findContainer(targets []entity.Target, name string) (entity.Target, bool) {
	for _, t := range targets {
		if t.Kind == entity.KindContainer && t.Label == name {
			return t, true
		}
	}

	return entity.Target{}, false
}

// containerActivity считает события и совпадения логов по часам за последние
// сутки. Час без записей - честный ноль (не разрыв): в отличие от latency,
// «событий не было» - это ровно то, что означает отсутствие строк в этот час.
func containerActivity(events []entity.ContainerEvent, matches []entity.LogMatch) []entity.ActivityPoint {
	now := time.Now().Truncate(time.Hour)
	points := make([]entity.ActivityPoint, 24)

	for i := range points {
		points[i].Hour = now.Add(-time.Duration(23-i) * time.Hour)
	}

	for _, e := range events {
		i, ok := hourIndex(now, e.OccurredAt, len(points))
		if ok {
			points[i].Events++
		}
	}

	for _, m := range matches {
		i, ok := hourIndex(now, m.CreatedAt, len(points))
		if ok {
			points[i].LogMatches++
		}
	}

	return points
}

func hourIndex(lastHour, at time.Time, n int) (int, bool) {
	back := lastHour.Sub(at.Truncate(time.Hour)) / time.Hour
	if back < 0 || int(back) >= n {
		return 0, false
	}

	return n - 1 - int(back), true
}
