package usecase_test

import (
	"context"
	"testing"

	"argus-collector/internal/domain/entity"
	"argus-collector/internal/usecase"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	mocks "argus-collector/internal/usecase/mocks"
)

func TestUseCase_CreateTarget(t *testing.T) {
	t.Parallel()

	clientID := uuid.New()

	cases := []struct {
		name           string
		in             *entity.Target
		storeErr       error
		wantValidation bool
	}{
		{name: "probe", in: &entity.Target{Source: "https://api.local/health", Name: "api", TargetType: entity.TargetTypeProbe}},
		{name: "container", in: &entity.Target{Source: "web", Name: "web", TargetType: entity.TargetTypeContainer}},
		{name: "client cannot pick id or status", in: &entity.Target{ID: clientID, Status: "up", Source: "http://api.local", Name: "api", TargetType: entity.TargetTypeProbe}},
		{name: "duplicate keeps its sentinel for 409", in: &entity.Target{Source: "web", Name: "web", TargetType: entity.TargetTypeContainer}, storeErr: entity.ErrTargetAlreadyExists},
		{name: "blank name", in: &entity.Target{Source: "https://api.local", Name: " ", TargetType: entity.TargetTypeProbe}, wantValidation: true},
		{name: "probe without scheme", in: &entity.Target{Source: "api.local/health", Name: "api", TargetType: entity.TargetTypeProbe}, wantValidation: true},
		{name: "probe with other scheme", in: &entity.Target{Source: "ftp://api.local", Name: "api", TargetType: entity.TargetTypeProbe}, wantValidation: true},
		{name: "probe without host", in: &entity.Target{Source: "http://", Name: "api", TargetType: entity.TargetTypeProbe}, wantValidation: true},
		{name: "container without name", in: &entity.Target{Source: " ", Name: "web", TargetType: entity.TargetTypeContainer}, wantValidation: true},
		{name: "unknown type", in: &entity.Target{Source: "https://api.local", Name: "api", TargetType: "svc"}, wantValidation: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			db := mocks.NewMockStorage(t)

			if !tc.wantValidation {
				db.On("CreateTarget", mock.Anything, tc.in).Return(tc.in, tc.storeErr).Once()
			}

			got, err := usecase.NewUseCase(db).CreateTarget(t.Context(), tc.in)

			if tc.wantValidation {
				var verr *entity.ValidationError
				require.ErrorAs(t, err, &verr, "validation errors map to 400")

				return
			}

			if tc.storeErr != nil {
				require.ErrorIs(t, err, tc.storeErr)
				require.ErrorContains(t, err, "UseCase.CreateTarget")

				return
			}

			require.NoError(t, err)
			require.Same(t, tc.in, got)
			require.NotEqual(t, uuid.Nil, got.ID)
			require.NotEqual(t, clientID, got.ID)
			require.Equal(t, "unknown", got.Status)
		})
	}
}

func TestUseCase_TargetDelegation(t *testing.T) {
	t.Parallel()

	id := uuid.New()

	assertDelegates(t, []delegation{
		{
			method: "UpdateTargetStatus", args: []any{id, "down"}, want: &entity.Target{ID: id, Status: "down"}, zero: (*entity.Target)(nil),
			call: func(ctx context.Context, uc *usecase.UseCase) (any, error) {
				return uc.UpdateTargetStatus(ctx, id, "down")
			},
		},
		{
			method: "DeleteTarget", args: []any{id},
			call: func(ctx context.Context, uc *usecase.UseCase) (any, error) { return nil, uc.DeleteTarget(ctx, id) },
		},
		{
			method: "GetTarget", args: []any{id}, want: &entity.Target{ID: id}, zero: (*entity.Target)(nil),
			call: func(ctx context.Context, uc *usecase.UseCase) (any, error) { return uc.GetTarget(ctx, id) },
		},
		{
			method: "GetTargetsByIDs", args: []any{[]uuid.UUID{id}}, want: []*entity.Target{{ID: id}}, zero: []*entity.Target(nil),
			call: func(ctx context.Context, uc *usecase.UseCase) (any, error) {
				return uc.GetTargetsByIDs(ctx, []uuid.UUID{id})
			},
		},
		{
			method: "GetTargetsByType", args: []any{entity.TargetTypeProbe}, want: []*entity.Target{{ID: id}}, zero: []*entity.Target(nil),
			call: func(ctx context.Context, uc *usecase.UseCase) (any, error) {
				return uc.GetTargetsByType(ctx, entity.TargetTypeProbe)
			},
		},
		{
			method: "GetTargetsByStatus", args: []any{"down"}, want: []*entity.Target{{ID: id}}, zero: []*entity.Target(nil),
			call: func(ctx context.Context, uc *usecase.UseCase) (any, error) { return uc.GetTargetsByStatus(ctx, "down") },
		},
		{
			method: "GetAllTargets", args: []any{uint64(50), uint64(10)}, want: []*entity.Target{{ID: id}}, zero: []*entity.Target(nil),
			call: func(ctx context.Context, uc *usecase.UseCase) (any, error) { return uc.GetAllTargets(ctx, 50, 10) },
		},
	})
}
