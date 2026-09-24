package storage

import (
	"argus-collector/internal/config"
	"argus-collector/internal/domain/entity"
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/google/uuid"
)

func TestLogMatchKeepsLineVerbatim(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_URL is not set, run `make test-db`")
	}

	ctx := t.Context()

	err := Migrate(ctx, dsn, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}

	s, err := NewStorage(ctx, &config.Config{Postgres: dsn})
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	t.Cleanup(s.Close)

	target, err := s.CreateTarget(ctx, &entity.Target{
		ID:         uuid.New(),
		Source:     "log-match-test-" + uuid.NewString(),
		Name:       "log-match-test",
		TargetType: entity.TargetTypeContainer,
		Status:     "unknown",
	})
	if err != nil {
		t.Fatalf("create target: %v", err)
	}
	t.Cleanup(func() { _ = s.DeleteTarget(context.Background(), target.ID) })

	lines := []string{
		"panic: runtime error: invalid memory address",
		`{"level":"ERROR","msg":"db down"}`,
		`FATAL: "quoted" \ backslash, кириллица`,
	}

	for _, line := range lines {
		saved, err := s.SaveLogMatch(ctx, &entity.LogMatch{TargetID: target.ID, Severity: entity.SeverityError, Message: line})
		if err != nil {
			t.Fatalf("save %q: %v", line, err)
		}

		got, err := s.GetLogMatch(ctx, saved.ID)
		if err != nil {
			t.Fatalf("get %q: %v", line, err)
		}

		if got.Message != line {
			t.Errorf("message = %q, want %q", got.Message, line)
		}
	}
}
