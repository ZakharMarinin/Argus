package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"argus-frontend/internal/domain/entity"
	"argus-frontend/internal/gateway/backend"
	"argus-frontend/internal/usecase"
)

func TestCreateProbe(t *testing.T) {
	uc := newUseCase(t, fakeBackend{})

	cases := []struct {
		name, source string
		wantReason   string
	}{
		{"api", "https://api.local/health", ""},
		{"", "https://api.local/health", "name is required"},
		{"api", "https://dup.local", "This URL is already monitored"},
	}

	for _, c := range cases {
		err := uc.CreateProbe(context.Background(), c.name, c.source)

		var invalid *entity.ValidationError
		switch {
		case c.wantReason == "" && err != nil:
			t.Errorf("CreateProbe(%q, %q) = %v", c.name, c.source, err)
		case c.wantReason != "" && (!errors.As(err, &invalid) || invalid.Reason != c.wantReason):
			t.Errorf("CreateProbe(%q, %q) = %v, want reason %q", c.name, c.source, err, c.wantReason)
		}
	}
}

func TestDeleteTargetIsIdempotent(t *testing.T) {
	uc := newUseCase(t, fakeBackend{targets: []targetJSON{{ID: "t1", Name: "api", TargetType: "probe"}}})

	for _, id := range []string{"t1", "gone"} {
		err := uc.DeleteTarget(context.Background(), id)
		if err != nil {
			t.Errorf("DeleteTarget(%q) = %v", id, err)
		}
	}
}

func TestWritesReportCollectorDown(t *testing.T) {
	uc := usecase.NewUseCase(backend.NewClient("http://127.0.0.1:1", time.Second))

	err := uc.CreateProbe(context.Background(), "api", "https://api.local")
	if !errors.Is(err, entity.ErrNotReady) {
		t.Errorf("CreateProbe = %v, want ErrNotReady", err)
	}

	err = uc.DeleteTarget(context.Background(), "t1")
	if !errors.Is(err, entity.ErrNotReady) {
		t.Errorf("DeleteTarget = %v, want ErrNotReady", err)
	}
}
