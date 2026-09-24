package matcher

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"argus-collector/internal/config"
	"argus-collector/internal/domain/entity"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/google/uuid"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	mocks "argus-collector/internal/usecase/mocks"
)

type fakeDocker struct {
	mu         sync.Mutex
	tty        bool
	startedAt  string
	inspectErr error
	noState    bool
	stopped    func(logCalls int) bool
	logs       func(ctx context.Context, call int) io.ReadCloser
	since      map[string][]string
}

func (f *fakeDocker) ContainerInspect(_ context.Context, name string, _ client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.inspectErr != nil {
		return client.ContainerInspectResult{}, f.inspectErr
	}

	resp := container.InspectResponse{Config: &container.Config{Tty: f.tty}}
	if !f.noState {
		running := f.stopped == nil || !f.stopped(len(f.since[name]))
		resp.State = &container.State{Running: running, StartedAt: f.startedAt}
	}

	return client.ContainerInspectResult{Container: resp}, nil
}

func (f *fakeDocker) ContainerLogs(ctx context.Context, name string, opts client.ContainerLogsOptions) (client.ContainerLogsResult, error) {
	f.mu.Lock()
	if f.since == nil {
		f.since = make(map[string][]string)
	}

	f.since[name] = append(f.since[name], opts.Since)
	call := len(f.since[name])
	f.mu.Unlock()

	return f.logs(ctx, call), nil
}

func (f *fakeDocker) sinces(name string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.since[name]...)
}

func static(logs string) func(context.Context, int) io.ReadCloser {
	return func(context.Context, int) io.ReadCloser { return io.NopCloser(strings.NewReader(logs)) }
}

func line(at time.Time, msg string) string {
	return at.Format(time.RFC3339Nano) + " " + msg + "\n"
}

func frame(stream byte, payload string) string {
	header := []byte{stream, 0, 0, 0, 0, 0, 0, 0}
	binary.BigEndian.PutUint32(header[4:], uint32(len(payload))) //nolint:gosec // test payloads are a few bytes

	return string(header) + payload
}

func matchAll(pattern, container string) rule {
	return rule{re: regexp.MustCompile(pattern), severity: entity.SeverityCritical, container: container}
}

func TestNewMatcher(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		rules   []config.Rule
		want    []entity.Severity
		wantErr string
	}{
		{
			name: "rules compile with their severities",
			rules: []config.Rule{
				{Pattern: "panic|FATAL", Severity: "critical", Container: "*"},
				{Pattern: "(?i)connection refused", Severity: "Error", Container: "db"},
			},
			want: []entity.Severity{entity.SeverityCritical, entity.SeverityError},
		},
		{name: "severity is case-insensitive", rules: []config.Rule{{Pattern: "x", Severity: "WARNING"}}, want: []entity.Severity{entity.SeverityWarning}},
		{name: "broken pattern names the rule", rules: []config.Rule{{Pattern: "([", Severity: "error"}}, wantErr: `rule "(["`},
		{name: "warn is not a severity, warning is", rules: []config.Rule{{Pattern: "x", Severity: "warn"}}, wantErr: "invalid severity: warn"},
		{name: "empty severity is rejected", rules: []config.Rule{{Pattern: "x"}}, wantErr: "invalid severity"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := &config.Config{Matcher: config.Matcher{Rules: tc.rules}}

			m, err := NewMatcher(cfg, nil, nil, nil, nil)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)

				return
			}

			require.NoError(t, err)

			got := make([]entity.Severity, 0, len(m.rules))
			for _, r := range m.rules {
				got = append(got, r.severity)
			}

			require.Equal(t, tc.want, got)
		})
	}
}

