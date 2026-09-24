package prober

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"argus-collector/internal/config"
	"argus-collector/internal/domain/entity"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	mocks "argus-collector/internal/usecase/mocks"
)

func respond(code int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) }
}

func TestCheckProbe(t *testing.T) {
	t.Parallel()

	hang := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() })

	cases := []struct {
		name    string
		handler http.Handler
		success bool
		reason  string
	}{
		{name: "200 is up", handler: respond(http.StatusOK), success: true},
		{name: "299 is up", handler: respond(299), success: true},
		{name: "300 is down", handler: respond(http.StatusMultipleChoices), reason: "Health check returned HTTP 300"},
		{name: "500 is down", handler: respond(http.StatusInternalServerError), reason: "Health check returned HTTP 500"},
		{name: "slow service times out", handler: hang, reason: "Health check timed out"},
		{
			name:   "closed port is refused, with a hint about loopback",
			reason: "Service is unreachable: connection refused. Inside Argus, 127.0.0.1 is the Argus container itself",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			handler := tc.handler
			if handler == nil {
				handler = respond(http.StatusOK)
			}

			srv := httptest.NewServer(handler)
			if tc.handler == nil {
				srv.Close()
			} else {
				t.Cleanup(srv.Close)
			}

			id := uuid.New()
			p := NewProber(&config.Config{Prober: config.Prober{ProbeTimeout: 50 * time.Millisecond}}, nil, nil)

			probe, reason, err := p.checkProbe(t.Context(), &entity.Target{ID: id, Source: srv.URL + "/health"})
			require.NoError(t, err)
			require.Equal(t, id, probe.TargetID)
			require.Equal(t, tc.success, probe.Success)

			if tc.success {
				require.Empty(t, reason)
				require.Positive(t, probe.Latency)

				return
			}

			require.True(t, strings.HasPrefix(reason, tc.reason), "reason: %q", reason)
		})
	}
}

func TestTransition(t *testing.T) {
	cfg := &config.Config{Prober: config.Prober{FailureThreshold: 2}}
	p := NewProber(cfg, nil, nil)
	target := &entity.Target{ID: uuid.New(), Status: statusUp}

	steps := []struct {
		name       string
		status     string
		success    bool
		wantStatus string
		wantAlert  entity.Severity
		alert      bool
	}{
		{name: "first failure is tolerated", status: statusUp},
		{name: "second failure goes down", status: statusUp, wantStatus: statusDown, alert: true, wantAlert: entity.SeverityError},
		{name: "already down stays quiet", status: statusDown},
		{name: "success recovers", status: statusDown, success: true, wantStatus: statusUp, alert: true, wantAlert: entity.SeverityInfo},
		{name: "single failure after recovery", status: statusUp},
		{name: "success resets the counter", status: statusUp, success: true},
		{name: "failure after reset is tolerated again", status: statusUp},
		{name: "unknown becomes up without alert", status: "unknown", success: true, wantStatus: statusUp},
	}

	for _, s := range steps {
		target.Status = s.status
		result := probeResult{target: target, probe: &entity.Probe{Success: s.success}, reason: "Health check timed out"}

		status, alert := p.transition(result)

		require.Equal(t, s.wantStatus, status, s.name)

		if !s.alert {
			require.Nil(t, alert, s.name)

			continue
		}

		require.NotNil(t, alert, s.name)
		require.Equal(t, s.wantAlert, alert.Severity, s.name)
	}
}

func TestProbeFailureReason(t *testing.T) {
	wrap := func(err error) error {
		return &url.Error{Op: "Get", URL: "http://api.local/health", Err: err}
	}

	cases := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "timeout",
			err:  wrap(context.DeadlineExceeded),
			want: "Health check timed out",
		},
		{
			name: "dns failure",
			err:  wrap(&net.DNSError{Name: "api.local", IsNotFound: true}),
			want: "Host api.local could not be resolved",
		},
		{
			name: "connection refused",
			err:  wrap(&net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}),
			want: "Service is unreachable: connection refused",
		},
		{
			name: "bad certificate",
			err:  wrap(&tls.CertificateVerificationError{}),
			want: "TLS certificate verification failed",
		},
		{
			name: "unknown error keeps the original text",
			err:  errors.New("something odd"),
			want: "Health check request failed: something odd",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, probeFailureReason(tc.err))
		})
	}
}

func TestSelfAddressHint(t *testing.T) {
	cases := []struct {
		source string
		hinted bool
	}{
		{"http://0.0.0.0:8093/health", true},
		{"http://localhost:8093/health", true},
		{"http://127.0.0.1:8093/health", true},
		{"http://[::1]:8093/health", true},
		{"http://host.docker.internal:8093/health", false},
		{"http://gateway:8093/health", false},
		{"https://example.com/health", false},
		{"http://192.168.0.10/health", false},
	}

	for _, tc := range cases {
		t.Run(tc.source, func(t *testing.T) {
			require.Equal(t, tc.hinted, selfAddressHint(tc.source) != "")
		})
	}
}

