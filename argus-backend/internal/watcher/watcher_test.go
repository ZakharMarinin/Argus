package watcher

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"argus-collector/internal/config"
	"argus-collector/internal/domain/entity"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	mocks "argus-collector/internal/usecase/mocks"
)

func TestRecordDie(t *testing.T) {
	cfg := &config.Config{Watcher: config.Watcher{CrashLoopRestarts: 3, CrashLoopWindow: time.Minute}}
	w := NewWatcher(cfg, nil, nil, nil, nil)

	looping := uuid.New()
	t0 := time.Now()

	require.False(t, w.recordDie(looping, t0))
	require.False(t, w.recordDie(looping, t0.Add(10*time.Second)))
	require.True(t, w.recordDie(looping, t0.Add(20*time.Second)), "3 dies within the window")
	require.True(t, w.recordDie(looping, t0.Add(30*time.Second)), "loop keeps going")
	require.False(t, w.recordDie(looping, t0.Add(3*time.Minute)), "old dies left the window")
	require.Len(t, w.dies[looping], 1)

	require.False(t, w.recordDie(uuid.New(), t0.Add(3*time.Minute)), "other targets have their own window")
}

func TestPlannedStop(t *testing.T) {
	w := NewWatcher(&config.Config{}, nil, nil, nil, nil)
	id := uuid.New()
	t0 := time.Now()

	require.False(t, w.plannedStop(id, entity.ContainerDie, t0), "die without kill is a crash")

	require.False(t, w.plannedStop(id, entity.ContainerKill, t0))
	require.True(t, w.plannedStop(id, entity.ContainerDie, t0.Add(time.Second)), "docker stop: kill then die")
	require.False(t, w.plannedStop(id, entity.ContainerDie, t0.Add(2*time.Second)), "a kill covers only the next die")

	require.False(t, w.plannedStop(id, entity.ContainerKill, t0))
	require.False(t, w.plannedStop(id, entity.ContainerDie, t0.Add(time.Minute)), "a stale kill doesn't excuse a later crash")

	require.False(t, w.plannedStop(uuid.New(), entity.ContainerDie, t0), "kills are per target")
}

func TestSyncStatuses(t *testing.T) {
	stillUp := &entity.Target{ID: uuid.New(), Source: "api", Status: statusUp}
	fresh := &entity.Target{ID: uuid.New(), Source: "db", Status: "unknown"}
	stopped := &entity.Target{ID: uuid.New(), Source: "worker", Status: statusUp}
	stillDown := &entity.Target{ID: uuid.New(), Source: "cron", Status: statusDown}

	uc := mocks.NewMockStorage(t)
	uc.On("GetTargetsByType", mock.Anything, watcherTargetType).
		Return([]*entity.Target{stillUp, fresh, stopped, stillDown}, nil).Once()
	uc.On("UpdateTargetStatus", mock.Anything, fresh.ID, statusUp).Return(fresh, nil).Once()
	uc.On("UpdateTargetStatus", mock.Anything, stopped.ID, statusDown).Return(stopped, nil).Once()

	w := &Watcher{log: slog.New(slog.DiscardHandler), uc: uc}

	err := w.syncStatuses(context.Background(), map[string]bool{"api": true, "db": true})
	require.NoError(t, err)
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
