package retention

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"argus-collector/internal/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	mocks "argus-collector/internal/usecase/mocks"
)

func TestClean(t *testing.T) {
	keep := 14 * 24 * time.Hour
	want := time.Now().Add(-keep)

	aroundCutoff := mock.MatchedBy(func(before time.Time) bool {
		return before.Sub(want).Abs() < time.Second
	})

	uc := mocks.NewMockStorage(t)
	uc.On("DeleteProbesOlderThan", mock.Anything, aroundCutoff).Return(assert.AnError).Once()
	uc.On("DeleteEventsOlderThan", mock.Anything, aroundCutoff).Return(nil).Once()
	uc.On("DeleteLogMatchesOlderThan", mock.Anything, aroundCutoff).Return(nil).Once()

	cfg := &config.Config{Retention: config.Retention{Keep: keep, Interval: time.Hour}}

	NewCleaner(cfg, slog.New(slog.DiscardHandler), uc).Clean(context.Background())
}
