package usecase

import (
	"context"
	"errors"
	"sort"
	"time"

	"argus-frontend/internal/domain/entity"
)

// stripLimit is how many recent checks the bar on the card shows.
const stripLimit = 24

// TargetStatus returns all targets together with their aggregates for the last 24 hours.
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
			summary.Probe, summary.Outages24h, err = uc.probeStats(ctx, t.ID)
		case entity.KindContainer:
			summary.Container, summary.Outages24h, err = uc.containerStats(ctx, t.ID)
		case entity.KindHost:
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

// probeStats computes an uptime target's aggregates from its full probe history.
//
// Вторым значением возвращает сводку интервалов простоя за 24 часа: те же
// probeIncidents и withinWindow, что у Incidents, поэтому сводка на главной и
// страница Outages сходятся на одних данных без лишних запросов к бэкенду.
func (uc *UseCase) probeStats(ctx context.Context, targetID string) (*entity.ProbeStats, entity.IncidentStats, error) {
	probes, err := uc.backend.Probes(ctx, targetID)
	if err != nil {
		return nil, entity.IncidentStats{}, notReady(err)
	}

	sort.Slice(probes, func(i, j int) bool { return probes[i].CheckedAt.Before(probes[j].CheckedAt) })

	stats := &entity.ProbeStats{}
	dayAgo := time.Now().Add(-24 * time.Hour)

	var last24h []entity.ProbeOutcome

	for _, p := range probes {
		if p.CheckedAt.Before(dayAgo) {
			continue
		}

		if stats.FirstCheckedAt == nil {
			first := p.CheckedAt
			stats.FirstCheckedAt = &first
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

	return stats, incidentStats(withinWindow(probeIncidents(entity.Target{}, probes), dayAgo)), nil
}

const crashWindow = 15 * time.Minute

// containerStats computes a container's aggregates from its Docker events.
//
// Вторым значением, как и probeStats, возвращает сводку интервалов простоя за
// 24 часа по тем же правилам, что у Incidents.
func (uc *UseCase) containerStats(ctx context.Context, targetID string) (*entity.ContainerStats, entity.IncidentStats, error) {
	events, err := uc.backend.Events(ctx, targetID)
	if err != nil {
		return nil, entity.IncidentStats{}, notReady(err)
	}

	outages := incidentStats(withinWindow(containerIncidents(entity.Target{}, events), time.Now().Add(-incidentWindow)))

	return containerStatsFromEvents(events), outages, nil
}

func containerStatsFromEvents(events []entity.ContainerEvent) *entity.ContainerStats {
	crashes, lastReturn := crashesSince(events, time.Now().Add(-crashWindow))
	stats := &entity.ContainerStats{Crashes: crashes}

	if lastReturn != nil {
		clearAt := lastReturn.Add(crashWindow)
		stats.CrashesClearAt = &clearAt
	}

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

func (uc *UseCase) CreateProbe(ctx context.Context, name, source string) error {
	err := uc.backend.CreateTarget(ctx, entity.Target{Kind: entity.KindProbe, Label: name, Source: source})
	if _, ok := errors.AsType[*entity.ValidationError](err); ok {
		return err
	}

	return notReady(err)
}

func (uc *UseCase) DeleteTarget(ctx context.Context, id string) error {
	return notReady(uc.backend.DeleteTarget(ctx, id))
}
