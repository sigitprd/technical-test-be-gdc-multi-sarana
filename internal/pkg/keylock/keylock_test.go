package keylock_test

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"technical-test-be-gdc-multi-sarana/internal/pkg/keylock"
)

func TestLocker_ConcurrentSameKeyIsSerialized(t *testing.T) {
	l := keylock.New()
	var inside, maxInside atomic.Int32
	var wg sync.WaitGroup

	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			unlock := l.Lock("key")
			defer unlock()
			n := inside.Add(1)
			for {
				m := maxInside.Load()
				if n <= m || maxInside.CompareAndSwap(m, n) {
					break
				}
			}
			time.Sleep(time.Millisecond)
			inside.Add(-1)
		}()
	}
	wg.Wait()

	assert.EqualValues(t, 1, maxInside.Load(), "never more than one holder per key")
	assert.Zero(t, l.Len(), "released keys are forgotten")
}

func TestLocker_ConcurrentDifferentKeysDoNotBlock(t *testing.T) {
	l := keylock.New()
	unlockA := l.Lock("a")
	defer unlockA()

	done := make(chan struct{})
	go func() {
		unlock := l.Lock("b")
		unlock()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("a lock on another key blocked")
	}
	assert.Equal(t, 1, l.Len())
}

func TestLocker_UnlockIsIdempotent(t *testing.T) {
	l := keylock.New()
	unlock := l.Lock("key")
	unlock()
	unlock()

	// The key is free again and the bookkeeping did not go negative.
	l.Lock("key")()
	assert.Zero(t, l.Len())
}
