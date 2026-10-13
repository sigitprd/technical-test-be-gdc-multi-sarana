// Package keylock provides a mutex per key, so that work on the same key is
// serialized while work on different keys runs in parallel.
package keylock

import "sync"

type entry struct {
	mu   sync.Mutex
	refs int
}

// Locker hands out per-key locks. Entries are removed once no goroutine holds
// or waits for them, so memory does not grow with the number of keys seen.
type Locker struct {
	mu    sync.Mutex
	locks map[string]*entry
}

// New creates an empty Locker.
func New() *Locker {
	return &Locker{locks: make(map[string]*entry)}
}

// Lock blocks until the lock for key is acquired and returns its unlock
// function. Calling unlock more than once is a no-op.
func (l *Locker) Lock(key string) (unlock func()) {
	l.mu.Lock()
	e, ok := l.locks[key]
	if !ok {
		e = &entry{}
		l.locks[key] = e
	}
	e.refs++
	l.mu.Unlock()

	e.mu.Lock()

	var once sync.Once
	return func() {
		once.Do(func() {
			e.mu.Unlock()
			l.mu.Lock()
			e.refs--
			if e.refs == 0 {
				delete(l.locks, key)
			}
			l.mu.Unlock()
		})
	}
}

// Len returns the number of keys currently tracked. Useful for tests.
func (l *Locker) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.locks)
}
