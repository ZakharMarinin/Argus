package matcher

import (
	"context"
	"io"
	"log/slog"
	"regexp"
	"strings"
	"testing"
	"time"

	"argus-collector/internal/domain/entity"

	"github.com/google/uuid"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	mocks "argus-collector/internal/usecase/mocks"
)

type fakeDocker struct {
	startedAt string
	logs      string
	since     string
}

func (f *fakeDocker) ContainerInspect(context.Context, string, client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	return client.ContainerInspectResult{Container: container.InspectResponse{
		Config: &container.Config{Tty: true},
		State:  &container.State{StartedAt: f.startedAt},
	}}, nil
}

func (f *fakeDocker) ContainerLogs(_ context.Context, _ string, opts client.ContainerLogsOptions) (client.ContainerLogsResult, error) {
	f.since = opts.Since

	return io.NopCloser(strings.NewReader(f.logs)), nil
}

func TestFollowStartsFrom(t *testing.T) {
	started := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	crash := started.Add(20 * time.Millisecond)
	later := started.Add(time.Minute)

	logs := strings.Join([]string{
		started.Add(-time.Second).Format(time.RFC3339Nano) + " panic: previous run",
		crash.Format(time.RFC3339Nano) + " panic: boom at startup",
		later.Format(time.RFC3339Nano) + " panic: much later",
	}, "\n") + "\n"

	cases := []struct {
		name      string
		since     time.Time
		wantSince time.Time
		want      []string
	}{
		{"start event reads from StartedAt", time.Time{}, started, []string{"panic: boom at startup", "panic: much later"}},
		{"reconnect resumes from the last line", crash, crash, []string{"panic: much later"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			docker := &fakeDocker{startedAt: started.Format(time.RFC3339Nano), logs: logs}
			uc := mocks.NewMockStorage(t)

			var got []string

			uc.On("SaveLogMatch", mock.Anything, mock.Anything).
				Run(func(args mock.Arguments) {
					lm, ok := args.Get(1).(*entity.LogMatch)
					require.True(t, ok)
					got = append(got, lm.Message)
				}).
				Return(&entity.LogMatch{}, nil)
			uc.On("CreateAlert", mock.Anything, mock.Anything).Return(&entity.Alert{}, nil)

			m := &Matcher{
				log:    slog.New(slog.DiscardHandler),
				client: docker,
				uc:     uc,
				rules:  []rule{{re: regexp.MustCompile("panic"), severity: entity.SeverityCritical, container: "*"}},
			}

			last, err := m.follow(t.Context(), "web", uuid.New(), tc.since)
			require.NoError(t, err)
			require.Equal(t, tc.wantSince.Format(time.RFC3339Nano), docker.since)
			require.Equal(t, tc.want, got)
			require.True(t, last.Equal(later))
		})
	}
}

func TestStartedAtFallsBackToNow(t *testing.T) {
	before := time.Now()

	for _, state := range []*container.State{nil, {StartedAt: ""}, {StartedAt: "0001-01-01T00:00:00Z"}} {
		require.False(t, startedAt(container.InspectResponse{State: state}).Before(before))
	}
}