func TestMakeProbes(t *testing.T) {
	t.Parallel()

	healthy := httptest.NewServer(respond(http.StatusOK))
	t.Cleanup(healthy.Close)

	target := func(status, source string) *entity.Target {
		return &entity.Target{ID: uuid.New(), Name: "api", Source: source, Status: status, TargetType: probeTargetType}
	}

	savedOne := func(id uuid.UUID) any {
		return mock.MatchedBy(func(probes []*entity.Probe) bool {
			return len(probes) == 1 && probes[0].TargetID == id && probes[0].Success
		})
	}

	recovered := func(id uuid.UUID) any {
		return mock.MatchedBy(func(a *entity.Alert) bool {
			return *a.TargetID == id && a.Severity == entity.SeverityInfo && a.Details == "Service is back up"
		})
	}

	upTarget := target(statusUp, healthy.URL)
	unknownTarget := target("unknown", healthy.URL)
	downTarget := target(statusDown, healthy.URL)
	alertFails := target(statusDown, healthy.URL)
	saveFails := target("unknown", healthy.URL)
	unbuildable := target("unknown", "http://%zz")
	buildable := target("unknown", healthy.URL)

	cases := []struct {
		name    string
		targets []*entity.Target
		listErr error
		expect  func(m *mocks.MockStorage)
		wantErr bool
	}{
		{name: "no targets, nothing saved", expect: func(*mocks.MockStorage) {}},
		{name: "listing fails", listErr: assert.AnError, expect: func(*mocks.MockStorage) {}, wantErr: true},
		{
			name: "healthy up target is saved and keeps its status", targets: []*entity.Target{upTarget},
			expect: func(m *mocks.MockStorage) {
				m.On("SaveProbes", mock.Anything, savedOne(upTarget.ID)).Return(nil, nil).Once()
			},
		},
		{
			name: "unknown target becomes up without an alert", targets: []*entity.Target{unknownTarget},
			expect: func(m *mocks.MockStorage) {
				m.On("SaveProbes", mock.Anything, savedOne(unknownTarget.ID)).Return(nil, nil).Once()
				m.On("UpdateTargetStatus", mock.Anything, unknownTarget.ID, statusUp).Return(unknownTarget, nil).Once()
			},
		},
		{
			name: "down target recovers with an alert", targets: []*entity.Target{downTarget},
			expect: func(m *mocks.MockStorage) {
				m.On("SaveProbes", mock.Anything, savedOne(downTarget.ID)).Return(nil, nil).Once()
				m.On("CreateAlert", mock.Anything, recovered(downTarget.ID)).Return(&entity.Alert{}, nil).Once()
				m.On("UpdateTargetStatus", mock.Anything, downTarget.ID, statusUp).Return(downTarget, nil).Once()
			},
		},
		{
			name: "failed alert leaves the status for the next tick", targets: []*entity.Target{alertFails},
			expect: func(m *mocks.MockStorage) {
				m.On("SaveProbes", mock.Anything, savedOne(alertFails.ID)).Return(nil, nil).Once()
				m.On("CreateAlert", mock.Anything, recovered(alertFails.ID)).Return(nil, assert.AnError).Once()
			},
		},
		{
			name: "failed save skips transitions", targets: []*entity.Target{saveFails},
			expect: func(m *mocks.MockStorage) {
				m.On("SaveProbes", mock.Anything, savedOne(saveFails.ID)).Return(nil, assert.AnError).Once()
			},
			wantErr: true,
		},
		{
			name: "unbuildable url is skipped, the rest is saved", targets: []*entity.Target{unbuildable, buildable},
			expect: func(m *mocks.MockStorage) {
				m.On("SaveProbes", mock.Anything, savedOne(buildable.ID)).Return(nil, nil).Once()
				m.On("UpdateTargetStatus", mock.Anything, buildable.ID, statusUp).Return(buildable, nil).Once()
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m := mocks.NewMockStorage(t)
			m.On("GetTargetsByType", mock.Anything, probeTargetType).Return(tc.targets, tc.listErr).Once()
			tc.expect(m)

			cfg := &config.Config{Prober: config.Prober{ProbeTimeout: time.Second, FailureThreshold: 2}}
			err := NewProber(cfg, slog.New(slog.DiscardHandler), m).MakeProbes(t.Context())

			if tc.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
		})
	}
}

func TestMakeProbesGoesDownAfterThreshold(t *testing.T) {
	t.Parallel()

	failing := httptest.NewServer(respond(http.StatusBadGateway))
	t.Cleanup(failing.Close)

	target := &entity.Target{ID: uuid.New(), Name: "api", Source: failing.URL, Status: statusUp, TargetType: probeTargetType}

	m := mocks.NewMockStorage(t)
	m.On("GetTargetsByType", mock.Anything, probeTargetType).Return([]*entity.Target{target}, nil).Twice()
	m.On("SaveProbes", mock.Anything, mock.Anything).Return(nil, nil).Twice()

	p := NewProber(&config.Config{Prober: config.Prober{ProbeTimeout: time.Second, FailureThreshold: 2}}, slog.New(slog.DiscardHandler), m)

	require.NoError(t, p.MakeProbes(t.Context()), "first failure is tolerated: no alert, no status change")

	m.On("CreateAlert", mock.Anything, mock.MatchedBy(func(a *entity.Alert) bool {
		return *a.TargetID == target.ID && a.Severity == entity.SeverityError && a.Details == "Health check returned HTTP 502"
	})).Return(&entity.Alert{}, nil).Once()
	m.On("UpdateTargetStatus", mock.Anything, target.ID, statusDown).Return(target, nil).Once()

	require.NoError(t, p.MakeProbes(t.Context()))
}

func TestRunProber(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		const interval = time.Minute

		m := mocks.NewMockStorage(t)
		m.On("GetTargetsByType", mock.Anything, probeTargetType).Return(nil, nil)

		p := NewProber(&config.Config{Prober: config.Prober{ProbeInterval: interval}}, slog.New(slog.DiscardHandler), m)

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})

		go func() {
			p.RunProber(ctx)
			close(done)
		}()

		synctest.Sleep(3*interval + time.Second)
		m.AssertNumberOfCalls(t, "GetTargetsByType", 3)

		cancel()
		<-done
	})
}
