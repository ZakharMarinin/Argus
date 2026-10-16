package logger

import (
	"argus-collector/internal/domain/entity"
	"context"
	"errors"
	"log/slog"
	"os"
)

func SetupLogger(env string) *slog.Logger {
	var h slog.Handler

	switch env {
	case entity.EnvLocal, entity.EnvDev:
		h = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})
	case entity.EnvProd:
		h = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	default:
		return nil
	}

	return slog.New(skipCanceled{h})
}

// skipCanceled drops records that carry a context.Canceled error:
// on shutdown every background loop gets one, and it is not a failure.
type skipCanceled struct {
	slog.Handler
}

func (h skipCanceled) Handle(ctx context.Context, r slog.Record) error {
	if r.Level < slog.LevelError {
		return h.Handler.Handle(ctx, r)
	}

	canceled := false

	r.Attrs(func(a slog.Attr) bool {
		err, ok := a.Value.Any().(error)
		canceled = ok && errors.Is(err, context.Canceled)

		return !canceled
	})

	if canceled {
		return nil
	}

	return h.Handler.Handle(ctx, r)
}

func (h skipCanceled) WithAttrs(attrs []slog.Attr) slog.Handler {
	return skipCanceled{h.Handler.WithAttrs(attrs)}
}

func (h skipCanceled) WithGroup(name string) slog.Handler {
	return skipCanceled{h.Handler.WithGroup(name)}
}
