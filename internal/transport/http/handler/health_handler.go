package handler

import (
	"context"
	"time"

	"github.com/labstack/echo/v4"

	"technical-test-be-gdc-multi-sarana/internal/apperror"
	"technical-test-be-gdc-multi-sarana/internal/transport/http/response"
)

// Pinger checks a dependency is reachable.
type Pinger interface {
	Ping(ctx context.Context) error
}

// HealthHandler serves the health check.
type HealthHandler struct {
	db Pinger
}

// NewHealthHandler creates a HealthHandler.
func NewHealthHandler(db Pinger) *HealthHandler {
	return &HealthHandler{db: db}
}

// Health handles GET /health. It reports the service as unavailable when the database is unreachable.
func (h *HealthHandler) Health(c echo.Context) error {
	ctx, cancel := context.WithTimeout(c.Request().Context(), 2*time.Second)
	defer cancel()
	if err := h.db.Ping(ctx); err != nil {
		return apperror.Unavailable(apperror.CodeUnavailable, "database is unreachable").Wrap(err)
	}
	return response.OK(c, map[string]string{"status": "ok"})
}
