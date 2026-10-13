// Package apperror defines the application error type returned by use cases.
//
// It is transport agnostic: an error carries a Kind describing what went
// wrong, and each delivery mechanism (HTTP today) maps that Kind to its own
// status codes.
package apperror

import (
	"errors"
	"fmt"
)

// Kind classifies an error independently of any transport.
type Kind uint8

const (
	KindInternal Kind = iota
	KindValidation
	KindBadRequest
	KindUnauthorized
	KindForbidden
	KindNotFound
	KindMethodNotAllowed
	KindConflict
	KindPayloadTooLarge
	KindUnprocessable
	KindTooManyRequests
	KindUnavailable
)

// Generic error codes. Feature specific codes live next to the feature.
const (
	CodeValidation       = "VALIDATION_ERROR"
	CodeBadRequest       = "BAD_REQUEST"
	CodeUnauthorized     = "UNAUTHORIZED"
	CodeForbidden        = "FORBIDDEN"
	CodeNotFound         = "NOT_FOUND"
	CodeMethodNotAllowed = "METHOD_NOT_ALLOWED"
	CodeConflict         = "CONFLICT"
	CodePayloadTooLarge  = "PAYLOAD_TOO_LARGE"
	CodeUnprocessable    = "UNPROCESSABLE_ENTITY"
	CodeTooManyRequests  = "TOO_MANY_REQUESTS"
	CodeUnavailable      = "SERVICE_UNAVAILABLE"
	CodeInternal         = "INTERNAL_SERVER_ERROR"
)

// Error is an error that knows how it should be presented to clients.
//
// Message and Details are safe to expose. Err holds the internal cause and is
// only ever logged.
type Error struct {
	Kind    Kind
	Code    string
	Message string
	Details any
	Err     error
}

// New creates an application error.
func New(kind Kind, code, message string) *Error {
	return &Error{Kind: kind, Code: code, Message: message}
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error { return e.Err }

// Is matches application errors by kind and code so that sentinel errors
// still match after being copied with Wrap or WithDetails.
func (e *Error) Is(target error) bool {
	var t *Error
	if !errors.As(target, &t) {
		return false
	}
	return e.Kind == t.Kind && e.Code == t.Code
}

// Wrap returns a copy of e carrying err as the internal cause.
func (e *Error) Wrap(err error) *Error {
	cp := *e
	cp.Err = err
	return &cp
}

// WithDetails returns a copy of e carrying client-facing details.
func (e *Error) WithDetails(details any) *Error {
	cp := *e
	cp.Details = details
	return &cp
}

// IsServerError reports whether the error is the server's fault rather than
// the client's.
func (e *Error) IsServerError() bool {
	return e.Kind == KindInternal || e.Kind == KindUnavailable
}

// Validation builds a validation error with field level details.
func Validation(details any) *Error {
	return New(KindValidation, CodeValidation, "request validation failed").WithDetails(details)
}

// BadRequest builds a malformed request error.
func BadRequest(code, message string) *Error { return New(KindBadRequest, code, message) }

// Unauthorized builds an authentication error.
func Unauthorized(code, message string) *Error { return New(KindUnauthorized, code, message) }

// Forbidden builds an authorization error.
func Forbidden(code, message string) *Error { return New(KindForbidden, code, message) }

// NotFound builds a missing resource error.
func NotFound(code, message string) *Error { return New(KindNotFound, code, message) }

// Conflict builds a state conflict error.
func Conflict(code, message string) *Error { return New(KindConflict, code, message) }

// Unprocessable builds a business rule violation error.
func Unprocessable(code, message string) *Error { return New(KindUnprocessable, code, message) }

// TooManyRequests builds a rate limiting error.
func TooManyRequests(code, message string) *Error { return New(KindTooManyRequests, code, message) }

// Unavailable builds a dependency unavailable error.
func Unavailable(code, message string) *Error { return New(KindUnavailable, code, message) }

// Internal wraps an unexpected error into a generic error that hides the cause.
func Internal(err error) *Error {
	return New(KindInternal, CodeInternal, "an unexpected error occurred").Wrap(err)
}

// From converts any error into an *Error. Unknown errors become internal errors.
func From(err error) *Error {
	if err == nil {
		return nil
	}
	var appErr *Error
	if errors.As(err, &appErr) {
		return appErr
	}
	return Internal(err)
}
