// Package logger provides a JSON structured logger and helpers to carry a
// request scoped logger through context.Context.
package logger

import (
	"context"
	"io"
	"log/slog"
	"strings"
)

type ctxKey struct{}

// New creates a JSON logger writing to w at the given level
// ("debug", "info", "warn", "error"; defaults to info).
func New(w io.Writer, level string) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: ParseLevel(level)}))
}

// ParseLevel converts a textual level into a slog.Level, defaulting to info.
func ParseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// WithContext returns a copy of ctx that carries l.
func WithContext(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, l)
}

// FromContext returns the logger stored in ctx, or slog.Default().
func FromContext(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(ctxKey{}).(*slog.Logger); ok && l != nil {
		return l
	}
	return slog.Default()
}
