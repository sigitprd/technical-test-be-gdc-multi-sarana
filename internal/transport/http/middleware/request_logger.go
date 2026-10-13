// Package middleware contains the Echo middlewares of the API.
package middleware

import (
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"technical-test-be-gdc-multi-sarana/internal/apperror"
	"technical-test-be-gdc-multi-sarana/internal/pkg/logger"
	"technical-test-be-gdc-multi-sarana/internal/transport/http/response"
)

// HeaderRequestID is the header carrying the request id.
const HeaderRequestID = echo.HeaderXRequestID

// maxClientRequestIDLen caps how much of a client supplied X-Request-ID is logged.
const maxClientRequestIDLen = 128

// RequestLogger assigns a request id to every request, stores a request
// scoped logger in the request context and writes one structured log line
// per request once it has completed.
//
// The request id is always a new UUID generated here, so it is unique per
// request even when clients resend the same X-Request-ID. A client supplied
// X-Request-ID is logged as client_request_id for correlation. Level is INFO
// for 1xx-3xx, WARN for 4xx and ERROR for 5xx.
//
// It must be the outermost middleware so that it observes the final status,
// including errors turned into responses by the error handler.
func RequestLogger(base *slog.Logger) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			start := time.Now()
			req := c.Request()

			requestID := uuid.NewString()
			c.Set(response.RequestIDKey, requestID)
			c.Response().Header().Set(HeaderRequestID, requestID)

			reqLogger := base.With(slog.String("request_id", requestID))
			if clientID := req.Header.Get(HeaderRequestID); clientID != "" {
				if len(clientID) > maxClientRequestIDLen {
					clientID = clientID[:maxClientRequestIDLen]
				}
				reqLogger = reqLogger.With(slog.String("client_request_id", clientID))
			}
			c.SetRequest(req.WithContext(logger.WithContext(req.Context(), reqLogger)))

			err := next(c)
			if err != nil {
				// Render the error now so the logged status is the real one.
				c.Error(err)
			}

			status := c.Response().Status
			attrs := []slog.Attr{
				slog.String("method", req.Method),
				slog.String("path", req.URL.Path),
				slog.String("route", c.Path()),
				slog.Int("status", status),
				slog.Float64("latency_ms", float64(time.Since(start).Microseconds())/1000),
				slog.String("client_ip", c.RealIP()),
				slog.String("user_agent", req.UserAgent()),
				slog.Int64("bytes_out", c.Response().Size),
			}
			if userID, ok := UserIDFromContext(c); ok {
				attrs = append(attrs, slog.String("user_id", userID.String()))
			}
			if err != nil {
				appErr := apperror.From(err)
				attrs = append(attrs, slog.String("error_code", appErr.Code))
				if appErr.IsServerError() {
					attrs = append(attrs, slog.String("error", err.Error()))
				}
			}

			reqLogger.LogAttrs(req.Context(), LevelForStatus(status), "http request", attrs...)
			return nil
		}
	}
}

// LevelForStatus maps an HTTP status to a log level.
func LevelForStatus(status int) slog.Level {
	switch {
	case status >= 500:
		return slog.LevelError
	case status >= 400:
		return slog.LevelWarn
	default:
		return slog.LevelInfo
	}
}
