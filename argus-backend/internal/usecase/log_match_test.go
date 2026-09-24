package usecase_test

import (
	"context"
	"testing"
	"time"

	"argus-collector/internal/domain/entity"
	"argus-collector/internal/usecase"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	mocks "argus-collector/internal/usecase/mocks"
)

func TestUseCase_SaveLogMatch(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		severity entity.Severity
		storeErr error
		wantErr  error
	}{
		{name: "valid severity is stored", severity: entity.SeverityError},
		{name: "storage error is wrapped", severity: entity.SeverityCritical, storeErr: assert.AnError, wantErr: assert.AnError},
		{name: "severity past debug is rejected", severity: entity.SeverityDebug + 1, wantErr: entity.ErrInvalidSeverity},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			db := mocks.NewMockStorage(t)
			logMatch := &entity.LogMatch{Severity: tc.severity, Message: "panic: boom"}

			if tc.wantErr != entity.ErrInvalidSeverity {
				db.On("SaveLogMatch", mock.Anything, logMatch).Return(logMatch, tc.storeErr).Once()
			}

			got, err := usecase.NewUseCase(db).SaveLogMatch(t.Context(), logMatch)

			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				require.ErrorContains(t, err, "UseCase.SaveLogMatch")

				return
			}

			require.NoError(t, err)
			require.Same(t, logMatch, got)
		})
	}
}

func TestUseCase_LogMatchDelegation(t *testing.T) {
	t.Parallel()

	before := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)

	assertDelegates(t, []delegation{
		{
			method: "DeleteLogMatch", args: []any{7}, want: 7, zero: 0,
			call: func(ctx context.Context, uc *usecase.UseCase) (any, error) { return uc.DeleteLogMatch(ctx, 7) },
		},
		{
			method: "DeleteLogMatchesOlderThan", args: []any{before},
			call: func(ctx context.Context, uc *usecase.UseCase) (any, error) {
				return nil, uc.DeleteLogMatchesOlderThan(ctx, before)
			},
		},
		{
			method: "GetLogMatch", args: []any{7}, want: &entity.LogMatch{ID: 7}, zero: (*entity.LogMatch)(nil),
			call: func(ctx context.Context, uc *usecase.UseCase) (any, error) { return uc.GetLogMatch(ctx, 7) },
		},
	})
}
