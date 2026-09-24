package watcher

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"argus-collector/internal/config"
	"argus-collector/internal/domain/entity"

	"github.com/google/uuid"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	mocks "argus-collector/internal/usecase/mocks"
)

func TestEventTime(t *testing.T) {
	t.Parallel()

	reported := time.Date(2026, 9, 24, 12, 0, 0, 123, time.UTC)

	cases := []struct {
		name     string
		timeNano int64
		want     func(t *testing.T, got time.Time)
	}{
		{name: "Docker's time is used as is", timeNano: reported.UnixNano(), want: func(t *testing.T, got time.Time) {
			require.True(t, got.Equal(reported))
		}},
		{name: "missing time falls back to now", want: func(t *testing.T, got time.Time) {
			require.WithinDuration(t, time.Now(), got, time.Minute)
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tc.want(t, eventTime(events.Message{TimeNano: tc.timeNano}))
		})
	}
}

func dockerEvent(action events.Action, at time.Time, attrs ...string) events.Message {
	attributes := map[string]string{"name": "web", "image": "nginx"}
	for i := 0; i+1 < len(attrs); i += 2 {
		attributes[attrs[i]] = attrs[i+1]
	}

	return events.Message{Action: action, Actor: events.Actor{Attributes: attributes}, TimeNano: at.UnixNano()}
}

func alertLike(severity entity.Severity, details string) any {
	return mock.MatchedBy(func(a *entity.Alert) bool {
		return a.Severity == severity && a.Details == details
	})
}

func TestCreateEvent(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	crashed := "Container exited unexpectedly with code 1 (image: nginx)"

	cases := []struct {
		name     string
		status   string
		messages []events.Message
		expect   func(m *mocks.MockStorage, id uuid.UUID)
		wantErr  error
	}{
		{
			name: "container not in the database is ignored", status: statusUp,
			messages: []events.Message{dockerEvent(events.ActionDie, t0, "name", "other", "exitCode", "1")},
			expect:   func(*mocks.MockStorage, uuid.UUID) {},
		},
		{
			name: "unsubscribed action is ignored", status: statusUp,
			messages: []events.Message{dockerEvent(events.ActionPause, t0)},
			expect:   func(*mocks.MockStorage, uuid.UUID) {},
		},
		{
			name: "crash exit marks down and alerts", status: statusUp,
			messages: []events.Message{dockerEvent(events.ActionDie, t0, "exitCode", "1")},
			expect: func(m *mocks.MockStorage, id uuid.UUID) {
				m.On("UpdateTargetStatus", mock.Anything, id, statusDown).Return(nil, nil).Once()
				m.On("CreateEvent", mock.Anything, mock.MatchedBy(func(e *entity.ContainerEvent) bool {
					return e.TargetID == id && e.Event == entity.ContainerDie && e.Details == "name=web image=nginx exitCode=1"
				})).Return(&entity.ContainerEvent{}, nil).Once()
				m.On("CreateAlert", mock.Anything, alertLike(entity.SeverityError, crashed)).Return(&entity.Alert{}, nil).Once()
			},
		},
		{
			name: "clean exit marks down without an alert", status: statusUp,
			messages: []events.Message{dockerEvent(events.ActionDie, t0, "exitCode", "0")},
			expect: func(m *mocks.MockStorage, id uuid.UUID) {
				m.On("UpdateTargetStatus", mock.Anything, id, statusDown).Return(nil, nil).Once()
				m.On("CreateEvent", mock.Anything, mock.Anything).Return(&entity.ContainerEvent{}, nil).Once()
			},
		},
		{
			name: "oom alerts critical and leaves the status to die", status: statusUp,
			messages: []events.Message{dockerEvent(events.ActionOOM, t0)},
			expect: func(m *mocks.MockStorage, _ uuid.UUID) {
				m.On("CreateEvent", mock.Anything, mock.Anything).Return(&entity.ContainerEvent{}, nil).Once()
				m.On("CreateAlert", mock.Anything, alertLike(entity.SeverityCritical,
					"Container was killed after running out of memory (image: nginx)")).Return(&entity.Alert{}, nil).Once()
			},
		},
		{
			name: "start brings a down container up quietly", status: statusDown,
			messages: []events.Message{dockerEvent(events.ActionStart, t0)},
			expect: func(m *mocks.MockStorage, id uuid.UUID) {
				m.On("UpdateTargetStatus", mock.Anything, id, statusUp).Return(nil, nil).Once()
				m.On("CreateEvent", mock.Anything, mock.Anything).Return(&entity.ContainerEvent{}, nil).Once()
			},
		},
		{
			name: "a stop you asked for is not a crash", status: statusDown,
			messages: []events.Message{
				dockerEvent(events.ActionKill, t0, "signal", "15"),
				dockerEvent(events.ActionDie, t0.Add(5*time.Second), "exitCode", "143"),
			},
			expect: func(m *mocks.MockStorage, _ uuid.UUID) {
				m.On("CreateEvent", mock.Anything, mock.Anything).Return(&entity.ContainerEvent{}, nil).Twice()
			},
		},
		{
			name: "third crash within the window is a crash loop", status: statusDown,
			messages: []events.Message{
				dockerEvent(events.ActionDie, t0, "exitCode", "1"),
				dockerEvent(events.ActionDie, t0.Add(time.Minute), "exitCode", "1"),
				dockerEvent(events.ActionDie, t0.Add(2*time.Minute), "exitCode", "1"),
			},
			expect: func(m *mocks.MockStorage, _ uuid.UUID) {
				m.On("CreateEvent", mock.Anything, mock.Anything).Return(&entity.ContainerEvent{}, nil).Times(3)
				m.On("CreateAlert", mock.Anything, alertLike(entity.SeverityError, crashed)).Return(&entity.Alert{}, nil).Times(3)
				m.On("CreateAlert", mock.Anything, alertLike(entity.SeverityCritical,
					"Container is crash-looping: 3 restarts within 5m0s")).Return(&entity.Alert{}, nil).Once()
			},
		},
		{
			name: "an event already stored by a replay raises nothing", status: statusDown,
			messages: []events.Message{dockerEvent(events.ActionDie, t0, "exitCode", "1")},
			expect: func(m *mocks.MockStorage, _ uuid.UUID) {
				m.On("CreateEvent", mock.Anything, mock.Anything).Return(nil, entity.ErrContainerEventAlreadyExists).Once()
			},
			wantErr: entity.ErrContainerEventAlreadyExists,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			target := &entity.Target{ID: uuid.New(), Source: "web", Status: tc.status}

			m := mocks.NewMockStorage(t)
			m.On("GetTargetsByType", mock.Anything, watcherTargetType).Return([]*entity.Target{target}, nil)
			tc.expect(m, target.ID)

			cfg := &config.Config{Watcher: config.Watcher{CrashLoopRestarts: 3, CrashLoopWindow: 5 * time.Minute}}
			w := NewWatcher(cfg, slog.New(slog.DiscardHandler), m, nil, nil)

			var err error
			for _, msg := range tc.messages {
				err = w.createEvent(t.Context(), msg)
			}

			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)

				return
			}

			require.NoError(t, err)
		})
	}
}

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