func TestFollow(t *testing.T) {
	t.Parallel()

	started := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	crash := started.Add(20 * time.Millisecond)
	later := started.Add(time.Minute)

	history := line(started.Add(-time.Second), "panic: previous run") +
		line(crash, "panic: boom at startup") +
		line(later, "panic: much later")

	cases := []struct {
		name      string
		rules     []rule
		tty       bool
		logs      string
		since     time.Time
		wantSince time.Time
		want      []string
		wantLast  time.Time
	}{
		{
			name: "start event reads from StartedAt", rules: []rule{matchAll("panic", "*")}, tty: true, logs: history,
			wantSince: started, want: []string{"panic: boom at startup", "panic: much later"}, wantLast: later,
		},
		{
			name: "reconnect resumes after the last line", rules: []rule{matchAll("panic", "*")}, tty: true, logs: history,
			since: crash, wantSince: crash, want: []string{"panic: much later"}, wantLast: later,
		},
		{
			name: "a rule for another container stays quiet", rules: []rule{matchAll("panic", "db")}, tty: true,
			logs: line(crash, "panic: boom"), since: started, wantSince: started, wantLast: crash,
		},
		{
			name: "every matching rule records the line", rules: []rule{matchAll("panic", "*"), matchAll("boom", "web")}, tty: true,
			logs: line(crash, "panic: boom"), since: started, wantSince: started,
			want: []string{"panic: boom", "panic: boom"}, wantLast: crash,
		},
		{
			name: "lines without a timestamp are skipped", rules: []rule{matchAll("panic", "*")}, tty: true,
			logs: "panic-without-a-space\nyesterday panic: boom\n", since: started, wantSince: started, wantLast: started,
		},
		{
			name: "stdout and stderr of a non-TTY container are demultiplexed", rules: []rule{matchAll("panic", "*")},
			logs:  frame(1, line(crash, "panic: on stdout")) + frame(2, line(later, "panic: on stderr")),
			since: started, wantSince: started, want: []string{"panic: on stdout", "panic: on stderr"}, wantLast: later,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			docker := &fakeDocker{tty: tc.tty, startedAt: started.Format(time.RFC3339Nano), logs: static(tc.logs)}
			uc := mocks.NewMockStorage(t)

			var got []string

			uc.On("SaveLogMatch", mock.Anything, mock.Anything).
				Run(func(args mock.Arguments) {
					lm, ok := args.Get(1).(*entity.LogMatch)
					require.True(t, ok)
					got = append(got, lm.Message)
				}).
				Return(&entity.LogMatch{}, nil).Maybe()
			uc.On("CreateAlert", mock.Anything, mock.Anything).Return(&entity.Alert{}, nil).Maybe()

			m := &Matcher{log: slog.New(slog.DiscardHandler), client: docker, uc: uc, rules: tc.rules}

			last, err := m.follow(t.Context(), "web", uuid.New(), tc.since)
			require.NoError(t, err)
			require.Equal(t, []string{tc.wantSince.Format(time.RFC3339Nano)}, docker.sinces("web"))
			require.Equal(t, tc.want, got)
			require.True(t, last.Equal(tc.wantLast), "last = %s, want %s", last, tc.wantLast)
		})
	}
}

func TestStartedAtFallsBackToNow(t *testing.T) {
	t.Parallel()

	before := time.Now()

	for _, state := range []*container.State{nil, {StartedAt: ""}, {StartedAt: "0001-01-01T00:00:00Z"}} {
		require.False(t, startedAt(container.InspectResponse{State: state}).Before(before))
	}
}

func TestOnMatch(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	timeout := rule{re: regexp.MustCompile("timeout"), severity: entity.SeverityWarning, container: "*"}

	cases := []struct {
		name     string
		saveErr  error
		alertErr error
	}{
		{name: "a saved match raises an alert without the line text"},
		{name: "a failed save raises no alert", saveErr: assert.AnError},
		{name: "a failed alert is only logged", alertErr: assert.AnError},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			id := uuid.New()
			uc := mocks.NewMockStorage(t)

			uc.On("SaveLogMatch", mock.Anything, mock.MatchedBy(func(lm *entity.LogMatch) bool {
				return lm.TargetID == id && lm.Severity == entity.SeverityWarning &&
					lm.Message == "upstream timeout after 30s" && lm.CreatedAt.Equal(at)
			})).Return(&entity.LogMatch{}, tc.saveErr).Once()

			if tc.saveErr == nil {
				uc.On("CreateAlert", mock.Anything, mock.MatchedBy(func(a *entity.Alert) bool {
					return *a.TargetID == id && a.Severity == entity.SeverityWarning && a.Details == `Log line matched rule "timeout"`
				})).Return(&entity.Alert{}, tc.alertErr).Once()
			}

			m := &Matcher{log: slog.New(slog.DiscardHandler), uc: uc}
			m.onMatch(t.Context(), id, at, "upstream timeout after 30s", timeout)
		})
	}
}

