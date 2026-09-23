package usecase_test

import (
	"context"
	"testing"

	"argus-collector/internal/domain/entity"
	"argus-collector/internal/usecase"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	mocks "argus-collector/internal/usecase/mocks"
)

func TestUseCase_MarkAlertsNotified(t *testing.T) {
	cases := []struct {
		name    string
		setup   func(*mocks.MockStorage)
		id      []*entity.Alert
		wantErr bool
	}{
		{name: "ok", setup: func(m *mocks.MockStorage) {
			m.On("MarkAlertsNotified", mock.Anything, []int{1}).Return(nil).Once()
		}, id: []*entity.Alert{{ID: 1}}, wantErr: false},
		{name: "err", setup: func(m *mocks.MockStorage) {
			m.On("MarkAlertsNotified", mock.Anything, []int{2}).Return(assert.AnError).Once()
		}, id: []*entity.Alert{{ID: 2}}, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mockRepo := mocks.NewMockStorage(t)
			tc.setup(mockRepo)
			uc := usecase.NewUseCase(mockRepo)
			err := uc.MarkAlertsNotified(context.Background(), tc.id)
			if tc.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), "MarkAlertsNotified")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestUseCase_FindPassesFilterThrough(t *testing.T) {
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
