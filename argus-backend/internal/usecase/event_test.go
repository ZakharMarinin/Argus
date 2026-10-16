package usecase_test

import (
	"context"
	"testing"
	"time"

	"argus-collector/internal/domain/entity"
	"argus-collector/internal/usecase"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	mocks "argus-collector/internal/usecase/mocks"
)

func TestUseCase_CreateEvent(t *testing.T) {
	t.Parallel()

	dockerTime := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name      string
		event     entity.EventType
		createdAt time.Time
		storeErr  error
		wantErr   error
	}{
		{name: "keeps the time Docker reported", event: entity.ContainerDie, createdAt: dockerTime},
		{name: "stamps now when unset", event: entity.ContainerStart},
		{name: "storage error is wrapped", event: entity.ContainerOOM, createdAt: dockerTime, storeErr: entity.ErrAlreadyExists, wantErr: entity.ErrAlreadyExists},
		{name: "unsubscribed event type is rejected", event: "stop", wantErr: entity.ErrInvalidEventType},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			db := mocks.NewMockStorage(t)
			event := &entity.ContainerEvent{Event: tc.event, CreatedAt: tc.createdAt}

			if tc.wantErr != entity.ErrInvalidEventType {
				db.On("CreateEvent", mock.Anything, event).Return(event, tc.storeErr).Once()
			}

			before := time.Now()
			got, err := usecase.NewUseCase(db).CreateEvent(t.Context(), event)

			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				require.ErrorContains(t, err, "UseCase.CreateEvent")

				return
			}

			require.NoError(t, err)
			require.Same(t, event, got)

			if tc.createdAt.IsZero() {
				require.False(t, event.CreatedAt.Before(before))
			} else {
				require.True(t, event.CreatedAt.Equal(tc.createdAt))
			}
		})
	}
}

func TestUseCase_EventDelegation(t *testing.T) {
	t.Parallel()

	targetID := uuid.New()
	before := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)

	assertDelegates(t, []delegation{
		{
			method: "DeleteEvent", args: []any{7}, want: 7, zero: 0,
			call: func(ctx context.Context, uc *usecase.UseCase) (any, error) { return uc.DeleteEvent(ctx, 7) },
		},
		{
			method: "DeleteEventsOlderThan", args: []any{before},
			call: func(ctx context.Context, uc *usecase.UseCase) (any, error) {
				return nil, uc.DeleteEventsOlderThan(ctx, before)
			},
		},
		{
			method: "GetEvent", args: []any{7}, want: &entity.ContainerEvent{ID: 7}, zero: (*entity.ContainerEvent)(nil),
			call: func(ctx context.Context, uc *usecase.UseCase) (any, error) { return uc.GetEvent(ctx, 7) },
		},
		{
			method: "GetEventsByTarget", args: []any{targetID}, want: []*entity.ContainerEvent{{ID: 7}}, zero: []*entity.ContainerEvent(nil),
			call: func(ctx context.Context, uc *usecase.UseCase) (any, error) {
				return uc.GetEventsByTarget(ctx, targetID)
			},
		},
		{
			method: "GetEventsByType", args: []any{entity.ContainerDie}, want: []*entity.ContainerEvent{{ID: 7}}, zero: []*entity.ContainerEvent(nil),
			call: func(ctx context.Context, uc *usecase.UseCase) (any, error) {
				return uc.GetEventsByType(ctx, entity.ContainerDie)
			},
		},
		{
			method: "GetAllEvents", args: []any{uint64(50), 10}, want: []*entity.ContainerEvent{{ID: 7}}, zero: []*entity.ContainerEvent(nil),
			call: func(ctx context.Context, uc *usecase.UseCase) (any, error) { return uc.GetAllEvents(ctx, 50, 10) },
		},
	})
}
