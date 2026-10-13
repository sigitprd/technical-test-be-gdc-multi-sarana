// Package servicetest provides in-memory implementations of the service ports
// so the use cases can be unit tested without a database.
//
// The fakes reproduce the database behavior the services rely on:
//   - TxManager rolls back every change made inside a failed transaction;
//   - IdempotencyRepository.Claim holds a per-key lock until the surrounding
//     transaction ends, like the row lock PostgreSQL takes for
//     INSERT ... ON CONFLICT, so a concurrent claim of the same key waits for
//     the first transaction to commit or roll back;
//   - TaskRepository.GetByIDForUpdate holds a per-task lock the same way.
package servicetest

import (
	"context"
	"sync"
	"sync/atomic"
)

type txKey struct{}

// tx collects what to undo on rollback and which locks to release when the
// transaction ends.
type tx struct {
	mu      sync.Mutex
	undo    []func()
	release []func()
}

// TxManager implements service.TxManager.
type TxManager struct {
	// BeginErr, when set, is returned instead of starting a transaction.
	BeginErr error

	commits   atomic.Int64
	rollbacks atomic.Int64
}

// WithinTx runs fn in a transaction. Nested calls join the outer transaction.
func (m *TxManager) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if _, ok := ctx.Value(txKey{}).(*tx); ok {
		return fn(ctx)
	}
	if m.BeginErr != nil {
		return m.BeginErr
	}

	t := &tx{}
	err := fn(context.WithValue(ctx, txKey{}, t))

	t.mu.Lock()
	undo, release := t.undo, t.release
	t.mu.Unlock()
	if err != nil {
		for i := len(undo) - 1; i >= 0; i-- {
			undo[i]()
		}
		m.rollbacks.Add(1)
	} else {
		m.commits.Add(1)
	}
	for _, unlock := range release {
		unlock()
	}
	return err
}

// Commits returns the number of committed transactions.
func (m *TxManager) Commits() int64 { return m.commits.Load() }

// Rollbacks returns the number of rolled back transactions.
func (m *TxManager) Rollbacks() int64 { return m.rollbacks.Load() }

// onRollback registers undo to run if the transaction in ctx rolls back.
// Outside a transaction a change is final, so undo is dropped.
func onRollback(ctx context.Context, undo func()) {
	if t, ok := ctx.Value(txKey{}).(*tx); ok {
		t.mu.Lock()
		t.undo = append(t.undo, undo)
		t.mu.Unlock()
	}
}

// holdUntilTxEnd keeps a lock until the transaction in ctx ends. Outside a
// transaction, as for an auto-committed statement, it is released at once.
func holdUntilTxEnd(ctx context.Context, unlock func()) {
	if t, ok := ctx.Value(txKey{}).(*tx); ok {
		t.mu.Lock()
		t.release = append(t.release, unlock)
		t.mu.Unlock()
		return
	}
	unlock()
}
