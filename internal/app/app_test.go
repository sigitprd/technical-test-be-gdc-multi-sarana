package app

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"technical-test-be-gdc-multi-sarana/internal/config"
)

func testConfig() config.Config {
	return config.Config{
		AppEnv:         config.EnvDevelopment,
		HTTPPort:       8080,
		DBMaxConns:     1,
		JWTSecret:      "0123456789abcdef0123456789abcdef",
		JWTTTL:         time.Hour,
		AuthRateLimit:  60,
		IdempotencyTTL: 24 * time.Hour,
	}
}

// syncBuffer is a bytes.Buffer safe for concurrent writes.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestServe_ServesUntilCancelled(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()
	mock.ExpectPing()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	var logs syncBuffer
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() { done <- Serve(ctx, testConfig(), slog.New(slog.NewJSONHandler(&logs, nil)), mock, ln) }()

	resp, err := http.Get("http://" + ln.Addr().String() + "/health")
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode, "the wired router answers")

	cancel()
	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not stop after cancellation")
	}
	assert.NoError(t, mock.ExpectationsWereMet())
	for _, msg := range []string{"http server listening", "shutting down", "shutdown complete"} {
		assert.Contains(t, logs.String(), `"msg":"`+msg+`"`)
	}
}

func TestServe_ReturnsServerErrors(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	require.NoError(t, ln.Close()) // serving on a closed listener fails at once

	err = Serve(context.Background(), testConfig(), slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)), mock, ln)

	assert.ErrorContains(t, err, "http server")
}

func TestRun_FailsWithoutDatabase(t *testing.T) {
	cfg := testConfig()
	cfg.DBURL = "postgres://user:pass@127.0.0.1:1/db?sslmode=disable"
	cfg.DBConnectRetries = 1

	err := Run(context.Background(), cfg, slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)))

	assert.ErrorContains(t, err, "connect database")
}

// fakeDeleter returns n deleted keys on the first call and an error afterwards.
type fakeDeleter struct {
	calls atomic.Int32
	n     int64
}

func (f *fakeDeleter) DeleteExpired(context.Context, time.Time) (int64, error) {
	if f.calls.Add(1) == 1 {
		return f.n, nil
	}
	return 0, errors.New("db down")
}

func TestCleanupIdempotencyKeys(t *testing.T) {
	var logs syncBuffer
	deleter := &fakeDeleter{n: 3}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		cleanupIdempotencyKeys(ctx, deleter, time.Millisecond, slog.New(slog.NewJSONHandler(&logs, nil)))
		close(done)
	}()
	require.Eventually(t, func() bool { return deleter.calls.Load() >= 3 }, 5*time.Second, time.Millisecond)
	cancel()
	<-done

	assert.Contains(t, logs.String(), `"msg":"expired idempotency keys deleted","count":3`)
	assert.Contains(t, logs.String(), `"msg":"cleanup idempotency keys failed","error":"db down"`, "a failure does not stop the job")
}

func TestCleanupIdempotencyKeys_NothingToDelete(t *testing.T) {
	var logs syncBuffer
	deleter := &fakeDeleter{n: 0}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		cleanupIdempotencyKeys(ctx, deleter, time.Millisecond, slog.New(slog.NewJSONHandler(&logs, nil)))
		close(done)
	}()
	require.Eventually(t, func() bool { return deleter.calls.Load() >= 1 }, 5*time.Second, time.Millisecond)
	cancel()
	<-done

	assert.NotContains(t, logs.String(), "expired idempotency keys deleted", "quiet when nothing expired")
}
