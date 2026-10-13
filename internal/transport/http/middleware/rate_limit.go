package middleware

import (
	"time"

	"github.com/labstack/echo/v4"
	echomw "github.com/labstack/echo/v4/middleware"
	"golang.org/x/time/rate"

	"technical-test-be-gdc-multi-sarana/internal/apperror"
)

var errRateLimited = apperror.TooManyRequests("RATE_LIMIT_EXCEEDED", "too many requests, please retry later")

// RateLimit allows each client IP perMinute requests per minute, with bursts
// of up to perMinute requests. A perMinute of zero or less disables limiting.
//
// The client IP comes from echo.Context.RealIP, so the router's IPExtractor
// decides whether proxy headers are trusted. Counters live in memory, so the
// limit applies per API instance.
func RateLimit(perMinute int) echo.MiddlewareFunc {
	if perMinute <= 0 {
		return func(next echo.HandlerFunc) echo.HandlerFunc { return next }
	}
	return echomw.RateLimiterWithConfig(echomw.RateLimiterConfig{
		Store: echomw.NewRateLimiterMemoryStoreWithConfig(echomw.RateLimiterMemoryStoreConfig{
			Rate:      rate.Limit(float64(perMinute) / 60),
			Burst:     perMinute,
			ExpiresIn: 3 * time.Minute,
		}),
		IdentifierExtractor: func(c echo.Context) (string, error) {
			return c.RealIP(), nil
		},
		DenyHandler: func(echo.Context, string, error) error {
			return errRateLimited
		},
	})
}
