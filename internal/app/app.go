// Package app is the composition root: it builds every dependency and runs
// the HTTP server and background jobs until the context is cancelled.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"technical-test-be-gdc-multi-sarana/internal/config"
	"technical-test-be-gdc-multi-sarana/internal/notifier"
	"technical-test-be-gdc-multi-sarana/internal/pkg/concurrent"
	"technical-test-be-gdc-multi-sarana/internal/pkg/hasher"
	"technical-test-be-gdc-multi-sarana/internal/pkg/keylock"
	"technical-test-be-gdc-multi-sarana/internal/pkg/token"
	"technical-test-be-gdc-multi-sarana/internal/repository/postgres"
	"technical-test-be-gdc-multi-sarana/internal/service"
	httptransport "technical-test-be-gdc-multi-sarana/internal/transport/http"
	"technical-test-be-gdc-multi-sarana/internal/transport/http/handler"
)

// Fixed settings that are not worth exposing as configuration.
const (
	jwtIssuer                  = "task-management-api"
	bcryptCost                 = 12
	idempotencyCleanupInterval = time.Hour
	shutdownTimeout            = 10 * time.Second
)

// Database is what the application needs from the connection pool.
type Database interface {
	postgres.DB
	Ping(ctx context.Context) error
}

// Run connects to the database, applies migrations and serves the API on
// cfg.HTTPPort until ctx is cancelled, then shuts down gracefully.
func Run(ctx context.Context, cfg config.Config, log *slog.Logger) error {
	pool, err := postgres.NewPool(ctx, postgres.PoolConfig{
		DSN:            cfg.DBURL,
		MaxConns:       cfg.DBMaxConns,
		ConnectRetries: cfg.DBConnectRetries,
	})
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer pool.Close()
	log.Info("database connected")

	if cfg.DBAutoMigrate {
		if err := postgres.Migrate(ctx, pool, log); err != nil {
			return fmt.Errorf("migrate database: %w", err)
		}
	}

	ln, err := net.Listen("tcp", ":"+strconv.Itoa(cfg.HTTPPort))
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	return Serve(ctx, cfg, log, pool, ln)
}

// Serve wires the application on db and serves HTTP on ln until ctx is
// cancelled. It closes ln.
func Serve(ctx context.Context, cfg config.Config, log *slog.Logger, db Database, ln net.Listener) error {
	txManager := postgres.NewTxManager(db)
	teamRepo := postgres.NewTeamRepository(db)
	userRepo := postgres.NewUserRepository(db)
	taskRepo := postgres.NewTaskRepository(db)
	taskLogRepo := postgres.NewTaskLogRepository(db)
	idempotencyRepo := postgres.NewIdempotencyRepository(db)

	tokens := token.NewManager(cfg.JWTSecret, jwtIssuer, cfg.JWTTTL, nil)

	authService := service.NewAuthService(txManager, teamRepo, userRepo, hasher.NewBcrypt(bcryptCost), tokens)
	userService := service.NewUserService(userRepo, teamRepo)
	taskService := service.NewTaskService(service.TaskServiceDeps{
		Tx:             txManager,
		Tasks:          taskRepo,
		TaskLogs:       taskLogRepo,
		Users:          userRepo,
		Idempotency:    idempotencyRepo,
		Notifier:       notifier.NewLogNotifier(cfg.NotifierSimulateFailure),
		Locker:         keylock.New(),
		IdempotencyTTL: cfg.IdempotencyTTL,
	})

	router := httptransport.NewRouter(httptransport.RouterConfig{
		Logger:         log,
		ExposeInternal: !cfg.IsProduction(),
		AuthRateLimit:  cfg.AuthRateLimit,
		TokenParser:    tokens,
		Health:         handler.NewHealthHandler(db),
		Auth:           handler.NewAuthHandler(authService),
		Users:          handler.NewUserHandler(userService),
		Tasks:          handler.NewTaskHandler(taskService),
	})

	server := &http.Server{
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		// net/http reports connection level errors here; keep them JSON too.
		ErrorLog: slog.NewLogLogger(log.Handler(), slog.LevelError),
	}

	// The server, its shutdown and the cleanup job run in one group: a
	// signal, a server failure or a panic in any of them stops all three.
	group := concurrent.NewSafeGroup(ctx, log)
	group.Go(func(context.Context) error {
		log.Info("http server listening", slog.String("addr", ln.Addr().String()), slog.String("env", cfg.AppEnv))
		if err := server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("http server: %w", err)
		}
		return nil
	})
	group.Go(func(ctx context.Context) error {
		<-ctx.Done()
		log.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown http server: %w", err)
		}
		return nil
	})
	group.Go(func(ctx context.Context) error {
		cleanupIdempotencyKeys(ctx, idempotencyRepo, idempotencyCleanupInterval, log)
		return nil
	})

	if err := group.Wait(); err != nil {
		return err
	}
	log.Info("shutdown complete")
	return nil
}

// expiredKeyDeleter removes expired idempotency keys.
type expiredKeyDeleter interface {
	DeleteExpired(ctx context.Context, now time.Time) (int64, error)
}

// cleanupIdempotencyKeys periodically deletes expired idempotency keys so the
// table does not grow forever. Expired keys are already ignored by Claim, so
// this is housekeeping only.
func cleanupIdempotencyKeys(ctx context.Context, repo expiredKeyDeleter, every time.Duration, log *slog.Logger) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := repo.DeleteExpired(ctx, time.Now())
			if err != nil {
				log.Error("cleanup idempotency keys failed", slog.String("error", err.Error()))
				continue
			}
			if n > 0 {
				log.Info("expired idempotency keys deleted", slog.Int64("count", n))
			}
		}
	}
}