func TestContainerStatus(t *testing.T) {
	t.Parallel()

	cases := []struct {
		event   entity.EventType
		want    string
		changes bool
	}{
		{event: entity.ContainerStart, want: statusUp, changes: true},
		{event: entity.ContainerRestart, want: statusUp, changes: true},
		{event: entity.ContainerDie, want: statusDown, changes: true},
		{event: entity.ContainerDestroy, want: statusDown, changes: true},
		{event: entity.ContainerKill},
		{event: entity.ContainerOOM},
	}

	for _, tc := range cases {
		t.Run(string(tc.event), func(t *testing.T) {
			t.Parallel()

			got, changes := containerStatus(tc.event)
			require.Equal(t, tc.changes, changes)
			require.Equal(t, tc.want, got)
		})
	}
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
	mu       sync.Mutex
	running  []container.Summary
	stream   []events.Message
	hold     time.Duration
	lists    int
	since    []string
	connects []time.Time
}

func (d *fakeDocker) ContainerList(context.Context, client.ContainerListOptions) (client.ContainerListResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.lists++

	return client.ContainerListResult{Items: d.running}, nil
}

func (d *fakeDocker) Events(ctx context.Context, opts client.EventsListOptions) client.EventsResult {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.since = append(d.since, opts.Since)
	d.connects = append(d.connects, time.Now())

	msgs := make(chan events.Message)
	errs := make(chan error, 1)
	stream, hold := d.stream, d.hold
	d.stream = nil

	go func() {
		for _, m := range stream {
			select {
			case msgs <- m:
			case <-ctx.Done():
				return
			}
		}

		select {
		case <-time.After(hold):
		case <-ctx.Done():
		}

		errs <- errors.New("daemon restarted")
	}()

	return client.EventsResult{Messages: msgs, Err: errs}
}

