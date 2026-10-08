package usecase_test

import (
	"context"
	"testing"
	"time"

	"argus-frontend/internal/domain/entity"
)

// TestProbeIncidentMeasuresClosedDowntime: a run of failed probes between two
// successful ones must become one interval with a measured duration.
func TestProbeIncidentMeasuresClosedDowntime(t *testing.T) {
	start := now.Add(-3 * time.Hour)
	fb := fakeBackend{
		targets: []targetJSON{
			{ID: "t1", Name: "api", TargetType: "probe", Status: "up"},
		},
		probes: map[string][]probeJSON{
			"t1": {
				{ID: 1, TargetID: "t1", Success: true, Latency: 10_000, CreatedAt: start},
				{ID: 2, TargetID: "t1", Success: false, CreatedAt: start.Add(time.Minute)},
				{ID: 3, TargetID: "t1", Success: false, CreatedAt: start.Add(2 * time.Minute)},
				{ID: 4, TargetID: "t1", Success: true, Latency: 12_000, CreatedAt: start.Add(6 * time.Minute)},
			},
		},
	}

	incidents, stats, err := newUseCase(t, fb).Incidents(context.Background())
	if err != nil {
		t.Fatalf("Incidents: %v", err)
	}
	if len(incidents) != 1 {
		t.Fatalf("incidents = %+v, wanted one interval", incidents)
	}

	i := incidents[0]
	if i.State != entity.IncidentClosed {
		t.Errorf("State = %q, wanted closed", i.State)
	}

	d, ok := i.Duration()
	if !ok || d != 5*time.Minute {
		t.Errorf("Duration = %v (ok=%v), wanted 5m: from the first failure to the first successful probe", d, ok)
	}
	if stats.Total != 5*time.Minute || stats.Longest != 5*time.Minute || stats.Unclosed != 0 {
		t.Errorf("stats = %+v, wanted 5m in total and no unclosed ones", stats)
	}
}

// TestProbeIncidentStaysOpenUntilLastFailure: an ongoing outage is counted up
// to the latest failed probe, not up to time.Now(): if the collector stopped
// checking, the counter must not keep growing on its own.
func TestProbeIncidentStaysOpenUntilLastFailure(t *testing.T) {
	start := now.Add(-2 * time.Hour)
	fb := fakeBackend{
		targets: []targetJSON{
			{ID: "t1", Name: "api", TargetType: "probe", Status: "down"},
		},
		probes: map[string][]probeJSON{
			"t1": {
				{ID: 1, TargetID: "t1", Success: false, CreatedAt: start},
				{ID: 2, TargetID: "t1", Success: false, CreatedAt: start.Add(10 * time.Minute)},
			},
		},
	}

	incidents, _, err := newUseCase(t, fb).Incidents(context.Background())
	if err != nil {
		t.Fatalf("Incidents: %v", err)
	}
	if len(incidents) != 1 || incidents[0].State != entity.IncidentOpen {
		t.Fatalf("incidents = %+v, wanted one open", incidents)
	}
	if incidents[0].EndedAt != nil {
		t.Error("an ongoing outage must not have an end")
	}

	d, ok := i0Duration(t, incidents)
	if !ok || d != 10*time.Minute {
		t.Errorf("Duration = %v (ok=%v), wanted 10m, up to the latest failed probe", d, ok)
	}
}

// TestContainerDieWithRestartIsMeasured: a die closed by a restart gives the
// exact duration of the container outage.
func TestContainerDieWithRestartIsMeasured(t *testing.T) {
	died := now.Add(-90 * time.Minute)
	fb := fakeBackend{
		targets: []targetJSON{
			{ID: "c1", Name: "worker", TargetType: "container", Status: "up"},
		},
		events: map[string][]eventJSON{
			"c1": {
				{ID: 1, TargetID: "c1", Event: "die", Details: "exit 1", CreatedAt: died},
				{ID: 2, TargetID: "c1", Event: "restart", CreatedAt: died.Add(4 * time.Minute)},
			},
		},
	}

	incidents, stats, err := newUseCase(t, fb).Incidents(context.Background())
	if err != nil {
		t.Fatalf("Incidents: %v", err)
	}
	if len(incidents) != 1 {
		t.Fatalf("incidents = %+v", incidents)
	}
	if incidents[0].State != entity.IncidentClosed || incidents[0].Cause != "die" {
		t.Errorf("incident = %+v, wanted closed with cause die", incidents[0])
	}

	d, ok := i0Duration(t, incidents)
	if !ok || d != 4*time.Minute {
		t.Errorf("Duration = %v (ok=%v), wanted 4m", d, ok)
	}
	if stats.Total != 4*time.Minute || stats.Unclosed != 0 {
		t.Errorf("stats = %+v", stats)
	}
}

