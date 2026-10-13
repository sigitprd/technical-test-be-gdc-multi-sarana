package concurrent_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"technical-test-be-gdc-multi-sarana/internal/pkg/concurrent"
)

func TestSafeGroup_WaitReturnsFirstError(t *testing.T) {
	boom := errors.New("boom")
	g := concurrent.NewSafeGroup(context.Background(), nil)

	g.Go(func(context.Context) error { return nil })
	g.Go(func(context.Context) error { return boom })

	assert.ErrorIs(t, g.Wait(), boom)
}

func TestSafeGroup_FailureCancelsTheOthers(t *testing.T) {
	g := concurrent.NewSafeGroup(context.Background(), nil)
	stopped := make(chan struct{})

	g.Go(func(ctx context.Context) error {
		<-ctx.Done()
		close(stopped)
		return nil
	})
	g.Go(func(context.Context) error { return errors.New("boom") })

	require.Error(t, g.Wait())
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("the context was not cancelled")
	}
}

func TestSafeGroup_RecoversPanics(t *testing.T) {
	var logs bytes.Buffer
	g := concurrent.NewSafeGroup(context.Background(), slog.New(slog.NewJSONHandler(&logs, nil)))

	g.Go(func(context.Context) error { panic("kaboom") })

	err := g.Wait()
	assert.ErrorIs(t, err, concurrent.ErrPanic)
	assert.Contains(t, err.Error(), "kaboom")
	assert.Contains(t, logs.String(), `"msg":"goroutine panic recovered"`)
	assert.Contains(t, logs.String(), `"stack":`)
}
