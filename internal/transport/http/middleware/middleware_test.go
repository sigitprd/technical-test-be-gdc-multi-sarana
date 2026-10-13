package middleware

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"technical-test-be-gdc-multi-sarana/internal/apperror"
	"technical-test-be-gdc-multi-sarana/internal/transport/http/response"
)

var errBoom = errors.New("boom")

// newEcho returns an Echo instance with the request logger, Recover and the
// global error handler installed, and a buffer receiving the logs.
func newEcho(exposeInternal bool) (*echo.Echo, *bytes.Buffer) {
	var logs bytes.Buffer
	e := echo.New()
	e.HTTPErrorHandler = ErrorHandler(exposeInternal, func() time.Time {
		return time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
	})
	e.Use(RequestLogger(slog.New(slog.NewJSONHandler(&logs, nil))), Recover())
	return e, &logs
}

func serve(e *echo.Echo, method, target string, header map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// logLines returns the "http request" log lines.
func logLines(t *testing.T, logs *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(logs.Bytes()), []byte("\n")) {
		var m map[string]any
		require.NoError(t, json.Unmarshal(line, &m), string(line))
		if m["msg"] == "http request" {
			out = append(out, m)
		}
	}
	return out
}

func errorBody(t *testing.T, rec *httptest.ResponseRecorder) response.Error {
	t.Helper()
	var body response.Error
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), rec.Body.String())
	return body
}

func TestRequestLogger_FieldsAndLevels(t *testing.T) {
	e, logs := newEcho(false)
	userID := uuid.New()
	e.GET("/ok", func(c echo.Context) error {
		c.Set(userIDKey, userID)
		return c.NoContent(http.StatusOK)
	})
	e.GET("/client-error", func(echo.Context) error { return apperror.NotFound("TASK_NOT_FOUND", "task not found") })
	e.GET("/server-error", func(echo.Context) error { return errBoom })

	ok := serve(e, http.MethodGet, "/ok", nil)
	serve(e, http.MethodGet, "/client-error", nil)
	serve(e, http.MethodGet, "/server-error", nil)

	lines := logLines(t, logs)
	require.Len(t, lines, 3)

	info := lines[0]
	assert.Equal(t, "INFO", info["level"])
	assert.Equal(t, http.MethodGet, info["method"])
	assert.Equal(t, "/ok", info["path"])
	assert.Equal(t, "/ok", info["route"])
	assert.EqualValues(t, 200, info["status"])
	assert.Contains(t, info, "latency_ms")
	assert.Equal(t, userID.String(), info["user_id"])
	assert.Equal(t, ok.Header().Get(HeaderRequestID), info["request_id"], "the logged id is the one returned to the client")
	_, err := uuid.Parse(info["request_id"].(string))
	assert.NoError(t, err)

	warn := lines[1]
	assert.Equal(t, "WARN", warn["level"])
	assert.Equal(t, "TASK_NOT_FOUND", warn["error_code"])
	assert.NotContains(t, warn, "error", "client errors do not log internals")

	errLine := lines[2]
	assert.Equal(t, "ERROR", errLine["level"])
	assert.Equal(t, apperror.CodeInternal, errLine["error_code"])
	assert.Contains(t, errLine["error"], "boom", "server errors log their cause")
}

func TestRequestLogger_RequestIDIsAlwaysNew(t *testing.T) {
	e, logs := newEcho(false)
	e.GET("/", func(c echo.Context) error { return c.NoContent(http.StatusNoContent) })
	clientID := uuid.NewString()

	first := serve(e, http.MethodGet, "/", map[string]string{HeaderRequestID: clientID})
	second := serve(e, http.MethodGet, "/", map[string]string{HeaderRequestID: clientID})
	serve(e, http.MethodGet, "/", map[string]string{HeaderRequestID: strings.Repeat("x", 500)})

	assert.NotEqual(t, clientID, first.Header().Get(HeaderRequestID))
	assert.NotEqual(t, first.Header().Get(HeaderRequestID), second.Header().Get(HeaderRequestID))

	lines := logLines(t, logs)
	require.Len(t, lines, 3)
	assert.Equal(t, clientID, lines[0]["client_request_id"])
	assert.Len(t, lines[2]["client_request_id"], maxClientRequestIDLen, "long client ids are truncated")
}

func TestLevelForStatus(t *testing.T) {
	assert.Equal(t, slog.LevelInfo, LevelForStatus(200))
	assert.Equal(t, slog.LevelInfo, LevelForStatus(304))
	assert.Equal(t, slog.LevelWarn, LevelForStatus(400))
	assert.Equal(t, slog.LevelWarn, LevelForStatus(499))
	assert.Equal(t, slog.LevelError, LevelForStatus(500))
}