// TestContainerDieWithStartIsMeasured: argus-backend records "start" as an
// event, and that is what closes the outage when the container comes back,
// both after a restart policy and after a manual docker start.
func TestContainerDieWithStartIsMeasured(t *testing.T) {
	died := now.Add(-2 * time.Hour)
	fb := fakeBackend{
		targets: []targetJSON{
			{ID: "c1", Name: "worker", TargetType: "container", Status: "up"},
		},
		events: map[string][]eventJSON{
			"c1": {
				{ID: 1, TargetID: "c1", Event: "die", Details: "exit 1", CreatedAt: died},
				{ID: 2, TargetID: "c1", Event: "start", CreatedAt: died.Add(3 * time.Minute)},
			},
		},
	}

	incidents, stats, err := newUseCase(t, fb).Incidents(context.Background())
	if err != nil {
		t.Fatalf("Incidents: %v", err)
	}
	if len(incidents) != 1 {
		t.Fatalf("incidents = %+v", incidents)
	}
	if incidents[0].State != entity.IncidentClosed {
		t.Errorf("State = %q, wanted closed: start is the recovery record", incidents[0].State)
	}

	d, ok := i0Duration(t, incidents)
	if !ok || d != 3*time.Minute {
		t.Errorf("Duration = %v (ok=%v), wanted 3m", d, ok)
	}
	if stats.Unclosed != 0 {
		t.Errorf("stats = %+v, there must be no unclosed ones", stats)
	}
}

// TestContainerDieWithoutReturnHasNoDuration: the main honest case: argus-backend
// doesn't record the container's return, so the duration is unknown and can
// be neither computed nor mixed into the total.
func TestContainerDieWithoutReturnHasNoDuration(t *testing.T) {
	fb := fakeBackend{
		targets: []targetJSON{
			{ID: "c1", Name: "worker", TargetType: "container", Status: "up"},
		},
		events: map[string][]eventJSON{
			"c1": {
				{ID: 1, TargetID: "c1", Event: "die", Details: "exit 137", CreatedAt: now.Add(-40 * time.Minute)},
			},
		},
	}

	incidents, stats, err := newUseCase(t, fb).Incidents(context.Background())
	if err != nil {
		t.Fatalf("Incidents: %v", err)
	}
	if len(incidents) != 1 || incidents[0].State != entity.IncidentUnclosed {
		t.Fatalf("incidents = %+v, wanted one unclosed", incidents)
	}

	_, ok := incidents[0].Duration()
	if ok {
		t.Error("an unclosed interval must not have a duration")
	}
	if stats.Total != 0 || stats.Longest != 0 {
		t.Errorf("stats = %+v, an unclosed interval must not be included in the total", stats)
	}
	if stats.Count != 1 || stats.Unclosed != 1 {
		t.Errorf("stats = %+v, wanted one interval and one unclosed", stats)
	}
}

