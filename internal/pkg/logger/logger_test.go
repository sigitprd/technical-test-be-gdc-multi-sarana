package logger_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"technical-test-be-gdc-multi-sarana/internal/pkg/logger"
)

func TestParseLevel(t *testing.T) {
	tests := map[string]slog.Level{
		"debug":   slog.LevelDebug,
		" INFO ":  slog.LevelInfo,
		"warn":    slog.LevelWarn,
		"warning": slog.LevelWarn,
		"error":   slog.LevelError,
		"unknown": slog.LevelInfo,
		"":        slog.LevelInfo,
	}
	for in, want := range tests {
		assert.Equal(t, want, logger.ParseLevel(in), in)
	}
}

func TestNew_WritesJSONAtLevel(t *testing.T) {
	var buf bytes.Buffer
	l := logger.New(&buf, "warn")

	l.Info("skipped")
	l.Warn("kept", slog.String("k", "v"))

	var line map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &line), "exactly one JSON line")
	assert.Equal(t, "WARN", line["level"])
	assert.Equal(t, "kept", line["msg"])
	assert.Equal(t, "v", line["k"])
}

func TestContext(t *testing.T) {
	assert.Same(t, slog.Default(), logger.FromContext(context.Background()))

	l := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	assert.Same(t, l, logger.FromContext(logger.WithContext(context.Background(), l)))
}
