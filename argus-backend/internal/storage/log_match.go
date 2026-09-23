package storage

import (
	"argus-collector/internal/domain/entity"
	"context"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type LogMatchRepository struct {
	db *pgxpool.Pool
}

func (lmr *LogMatchRepository) SaveLogMatch(ctx context.Context, logMatch *entity.LogMatch) (*entity.LogMatch, error) {
	const op = "Storage.LogMatchRepository.SaveLogMatch"

	var saved entity.LogMatch

	err := insertReturning(ctx, lmr.db,
		"log_matches",
		[]string{"target_id", "severity", "message", "created_at"},
		[]any{logMatch.TargetID, logMatch.Severity, logMatch.Message, time.Now()},
		"RETURNING id, target_id, severity, message, created_at",
		&saved.ID, &saved.TargetID, &saved.Severity, &saved.Message, &saved.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return &saved, nil
}

func (lmr *LogMatchRepository) DeleteLogMatch(ctx context.Context, id int) (int, error) {
	const op = "Storage.LogMatchRepository.DeleteLogMatch"

	query, args, err := sq.
		Delete("log_matches").
		Where(sq.Eq{"id": id}).
		PlaceholderFormat(sq.Dollar).
		ToSql()
	if err != nil {
		return 0, fmt.Errorf("%s: %w", op, err)
	}

	result, err := lmr.db.Exec(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", op, err)
	}

	if result.RowsAffected() == 0 {
		return 0, fmt.Errorf("%s: %w", op, entity.ErrLogMatchNotFound)
	}

	return id, nil
}

func (lmr *LogMatchRepository) DeleteLogMatchesOlderThan(ctx context.Context, before time.Time) error {
	const op = "Storage.LogMatchRepository.DeleteLogMatchesOlderThan"

	query, args, err := sq.
		Delete("log_matches").
		Where(sq.Lt{"created_at": before}).
		PlaceholderFormat(sq.Dollar).
		ToSql()
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	_, err = lmr.db.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

func (lmr *LogMatchRepository) GetLogMatch(ctx context.Context, id int) (*entity.LogMatch, error) {
	const op = "Storage.LogMatchRepository.GetLogMatch"

	var logMatch entity.LogMatch

	err := getByID(ctx, lmr.db,
		"log_matches",
		[]string{"id", "target_id", "severity", "message", "created_at"},
		id,
		entity.ErrLogMatchNotFound,
		&logMatch.ID, &logMatch.TargetID, &logMatch.Severity, &logMatch.Message, &logMatch.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return &logMatch, nil
}

func (lmr *LogMatchRepository) FindLogMatches(ctx context.Context, f entity.ListFilter) ([]*entity.LogMatch, int, error) {
	const op = "Storage.LogMatchRepository.FindLogMatches"

	logMatches, total, err := findPage(ctx, lmr.db,
		"log_matches",
		[]string{"id", "target_id", "severity", "message", "created_at"},
		f,
		func(rows pgx.Rows) (*entity.LogMatch, error) {
			var m entity.LogMatch

			err := rows.Scan(&m.ID, &m.TargetID, &m.Severity, &m.Message, &m.CreatedAt)

			return &m, err
		},
	)
	if err != nil {
		return nil, 0, fmt.Errorf("%s: %w", op, err)
	}

	return logMatches, total, nil
}
