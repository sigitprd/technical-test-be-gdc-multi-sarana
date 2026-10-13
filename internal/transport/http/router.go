// Package http wires the Echo server: middlewares, error handling and routes.
package http

import (
	"log/slog"
	"time"

	"github.com/labstack/echo/v4"
	echomw "github.com/labstack/echo/v4/middleware"

	"technical-test-be-gdc-multi-sarana/internal/transport/http/handler"
	"technical-test-be-gdc-multi-sarana/internal/transport/http/middleware"
)

// maxBodySize caps request bodies; larger requests get 413.
const maxBodySize = "1M"

// RouterConfig groups everything needed to build the HTTP server.
type RouterConfig struct {
	Logger         *slog.Logger
	ExposeInternal bool
	// AuthRateLimit is the number of /auth requests allowed per client IP per
	// minute. Zero disables the limit.
	AuthRateLimit int
	TokenParser   middleware.TokenParser
	Health        *handler.HealthHandler
	Auth          *handler.AuthHandler
	Users         *handler.UserHandler
	Tasks         *handler.TaskHandler
	// Clock stamps error responses; nil means time.Now.
	Clock func() time.Time
}

// NewRouter builds the Echo instance with every route registered.
func NewRouter(cfg RouterConfig) *echo.Echo {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	e.Validator = handler.NewValidator()
	e.HTTPErrorHandler = middleware.ErrorHandler(cfg.ExposeInternal, cfg.Clock)
	// Take the client IP from the TCP connection. X-Forwarded-For and
	// X-Real-IP are ignored because anyone can set them to dodge the rate
	// limit; behind a trusted proxy use echo.ExtractIPFromXFFHeader instead.
	e.IPExtractor = echo.ExtractIPDirect()

	// Order matters: the request logger must wrap everything so it sees the
	// final status, and Recover must sit right inside it so panics are
	// converted into errors that still get logged and rendered.
	e.Use(
		middleware.RequestLogger(cfg.Logger),
		middleware.Recover(),
		echomw.SecureWithConfig(echomw.SecureConfig{
			XSSProtection:      "0",
			ContentTypeNosniff: "nosniff",
			XFrameOptions:      "DENY",
			ReferrerPolicy:     "no-referrer",
		}),
		echomw.BodyLimit(maxBodySize),
	)

	e.GET("/health", cfg.Health.Health)

	v1 := e.Group("/api/v1")

	// Middlewares are attached per route rather than with Group.Use: a group
	// middleware makes Echo register catch-all routes, which turns 405 Method
	// Not Allowed into 404 and answers unknown paths with 401 instead of 404.
	authRateLimit := middleware.RateLimit(cfg.AuthRateLimit)
	requireAuth := middleware.JWTAuth(cfg.TokenParser)

	v1.POST("/auth/register", cfg.Auth.Register, authRateLimit)
	v1.POST("/auth/login", cfg.Auth.Login, authRateLimit)

	v1.GET("/users/me", cfg.Users.Me, requireAuth)
	v1.GET("/users/team-members", cfg.Users.TeamMembers, requireAuth)

	v1.POST("/tasks", cfg.Tasks.Create, requireAuth)
	v1.GET("/tasks", cfg.Tasks.List, requireAuth)
	v1.GET("/tasks/:id", cfg.Tasks.Get, requireAuth)
	v1.PUT("/tasks/:id", cfg.Tasks.Update, requireAuth)
	v1.DELETE("/tasks/:id", cfg.Tasks.Delete, requireAuth)
	v1.POST("/tasks/:id/assign", cfg.Tasks.Assign, requireAuth)

	return e
}
