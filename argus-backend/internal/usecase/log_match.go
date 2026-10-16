//nolint:dupl
package usecase

import (
	"argus-collector/internal/domain/entity"
	"context"
	"time"
)

func (uc *UseCase) SaveLogMatch(ctx context.Context, logMatch *entity.LogMatch) (*entity.LogMatch, error) {
	const op = "UseCase.SaveLogMatch"

	if !logMatch.Severity.Valid() {
		return nil, wrapErr(op, &entity.ValidationError{Reason: "invalid severity"})
	}

	saved, err := uc.db.SaveLogMatch(ctx, logMatch)

	return saved, wrapErr(op, err)
}

func (uc *UseCase) DeleteLogMatch(ctx context.Context, id int) (int, error) {
	return uc.db.DeleteLogMatch(ctx, id)
}

func (uc *UseCase) DeleteLogMatchesOlderThan(ctx context.Context, before time.Time) error {
	return uc.db.DeleteLogMatchesOlderThan(ctx, before)
}

func (uc *UseCase) GetLogMatch(ctx context.Context, id int) (*entity.LogMatch, error) {
	return uc.db.GetLogMatch(ctx, id)
}

func (uc *UseCase) FindLogMatches(ctx context.Context, f entity.ListFilter) ([]*entity.LogMatch, int, error) {
	return uc.db.FindLogMatches(ctx, f)
}
