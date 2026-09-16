package watcher

import (
	"testing"

	"argus-collector/internal/domain/entity"

	"github.com/stretchr/testify/require"
)

func TestAlertForEvent(t *testing.T) {
	cases := []struct {
		name         string
		event        entity.EventType
		exitCode     string
		wantSeverity entity.Severity
		wantAlert    bool
	}{
		{name: "oom always alerts", event: entity.ContainerOOM, exitCode: "137", wantSeverity: entity.SeverityCritical, wantAlert: true},
		{name: "crash exit alerts", event: entity.ContainerDie, exitCode: "1", wantSeverity: entity.SeverityError, wantAlert: true},
		{name: "clean exit is silent", event: entity.ContainerDie, exitCode: "0", wantSeverity: entity.SeverityInfo, wantAlert: false},
		{name: "kill is silent", event: entity.ContainerKill, exitCode: "", wantSeverity: entity.SeverityInfo, wantAlert: false},
		{name: "restart is silent", event: entity.ContainerRestart, exitCode: "", wantSeverity: entity.SeverityInfo, wantAlert: false},
		{name: "destroy is silent", event: entity.ContainerDestroy, exitCode: "", wantSeverity: entity.SeverityInfo, wantAlert: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			severity, ok := alertForEvent(tc.event, tc.exitCode)

			require.Equal(t, tc.wantAlert, ok)
			require.Equal(t, tc.wantSeverity, severity)
		})
	}
}

func TestAlertDetails(t *testing.T) {
	cases := []struct {
		name  string
		event entity.EventType
		attrs map[string]string
		want  string
	}{
		{
			name:  "oom",
			event: entity.ContainerOOM,
			attrs: map[string]string{"image": "nginx"},
			want:  "Container was killed after running out of memory (image: nginx)",
		},
		{
			name:  "crash exit",
			event: entity.ContainerDie,
			attrs: map[string]string{"image": "alpine", "exitCode": "137"},
			want:  "Container exited unexpectedly with code 137 (image: alpine)",
		},
		{
			name:  "other events stay generic",
			event: entity.ContainerRestart,
			attrs: map[string]string{"image": "redis"},
			want:  "Container reported a restart event (image: redis)",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, alertDetails(tc.event, tc.attrs))
		})
	}
}
