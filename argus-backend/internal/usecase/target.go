package usecase

import (
	"argus-collector/internal/domain/entity"
	"context"

	"github.com/google/uuid"
)

func (uc *UseCase) CreateTarget(ctx context.Context, target *entity.Target) (*entity.Target, error) {
	const op = "UseCase.CreateTarget"

	target.ID = uuid.New()

	created, err := uc.db.CreateTarget(ctx, target)

	return created, wrapErr(op, err)
}

func (uc *UseCase) UpdateTargetStatus(ctx context.Context, id uuid.UUID, status string) (*entity.Target, error) {
	const op = "UseCase.UpdateTargetStatus"

	updated, err := uc.db.UpdateTargetStatus(ctx, id, status)

	return updated, wrapErr(op, err)
}

func (uc *UseCase) DeleteTarget(ctx context.Context, id uuid.UUID) error {
	const op = "UseCase.DeleteTarget"

	err := uc.db.DeleteTarget(ctx, id)

	return wrapErr(op, err)
}

func (uc *UseCase) GetTarget(ctx context.Context, id uuid.UUID) (*entity.Target, error) {
	const op = "UseCase.GetTarget"

	target, err := uc.db.GetTarget(ctx, id)

	return target, wrapErr(op, err)
}

func (uc *UseCase) GetTargetsByIDs(ctx context.Context, ids []uuid.UUID) ([]*entity.Target, error) {
	const op = "UseCase.GetTargetsByIDs"

	targets, err := uc.db.GetTargetsByIDs(ctx, ids)

	return targets, wrapErr(op, err)
}

func (uc *UseCase) GetTargetsByType(ctx context.Context, targetType string) ([]*entity.Target, error) {
	const op = "UseCase.GetTargetsByType"

	targets, err := uc.db.GetTargetsByType(ctx, targetType)

	return targets, wrapErr(op, err)
}

func (uc *UseCase) GetTargetsByStatus(ctx context.Context, status string) ([]*entity.Target, error) {
	const op = "UseCase.GetTargetsByStatus"

	targets, err := uc.db.GetTargetsByStatus(ctx, status)

	return targets, wrapErr(op, err)
}

func (uc *UseCase) GetAllTargets(ctx context.Context, limit, offset uint64) ([]*entity.Target, error) {
	const op = "UseCase.GetAllTargets"

	targets, err := uc.db.GetAllTargets(ctx, limit, offset)

	return targets, wrapErr(op, err)
}
