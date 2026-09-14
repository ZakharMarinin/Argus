package usecase

import (
	"argus-collector/internal/domain/entity"
	"context"
	"time"

	"github.com/google/uuid"
)

func (uc *UseCase) SaveProbe(ctx context.Context, probe *entity.Probe) (*entity.Probe, error) {
	const op = "UseCase.SaveProbe"

	saved, err := uc.db.SaveProbe(ctx, probe)

	return saved, wrapErr(op, err)
}

func (uc *UseCase) SaveProbes(ctx context.Context, probes []*entity.Probe) ([]*entity.Probe, error) {
	const op = "UseCase.SaveProbes"

	saved, err := uc.db.SaveProbes(ctx, probes)

	return saved, wrapErr(op, err)
}

func (uc *UseCase) DeleteProbe(ctx context.Context, id int) (int, error) {
	const op = "UseCase.DeleteProbe"

	deletedID, err := uc.db.DeleteProbe(ctx, id)

	return deletedID, wrapErr(op, err)
}

func (uc *UseCase) DeleteProbesOlderThan(ctx context.Context, before time.Time) error {
	const op = "UseCase.DeleteProbesOlderThan"

	err := uc.db.DeleteProbesOlderThan(ctx, before)

	return wrapErr(op, err)
}

func (uc *UseCase) GetProbe(ctx context.Context, id int) (*entity.Probe, error) {
	const op = "UseCase.GetProbe"

	probe, err := uc.db.GetProbe(ctx, id)

	return probe, wrapErr(op, err)
}

func (uc *UseCase) GetProbesByTarget(ctx context.Context, targetID uuid.UUID) ([]*entity.Probe, error) {
	const op = "UseCase.GetProbesByTarget"

	probes, err := uc.db.GetProbesByTarget(ctx, targetID)

	return probes, wrapErr(op, err)
}

func (uc *UseCase) GetProbesBySuccess(ctx context.Context, success bool) ([]*entity.Probe, error) {
	const op = "UseCase.GetProbesBySuccess"

	probes, err := uc.db.GetProbesBySuccess(ctx, success)

	return probes, wrapErr(op, err)
}

func (uc *UseCase) GetAllProbes(ctx context.Context, limit uint64, offset int) ([]*entity.Probe, error) {
	const op = "UseCase.GetAllProbes"

	probes, err := uc.db.GetAllProbes(ctx, limit, offset)

	return probes, wrapErr(op, err)
}
