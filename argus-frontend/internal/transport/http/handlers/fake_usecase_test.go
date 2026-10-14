package handlers_test

import (
	"context"
	"time"

	"argus-frontend/internal/domain/entity"
)

// fakeUseCase stands in for the use cases in view tests.
//
// It computes nothing: its job is to return exactly the structures usecase
// would return, and to check that transport lays them out in markup without
// making anything up. Errors are set through the err field, to test the waiting screen.
type fakeUseCase struct {
	targets   []entity.TargetSummary
	alerts    []entity.Alert
	counts    entity.AlertCounts
	detail    entity.ContainerDetail
	latency   []entity.LatencyPoint
	incidents []entity.Incident
	stats     entity.IncidentStats
	logs      entity.LogMatchReport
	logPage   *entity.Page[entity.LogMatch]
	err       error
	detailErr error
	writeErr  error
	host      entity.Host
	hostErr   error
}

func (f fakeUseCase) TargetStatus(context.Context) ([]entity.TargetSummary, error) {
	return f.targets, f.err
}

func (f fakeUseCase) HistoryAlerts(context.Context, entity.HistoryQuery) (entity.Page[entity.Alert], error) {
	return entity.Page[entity.Alert]{Items: f.alerts, Total: len(f.alerts)}, f.err
}

func (f fakeUseCase) HistoryLogMatches(context.Context, entity.HistoryQuery) (entity.Page[entity.LogMatch], error) {
	if f.logPage != nil {
		return *f.logPage, f.err
	}

	return entity.Page[entity.LogMatch]{Items: f.logs.Matches, Total: len(f.logs.Matches)}, f.err
}

func (f fakeUseCase) Recent(context.Context) (entity.Recent, error) {
	return entity.Recent{Alerts: f.alerts, LogMatches: f.logs.Matches, Incidents: f.incidents}, f.err
}

func (f fakeUseCase) AlertCounts(context.Context) (entity.AlertCounts, error) {
	return f.counts, f.err
}

func (f fakeUseCase) ContainerDetail(context.Context, string) (entity.ContainerDetail, error) {
	if f.detailErr != nil {
		return entity.ContainerDetail{}, f.detailErr
	}

	return f.detail, f.err
}

func (f fakeUseCase) LatencySeries(context.Context) ([]entity.LatencyPoint, error) {
	return f.latency, f.err
}

func (f fakeUseCase) Incidents(context.Context) ([]entity.Incident, entity.IncidentStats, error) {
	return f.incidents, f.stats, f.err
}

func (f fakeUseCase) LogMatches(context.Context, string) (entity.LogMatchReport, error) {
	return f.logs, f.err
}

func (f fakeUseCase) Ping(context.Context) error { return f.err }

func (f fakeUseCase) Host(context.Context) (entity.Host, error) { return f.host, f.hostErr }

func (f fakeUseCase) CreateProbe(context.Context, string, string) error { return f.writeErr }

func (f fakeUseCase) DeleteTarget(context.Context, string) error { return f.writeErr }

// ---------------------------------------------------------------------------
// Test data
// ---------------------------------------------------------------------------

var testNow = time.Date(2026, time.September, 5, 14, 30, 0, 0, time.UTC)

