package prober

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/url"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

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
