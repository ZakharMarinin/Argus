package usecase

import (
	"context"
	"sort"
	"strings"
	"time"

	"argus-frontend/internal/domain/entity"
)

// incidentWindow is the period over which the dashboard shows incidents.
const incidentWindow = 24 * time.Hour

// opensDowntime reports whether an event starts a container outage.
// "restart" isn't included: it closes an outage, it doesn't open one.
func opensDowntime(event string) bool {
	switch event {
	case "die", "oom", "kill":
		return true
	default:
		return false
	}
}

// closesDowntime reports whether an event closes an outage.
//
// "start" is the main case: argus-backend uses it to mark a container's return
// both after a restart policy and after a manual docker start. "destroy" also
// closes an outage, just differently: the container is gone, and there is
// nothing left to count.
//
// While "start" wasn't in this list, every die -> start pair stayed
// unclosed, and the dashboard said "return not recorded" for a container that
// had come back (TestContainerDieWithStartIsMeasured).
func closesDowntime(event string) bool {
	switch event {
	case "start", "restart", "destroy":
		return true
	default:
		return false
	}
}

// Incidents returns outage intervals across all targets over the last 24 hours,
// newest first, together with a summary.
//
// ponytail: targets, probes and events are fetched one target per request, the
// same N+1 that TargetStatus already does. The incidents partial polls three
// times less often than the page so this doesn't double the traffic to the
// backend; if there are many targets, this needs one combined request on the
// backend, not a cache here.
func (uc *UseCase) Incidents(ctx context.Context) ([]entity.Incident, entity.IncidentStats, error) {
	targets, err := uc.backend.Targets(ctx)
	if err != nil {
		return nil, entity.IncidentStats{}, notReady(err)
	}

	since := time.Now().Add(-incidentWindow)

	var out []entity.Incident

	for _, t := range targets {
		found, err := uc.incidentsFor(ctx, t)
		if err != nil {
			return nil, entity.IncidentStats{}, err
		}

		out = append(out, withinWindow(found, since)...)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })

	return out, incidentStats(out), nil
}

func (uc *UseCase) incidentsFor(ctx context.Context, t entity.Target) ([]entity.Incident, error) {
	switch t.Kind {
	case entity.KindProbe:
		probes, err := uc.backend.Probes(ctx, t.ID)
		if err != nil {
			return nil, notReady(err)
		}

		return probeIncidents(t, probes), nil

	case entity.KindContainer:
		events, err := uc.backend.Events(ctx, t.ID)
		if err != nil {
			return nil, notReady(err)
		}

		return containerIncidents(t, events), nil

	case entity.KindHost:
	}

	return nil, nil
}

// probeIncidents builds intervals from runs of failed probes.
//
// A run is computed over the whole history, not the window: an interval that
// started before midnight and closed in the morning must have its real start,
// not one cut off at the period boundary.
func probeIncidents(t entity.Target, probes []entity.ProbeOutcome) []entity.Incident {
	probes = sortedByTime(probes, func(p entity.ProbeOutcome) time.Time { return p.CheckedAt })

	var (
		out     []entity.Incident
		current *entity.Incident
	)

	for _, p := range probes {
		if !p.Success {
			if current == nil {
				current = &entity.Incident{
					TargetID:    t.ID,
					TargetLabel: t.Label,
					Kind:        t.Kind,
					StartedAt:   p.CheckedAt,
					Cause:       "probe failed",
					State:       entity.IncidentOpen,
				}
			}

			current.LastKnownAt = p.CheckedAt

			continue
		}

		if current != nil {
			endedAt := p.CheckedAt
			current.EndedAt = &endedAt
			current.State = entity.IncidentClosed
			out = append(out, *current)
			current = nil
		}
	}

	// A run that reaches the latest probe is ongoing: checks continue, and the
	// duration is counted up to the latest failure, not up to "now".
	if current != nil {
		out = append(out, *current)
	}

	return out
}

