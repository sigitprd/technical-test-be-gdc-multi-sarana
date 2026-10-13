package postgres

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// migrationLockID is an arbitrary key for pg_advisory_xact_lock so that
// several API instances starting at once do not run migrations concurrently.
const migrationLockID = 724_118_031

// Migrate applies every embedded migration that has not been applied yet.
//
// Everything runs in one transaction holding a transaction-level advisory
// lock: concurrent instances wait for each other, and a failing migration
// leaves the schema untouched.
func Migrate(ctx context.Context, db DB, log *slog.Logger) error {
	names, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	sort.Strings(names)

	return NewTxManager(db).WithinTx(ctx, func(ctx context.Context) error {
		q := conn(ctx, db)
		if err := lockMigrations(ctx, q); err != nil {
			return err
		}
		for _, name := range names {
			if err := applyMigration(ctx, q, name, log); err != nil {
				return err
			}
		}
		return nil
	})
}

// lockMigrations takes the migration lock and creates the bookkeeping table.
func lockMigrations(ctx context.Context, q dbtx) error {
	if _, err := q.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", migrationLockID); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	if _, err := q.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	return nil
}

// applyMigration runs one embedded migration file unless it was applied before.
func applyMigration(ctx context.Context, q dbtx, name string, log *slog.Logger) error {
	version := strings.TrimSuffix(strings.TrimPrefix(name, "migrations/"), ".sql")

	var applied bool
	if err := q.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)", version).Scan(&applied); err != nil {
		return fmt.Errorf("check migration %s: %w", version, err)
	}
	if applied {
		return nil
	}

	script, err := migrationFiles.ReadFile(name)
	if err != nil {
		return fmt.Errorf("read migration %s: %w", version, err)
	}
	if _, err := q.Exec(ctx, string(script)); err != nil {
		return fmt.Errorf("apply migration %s: %w", version, err)
	}
	if _, err := q.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", version); err != nil {
		return fmt.Errorf("record migration %s: %w", version, err)
	}
	log.Info("migration applied", slog.String("version", version))
	return nil
}
