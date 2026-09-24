package watcher

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"argus-collector/internal/config"
	"argus-collector/internal/domain/entity"

	"github.com/google/uuid"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/client"
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

type fakeDocker struct {
	running []container.Summary
	stream  []events.Message
	lists   int
	since   []string
}

func (d *fakeDocker) ContainerList(context.Context, client.ContainerListOptions) (client.ContainerListResult, error) {
	d.lists++

	return client.ContainerListResult{Items: d.running}, nil
}

func (d *fakeDocker) Events(_ context.Context, opts client.EventsListOptions) client.EventsResult {
	d.since = append(d.since, opts.Since)

	msgs := make(chan events.Message)
	errs := make(chan error, 1)
	stream := d.stream
	d.stream = nil

	go func() {
		for _, m := range stream {
			msgs <- m
		}

		errs <- errors.New("daemon restarted")
	}()

	return client.EventsResult{Messages: msgs, Err: errs}
}

func TestStartListeningResyncsAfterReconnect(t *testing.T) {
	diedDuringGap := &entity.Target{ID: uuid.New(), Source: "worker", Status: statusUp}
	startedDuringGap := &entity.Target{ID: uuid.New(), Source: "web", Status: statusUp}

	uc := mocks.NewMockStorage(t)
	uc.On("CreateTarget", mock.Anything, mock.Anything).Return(nil, entity.ErrTargetAlreadyExists).Once()
	uc.On("GetTargetsByType", mock.Anything, watcherTargetType).
		Return([]*entity.Target{diedDuringGap, startedDuringGap}, nil).Once()
	uc.On("UpdateTargetStatus", mock.Anything, diedDuringGap.ID, statusDown).Return(diedDuringGap, nil).Once()

	starts := make(chan string, 2)
	docker := &fakeDocker{running: []container.Summary{
		{Names: []string{"/web"}, Labels: map[string]string{labelMonitor: "true"}},
		{Names: []string{"/unwatched"}},
	}}
	w := &Watcher{log: slog.New(slog.DiscardHandler), uc: uc, client: docker, starts: starts}

	require.Error(t, w.startListening(t.Context(), false))
	require.Zero(t, docker.lists, "the first connection relies on RegisterContainers from app startup")

	require.Error(t, w.startListening(t.Context(), true))
	require.Equal(t, 1, docker.lists)
	require.Len(t, starts, 1, "only labelled containers wake the matcher")
	require.Equal(t, "web", <-starts)
}

func TestReconnectReplaysFromLastEvent(t *testing.T) {
	web := &entity.Target{ID: uuid.New(), Source: "web", Status: statusDown}
	killedAt := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	diedAt := killedAt.Add(40 * time.Second)

	message := func(action events.Action, at time.Time) events.Message {
		return events.Message{
			Action:   action,
			Actor:    events.Actor{Attributes: map[string]string{"name": "web", "exitCode": "143"}},
			TimeNano: at.UnixNano(),
		}
	}

	uc := mocks.NewMockStorage(t)
	uc.On("GetTargetsByType", mock.Anything, watcherTargetType).Return([]*entity.Target{web}, nil)

	for _, at := range []time.Time{killedAt, diedAt} {
		uc.On("CreateEvent", mock.Anything, mock.MatchedBy(func(e *entity.ContainerEvent) bool {
			return e.CreatedAt.Equal(at)
		})).Return(&entity.ContainerEvent{}, nil).Once()
	}

	uc.On("CreateAlert", mock.Anything, mock.MatchedBy(func(a *entity.Alert) bool {
		return a.Details == "Container exited unexpectedly with code 143 (image: )"
	})).Return(&entity.Alert{}, nil).Once()

	docker := &fakeDocker{stream: []events.Message{
		message(events.ActionKill, killedAt),
		message(events.ActionDie, diedAt),
	}}
	w := NewWatcher(&config.Config{Watcher: config.Watcher{CrashLoopRestarts: 5, CrashLoopWindow: time.Minute}},
		slog.New(slog.DiscardHandler), uc, docker, nil)
	w.cursor = killedAt.Add(-time.Minute)

	require.Error(t, w.startListening(t.Context(), false), "a die 40s after the kill by Docker time is a crash, even when replayed instantly")
	require.Error(t, w.startListening(t.Context(), false))

	require.Equal(t, []string{
		killedAt.Add(-time.Minute + time.Nanosecond).Format(time.RFC3339Nano),
		diedAt.Add(time.Nanosecond).Format(time.RFC3339Nano),
	}, docker.since)
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
