package middleware

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/labstack/echo/v4"

	"technical-test-be-gdc-multi-sarana/internal/apperror"
	"technical-test-be-gdc-multi-sarana/internal/pkg/logger"
)

// Recover turns a panic in a handler into a 500 error. The stack trace is
// logged and never sent to the client.
func Recover() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) (err error) {
			defer func() {
				r := recover()
				if r == nil {
					return
				}
				if r == http.ErrAbortHandler {
					panic(r)
				}
				ctx := c.Request().Context()
				logger.FromContext(ctx).LogAttrs(ctx, slog.LevelError, "panic recovered",
					slog.Any("panic", r),
					slog.String("stack", string(debug.Stack())),
				)
				err = apperror.Internal(fmt.Errorf("panic: %v", r))
			}()
			return next(c)
		}
	}
}
