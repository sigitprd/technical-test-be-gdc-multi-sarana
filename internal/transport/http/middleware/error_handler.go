package middleware

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"technical-test-be-gdc-multi-sarana/internal/apperror"
	"technical-test-be-gdc-multi-sarana/internal/pkg/logger"
	"technical-test-be-gdc-multi-sarana/internal/transport/http/response"
)

// ErrorHandler is the global Echo error handler. Every error, whether returned
// by a handler, a middleware or Echo itself (unknown route, body too large,
// ...), is rendered with the same JSON shape.
//
// Server errors are rendered with a generic message. Their cause is attached
// as "debug" only when exposeInternal is true (never in production).
func ErrorHandler(exposeInternal bool, now func() time.Time) echo.HTTPErrorHandler {
	if now == nil {
		now = time.Now
	}
	return func(err error, c echo.Context) {
		if c.Response().Committed {
			return
		}

		appErr := toAppError(err)
		status := StatusForKind(appErr.Kind)
		body := response.Error{
			Status:    status,
			Code:      appErr.Code,
			Message:   appErr.Message,
			Details:   appErr.Details,
			RequestID: response.RequestID(c),
			Timestamp: response.Timestamp(now()),
		}
		if exposeInternal && appErr.IsServerError() && appErr.Err != nil {
			body.Debug = appErr.Err.Error()
		}

		var writeErr error
		if c.Request().Method == http.MethodHead {
			writeErr = c.NoContent(status)
		} else {
			writeErr = c.JSON(status, body)
		}
		if writeErr != nil {
			ctx := c.Request().Context()
			logger.FromContext(ctx).LogAttrs(ctx, slog.LevelError, "write error response failed",
				slog.String("error", writeErr.Error()))
		}
	}
}

// kindStatus maps transport agnostic error kinds to HTTP status codes.
var kindStatus = map[apperror.Kind]int{
	apperror.KindValidation:       http.StatusBadRequest,
	apperror.KindBadRequest:       http.StatusBadRequest,
	apperror.KindUnauthorized:     http.StatusUnauthorized,
	apperror.KindForbidden:        http.StatusForbidden,
	apperror.KindNotFound:         http.StatusNotFound,
	apperror.KindMethodNotAllowed: http.StatusMethodNotAllowed,
	apperror.KindConflict:         http.StatusConflict,
	apperror.KindPayloadTooLarge:  http.StatusRequestEntityTooLarge,
	apperror.KindUnprocessable:    http.StatusUnprocessableEntity,
	apperror.KindTooManyRequests:  http.StatusTooManyRequests,
	apperror.KindUnavailable:      http.StatusServiceUnavailable,
	apperror.KindInternal:         http.StatusInternalServerError,
}

// StatusForKind returns the HTTP status for an error kind.
func StatusForKind(kind apperror.Kind) int {
	if status, ok := kindStatus[kind]; ok {
		return status
	}
	return http.StatusInternalServerError
}

// statusKind is the reverse mapping, used for errors raised by Echo itself.
var statusKind = map[int]struct {
	kind apperror.Kind
	code string
}{
	http.StatusBadRequest:            {apperror.KindBadRequest, apperror.CodeBadRequest},
	http.StatusUnauthorized:          {apperror.KindUnauthorized, apperror.CodeUnauthorized},
	http.StatusForbidden:             {apperror.KindForbidden, apperror.CodeForbidden},
	http.StatusNotFound:              {apperror.KindNotFound, apperror.CodeNotFound},
	http.StatusMethodNotAllowed:      {apperror.KindMethodNotAllowed, apperror.CodeMethodNotAllowed},
	http.StatusConflict:              {apperror.KindConflict, apperror.CodeConflict},
	http.StatusRequestEntityTooLarge: {apperror.KindPayloadTooLarge, apperror.CodePayloadTooLarge},
	http.StatusUnprocessableEntity:   {apperror.KindUnprocessable, apperror.CodeUnprocessable},
	http.StatusTooManyRequests:       {apperror.KindTooManyRequests, apperror.CodeTooManyRequests},
	http.StatusServiceUnavailable:    {apperror.KindUnavailable, apperror.CodeUnavailable},
}

func toAppError(err error) *apperror.Error {
	var appErr *apperror.Error
	if errors.As(err, &appErr) {
		return appErr
	}

	var httpErr *echo.HTTPError
	if errors.As(err, &httpErr) {
		mapped, ok := statusKind[httpErr.Code]
		if !ok {
			if httpErr.Code >= http.StatusInternalServerError {
				return apperror.Internal(fmt.Errorf("echo: %w", err))
			}
			mapped.kind, mapped.code = apperror.KindBadRequest, apperror.CodeBadRequest
		}
		message := http.StatusText(httpErr.Code)
		if m, ok := httpErr.Message.(string); ok && m != "" {
			message = m
		}
		e := apperror.New(mapped.kind, mapped.code, message)
		if httpErr.Internal != nil {
			e = e.Wrap(httpErr.Internal)
		}
		return e
	}

	return apperror.Internal(err)
}
