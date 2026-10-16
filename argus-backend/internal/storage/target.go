package storage

import (
	"argus-collector/internal/domain/entity"
	"context"
	"errors"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type TargetRepository struct {
	db *pgxpool.Pool
}

func (tr *TargetRepository) CreateTarget(ctx context.Context, target *entity.Target) (*entity.Target, error) {
	const op = "Storage.TargetRepository.CreateTarget"

	var created entity.Target

	err := insertReturning(ctx, tr.db,
		"targets",
		[]string{"id", "source", "name", "type", "status", "created_at", "updated_at"},
		[]any{target.ID, target.Source, target.Name, target.TargetType, target.Status, time.Now(), time.Now()},
		"RETURNING id, source, name, type, status, created_at, updated_at",
		&created.ID, &created.Source, &created.Name, &created.TargetType, &created.Status, &created.CreatedAt, &created.UpdatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError

		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, fmt.Errorf("%s: %w", op, entity.ErrAlreadyExists)
		}

		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return &created, nil
}

func (tr *TargetRepository) UpdateTargetStatus(ctx context.Context, id uuid.UUID, status string) (*entity.Target, error) {
	const op = "Storage.TargetRepository.UpdateTargetStatus"

	query, args, err := sq.Update("targets").
		Set("status", status).
		Set("updated_at", time.Now()).
		Where(sq.Eq{"id": id}).
		Suffix("RETURNING id, source, name, type, status, created_at, updated_at").
		PlaceholderFormat(sq.Dollar).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	var updatedTarget entity.Target

	err = tr.db.QueryRow(ctx, query, args...).Scan(
		&updatedTarget.ID, &updatedTarget.Source, &updatedTarget.Name, &updatedTarget.TargetType,
		&updatedTarget.Status, &updatedTarget.CreatedAt, &updatedTarget.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("%s: %w", op, entity.ErrNotFound)
		}

		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return &updatedTarget, nil
}

func (tr *TargetRepository) DeleteTarget(ctx context.Context, id uuid.UUID) error {
	const op = "Storage.TargetRepository.DeleteTarget"

	query, args, err := sq.Delete("targets").
		Where(sq.Eq{"id": id}).
		PlaceholderFormat(sq.Dollar).
		ToSql()
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	tag, err := tr.db.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%s: %w", op, entity.ErrNotFound)
	}

	return nil
}

func (tr *TargetRepository) GetTarget(ctx context.Context, id uuid.UUID) (*entity.Target, error) {
	const op = "Storage.TargetRepository.GetTarget"

	var target entity.Target

	err := getByID(ctx, tr.db,
		"targets",
		[]string{"id", "source", "name", "type", "status", "created_at", "updated_at"},
		id,
		entity.ErrNotFound,
		&target.ID, &target.Source, &target.Name, &target.TargetType, &target.Status, &target.CreatedAt, &target.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return &target, nil
}

func (tr *TargetRepository) getTargetsConstructor() sq.SelectBuilder {
	builder := sq.
		Select("id", "source", "name", "type", "status", "created_at", "updated_at").
		From("targets").
		OrderBy("created_at ASC", "id ASC").
		PlaceholderFormat(sq.Dollar)

	return builder
}

func (tr *TargetRepository) GetTargetsByType(ctx context.Context, targetType string) ([]*entity.Target, error) {
	const op = "Storage.TargetRepository.GetTargetsByType"

	query, args, err := tr.getTargetsConstructor().
		Where(sq.Eq{"type": targetType}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	targets, err := tr.getTargetsQuery(ctx, query, args)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return targets, nil
}

func (tr *TargetRepository) GetTargetsByIDs(ctx context.Context, ids []uuid.UUID) ([]*entity.Target, error) {
	const op = "Storage.TargetRepository.GetTargetsByIDs"

	if len(ids) == 0 {
		return nil, nil
	}

	query, args, err := tr.getTargetsConstructor().
		Where(sq.Eq{"id": ids}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	targets, err := tr.getTargetsQuery(ctx, query, args)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return targets, nil
}

func (tr *TargetRepository) GetTargetsByStatus(ctx context.Context, status string) ([]*entity.Target, error) {
	const op = "Storage.TargetRepository.GetTargetsByStatus"

	query, args, err := tr.getTargetsConstructor().
		Where(sq.Eq{"status": status}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	targets, err := tr.getTargetsQuery(ctx, query, args)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return targets, nil
}

func (tr *TargetRepository) GetAllTargets(ctx context.Context, limit, offset uint64) ([]*entity.Target, error) {
	const op = "Storage.TargetRepository.GetAllTargets"

	query, args, err := tr.getTargetsConstructor().
		Limit(limit).
		Offset(offset).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	targets, err := tr.getTargetsQuery(ctx, query, args)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return targets, nil
}

func (tr *TargetRepository) getTargetsQuery(ctx context.Context, query string, args []any) ([]*entity.Target, error) {
	var targets []*entity.Target

	rows, err := tr.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var target entity.Target

		err := rows.Scan(
			&target.ID, &target.Source, &target.Name, &target.TargetType,
			&target.Status, &target.CreatedAt, &target.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		targets = append(targets, &target)
	}

	err = rows.Err()
	if err != nil {
		return nil, err
	}

	return targets, nil
}
