package prober

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/url"
	"syscall"
	"testing"

	"argus-collector/internal/config"
	"argus-collector/internal/domain/entity"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

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
