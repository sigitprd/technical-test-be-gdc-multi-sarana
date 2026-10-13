// Package postgres implements the service repositories on PostgreSQL using pgx.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const pgUniqueViolation = "23505"

// pingTimeout bounds each connection attempt made by NewPool.
const pingTimeout = 5 * time.Second

// PoolConfig configures the connection pool.
type PoolConfig struct {
	DSN            string
	MaxConns       int32
	ConnectRetries int
}

// NewPool opens a connection pool and waits until the database answers,
// retrying a few times so the API tolerates a database that is still starting.
func NewPool(ctx context.Context, cfg PoolConfig) (*pgxpool.Pool, error) {
	pcfg, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("parse database config: %w", err)
	}
	if cfg.MaxConns > 0 {
		pcfg.MaxConns = cfg.MaxConns
	}

	pool, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	retries := max(cfg.ConnectRetries, 1)
	for attempt := 1; ; attempt++ {
		pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
		err = pool.Ping(pingCtx)
		cancel()
		if err == nil {
			return pool, nil
		}
		if attempt >= retries {
			pool.Close()
			return nil, fmt.Errorf("ping database after %d attempts: %w", attempt, err)
		}
		select {
		case <-ctx.Done():
			pool.Close()
			return nil, ctx.Err()
		case <-time.After(time.Duration(attempt) * time.Second):
		}
	}
}

// dbtx is the subset of pgx shared by *pgxpool.Pool and pgx.Tx.
type dbtx interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// DB is what the repositories need from the connection pool. *pgxpool.Pool
// implements it; tests use pgxmock instead of a real database.
type DB interface {
	dbtx
	Begin(ctx context.Context) (pgx.Tx, error)
}

type txKey struct{}

// TxManager implements service.TxManager. The active transaction travels in
// the context so repositories transparently join it.
type TxManager struct {
	db DB
}

// NewTxManager creates a TxManager.
func NewTxManager(db DB) *TxManager {
	return &TxManager{db: db}
}

// WithinTx runs fn in a transaction. Nested calls reuse the outer transaction.
// The transaction is rolled back when fn returns an error or panics.
func (m *TxManager) WithinTx(ctx context.Context, fn func(ctx context.Context) error) (err error) {
	if _, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return fn(ctx)
	}

	tx, err := m.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(context.WithoutCancel(ctx))
			panic(p)
		}
		if err != nil {
			if rbErr := tx.Rollback(context.WithoutCancel(ctx)); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
				err = errors.Join(err, fmt.Errorf("rollback: %w", rbErr))
			}
		}
	}()

	if err = fn(context.WithValue(ctx, txKey{}, tx)); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// conn returns the transaction stored in ctx, or db.
func conn(ctx context.Context, db DB) dbtx {
	if tx, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return tx
	}
	return db
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation
}