func TestRecover_TurnsPanicIntoInternalError(t *testing.T) {
	e, logs := newEcho(false)
	e.GET("/panic", func(echo.Context) error { panic("db password is hunter2") })

	rec := serve(e, http.MethodGet, "/panic", nil)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	body := errorBody(t, rec)
	assert.Equal(t, apperror.CodeInternal, body.Code)
	assert.NotContains(t, rec.Body.String(), "hunter2", "the panic value is not exposed")
	assert.Contains(t, logs.String(), `"msg":"panic recovered"`)
	assert.Contains(t, logs.String(), `"stack":`)
}

func TestRecover_RepanicsAbortHandler(t *testing.T) {
	h := Recover()(func(echo.Context) error { panic(http.ErrAbortHandler) })
	c := echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/", nil), httptest.NewRecorder())

	assert.PanicsWithValue(t, http.ErrAbortHandler, func() { _ = h(c) })
}

type stubParser struct {
	id  uuid.UUID
	err error
}

func (p stubParser) Parse(string) (uuid.UUID, error) { return p.id, p.err }

func TestJWTAuth(t *testing.T) {
	userID := uuid.New()
	tests := []struct {
		name   string
		header string
		parser stubParser
		code   string
	}{
		{"missing header", "", stubParser{id: userID}, "MISSING_TOKEN"},
		{"other scheme", "Basic abc", stubParser{id: userID}, "MISSING_TOKEN"},
		{"empty token", "Bearer   ", stubParser{id: userID}, "MISSING_TOKEN"},
		{"invalid token", "Bearer abc", stubParser{err: errBoom}, "INVALID_TOKEN"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			h := JWTAuth(tc.parser)(func(echo.Context) error { called = true; return nil })
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.header != "" {
				req.Header.Set(echo.HeaderAuthorization, tc.header)
			}

			err := h(echo.New().NewContext(req, httptest.NewRecorder()))

			var appErr *apperror.Error
			require.ErrorAs(t, err, &appErr)
			assert.Equal(t, apperror.KindUnauthorized, appErr.Kind)
			assert.Equal(t, tc.code, appErr.Code)
			assert.False(t, called)
		})
	}

	t.Run("valid token", func(t *testing.T) {
		var got uuid.UUID
		h := JWTAuth(stubParser{id: userID})(func(c echo.Context) error {
			got, _ = UserIDFromContext(c)
			return nil
		})
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set(echo.HeaderAuthorization, "bearer  token ")

		require.NoError(t, h(echo.New().NewContext(req, httptest.NewRecorder())))
		assert.Equal(t, userID, got)
	})

	t.Run("no user in context", func(t *testing.T) {
		_, ok := UserIDFromContext(echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/", nil), httptest.NewRecorder()))
		assert.False(t, ok)
	})
}

func TestErrorHandler_Format(t *testing.T) {
	e, _ := newEcho(false)
	e.GET("/validation", func(echo.Context) error {
		return apperror.Validation([]map[string]string{{"field": "title", "message": "is required"}})
	})

	rec := serve(e, http.MethodGet, "/validation", nil)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.JSONEq(t, `{
		"status": 400,
		"code": "VALIDATION_ERROR",
		"message": "request validation failed",
		"details": [{"field": "title", "message": "is required"}],
		"request_id": "`+rec.Header().Get(HeaderRequestID)+`",
		"timestamp": "2026-10-13T09:00:00Z"
	}`, rec.Body.String())
}

func TestErrorHandler_EchoErrors(t *testing.T) {
	e, _ := newEcho(false)
	e.GET("/only-get", func(c echo.Context) error { return c.NoContent(http.StatusOK) })
	e.GET("/teapot", func(echo.Context) error { return echo.NewHTTPError(http.StatusTeapot) })
	e.GET("/bad-gateway", func(echo.Context) error { return echo.NewHTTPError(http.StatusBadGateway, "upstream") })
	e.GET("/with-internal", func(echo.Context) error {
		return echo.NewHTTPError(http.StatusBadRequest, map[string]string{"not": "a string"}).SetInternal(errBoom)
	})
	e.GET("/too-large", func(echo.Context) error { return echo.ErrStatusRequestEntityTooLarge })

	tests := []struct {
		method, target string
		status         int
		code, message  string
	}{
		{http.MethodGet, "/missing", http.StatusNotFound, apperror.CodeNotFound, "Not Found"},
		{http.MethodPost, "/only-get", http.StatusMethodNotAllowed, apperror.CodeMethodNotAllowed, "Method Not Allowed"},
		{http.MethodGet, "/teapot", http.StatusBadRequest, apperror.CodeBadRequest, "I'm a teapot"},
		{http.MethodGet, "/bad-gateway", http.StatusInternalServerError, apperror.CodeInternal, "an unexpected error occurred"},
		{http.MethodGet, "/with-internal", http.StatusBadRequest, apperror.CodeBadRequest, "Bad Request"},
		{http.MethodGet, "/too-large", http.StatusRequestEntityTooLarge, apperror.CodePayloadTooLarge, "Request Entity Too Large"},
	}
	for _, tc := range tests {
		t.Run(tc.target, func(t *testing.T) {
			rec := serve(e, tc.method, tc.target, nil)
			assert.Equal(t, tc.status, rec.Code)
			body := errorBody(t, rec)
			assert.Equal(t, tc.status, body.Status)
			assert.Equal(t, tc.code, body.Code)
			assert.Equal(t, tc.message, body.Message)
		})
	}
}

