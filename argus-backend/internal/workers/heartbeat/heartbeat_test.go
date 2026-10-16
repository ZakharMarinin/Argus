package heartbeat

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"argus-collector/internal/config"

	"github.com/stretchr/testify/assert"
)

type pingFunc func(ctx context.Context) error

func (f pingFunc) Ping(ctx context.Context) error {
	return f(ctx)
}

func TestBeat(t *testing.T) {
	tests := []struct {
		name  string
		ping  error
		beats int32
	}{
		{name: "database up", ping: nil, beats: 1},
		{name: "database down", ping: assert.AnError, beats: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var beats atomic.Int32

			srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				beats.Add(1)
			}))
			defer srv.Close()

			cfg := config.Heartbeat{URL: srv.URL, Interval: time.Minute}
			db := pingFunc(func(context.Context) error { return tt.ping })

			NewHeartbeat(cfg, slog.New(slog.DiscardHandler), db).Beat(context.Background())

			assert.Equal(t, tt.beats, beats.Load())
		})
	}
}