// TestKillAndDiePairIsOneOutage: for a single stop Docker sends two
// events, kill and die. That's one outage, not two, and the first of them must
// not stay unclosed.
func TestKillAndDiePairIsOneOutage(t *testing.T) {
	stopped := now.Add(-30 * time.Minute)
	fb := fakeBackend{
		targets: []targetJSON{
			{ID: "c1", Name: "worker", TargetType: "container", Status: "up"},
		},
		events: map[string][]eventJSON{
			"c1": {
				{ID: 1, TargetID: "c1", Event: "kill", Details: "signal=9", CreatedAt: stopped},
				{ID: 2, TargetID: "c1", Event: "die", Details: "exitCode=137", CreatedAt: stopped.Add(time.Second)},
				{ID: 3, TargetID: "c1", Event: "start", CreatedAt: stopped.Add(7 * time.Minute)},
			},
		},
	}

	incidents, stats, err := newUseCase(t, fb).Incidents(context.Background())
	if err != nil {
		t.Fatalf("Incidents: %v", err)
	}
	if len(incidents) != 1 {
		t.Fatalf("incidents = %+v, wanted one outage for the kill+die pair", incidents)
	}
	if incidents[0].State != entity.IncidentClosed {
		t.Errorf("State = %q, wanted closed", incidents[0].State)
	}
	if incidents[0].Cause != "kill" {
		t.Errorf("Cause = %q, wanted the first failure cause, kill", incidents[0].Cause)
	}

	d, ok := i0Duration(t, incidents)
	if !ok || d != 7*time.Minute {
		t.Errorf("Duration = %v (ok=%v), wanted 7m from the first failure event", d, ok)
	}
	if stats.Unclosed != 0 {
		t.Errorf("stats = %+v, there must be no unclosed ones", stats)
	}
}

// TestIncidentDerivationKeepsEventOrder: building incidents must not
// reorder the page's events: the feed shows the newest first, and sorting
// inside interval derivation must not shuffle it.
func TestIncidentDerivationKeepsEventOrder(t *testing.T) {
	base := now.Add(-3 * time.Hour)
	fb := fakeBackend{
		targets: []targetJSON{
			{ID: "c1", Name: "worker", TargetType: "container", Status: "up"},
		},
		events: map[string][]eventJSON{
			"c1": {
				{ID: 1, TargetID: "c1", Event: "kill", Details: "first", CreatedAt: base},
				{ID: 2, TargetID: "c1", Event: "die", Details: "second", CreatedAt: base.Add(time.Minute)},
				{ID: 3, TargetID: "c1", Event: "start", Details: "third", CreatedAt: base.Add(2 * time.Minute)},
				{ID: 4, TargetID: "c1", Event: "kill", Details: "last", CreatedAt: base.Add(time.Hour)},
			},
		},
	}

	detail, err := newUseCase(t, fb).ContainerDetail(context.Background(), "worker")
	if err != nil {
		t.Fatalf("ContainerDetail: %v", err)
	}
	if len(detail.Events) != 4 {
		t.Fatalf("Events = %+v", detail.Events)
	}
	if detail.Events[0].Detail != "last" {
		t.Errorf("first entry is %q, expected the newest event", detail.Events[0].Detail)
	}
	if detail.Events[3].Detail != "first" {
		t.Errorf("last entry is %q, expected the oldest event", detail.Events[3].Detail)
	}
}

// TestDieAfterStartOpensNewIncident: after a recovery record the next
// failure starts a new outage rather than continuing the previous one.
func TestDieAfterStartOpensNewIncident(t *testing.T) {
	first := now.Add(-5 * time.Hour)
	fb := fakeBackend{
		targets: []targetJSON{
			{ID: "c1", Name: "worker", TargetType: "container", Status: "up"},
		},
		events: map[string][]eventJSON{
			"c1": {
				{ID: 1, TargetID: "c1", Event: "die", CreatedAt: first},
				{ID: 2, TargetID: "c1", Event: "start", CreatedAt: first.Add(time.Hour)},
				{ID: 3, TargetID: "c1", Event: "die", CreatedAt: first.Add(2 * time.Hour)},
			},
		},
	}

	incidents, stats, err := newUseCase(t, fb).Incidents(context.Background())
	if err != nil {
		t.Fatalf("Incidents: %v", err)
	}
	if len(incidents) != 2 {
		t.Fatalf("incidents = %+v, wanted two intervals", incidents)
	}
	if stats.Unclosed != 1 {
		t.Errorf("stats = %+v, wanted one unclosed: the last failure without a return", stats)
	}
	if !incidents[0].StartedAt.After(incidents[1].StartedAt) {
		t.Error("intervals must be ordered newest first")
	}
}

