package usecase

import (
	"context"
	"errors"

	"argus-frontend/internal/domain/entity"
)

func (uc *UseCase) Host(ctx context.Context) (entity.Host, error) {
	h, err := uc.backend.Host(ctx)
	if errors.Is(err, entity.ErrNotFound) {
		return entity.Host{}, err
	}

	return h, notReady(err)
}
