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
		return nil, wrapErr(op, entity.ErrInvalidSeverity)
	}

	saved, err := uc.db.SaveLogMatch(ctx, logMatch)

	return saved, wrapErr(op, err)
}

func (uc *UseCase) DeleteLogMatch(ctx context.Context, id int) (int, error) {
	const op = "UseCase.DeleteLogMatch"

	deletedID, err := uc.db.DeleteLogMatch(ctx, id)

	return deletedID, wrapErr(op, err)
}

func (uc *UseCase) DeleteLogMatchesOlderThan(ctx context.Context, before time.Time) error {
	const op = "UseCase.DeleteLogMatchesOlderThan"

	err := uc.db.DeleteLogMatchesOlderThan(ctx, before)

	return wrapErr(op, err)
}

func (uc *UseCase) GetLogMatch(ctx context.Context, id int) (*entity.LogMatch, error) {
	const op = "UseCase.GetLogMatch"

	logMatch, err := uc.db.GetLogMatch(ctx, id)

	return logMatch, wrapErr(op, err)
}

func (uc *UseCase) FindLogMatches(ctx context.Context, f entity.ListFilter) ([]*entity.LogMatch, int, error) {
	const op = "UseCase.FindLogMatches"

	logMatches, total, err := uc.db.FindLogMatches(ctx, f)

	return logMatches, total, wrapErr(op, err)
}
