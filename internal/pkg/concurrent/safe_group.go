// Package concurrent runs groups of goroutines that cannot crash the process.
package concurrent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"

	"golang.org/x/sync/errgroup"
)

// ErrPanic is wrapped by the error returned for a goroutine that panicked.
var ErrPanic = errors.New("goroutine panicked")

// SafeGroup is an errgroup.Group whose goroutines recover from panics. A
// panic is logged with its stack trace and reported by Wait like any other
// error, instead of killing the process.
//
// The context passed to each goroutine is cancelled as soon as one goroutine
// fails (returns an error or panics) or the parent context is cancelled, so
// the others can stop early.
type SafeGroup struct {
	g   *errgroup.Group
	ctx context.Context
	log *slog.Logger
}

// NewSafeGroup creates a group bound to ctx. log may be nil to use slog.Default().
func NewSafeGroup(ctx context.Context, log *slog.Logger) *SafeGroup {
	if log == nil {
		log = slog.Default()
	}
	g, gctx := errgroup.WithContext(ctx)
	return &SafeGroup{g: g, ctx: gctx, log: log}
}

// Go runs fn in a new goroutine.
func (sg *SafeGroup) Go(fn func(ctx context.Context) error) {
	sg.g.Go(func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				sg.log.LogAttrs(sg.ctx, slog.LevelError, "goroutine panic recovered",
					slog.Any("panic", r),
					slog.String("stack", string(debug.Stack())),
				)
				err = fmt.Errorf("%w: %v", ErrPanic, r)
			}
		}()
		return fn(sg.ctx)
	})
}

// Wait blocks until every goroutine has returned and reports the first error.
func (sg *SafeGroup) Wait() error {
	return sg.g.Wait()
}
