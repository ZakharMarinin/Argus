//nolint:dupl
package usecase

import (
	"argus-collector/internal/domain/entity"
	"context"
	"time"

	"github.com/google/uuid"
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

func (uc *UseCase) GetLogMatchesByTarget(ctx context.Context, targetID uuid.UUID) ([]*entity.LogMatch, error) {
	const op = "UseCase.GetLogMatchesByTarget"

	logMatches, err := uc.db.GetLogMatchesByTarget(ctx, targetID)

	return logMatches, wrapErr(op, err)
}

func (uc *UseCase) GetLogMatchesBySeverity(ctx context.Context, severity entity.Severity) ([]*entity.LogMatch, error) {
	const op = "UseCase.GetLogMatchesBySeverity"

	logMatches, err := uc.db.GetLogMatchesBySeverity(ctx, severity)

	return logMatches, wrapErr(op, err)
}

func (uc *UseCase) GetAllLogMatches(ctx context.Context, limit uint64, offset int) ([]*entity.LogMatch, error) {
	const op = "UseCase.GetAllLogMatches"

	logMatches, err := uc.db.GetAllLogMatches(ctx, limit, offset)

	return logMatches, wrapErr(op, err)
}
