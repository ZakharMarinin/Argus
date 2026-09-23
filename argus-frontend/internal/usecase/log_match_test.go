package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"argus-frontend/internal/domain/entity"
)

func TestAlertsPageBeyondListLimit(t *testing.T) {
	alerts := make([]alertJSON, 0, 5001)
	for i := 1; i <= 5001; i++ {
		alerts = append(alerts, alertJSON{ID: i, TargetID: "t1", Severity: 3, CreatedAt: now.Add(-time.Hour)})
	}

	alerts[5000].Severity = 0

	counts, err := newUseCase(t, fakeBackend{alerts: alerts}).AlertCounts(context.Background())
	if err != nil {
		t.Fatalf("AlertCounts: %v", err)
	}

	if counts.Day != 5001 || counts.Critical != 1 {
		t.Errorf("counts = %+v, хотели Day=5001 Critical=1", counts)
	}
}

func TestOldBackendIsARealError(t *testing.T) {
	fb := fakeBackend{old: true, alerts: []alertJSON{
		{ID: 1, TargetID: "t1", CreatedAt: now},
		{ID: 2, TargetID: "t1", CreatedAt: now},
	}}

	_, err := newUseCase(t, fb).AlertCounts(context.Background())
	if !errors.Is(err, entity.ErrBackendTooOld) || errors.Is(err, entity.ErrNotReady) {
		t.Errorf("err = %v, хотели ErrBackendTooOld, а не экран ожидания", err)
	}
}

func TestLogMatchesWindowAndHourly(t *testing.T) {
	fb := fakeBackend{
		targets: []targetJSON{{ID: "c1", Name: "nginx", TargetType: "container", Status: "up"}},
		logMatches: []logMatchJSON{
			{ID: 1, TargetID: "c1", Severity: 0, Message: "old", CreatedAt: now.Add(-25 * time.Hour)},
			{ID: 2, TargetID: "c1", Severity: 1, Message: "err", CreatedAt: now.Add(-3 * time.Hour)},
			{ID: 3, TargetID: "c1", Severity: 2, Message: "warn", CreatedAt: now.Add(-time.Minute)},
			{ID: 4, TargetID: "gone", Severity: 4, Message: "debug", CreatedAt: now.Add(-2 * time.Minute)},
			{ID: 5, TargetID: "c1", Severity: 0, Message: "future", CreatedAt: now.Add(19 * time.Hour)},
		},
	}

	report, err := newUseCase(t, fb).LogMatches(context.Background(), "")
	if err != nil {
		t.Fatalf("LogMatches: %v", err)
	}

	if len(report.Matches) != 3 || report.Future != 1 {
		t.Fatalf("matches = %+v, future = %d, хотели 3 за сутки и 1 из будущего", report.Matches, report.Future)
	}

	if report.Matches[0].Message != "warn" || report.Matches[2].Message != "err" {
		t.Errorf("порядок = %q..%q, хотели новые сверху", report.Matches[0].Message, report.Matches[2].Message)
	}

	if report.Matches[0].TargetLabel != "nginx" || report.Matches[1].TargetLabel != "gone" {
		t.Errorf("подписи = %q/%q", report.Matches[0].TargetLabel, report.Matches[1].TargetLabel)
	}

	var urgent, warning, quiet int
	for _, p := range report.Hourly {
		urgent += p.Urgent
		warning += p.Warning
		quiet += p.Quiet
	}

	if len(report.Hourly) != 24 || urgent != 1 || warning != 1 || quiet != 1 {
		t.Errorf("hourly: %d точек, urgent=%d warning=%d quiet=%d", len(report.Hourly), urgent, warning, quiet)
	}

	hour := now.Add(-time.Minute).Truncate(time.Hour)
	for _, p := range report.Hourly {
		if p.Hour.Equal(hour) && p.Warning != 1 {
			t.Errorf("час %s: warning=%d, хотели 1", hour.Format("15:04"), p.Warning)
		}
	}
}
