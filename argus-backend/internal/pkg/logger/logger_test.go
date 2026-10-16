package logger

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSkipCanceled(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	log := slog.New(skipCanceled{slog.NewJSONHandler(&buf, nil)}).With("op", "test")

	log.Error("shutdown", "error", fmt.Errorf("wrapped: %w", context.Canceled))
	require.Empty(t, buf.String(), "canceled context is not logged")

	log.Error("real failure", "error", fmt.Errorf("db down"))
	require.Contains(t, buf.String(), "real failure")
	require.Contains(t, buf.String(), `"op":"test"`, "attrs added by With survive the wrapper")
}
