package usecase_test

import (
	"context"
	"testing"
	"time"

	"argus-collector/internal/domain/entity"
	"argus-collector/internal/usecase"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	mocks "argus-collector/internal/usecase/mocks"
)

func TestUseCase_CreateEventKeepsDockerTime(t *testing.T) {
	dockerTime := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)

	repo := mocks.NewMockStorage(t)
	repo.On("CreateEvent", mock.Anything, mock.Anything).Return(&entity.ContainerEvent{}, nil).Once()

	event := &entity.ContainerEvent{Event: entity.ContainerDie, CreatedAt: dockerTime}

	_, err := usecase.NewUseCase(repo).CreateEvent(context.Background(), event)
	require.NoError(t, err)
	require.True(t, event.CreatedAt.Equal(dockerTime))
}
