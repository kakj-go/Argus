// Package postgres owns the PostgreSQL connection and generated query adapter.
package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

type Store struct {
	Pool    *pgxpool.Pool
	Queries *db.Queries
}

func Open(ctx context.Context, databaseURL string) (*Store, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse PostgreSQL configuration: %w", err)
	}
	config.MaxConns = 20
	config.MinConns = 2
	config.MaxConnIdleTime = 5 * time.Minute
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("open PostgreSQL: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping PostgreSQL: %w", err)
	}
	return &Store{Pool: pool, Queries: db.New(pool)}, nil
}

func (store *Store) Close() { store.Pool.Close() }

func (store *Store) Ready(ctx context.Context) error { return store.Pool.Ping(ctx) }

func (store *Store) InTx(ctx context.Context, fn func(*db.Queries) error) error {
	return store.inTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable}, fn)
}

// InReadCommittedTx is reserved for workflows that explicitly serialize their
// own writes with row locks, such as the append-only audit hash chain.
func (store *Store) InReadCommittedTx(ctx context.Context, fn func(*db.Queries) error) error {
	return store.inTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, fn)
}

func (store *Store) inTx(ctx context.Context, options pgx.TxOptions, fn func(*db.Queries) error) error {
	tx, err := store.Pool.BeginTx(ctx, options)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(store.Queries.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
