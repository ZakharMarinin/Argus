package usecase

import (
	"context"
	"sort"
	"time"

	"argus-frontend/internal/domain/entity"
)

// stripLimit - сколько последних проверок показывает полоска на карточке.
const stripLimit = 24

// TargetStatus отдаёт все цели вместе с агрегатами за последние сутки.
func (uc *UseCase) TargetStatus(ctx context.Context) ([]entity.TargetSummary, error) {
	targets, err := uc.backend.Targets(ctx)
	if err != nil {
		return nil, notReady(err)
	}

	out := make([]entity.TargetSummary, 0, len(targets))

	for _, t := range targets {
		summary := summaryOf(t)

		switch t.Kind {
		case entity.KindProbe:
			summary.Probe, err = uc.probeStats(ctx, t.ID)
		case entity.KindContainer:
			summary.Container, err = uc.containerStats(ctx, t.ID)
		}

		if err != nil {
			return nil, err
		}

		out = append(out, summary)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}

		return out[i].Label < out[j].Label
	})

	return out, nil
}

func summaryOf(t entity.Target) entity.TargetSummary {
	return entity.TargetSummary{
		ID:        t.ID,
		Kind:      t.Kind,
		Label:     t.Label,
		Status:    t.Status,
		Source:    t.Source,
		UpdatedAt: t.UpdatedAt,
	}
}

// probeStats считает агрегаты uptime-таргета из его полной истории проб.
func (uc *UseCase) probeStats(ctx context.Context, targetID string) (*entity.ProbeStats, error) {
	probes, err := uc.backend.Probes(ctx, targetID)
	if err != nil {
		return nil, notReady(err)
	}

	sort.Slice(probes, func(i, j int) bool { return probes[i].CheckedAt.Before(probes[j].CheckedAt) })

	stats := &entity.ProbeStats{}
	dayAgo := time.Now().Add(-24 * time.Hour)

	var last24h []entity.ProbeOutcome

	for _, p := range probes {
		if p.CheckedAt.Before(dayAgo) {
			continue
		}

		last24h = append(last24h, p)
		stats.Checks24h++

		if p.Success {
			stats.Successes24h++
		}
	}

	if len(probes) > 0 {
		latest := probes[len(probes)-1]
		stats.LastLatencyMS = latest.LatencyMS
		checkedAt := latest.CheckedAt
		stats.LastCheckedAt = &checkedAt
	}

	if len(last24h) > stripLimit {
		last24h = last24h[len(last24h)-stripLimit:]
	}

	stats.Recent = last24h

	return stats, nil
}

// containerStats считает агрегаты контейнера из его событий Docker.
func (uc *UseCase) containerStats(ctx context.Context, targetID string) (*entity.ContainerStats, error) {
	events, err := uc.backend.Events(ctx, targetID)
	if err != nil {
		return nil, notReady(err)
	}

	return containerStatsFromEvents(events), nil
}

func containerStatsFromEvents(events []entity.ContainerEvent) *entity.ContainerStats {
	hourAgo := time.Now().Add(-time.Hour)
	stats := &entity.ContainerStats{Restarts1h: returnsSince(events, hourAgo)}

	var latest *entity.ContainerEvent

	for i := range events {
		e := &events[i]
		if latest == nil || e.OccurredAt.After(latest.OccurredAt) {
			latest = e
		}
	}

	if latest != nil {
		stats.LastEvent = latest.Event
		stats.LastEventDetail = latest.Detail
		at := latest.OccurredAt
		stats.LastEventAt = &at
	}

	return stats
}
