package usecase

import (
	"context"
	"sort"
	"time"

	"argus-frontend/internal/domain/entity"
)

// detailEventLimit is how many recent events the container page shows.
const detailEventLimit = 20

// ContainerDetail assembles the page of a single container by its name.
//
// The lookup is by name, not by target id: the page is routed by name
// (/containers/{name}), and there is no longer a stable "container:<name>"
// identifier as in the old Postgres version, only the target's real UUID,
// which isn't visible from outside. Requesting the target by id doesn't work
// here: a container name isn't a UUID, and the backend would answer 400, not
// 404 (which we would take for "backend not ready" rather than "not found").
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

// findContainer looks up a container target by name among all targets.
func findContainer(targets []entity.Target, name string) (entity.Target, bool) {
	for _, t := range targets {
		if t.Kind == entity.KindContainer && t.Label == name {
			return t, true
		}
	}

	return entity.Target{}, false
}

// containerActivity counts events and log matches per hour over the last
// 24 hours. An hour without records is an honest zero (not a gap): unlike
// latency, "there were no events" is exactly what no rows in that hour means.
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
