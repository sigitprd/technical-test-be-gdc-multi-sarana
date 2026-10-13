// Package handler contains the HTTP handlers. Handlers translate HTTP to use
// case calls and depend on small interfaces so they can be tested with stubs.
package handler

import (
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"technical-test-be-gdc-multi-sarana/internal/apperror"
	"technical-test-be-gdc-multi-sarana/internal/transport/http/middleware"
)

var (
	errInvalidBody  = apperror.BadRequest("INVALID_REQUEST_BODY", "request body is not valid JSON or has wrong field types")
	errInvalidQuery = apperror.BadRequest("INVALID_QUERY_PARAMS", "page and limit must be integers")
)

// normalizer is implemented by requests that clean up their fields, such as
// trimming spaces, so that validation sees the value that will be stored.
type normalizer interface {
	normalize()
}

// bindAndValidate decodes the request body into req, normalizes and validates it.
func bindAndValidate(c echo.Context, req any) error {
	if err := c.Bind(req); err != nil {
		return errInvalidBody.Wrap(err)
	}
	return normalizeAndValidate(c, req)
}

// bindQueryAndValidate decodes the query string into req, normalizes and validates it.
func bindQueryAndValidate(c echo.Context, req any) error {
	if err := (&echo.DefaultBinder{}).BindQueryParams(c, req); err != nil {
		return errInvalidQuery.Wrap(err)
	}
	return normalizeAndValidate(c, req)
}

func normalizeAndValidate(c echo.Context, req any) error {
	if n, ok := req.(normalizer); ok {
		n.normalize()
	}
	return c.Validate(req)
}

// pathUUID parses a UUID path parameter.
func pathUUID(c echo.Context, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
		return uuid.Nil, apperror.BadRequest("INVALID_ID", name+" must be a valid UUID")
	}
	return id, nil
}

// currentUserID returns the authenticated user id set by the JWT middleware.
func currentUserID(c echo.Context) (uuid.UUID, error) {
	id, ok := middleware.UserIDFromContext(c)
	if !ok {
		return uuid.Nil, apperror.Unauthorized(apperror.CodeUnauthorized, "authentication required")
	}
	return id, nil
}
