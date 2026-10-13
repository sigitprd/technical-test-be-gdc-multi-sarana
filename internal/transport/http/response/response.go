// Package response renders the JSON envelopes used by every endpoint.
package response

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
)

// RequestIDKey is the echo context key holding the request id.
const RequestIDKey = "request_id"

// Success is the envelope for successful responses.
type Success struct {
	Data any   `json:"data"`
	Meta *Meta `json:"meta,omitempty"`
}

// Meta carries pagination information.
type Meta struct {
	Page       int   `json:"page"`
	Limit      int   `json:"limit"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
}

// Error is the envelope for error responses.
type Error struct {
	Status    int    `json:"status"`
	Code      string `json:"code"`
	Message   string `json:"message"`
	Details   any    `json:"details,omitempty"`
	RequestID string `json:"request_id,omitempty"`
	Timestamp string `json:"timestamp"`
	// Debug carries the internal cause of a 5xx error. Only populated
	// outside production.
	Debug string `json:"debug,omitempty"`
}

// OK writes a 200 response.
func OK(c echo.Context, data any) error {
	return c.JSON(http.StatusOK, Success{Data: data})
}

// Created writes a 201 response.
func Created(c echo.Context, data any) error {
	return c.JSON(http.StatusCreated, Success{Data: data})
}

// Paginated writes a 200 response with pagination metadata.
func Paginated(c echo.Context, data any, meta Meta) error {
	return c.JSON(http.StatusOK, Success{Data: data, Meta: &meta})
}

// NoContent writes a 204 response.
func NoContent(c echo.Context) error {
	return c.NoContent(http.StatusNoContent)
}

// RequestID returns the request id of the current request, if any.
func RequestID(c echo.Context) string {
	id, _ := c.Get(RequestIDKey).(string)
	return id
}

// Timestamp formats t the way every error response does.
func Timestamp(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}