// filledUseCase is a database with live targets, down targets, and a target with
// no checks at all: the last one catches attempts to draw 100% where there is no data.
func filledUseCase() fakeUseCase {
	return fakeUseCase{
		targets: []entity.TargetSummary{
			{
				ID: "probe:https://api.local/health", Kind: entity.KindProbe,
				Label: "API health", Status: entity.StatusUp, UpdatedAt: testNow,
				Probe: &entity.ProbeStats{
					LastLatencyMS: new(42.0),
					LastCheckedAt: new(testNow.Add(-30 * time.Second)),
					Checks24h:     100, Successes24h: 99,
					Recent: []entity.ProbeOutcome{
						{Success: true, CheckedAt: testNow.Add(-2 * time.Minute)},
						{Success: false, CheckedAt: testNow.Add(-time.Minute)},
					},
				},
			},
			{
				ID: "probe:tcp://mail.local:587", Kind: entity.KindProbe,
				Label: "SMTP relay", Status: entity.StatusDown, UpdatedAt: testNow,
				// No checks yet: the target is in the config, the collector hasn't reached it.
				Probe: &entity.ProbeStats{},
				// Тот же закрытый интервал, что в incidents ниже.
				Outages24h: entity.IncidentStats{Count: 1, Total: 5 * time.Minute, Longest: 5 * time.Minute},
			},
			{
				ID: "container:app-worker", Kind: entity.KindContainer,
				Label: "app-worker", Status: entity.StatusUp, UpdatedAt: testNow,
				Container: &entity.ContainerStats{
					Crashes:     2,
					LastEvent:   "restart",
					LastEventAt: new(testNow.Add(-20 * time.Minute)),
				},
				// Тот же интервал без записанного возвращения, что в incidents ниже.
				Outages24h: entity.IncidentStats{Count: 1, Unclosed: 1},
			},
			{
				ID: "container:grafana-agent", Kind: entity.KindContainer,
				Label: "grafana-agent", Status: entity.StatusDown, UpdatedAt: testNow,
				Container: &entity.ContainerStats{
					LastEvent: "die", LastEventDetail: "exit 137",
					LastEventAt: new(testNow.Add(-3 * time.Hour)),
				},
			},
		},
		counts: entity.AlertCounts{Day: 3, Critical: 1, Warning: 2},
		alerts: []entity.Alert{
			{
				ID: 1, TargetID: "probe:tcp://mail.local:587", TargetLabel: "SMTP relay",
				TargetKind: entity.KindProbe, Severity: "critical",
				Message: "TCP port is not responding", Status: entity.AlertStatusSent,
				CreatedAt: testNow.Add(-5 * time.Minute),
			},
			{
				ID: 2, TargetID: "container:app-worker", TargetLabel: "app-worker",
				TargetKind: entity.KindContainer, Severity: "warning",
				Message: "7 log matches", Status: entity.AlertStatusNotSent,
				CreatedAt: testNow.Add(-20 * time.Hour),
			},
		},
		incidents: []entity.Incident{
			{
				TargetID: "container:app-worker", TargetLabel: "app-worker",
				Kind: entity.KindContainer, Cause: "die",
				StartedAt: testNow.Add(-40 * time.Minute), LastKnownAt: testNow.Add(-40 * time.Minute),
				// The backend doesn't record the container's return: the duration is unknown.
				State: entity.IncidentUnclosed,
			},
			{
				TargetID: "probe:tcp://mail.local:587", TargetLabel: "SMTP relay",
				Kind: entity.KindProbe, Cause: "probe failed",
				StartedAt:   testNow.Add(-3 * time.Hour),
				LastKnownAt: testNow.Add(-3 * time.Hour).Add(4 * time.Minute),
				EndedAt:     new(testNow.Add(-3 * time.Hour).Add(5 * time.Minute)),
				State:       entity.IncidentClosed,
			},
		},
		stats: entity.IncidentStats{Count: 2, Total: 5 * time.Minute, Longest: 5 * time.Minute, Unclosed: 1},
		latency: []entity.LatencyPoint{
			{Hour: testNow.Add(-2 * time.Hour), P95MS: new(120.0), UptimePct: new(99.9)},
			{Hour: testNow.Add(-time.Hour)},
			{Hour: testNow, P95MS: new(880.0), UptimePct: new(96.4)},
		},
		logs: entity.LogMatchReport{
			Matches: []entity.LogMatch{
				{
					ID: 7, TargetID: "container:app-worker", TargetLabel: "app-worker",
					TargetKind: entity.KindContainer, Severity: "error",
					Message: "panic: connection refused", CreatedAt: testNow.Add(-3 * time.Minute),
				},
				{
					ID: 6, TargetID: "container:app-worker", TargetLabel: "app-worker",
					TargetKind: entity.KindContainer, Severity: "warning",
					Message: "retrying in 5s", CreatedAt: testNow.Add(-2 * time.Hour),
				},
			},
			Hourly: []entity.LogMatchPoint{
				{Hour: testNow.Add(-2 * time.Hour), Warning: 1},
				{Hour: testNow.Add(-time.Hour)},
				{Hour: testNow, Urgent: 1},
			},
		},
		detail: entity.ContainerDetail{
			Target: entity.TargetSummary{
				ID: "container:app-worker", Kind: entity.KindContainer,
				Label: "app-worker", Status: entity.StatusUp, UpdatedAt: testNow,
				Container: &entity.ContainerStats{
					Crashes:   2,
					LastEvent: "restart", LastEventAt: new(testNow.Add(-20 * time.Minute)),
				},
			},
			Events: []entity.ContainerEvent{
				{Event: "restart", Detail: "health-check", OccurredAt: testNow.Add(-20 * time.Minute)},
			},
			LogMatches: []entity.LogMatch{
				{
					ID: 7, TargetID: "container:app-worker", TargetLabel: "app-worker",
					TargetKind: entity.KindContainer, Severity: "error",
					Message: "panic: connection refused", CreatedAt: testNow.Add(-3 * time.Minute),
				},
			},
			Activity: []entity.ActivityPoint{
				{Hour: testNow.Add(-time.Hour), Events: 1},
				{Hour: testNow, Events: 0},
			},
		},
	}
}
