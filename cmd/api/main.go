// Command api runs the Task Management REST API.
package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"technical-test-be-gdc-multi-sarana/internal/app"
	"technical-test-be-gdc-multi-sarana/internal/config"
	"technical-test-be-gdc-multi-sarana/internal/pkg/logger"
)

func main() {
	os.Exit(run(os.Getenv, os.Stdout, os.Stderr))
}

// run starts the API and returns the process exit code. It is separate from
// main so it can be tested.
func run(getenv func(string) string, stdout, stderr io.Writer) int {
	cfg, err := config.Load(getenv)
	if err != nil {
		logger.New(stderr, "error").Error("invalid configuration", slog.String("error", err.Error()))
		return 1
	}

	log := logger.New(stdout, cfg.LogLevel)
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.Run(ctx, cfg, log); err != nil {
		log.Error("application stopped with error", slog.String("error", err.Error()))
		return 1
	}
	return 0
}
