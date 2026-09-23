package usecase

import (
	"context"
	"sort"
	"time"

	"argus-frontend/internal/domain/entity"
)

func (uc *UseCase) LogMatches(ctx context.Context, target string) (entity.LogMatchReport, error) {
	byID, err := uc.targetsByID(ctx)
	if err != nil {
		return entity.LogMatchReport{}, err
	}

	now := time.Now()
	q := entity.ListQuery{Since: now.Add(-24 * time.Hour)}

	if target != "" {
		id, ok := containerID(byID, target)
		if !ok {
			return entity.LogMatchReport{}, entity.ErrNotFound
		}

		q.TargetIDs = []string{id}
	}

	page, err := uc.backend.LogMatches(ctx, q)
	if err != nil {
		return entity.LogMatchReport{}, notReady(err)
	}

	matches, future := lastDay(page.Items, now)
	labelMatches(matches, byID)

	return entity.LogMatchReport{Matches: matches, Hourly: logMatchHours(matches, now), Future: future}, nil
}

func (uc *UseCase) HistoryLogMatches(ctx context.Context, q entity.HistoryQuery) (entity.Page[entity.LogMatch], error) {
	byID, err := uc.targetsByID(ctx)
	if err != nil {
		return entity.Page[entity.LogMatch]{}, err
	}

	lq := entity.ListQuery{Severity: q.Severity, Skip: skipFor(q.Page), Limit: historyPageSize}

	if q.Target != "" {
		id, ok := containerID(byID, q.Target)
		if !ok {
			return entity.Page[entity.LogMatch]{Skip: lq.Skip}, nil
		}

		lq.TargetIDs = []string{id}
	}

	page, err := uc.backend.LogMatches(ctx, lq)
	if err != nil {
		return entity.Page[entity.LogMatch]{}, notReady(err)
	}

	labelMatches(page.Items, byID)

	return page, nil
}

func containerID(byID map[string]entity.Target, name string) (string, bool) {
	for id, t := range byID {
		if t.Kind == entity.KindContainer && t.Label == name {
			return id, true
		}
	}

	return "", false
}

func labelMatches(matches []entity.LogMatch, byID map[string]entity.Target) {
	for i := range matches {
		matches[i].TargetLabel = matches[i].TargetID

		t, ok := byID[matches[i].TargetID]
		if ok {
			matches[i].TargetLabel = t.Label
			matches[i].TargetKind = t.Kind
		}
	}
}

func lastDay(matches []entity.LogMatch, now time.Time) ([]entity.LogMatch, int) {
	dayAgo := now.Add(-24 * time.Hour)
	out := make([]entity.LogMatch, 0, len(matches))
	future := 0

	for _, m := range matches {
		switch {
		case m.CreatedAt.After(now):
			future++
		case !m.CreatedAt.Before(dayAgo):
			out = append(out, m)
		}
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })

	return out, future
}

func logMatchHours(matches []entity.LogMatch, now time.Time) []entity.LogMatchPoint {
	last := now.Truncate(time.Hour)
	points := make([]entity.LogMatchPoint, 24)

	for i := range points {
		points[i].Hour = last.Add(-time.Duration(23-i) * time.Hour)
	}

	for _, m := range matches {
		i, ok := hourIndex(last, m.CreatedAt, len(points))
		if !ok {
			continue
		}

		switch m.Severity {
		case entity.SeverityCritical, entity.SeverityError:
			points[i].Urgent++
		case entity.SeverityWarning:
			points[i].Warning++
		default:
			points[i].Quiet++
		}
	}

	return points
}
