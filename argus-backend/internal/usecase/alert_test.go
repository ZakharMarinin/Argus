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

func TestUseCase_CreateAlert(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		severity entity.Severity
		storeErr error
		wantErr  error

		wantValidation bool
	}{
		{name: "valid severity is stored", severity: entity.SeverityWarning},
		{name: "storage error is wrapped", severity: entity.SeverityCritical, storeErr: assert.AnError, wantErr: assert.AnError},
		{name: "severity past debug is rejected", severity: entity.SeverityDebug + 1, wantValidation: true},
		{name: "negative severity is rejected", severity: -1, wantValidation: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			db := mocks.NewMockStorage(t)
			alert := &entity.Alert{Severity: tc.severity, Details: "disk full", CreatedAt: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)}

			if !tc.wantValidation {
				db.On("CreateAlert", mock.Anything, alert).Return(alert, tc.storeErr).Once()
			}

			before := time.Now()
			got, err := usecase.NewUseCase(db).CreateAlert(t.Context(), alert)

			if tc.wantValidation {
				var verr *entity.ValidationError
				require.ErrorAs(t, err, &verr)
				require.Equal(t, "invalid severity", verr.Reason)

				return
			}

			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				require.ErrorContains(t, err, "UseCase.CreateAlert")

				return
			}

			require.NoError(t, err)
			require.Same(t, alert, got)
			require.False(t, alert.CreatedAt.Before(before), "the caller's CreatedAt is replaced with the creation time")
		})
	}
}

func TestUseCase_AlertDelegation(t *testing.T) {
	t.Parallel()

	assertDelegates(t, []delegation{
		{
			method: "DeleteAlert", args: []any{7}, want: 7, zero: 0,
			call: func(ctx context.Context, uc *usecase.UseCase) (any, error) { return uc.DeleteAlert(ctx, 7) },
		},
		{
			method: "GetAlert", args: []any{7}, want: &entity.Alert{ID: 7}, zero: (*entity.Alert)(nil),
			call: func(ctx context.Context, uc *usecase.UseCase) (any, error) { return uc.GetAlert(ctx, 7) },
		},
		{
			method: "GetAlertsByNotification", args: []any{entity.AlertStatusNotSent, uint64(50)}, want: []*entity.Alert{{ID: 7}}, zero: []*entity.Alert(nil),
			call: func(ctx context.Context, uc *usecase.UseCase) (any, error) {
				return uc.GetAlertsByNotification(ctx, entity.AlertStatusNotSent, 50)
			},
		},
	})
}

func TestUseCase_MarkAlertsNotified(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		alerts   []*entity.Alert
		status   entity.AlertStatus
		wantIDs  []int
		storeErr error
	}{
		{name: "passes ids in order", alerts: []*entity.Alert{{ID: 3}, {ID: 1}, {ID: 2}}, status: entity.AlertStatus("sent"), wantIDs: []int{3, 1, 2}},
		{name: "no alerts", status: entity.AlertStatus("sent"), alerts: nil, wantIDs: []int{}},
		{name: "storage error is returned as is", alerts: []*entity.Alert{{ID: 1}}, wantIDs: []int{1}, status: entity.AlertStatus("sent"), storeErr: assert.AnError},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			db := mocks.NewMockStorage(t)
			db.On("MarkAlertsNotified", mock.Anything, tc.wantIDs, tc.status).Return(tc.storeErr).Once()

			err := usecase.NewUseCase(db).MarkAlertsNotified(t.Context(), tc.alerts, tc.status)

			if tc.storeErr != nil {
				require.Equal(t, tc.storeErr, err)

				return
			}

			require.NoError(t, err)
		})
	}
}

func TestUseCase_FindPassesFilterThrough(t *testing.T) {
	t.Parallel()

	severity := entity.SeverityError
	f := entity.ListFilter{Severity: &severity, Limit: 50, Skip: 50, Desc: true}

	mockRepo := mocks.NewMockStorage(t)
	mockRepo.On("FindAlerts", mock.Anything, f).Return([]*entity.Alert{{ID: 3}}, 120, nil).Once()
	mockRepo.On("FindLogMatches", mock.Anything, f).Return(nil, 0, assert.AnError).Once()

	uc := usecase.NewUseCase(mockRepo)

	alerts, total, err := uc.FindAlerts(context.Background(), f)
	require.NoError(t, err)
	assert.Equal(t, 120, total)
	assert.Len(t, alerts, 1)

	_, _, err = uc.FindLogMatches(context.Background(), f)
	require.ErrorIs(t, err, assert.AnError)
}
