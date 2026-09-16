package storage

import (
	"argus-collector/internal/config"
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
