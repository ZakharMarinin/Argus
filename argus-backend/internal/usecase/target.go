package usecase

import (
	"argus-collector/internal/domain/entity"
	"context"

	"github.com/google/uuid"
)

func (uc *UseCase) CreateTarget(ctx context.Context, target *entity.Target) (*entity.Target, error) {
	const op = "UseCase.CreateTarget"

	err := target.Validate()
	if err != nil {
		return nil, wrapErr(op, err)
	}

	target.ID = uuid.New()
	target.Status = "unknown"

	created, err := uc.db.CreateTarget(ctx, target)

	return created, wrapErr(op, err)
}

func (uc *UseCase) UpdateTargetStatus(ctx context.Context, id uuid.UUID, status string) (*entity.Target, error) {
	return uc.db.UpdateTargetStatus(ctx, id, status)
}

func (uc *UseCase) DeleteTarget(ctx context.Context, id uuid.UUID) error {
	return uc.db.DeleteTarget(ctx, id)
}

func (uc *UseCase) GetTarget(ctx context.Context, id uuid.UUID) (*entity.Target, error) {
	return uc.db.GetTarget(ctx, id)
}

func (uc *UseCase) GetTargetsByIDs(ctx context.Context, ids []uuid.UUID) ([]*entity.Target, error) {
	return uc.db.GetTargetsByIDs(ctx, ids)
}

func (uc *UseCase) GetTargetsByType(ctx context.Context, targetType string) ([]*entity.Target, error) {
	return uc.db.GetTargetsByType(ctx, targetType)
}

func (uc *UseCase) GetTargetsByStatus(ctx context.Context, status string) ([]*entity.Target, error) {
	return uc.db.GetTargetsByStatus(ctx, status)
}

func (uc *UseCase) GetAllTargets(ctx context.Context, limit, offset uint64) ([]*entity.Target, error) {
	return uc.db.GetAllTargets(ctx, limit, offset)
}
