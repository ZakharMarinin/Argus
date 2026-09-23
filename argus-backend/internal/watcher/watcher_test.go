package watcher

import (
	"testing"

	"argus-collector/internal/domain/entity"

	"github.com/stretchr/testify/require"
)

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