func TestGone(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		docker *fakeDocker
		want   bool
	}{
		{name: "removed container is gone", docker: &fakeDocker{inspectErr: fmt.Errorf("no such container: %w", cerrdefs.ErrNotFound)}, want: true},
		{name: "unreachable daemon is not proof it's gone", docker: &fakeDocker{inspectErr: assert.AnError}},
		{name: "running container stays", docker: &fakeDocker{}},
		{name: "exited container is gone", docker: &fakeDocker{stopped: func(int) bool { return true }}, want: true},
		{name: "container without state is gone", docker: &fakeDocker{noState: true}, want: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m := &Matcher{client: tc.docker}
			require.Equal(t, tc.want, m.gone(t.Context(), "web"))
		})
	}
}

func TestStartMatching(t *testing.T) {
	t.Parallel()

	t.Run("reconnects after the last line until the container stops", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			start := time.Now()
			batches := []string{
				line(start.Add(time.Millisecond), "one") + line(start.Add(2*time.Millisecond), "two"),
				line(start.Add(3*time.Millisecond), "three"),
			}

			docker := &fakeDocker{
				tty:     true,
				stopped: func(calls int) bool { return calls >= len(batches) },
				logs: func(_ context.Context, call int) io.ReadCloser {
					return io.NopCloser(strings.NewReader(batches[call-1]))
				},
			}
			m := &Matcher{log: slog.New(slog.DiscardHandler), client: docker}

			m.startMatching(t.Context(), "web", uuid.New(), start)

			require.Equal(t, []string{
				start.Format(time.RFC3339Nano),
				start.Add(2 * time.Millisecond).Format(time.RFC3339Nano),
			}, docker.sinces("web"))
			require.Equal(t, time.Second, time.Since(start), "one minimum backoff between the two streams")
		})
	})

	t.Run("cancel during backoff stops it", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			docker := &fakeDocker{tty: true, logs: static("")}
			m := &Matcher{log: slog.New(slog.DiscardHandler), client: docker}

			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan struct{})

			go func() {
				m.startMatching(ctx, "web", uuid.New(), time.Now())
				close(done)
			}()

			synctest.Wait()
			cancel()
			<-done

			require.Len(t, docker.sinces("web"), 1)
		})
	})
}

func TestRunMatcher(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		bootedAt := start.Add(-time.Minute).Format(time.RFC3339Nano)

		web := &entity.Target{ID: uuid.New(), Source: "web"}
		db := &entity.Target{ID: uuid.New(), Source: "db"}

		uc := mocks.NewMockStorage(t)
		uc.On("GetTargetsByType", mock.Anything, "container").Return([]*entity.Target{web}, nil).Once()
		uc.On("GetTargetsByType", mock.Anything, "container").Return([]*entity.Target{web, db}, nil)

		docker := &fakeDocker{
			tty:       true,
			startedAt: bootedAt,
			logs: func(ctx context.Context, _ int) io.ReadCloser {
				pr, pw := io.Pipe()

				go func() {
					<-ctx.Done()
					_ = pw.Close()
				}()

				return pr
			},
		}

		starts := make(chan string)

		m, err := NewMatcher(&config.Config{}, slog.New(slog.DiscardHandler), uc, docker, starts)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})

		go func() {
			m.RunMatcher(ctx)
			close(done)
		}()

		synctest.Wait()
		require.Equal(t, []string{start.Format(time.RFC3339Nano)}, docker.sinces("web"), "known containers are followed from now, old logs aren't rescanned")

		starts <- "db"
		synctest.Wait()
		require.Equal(t, []string{bootedAt}, docker.sinces("db"), "a started container is read from its StartedAt")

		starts <- "web"
		synctest.Wait()
		require.Len(t, docker.sinces("web"), 1, "a container already followed gets no second stream")

		starts <- "ghost"
		synctest.Wait()
		require.Empty(t, docker.sinces("ghost"))

		cancel()
		<-done
	})
}
