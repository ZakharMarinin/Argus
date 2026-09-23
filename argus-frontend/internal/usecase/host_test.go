package usecase_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"argus-frontend/internal/domain/entity"
)

func TestHostMapsSnapshot(t *testing.T) {
	var host any
	_ = json.Unmarshal([]byte(`{
		"hostname": "fedora",
		"sampled_at": "2026-09-24T04:40:15Z",
		"cpu": {"percent": 12.4, "level": "ok"},
		"memory": null,
		"disk": {"percent": 93.1, "used": 900, "total": 1000, "level": "warning"}
	}`), &host)

	h, err := newUseCase(t, fakeBackend{host: host}).Host(context.Background())
	if err != nil {
		t.Fatalf("Host: %v", err)
	}

	if h.Hostname != "fedora" || h.SampledAt == nil || h.CPU == nil || h.CPU.Percent != 12.4 {
		t.Errorf("host = %+v", h)
	}
	if h.Memory != nil {
		t.Errorf("Memory = %+v, want nil for a metric the collector couldn't read", h.Memory)
	}
	if h.Disk == nil || h.Disk.Level != entity.HostLevelWarning || h.Disk.Used != 900 || h.Disk.Total != 1000 {
		t.Errorf("Disk = %+v", h.Disk)
	}
}

func TestHostOnOldBackendIsNotFound(t *testing.T) {
	_, err := newUseCase(t, fakeBackend{}).Host(context.Background())
	if !errors.Is(err, entity.ErrNotFound) || errors.Is(err, entity.ErrNotReady) {
		t.Errorf("Host = %v, want ErrNotFound and not ErrNotReady", err)
	}
}

func TestHostAlertsAreLabelledAndFiltered(t *testing.T) {
	fb := fakeBackend{
		targets: []targetJSON{{ID: "t1", Name: "api", TargetType: "probe"}},
		alerts: []alertJSON{
			{ID: 1, TargetID: "t1", Severity: 1, Details: "Service is unreachable", CreatedAt: now.Add(-time.Minute)},
			{ID: 2, Severity: 2, Details: "Disk usage is 93% (warning above 90%)", CreatedAt: now.Add(-time.Minute)},
		},
		host: map[string]string{"hostname": "fedora"},
	}
	uc := newUseCase(t, fb)

	page, err := uc.HistoryAlerts(context.Background(), entity.HistoryQuery{Kind: entity.KindHost})
	if err != nil {
		t.Fatalf("HistoryAlerts: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != 2 {
		t.Fatalf("host filter = %+v, want only the host alert", page.Items)
	}
	if a := page.Items[0]; a.TargetLabel != "Host · fedora" || a.TargetKind != entity.KindHost {
		t.Errorf("host alert = %q/%q", a.TargetLabel, a.TargetKind)
	}

	page, err = uc.HistoryAlerts(context.Background(), entity.HistoryQuery{Kind: entity.KindProbe})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != 1 {
		t.Errorf("uptime filter = %+v, %v; host alerts must not leak into target filters", page.Items, err)
	}

	recent, err := newUseCase(t, fakeBackend{alerts: fb.alerts[1:]}).Recent(context.Background())
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(recent.Alerts) != 1 || recent.Alerts[0].TargetLabel != "Host" {
		t.Errorf("recent = %+v, want the plain Host label when the collector has no host endpoint", recent.Alerts)
	}
}
