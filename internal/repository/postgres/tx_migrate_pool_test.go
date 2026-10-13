package postgres_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"technical-test-be-gdc-multi-sarana/internal/repository/postgres"
)

func TestTxManager_RepositoriesJoinTheTransaction(t *testing.T) {
	mock := newMock(t)
	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM tasks`).WithArgs(anyArgs(1)...).WillReturnResult(pgxmock.NewResult("DELETE", 1))
	mock.ExpectCommit()
	tm := postgres.NewTxManager(mock)

	err := tm.WithinTx(ctx, func(ctx context.Context) error {
		// A nested call joins the outer transaction instead of beginning a new one.
		return tm.WithinTx(ctx, func(ctx context.Context) error {
			return postgres.NewTaskRepository(mock).Delete(ctx, uuid.New())
		})
	})

	assert.NoError(t, err)
}

func TestTxManager_RollsBackOnError(t *testing.T) {
	errRollback := errors.New("rollback failed")
	tests := []struct {
		name          string
		rollbackErr   error
		wantInMessage string
	}{
		{"rollback succeeds", nil, ""},
		{"rollback fails too", errRollback, "rollback failed"},
		{"transaction already closed", pgx.ErrTxClosed, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mock := newMock(t)
			mock.ExpectBegin()
			rollback := mock.ExpectRollback()
			if tc.rollbackErr != nil {
				rollback.WillReturnError(tc.rollbackErr)
			}

			err := postgres.NewTxManager(mock).WithinTx(ctx, func(context.Context) error { return errBoom })

			assert.ErrorIs(t, err, errBoom)
			if tc.wantInMessage != "" {
				assert.ErrorIs(t, err, errRollback)
			} else {
				assert.NotContains(t, err.Error(), "rollback")
			}
		})
	}
}

func TestTxManager_RollsBackAndRepanics(t *testing.T) {
	mock := newMock(t)
	mock.ExpectBegin()
	mock.ExpectRollback()

	assert.PanicsWithValue(t, "kaboom", func() {
		_ = postgres.NewTxManager(mock).WithinTx(ctx, func(context.Context) error { panic("kaboom") })
	})
}

func TestTxManager_BeginAndCommitFailures(t *testing.T) {
	t.Run("begin", func(t *testing.T) {
		mock := newMock(t)
		mock.ExpectBegin().WillReturnError(errBoom)
		called := false

		err := postgres.NewTxManager(mock).WithinTx(ctx, func(context.Context) error { called = true; return nil })

		assert.ErrorIs(t, err, errBoom)
		assert.False(t, called)
	})

	t.Run("commit", func(t *testing.T) {
		mock := newMock(t)
		mock.ExpectBegin()
		mock.ExpectCommit().WillReturnError(errBoom)
		mock.ExpectRollback()

		err := postgres.NewTxManager(mock).WithinTx(ctx, func(context.Context) error { return nil })

		assert.ErrorIs(t, err, errBoom)
		assert.Contains(t, err.Error(), "commit transaction")
	})
}

const migrationLockID = 724_118_031

func expectMigrationSetup(mock pgxmock.PgxPoolIface) {
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT pg_advisory_xact_lock\(\$1\)`).WithArgs(migrationLockID).WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectExec(`CREATE TABLE IF NOT EXISTS schema_migrations`).WillReturnResult(pgxmock.NewResult("CREATE TABLE", 0))
}

func expectApplied(mock pgxmock.PgxPoolIface, applied bool) {
	mock.ExpectQuery(`SELECT EXISTS \(SELECT 1 FROM schema_migrations WHERE version = \$1\)`).WithArgs("0001_init").
		WillReturnRows(mock.NewRows([]string{"exists"}).AddRow(applied))
}

