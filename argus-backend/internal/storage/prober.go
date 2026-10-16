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

type ProbeRepository struct {
	db *pgxpool.Pool
}

func (pr *ProbeRepository) SaveProbe(ctx context.Context, probe *entity.Probe) (*entity.Probe, error) {
	const op = "Storage.ProbeRepository.SaveProbe"

	var saved entity.Probe

	err := insertReturning(ctx, pr.db,
		"probes",
		[]string{"target_id", "success", "latency", "created_at"},
		[]any{probe.TargetID, probe.Success, probe.Latency, time.Now()},
		"RETURNING id, target_id, success, latency, created_at",
		&saved.ID, &saved.TargetID, &saved.Success, &saved.Latency, &saved.CreatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return nil, fmt.Errorf("%s: %w", op, entity.ErrNotFound)
		}
		
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return &saved, nil
}

func (pr *ProbeRepository) SaveProbes(ctx context.Context, probes []*entity.Probe) ([]*entity.Probe, error) {
	const op = "Storage.ProbeRepository.SaveProbes"

	if len(probes) == 0 {
		return nil, nil
	}

	now := time.Now()

	batch := &pgx.Batch{}

	for _, probe := range probes {
		query, args, err := sq.
			Insert("probes").
			Columns("target_id", "success", "latency", "created_at").
			Values(probe.TargetID, probe.Success, probe.Latency, now).
			Suffix("RETURNING id, target_id, success, latency, created_at").
			PlaceholderFormat(sq.Dollar).
			ToSql()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", op, err)
		}

		batch.Queue(query, args...)
	}

	results := pr.db.SendBatch(ctx, batch)
	defer func() {
		_ = results.Close()
	}()

	saved := make([]*entity.Probe, 0, len(probes))

	for range probes {
		var s entity.Probe

		err := results.QueryRow().Scan(&s.ID, &s.TargetID, &s.Success, &s.Latency, &s.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", op, err)
		}

		saved = append(saved, &s)
	}

	return saved, nil
}

func (pr *ProbeRepository) DeleteProbe(ctx context.Context, id int) (int, error) {
	const op = "Storage.ProbeRepository.DeleteProbe"

	query, args, err := sq.
		Delete("probes").
		Where(sq.Eq{"id": id}).
		PlaceholderFormat(sq.Dollar).
		ToSql()
	if err != nil {
		return 0, fmt.Errorf("%s: %w", op, err)
	}

	result, err := pr.db.Exec(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", op, err)
	}

	if result.RowsAffected() == 0 {
		return 0, fmt.Errorf("%s: %w", op, entity.ErrNotFound)
	}

	return id, nil
}

func (pr *ProbeRepository) DeleteProbesOlderThan(ctx context.Context, before time.Time) error {
	const op = "Storage.ProbeRepository.DeleteProbesOlderThan"

	query, args, err := sq.Delete("probes").
		Where(sq.Lt{"created_at": before}).
		PlaceholderFormat(sq.Dollar).
		ToSql()
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	_, err = pr.db.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

func (pr *ProbeRepository) GetProbe(ctx context.Context, id int) (*entity.Probe, error) {
	const op = "Storage.ProbeRepository.GetProbe"

	var probe entity.Probe

	err := getByID(ctx, pr.db,
		"probes",
		[]string{"id", "target_id", "success", "latency", "created_at"},
		id,
		entity.ErrNotFound,
		&probe.ID, &probe.TargetID, &probe.Success, &probe.Latency, &probe.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return &probe, nil
}

func (pr *ProbeRepository) getProbesConstructor() sq.SelectBuilder {
	builder := sq.
		Select("id", "target_id", "success", "latency", "created_at").
		From("probes").
		OrderBy("id ASC").
		PlaceholderFormat(sq.Dollar)

	return builder
}

func (pr *ProbeRepository) GetProbesByTarget(ctx context.Context, targetID uuid.UUID) ([]*entity.Probe, error) {
	const op = "Storage.ProbeRepository.GetProbesByTarget"

	query, args, err := pr.getProbesConstructor().
		Where(sq.Eq{"target_id": targetID}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	probes, err := pr.getProbesQuery(ctx, query, args)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return probes, nil
}

func (pr *ProbeRepository) GetProbesBySuccess(ctx context.Context, success bool) ([]*entity.Probe, error) {
	const op = "Storage.ProbeRepository.GetProbesBySuccess"

	query, args, err := pr.getProbesConstructor().
		Where(sq.Eq{"success": success}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	probes, err := pr.getProbesQuery(ctx, query, args)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return probes, nil
}

func (pr *ProbeRepository) GetAllProbes(ctx context.Context, limit uint64, offset int) ([]*entity.Probe, error) {
	const op = "Storage.ProbeRepository.GetAllProbes"

	query, args, err := pr.getProbesConstructor().
		Where(sq.Gt{"id": offset}).
		Limit(limit).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	probes, err := pr.getProbesQuery(ctx, query, args)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return probes, nil
}

func (pr *ProbeRepository) getProbesQuery(ctx context.Context, query string, args []any) ([]*entity.Probe, error) {
	var probes []*entity.Probe

	rows, err := pr.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var probe entity.Probe

		err := rows.Scan(&probe.ID, &probe.TargetID, &probe.Success, &probe.Latency, &probe.CreatedAt)
		if err != nil {
			return nil, err
		}

		probes = append(probes, &probe)
	}

	err = rows.Err()
	if err != nil {
		return nil, err
	}

	return probes, nil
}