func (d *fakeDocker) counts() ([]time.Time, int) {
	d.mu.Lock()
	defer d.mu.Unlock()

	return append([]time.Time(nil), d.connects...), d.lists
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

func TestRegisterFromLabels(t *testing.T) {
	t.Parallel()

	const probeURL = "http://web:8080/health"

	registers := func(typ, source string) any {
		return mock.MatchedBy(func(tg *entity.Target) bool {
			return tg.TargetType == typ && tg.Source == source && tg.Name == "web"
		})
	}

	cases := []struct {
		name    string
		labels  map[string]string
		expect  func(m *mocks.MockStorage)
		wantErr bool
	}{
		{
			name:   "a probe url without the monitor label is ignored",
			labels: map[string]string{labelProbeURL: probeURL},
			expect: func(*mocks.MockStorage) {},
		},
		{
			name:   "monitor label registers the container",
			labels: map[string]string{labelMonitor: "true"},
			expect: func(m *mocks.MockStorage) {
				m.On("CreateTarget", mock.Anything, registers(watcherTargetType, "web")).Return(&entity.Target{}, nil).Once()
			},
		},
		{
			name:   "probe url adds an uptime target named after the container",
			labels: map[string]string{labelMonitor: "true", labelProbeURL: probeURL},
			expect: func(m *mocks.MockStorage) {
				m.On("CreateTarget", mock.Anything, registers(watcherTargetType, "web")).Return(&entity.Target{}, nil).Once()
				m.On("CreateTarget", mock.Anything, registers(probeTargetType, probeURL)).Return(&entity.Target{}, nil).Once()
			},
		},
		{
			name:   "already registered is not an error",
			labels: map[string]string{labelMonitor: "true", labelProbeURL: probeURL},
			expect: func(m *mocks.MockStorage) {
				m.On("CreateTarget", mock.Anything, mock.Anything).Return(nil, entity.ErrTargetAlreadyExists).Twice()
			},
		},
		{
			name:   "failed container registration skips the probe",
			labels: map[string]string{labelMonitor: "true", labelProbeURL: probeURL},
			expect: func(m *mocks.MockStorage) {
				m.On("CreateTarget", mock.Anything, registers(watcherTargetType, "web")).Return(nil, assert.AnError).Once()
			},
			wantErr: true,
		},
		{
			name:   "failed probe registration is reported",
			labels: map[string]string{labelMonitor: "true", labelProbeURL: probeURL},
			expect: func(m *mocks.MockStorage) {
				m.On("CreateTarget", mock.Anything, registers(watcherTargetType, "web")).Return(&entity.Target{}, nil).Once()
				m.On("CreateTarget", mock.Anything, registers(probeTargetType, probeURL)).Return(nil, assert.AnError).Once()
			},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m := mocks.NewMockStorage(t)
			tc.expect(m)

			err := (&Watcher{uc: m}).registerFromLabels(t.Context(), "web", tc.labels)
			if tc.wantErr {
				require.ErrorIs(t, err, assert.AnError)

				return
			}

			require.NoError(t, err)
		})
	}
}

func TestRunWatcher(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		hold     time.Duration
		run      time.Duration
		wantGaps []time.Duration
	}{
		{
			name: "a failing stream backs off exponentially up to a minute",
			run:  184 * time.Second,
			wantGaps: []time.Duration{
				2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second,
				32 * time.Second, time.Minute, time.Minute,
			},
		},
		{
			name:     "a stream that held for 10s resets the backoff",
			hold:     10 * time.Second,
			run:      35 * time.Second,
			wantGaps: []time.Duration{11 * time.Second, 11 * time.Second, 11 * time.Second},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				m := mocks.NewMockStorage(t)
				m.On("GetTargetsByType", mock.Anything, watcherTargetType).Return(nil, nil).Maybe()

				docker := &fakeDocker{hold: tc.hold}
				w := &Watcher{log: slog.New(slog.DiscardHandler), uc: m, client: docker}

				ctx, cancel := context.WithCancel(t.Context())
				done := make(chan struct{})
				start := time.Now()

				go func() {
					w.RunWatcher(ctx)
					close(done)
				}()

				synctest.Sleep(tc.run)
				cancel()
				<-done

				connects, lists := docker.counts()
				require.Len(t, connects, len(tc.wantGaps)+1)
				require.Equal(t, time.Second, connects[0].Sub(start), "the first subscription waits the minimum backoff")

				for i, want := range tc.wantGaps {
					require.Equal(t, want, connects[i+1].Sub(connects[i]), "gap before subscription %d", i+2)
				}

				require.Equal(t, len(connects)-1, lists, "every reconnect resyncs, the first subscription doesn't")
			})
		})
	}
}

func TestHandleStart(t *testing.T) {
	t.Parallel()

	labelled := map[string]string{"name": "web", labelMonitor: "true"}

	cases := []struct {
		name       string
		attrs      map[string]string
		registered error
		queued     []string
		want       []string
	}{
		{name: "labelled container wakes the matcher", attrs: labelled, want: []string{"web"}},
		{name: "unlabelled container is ignored", attrs: map[string]string{"name": "web"}},
		{name: "failed registration doesn't wake the matcher", attrs: labelled, registered: assert.AnError},
		{name: "a full queue drops the name instead of blocking", attrs: labelled, queued: []string{"db"}, want: []string{"db"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m := mocks.NewMockStorage(t)
			if tc.attrs[labelMonitor] == "true" {
				m.On("CreateTarget", mock.Anything, mock.Anything).Return(&entity.Target{}, tc.registered).Once()
			}

			starts := make(chan string, 1)
			for _, name := range tc.queued {
				starts <- name
			}

			w := &Watcher{log: slog.New(slog.DiscardHandler), uc: m, starts: starts}
			w.handleStart(t.Context(), tc.attrs)
			close(starts)

			var got []string
			for name := range starts {
				got = append(got, name)
			}

			require.Equal(t, tc.want, got)
		})
	}
}
