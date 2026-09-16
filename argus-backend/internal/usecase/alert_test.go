package usecase_test

import (
	"context"
	"testing"

	"argus-collector/internal/usecase"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	mocks "argus-collector/internal/usecase/mocks"
)

func TestUseCase_MarkAlertNotified(t *testing.T) {
	cases := []struct {
		name    string
		setup   func(*mocks.MockStorage)
		id      int
		wantErr bool
	}{
		{name: "ok", setup: func(m *mocks.MockStorage) {
			m.On("MarkAlertNotified", mock.Anything, 1).Return(nil).Once()
		}, id: 1, wantErr: false},
		{name: "err", setup: func(m *mocks.MockStorage) {
			m.On("MarkAlertNotified", mock.Anything, 2).Return(assert.AnError).Once()
		}, id: 2, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mockRepo := mocks.NewMockStorage(t)
			tc.setup(mockRepo)
			uc := usecase.NewUseCase(mockRepo)
			err := uc.MarkAlertNotified(context.Background(), tc.id)
			if tc.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), "MarkAlertNotified")
			} else {
				require.NoError(t, err)
			}
		})
	}
}
