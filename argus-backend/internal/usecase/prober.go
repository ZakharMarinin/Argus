package usecase

import (
	"argus-collector/internal/domain/entity"
	"context"
	"time"

	"github.com/google/uuid"
)

func (uc *UseCase) SaveProbe(ctx context.Context, probe *entity.Probe) (*entity.Probe, error) {
	return uc.db.SaveProbe(ctx, probe)
}

func (uc *UseCase) SaveProbes(ctx context.Context, probes []*entity.Probe) ([]*entity.Probe, error) {
	return uc.db.SaveProbes(ctx, probes)
}

func (uc *UseCase) DeleteProbe(ctx context.Context, id int) (int, error) {
	return uc.db.DeleteProbe(ctx, id)
}

func (uc *UseCase) DeleteProbesOlderThan(ctx context.Context, before time.Time) error {
	return uc.db.DeleteProbesOlderThan(ctx, before)
}

func (uc *UseCase) GetProbe(ctx context.Context, id int) (*entity.Probe, error) {
	return uc.db.GetProbe(ctx, id)
}

func (uc *UseCase) GetProbesByTarget(ctx context.Context, targetID uuid.UUID) ([]*entity.Probe, error) {
	return uc.db.GetProbesByTarget(ctx, targetID)
}

func (uc *UseCase) GetProbesBySuccess(ctx context.Context, success bool) ([]*entity.Probe, error) {
	return uc.db.GetProbesBySuccess(ctx, success)
}

func (uc *UseCase) GetAllProbes(ctx context.Context, limit uint64, offset int) ([]*entity.Probe, error) {
	return uc.db.GetAllProbes(ctx, limit, offset)
}