// containerIncidents builds intervals from Docker events.
//
// A failure event while an interval is already open doesn't open anything new:
// you can't go down without having come back. This isn't a detail: for a single
// stop Docker sends a kill + die pair, and counting it as two outages would make
// up the second one (TestKillAndDiePairIsOneOutage). A new interval starts only
// after a recovery record.
func containerIncidents(t entity.Target, events []entity.ContainerEvent) []entity.Incident {
	events = sortedByTime(events, func(e entity.ContainerEvent) time.Time { return e.OccurredAt })

	var (
		out     []entity.Incident
		current *entity.Incident
	)

	for _, e := range events {
		switch {
		case opensDowntime(e.Event):
			// Already down: this is the second event of the same stop, it keeps
			// the first cause. Only the latest observation moves.
			if current != nil {
				current.LastKnownAt = e.OccurredAt

				continue
			}

			current = &entity.Incident{
				TargetID:    t.ID,
				TargetLabel: t.Label,
				Kind:        t.Kind,
				StartedAt:   e.OccurredAt,
				LastKnownAt: e.OccurredAt,
				Cause:       e.Event,
				Crash:       crashed(e),
				State:       entity.IncidentUnclosed,
			}

		case closesDowntime(e.Event) && current != nil:
			endedAt := e.OccurredAt
			current.EndedAt = &endedAt
			current.State = entity.IncidentClosed
			out = append(out, *current)
			current = nil
		}
	}

	if current != nil {
		out = append(out, *current)
	}

	return out
}

func crashesSince(events []entity.ContainerEvent, since time.Time) (int, *time.Time) {
	var (
		n    int
		last *time.Time
	)

	for _, i := range containerIncidents(entity.Target{}, events) {
		if !i.Crash || i.State != entity.IncidentClosed || i.EndedAt == nil || i.EndedAt.Before(since) {
			continue
		}

		n++

		if last == nil || i.EndedAt.After(*last) {
			last = i.EndedAt
		}
	}

	return n, last
}

func crashed(e entity.ContainerEvent) bool {
	switch e.Event {
	case "oom":
		return true
	case "die":
		return exitCode(e.Detail) != "0"
	default:
		return false
	}
}

func exitCode(detail string) string {
	for f := range strings.FieldsSeq(detail) {
		if code, ok := strings.CutPrefix(f, "exitCode="); ok {
			return code
		}
	}

	return ""
}

// withinWindow keeps the intervals that overlap the period: those that
// started in it or ended in it.
func withinWindow(incidents []entity.Incident, since time.Time) []entity.Incident {
	out := make([]entity.Incident, 0, len(incidents))

	for _, i := range incidents {
		switch {
		case !i.StartedAt.Before(since):
			out = append(out, i)
		case i.EndedAt != nil && !i.EndedAt.Before(since):
			out = append(out, i)
		case i.State == entity.IncidentOpen && !i.LastKnownAt.Before(since):
			out = append(out, i)
		}
	}

	return out
}

// sortedByTime returns a copy sorted by ascending time.
//
// A copy, because the same slice of events is already laid out for the feed on
// the container page, and sorting in place would shuffle it for the caller: the
// feed would show the oldest events instead of the newest
// (TestIncidentDerivationKeepsEventOrder). The sort is stable: events of a
// single stop have timestamps a fraction of a second apart, and there is no
// reason to swap equal ones.
func sortedByTime[T any](items []T, at func(T) time.Time) []T {
	out := append([]T(nil), items...)
	sort.SliceStable(out, func(i, j int) bool { return at(out[i]).Before(at(out[j])) })

	return out
}

func incidentStats(incidents []entity.Incident) entity.IncidentStats {
	stats := entity.IncidentStats{Count: len(incidents)}

	for _, i := range incidents {
		d, ok := i.Duration()
		if !ok {
			stats.Unclosed++

			continue
		}

		stats.Total += d

		if d > stats.Longest {
			stats.Longest = d
		}
	}

	return stats
}