func TestMigrate_AppliesPendingMigrations(t *testing.T) {
	mock := newMock(t)
	expectMigrationSetup(mock)
	expectApplied(mock, false)
	mock.ExpectExec(`CREATE EXTENSION IF NOT EXISTS pg_trgm`).WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectExec(`INSERT INTO schema_migrations \(version\) VALUES \(\$1\)`).WithArgs("0001_init").WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	var logs bytes.Buffer

	require.NoError(t, postgres.Migrate(ctx, mock, slog.New(slog.NewJSONHandler(&logs, nil))))
	assert.Contains(t, logs.String(), `"version":"0001_init"`)
}

func TestMigrate_SkipsAppliedMigrations(t *testing.T) {
	mock := newMock(t)
	expectMigrationSetup(mock)
	expectApplied(mock, true)
	mock.ExpectCommit()

	assert.NoError(t, postgres.Migrate(ctx, mock, slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))))
}

func TestMigrate_FailuresRollBack(t *testing.T) {
	tests := map[string]func(mock pgxmock.PgxPoolIface){
		"lock": func(mock pgxmock.PgxPoolIface) {
			mock.ExpectBegin()
			mock.ExpectExec(`pg_advisory_xact_lock`).WithArgs(migrationLockID).WillReturnError(errBoom)
		},
		"bookkeeping table": func(mock pgxmock.PgxPoolIface) {
			mock.ExpectBegin()
			mock.ExpectExec(`pg_advisory_xact_lock`).WithArgs(migrationLockID).WillReturnResult(pgxmock.NewResult("SELECT", 1))
			mock.ExpectExec(`CREATE TABLE IF NOT EXISTS schema_migrations`).WillReturnError(errBoom)
		},
		"checking a version": func(mock pgxmock.PgxPoolIface) {
			expectMigrationSetup(mock)
			mock.ExpectQuery(`SELECT EXISTS`).WithArgs("0001_init").WillReturnError(errBoom)
		},
		"applying a migration": func(mock pgxmock.PgxPoolIface) {
			expectMigrationSetup(mock)
			expectApplied(mock, false)
			mock.ExpectExec(`CREATE EXTENSION`).WillReturnError(errBoom)
		},
		"recording a migration": func(mock pgxmock.PgxPoolIface) {
			expectMigrationSetup(mock)
			expectApplied(mock, false)
			mock.ExpectExec(`CREATE EXTENSION`).WillReturnResult(pgxmock.NewResult("CREATE", 0))
			mock.ExpectExec(`INSERT INTO schema_migrations`).WithArgs("0001_init").WillReturnError(errBoom)
		},
	}
	for name, expect := range tests {
		t.Run(name, func(t *testing.T) {
			mock := newMock(t)
			expect(mock)
			mock.ExpectRollback()

			err := postgres.Migrate(ctx, mock, slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)))

			assert.ErrorIs(t, err, errBoom)
		})
	}
}

// unreachableDSN points at a port nothing listens on, so connecting fails
// immediately without any database.
const unreachableDSN = "postgres://user:pass@127.0.0.1:1/db?sslmode=disable"

func TestNewPool_InvalidDSN(t *testing.T) {
	_, err := postgres.NewPool(ctx, postgres.PoolConfig{DSN: "://not a dsn"})
	assert.ErrorContains(t, err, "parse database config")
}

func TestNewPool_GivesUpAfterRetries(t *testing.T) {
	start := time.Now()

	_, err := postgres.NewPool(ctx, postgres.PoolConfig{DSN: unreachableDSN, MaxConns: 2, ConnectRetries: 2})

	assert.ErrorContains(t, err, "ping database after 2 attempts")
	assert.GreaterOrEqual(t, time.Since(start), time.Second, "waits between attempts")
}

func TestNewPool_StopsWaitingWhenCancelled(t *testing.T) {
	cctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()

	_, err := postgres.NewPool(cctx, postgres.PoolConfig{DSN: unreachableDSN, ConnectRetries: 10})

	assert.ErrorIs(t, err, context.DeadlineExceeded)
}
