package usecase_test

import (
	"context"
	"testing"
	"time"

	"argus-collector/internal/domain/entity"
	"argus-collector/internal/usecase"

	"github.com/google/uuid"
)

func TestUseCase_ProbeDelegation(t *testing.T) {
	t.Parallel()

	targetID := uuid.New()
	probe := &entity.Probe{TargetID: targetID, Success: true, Latency: 1200}
	probes := []*entity.Probe{probe, {TargetID: targetID}}
	before := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)

	assertDelegates(t, []delegation{
		{
			method: "SaveProbe", args: []any{probe}, want: &entity.Probe{ID: 7}, zero: (*entity.Probe)(nil),
			call: func(ctx context.Context, uc *usecase.UseCase) (any, error) { return uc.SaveProbe(ctx, probe) },
		},
		{
			method: "SaveProbes", args: []any{probes}, want: []*entity.Probe{{ID: 7}, {ID: 8}}, zero: []*entity.Probe(nil),
			call: func(ctx context.Context, uc *usecase.UseCase) (any, error) { return uc.SaveProbes(ctx, probes) },
		},
		{
			method: "DeleteProbe", args: []any{7}, want: 7, zero: 0,
			call: func(ctx context.Context, uc *usecase.UseCase) (any, error) { return uc.DeleteProbe(ctx, 7) },
		},
		{
			method: "DeleteProbesOlderThan", args: []any{before},
			call: func(ctx context.Context, uc *usecase.UseCase) (any, error) {
				return nil, uc.DeleteProbesOlderThan(ctx, before)
			},
		},
		{
			method: "GetProbe", args: []any{7}, want: &entity.Probe{ID: 7}, zero: (*entity.Probe)(nil),
			call: func(ctx context.Context, uc *usecase.UseCase) (any, error) { return uc.GetProbe(ctx, 7) },
		},
		{
			method: "GetProbesByTarget", args: []any{targetID}, want: []*entity.Probe{{ID: 7}}, zero: []*entity.Probe(nil),
			call: func(ctx context.Context, uc *usecase.UseCase) (any, error) {
				return uc.GetProbesByTarget(ctx, targetID)
			},
		},
		{
			method: "GetProbesBySuccess", args: []any{false}, want: []*entity.Probe{{ID: 7}}, zero: []*entity.Probe(nil),
			call: func(ctx context.Context, uc *usecase.UseCase) (any, error) { return uc.GetProbesBySuccess(ctx, false) },
		},
		{
			method: "GetAllProbes", args: []any{uint64(50), 10}, want: []*entity.Probe{{ID: 7}}, zero: []*entity.Probe(nil),
			call: func(ctx context.Context, uc *usecase.UseCase) (any, error) { return uc.GetAllProbes(ctx, 50, 10) },
		},
	})
}
