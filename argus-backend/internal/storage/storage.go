package storage

import (
	"argus-collector/internal/config"
	"argus-collector/internal/domain/entity"
	"context"
	"errors"
	"fmt"

	sq "github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Storage struct {
	db *pgxpool.Pool
	*TargetRepository
	*ProbeRepository
	*EventRepository
	*AlertRepository
	*LogMatchRepository
}

func NewStorage(ctx context.Context, cfg *config.Config) (*Storage, error) {
	const op = "storage.NewStorage"

	poolConfig, err := pgxpool.ParseConfig(cfg.Postgres)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	db, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return &Storage{
		db:                 db,
		TargetRepository:   &TargetRepository{db: db},
		ProbeRepository:    &ProbeRepository{db: db},
		EventRepository:    &EventRepository{db: db},
		AlertRepository:    &AlertRepository{db: db},
		LogMatchRepository: &LogMatchRepository{db: db},
	}, nil
}

func (s *Storage) Ping(ctx context.Context) error {
	return s.db.Ping(ctx)
}

func (s *Storage) Close() {
	if s == nil {
		return
	}

	s.db.Close()
}

func insertReturning(ctx context.Context, db *pgxpool.Pool, table string, columns []string, values []any, suffix string, dest ...any) error {
	query, args, err := sq.
		Insert(table).
		Columns(columns...).
		Values(values...).
		Suffix(suffix).
		PlaceholderFormat(sq.Dollar).
		ToSql()
	if err != nil {
		return err
	}

	return db.QueryRow(ctx, query, args...).Scan(dest...)
}

func getByID(ctx context.Context, db *pgxpool.Pool, table string, columns []string, id any, notFoundErr error, dest ...any) error {
	query, args, err := sq.
		Select(columns...).
		From(table).
		Where(sq.Eq{"id": id}).
		PlaceholderFormat(sq.Dollar).
		ToSql()
	if err != nil {
		return err
	}

	err = db.QueryRow(ctx, query, args...).Scan(dest...)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return notFoundErr
		}

		return err
	}

	return nil
}

func listWhere(b sq.SelectBuilder, f entity.ListFilter) sq.SelectBuilder {
	if len(f.TargetIDs) > 0 {
		b = b.Where(sq.Eq{"target_id": f.TargetIDs})
	}

	if f.Host {
		b = b.Where(sq.Eq{"target_id": nil})
	}

	if f.Severity != nil {
		b = b.Where(sq.Eq{"severity": *f.Severity})
	}

	if f.Status != nil {
		b = b.Where(sq.Eq{"status": *f.Status})
	}

	if f.Since != nil {
		b = b.Where(sq.GtOrEq{"created_at": *f.Since})
	}

	if f.After > 0 {
		b = b.Where(sq.Gt{"id": f.After})
	}

	return b
}

func findPage[T any](
	ctx context.Context,
	db *pgxpool.Pool,
	table string,
	columns []string,
	f entity.ListFilter,
	scan func(pgx.Rows) (T, error),
) ([]T, int, error) {
	order := "id ASC"
	if f.Desc {
		order = "id DESC"
	}

	query, args, err := listWhere(sq.Select(columns...).From(table), f).
		OrderBy(order).
		Limit(f.Limit).
		Offset(f.Skip).
		PlaceholderFormat(sq.Dollar).
		ToSql()
	if err != nil {
		return nil, 0, err
	}

	rows, err := db.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]T, 0, f.Limit)

	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return nil, 0, err
		}

		out = append(out, item)
	}

	err = rows.Err()
	if err != nil {
		return nil, 0, err
	}

	countQuery, countArgs, err := listWhere(sq.Select("count(*)").From(table), f).
		PlaceholderFormat(sq.Dollar).
		ToSql()
	if err != nil {
		return nil, 0, err
	}

	var total int

	err = db.QueryRow(ctx, countQuery, countArgs...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	return out, total, nil
}