// TestContainerDetailCarriesIncidents: the container page gets the same
// intervals, without a separate request to the backend.
func TestContainerDetailCarriesIncidents(t *testing.T) {
	died := now.Add(-time.Hour)
	fb := fakeBackend{
		targets: []targetJSON{
			{ID: "c1", Name: "worker", TargetType: "container", Status: "up"},
		},
		events: map[string][]eventJSON{
			"c1": {
				{ID: 1, TargetID: "c1", Event: "die", CreatedAt: died},
				{ID: 2, TargetID: "c1", Event: "restart", CreatedAt: died.Add(time.Minute)},
			},
		},
	}

	detail, err := newUseCase(t, fb).ContainerDetail(context.Background(), "worker")
	if err != nil {
		t.Fatalf("ContainerDetail: %v", err)
	}
	if len(detail.Incidents) != 1 {
		t.Fatalf("Incidents = %+v", detail.Incidents)
	}

	d, ok := detail.Incidents[0].Duration()
	if !ok || d != time.Minute {
		t.Errorf("Duration = %v (ok=%v), wanted 1m", d, ok)
	}
}

func i0Duration(t *testing.T, incidents []entity.Incident) (time.Duration, bool) {
	t.Helper()

	return incidents[0].Duration()
}

func TestCrashWarningClearsFifteenMinutesAfterLastReturn(t *testing.T) {
	back := now.Add(-5 * time.Minute)
	fb := fakeBackend{
		targets: []targetJSON{
			{ID: "c1", Name: "worker", TargetType: "container", Status: "up"},
			{ID: "c2", Name: "old", TargetType: "container", Status: "up"},
			{ID: "c3", Name: "deployed", TargetType: "container", Status: "up"},
		},
		events: map[string][]eventJSON{
			"c1": {
				{ID: 1, TargetID: "c1", Event: "die", Details: "exitCode=1", CreatedAt: now.Add(-12 * time.Minute)},
				{ID: 2, TargetID: "c1", Event: "start", CreatedAt: now.Add(-11 * time.Minute)},
				{ID: 3, TargetID: "c1", Event: "die", Details: "exitCode=1", CreatedAt: back.Add(-time.Minute)},
				{ID: 4, TargetID: "c1", Event: "start", CreatedAt: back},
			},
			"c2": {
				{ID: 5, TargetID: "c2", Event: "die", Details: "exitCode=1", CreatedAt: now.Add(-40 * time.Minute)},
				{ID: 6, TargetID: "c2", Event: "start", CreatedAt: now.Add(-39 * time.Minute)},
			},
			"c3": {
				{ID: 7, TargetID: "c3", Event: "kill", Details: "signal=15", CreatedAt: now.Add(-3 * time.Minute)},
				{ID: 8, TargetID: "c3", Event: "die", Details: "exitCode=0", CreatedAt: now.Add(-3 * time.Minute)},
				{ID: 9, TargetID: "c3", Event: "destroy", CreatedAt: now.Add(-3 * time.Minute)},
				{ID: 10, TargetID: "c3", Event: "start", CreatedAt: now.Add(-2 * time.Minute)},
			},
		},
	}

	targets, err := newUseCase(t, fb).TargetStatus(context.Background())
	if err != nil {
		t.Fatalf("TargetStatus: %v", err)
	}

	stats := map[string]*entity.ContainerStats{}
	for _, s := range targets {
		stats[s.Label] = s.Container
	}

	w := stats["worker"]
	if w.Crashes != 2 || w.CrashesClearAt == nil || !w.CrashesClearAt.Equal(back.Add(15*time.Minute)) {
		t.Errorf("worker = %+v, want 2 crashes clearing at %v", w, back.Add(15*time.Minute))
	}

	for _, name := range []string{"old", "deployed"} {
		if s := stats[name]; s.Crashes != 0 || s.CrashesClearAt != nil {
			t.Errorf("%s = %+v, want no crashes", name, s)
		}
	}
}