func TestErrorHandler_DebugOnlyOutsideProduction(t *testing.T) {
	for _, expose := range []bool{true, false} {
		e, _ := newEcho(expose)
		e.GET("/boom", func(echo.Context) error { return errBoom })
		e.GET("/client", func(echo.Context) error { return apperror.BadRequest("C", "m").Wrap(errBoom) })

		server := errorBody(t, serve(e, http.MethodGet, "/boom", nil))
		client := errorBody(t, serve(e, http.MethodGet, "/client", nil))

		if expose {
			assert.Equal(t, "boom", server.Debug)
		} else {
			assert.Empty(t, server.Debug, "production never exposes internals")
		}
		assert.Empty(t, client.Debug, "client errors never carry debug details")
	}
}

func TestErrorHandler_HeadAndCommittedResponses(t *testing.T) {
	e, _ := newEcho(false)
	e.HEAD("/missing-head", func(echo.Context) error { return apperror.NotFound("C", "m") })
	e.GET("/committed", func(c echo.Context) error {
		_ = c.String(http.StatusAccepted, "partial")
		return errBoom
	})

	head := serve(e, http.MethodHead, "/missing-head", nil)
	assert.Equal(t, http.StatusNotFound, head.Code)
	assert.Empty(t, head.Body.String())

	committed := serve(e, http.MethodGet, "/committed", nil)
	assert.Equal(t, http.StatusAccepted, committed.Code, "an already written response is left alone")
	assert.Equal(t, "partial", committed.Body.String())
}

// failingWriter fails every body write.
type failingWriter struct{ *httptest.ResponseRecorder }

func (failingWriter) Write([]byte) (int, error) { return 0, errBoom }

func TestErrorHandler_LogsWriteFailures(t *testing.T) {
	e, logs := newEcho(false)
	e.GET("/", func(echo.Context) error { return apperror.NotFound("C", "m") })

	e.ServeHTTP(failingWriter{httptest.NewRecorder()}, httptest.NewRequest(http.MethodGet, "/", nil))

	assert.Contains(t, logs.String(), `"msg":"write error response failed"`)
}

func TestErrorHandler_DefaultClock(t *testing.T) {
	e := echo.New()
	e.HTTPErrorHandler = ErrorHandler(false, nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/missing", nil))

	ts, err := time.Parse(time.RFC3339Nano, errorBody(t, rec).Timestamp)
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now(), ts, time.Minute)
}

func TestStatusForKind(t *testing.T) {
	assert.Equal(t, http.StatusTooManyRequests, StatusForKind(apperror.KindTooManyRequests))
	assert.Equal(t, http.StatusServiceUnavailable, StatusForKind(apperror.KindUnavailable))
	assert.Equal(t, http.StatusInternalServerError, StatusForKind(apperror.Kind(255)), "unknown kinds are server errors")
}

func TestRateLimit(t *testing.T) {
	t.Run("disabled", func(t *testing.T) {
		e, _ := newEcho(false)
		e.GET("/", func(c echo.Context) error { return c.NoContent(http.StatusOK) }, RateLimit(0))
		for range 5 {
			assert.Equal(t, http.StatusOK, serve(e, http.MethodGet, "/", nil).Code)
		}
	})

	t.Run("limit exceeded", func(t *testing.T) {
		e, _ := newEcho(false)
		e.IPExtractor = echo.ExtractIPDirect()
		e.GET("/", func(c echo.Context) error { return c.NoContent(http.StatusOK) }, RateLimit(2))

		assert.Equal(t, http.StatusOK, serve(e, http.MethodGet, "/", nil).Code)
		assert.Equal(t, http.StatusOK, serve(e, http.MethodGet, "/", nil).Code)
		rec := serve(e, http.MethodGet, "/", map[string]string{"X-Forwarded-For": "10.0.0.1"})

		assert.Equal(t, http.StatusTooManyRequests, rec.Code, "spoofed proxy headers do not reset the limit")
		assert.Equal(t, "RATE_LIMIT_EXCEEDED", errorBody(t, rec).Code)
	})
}
