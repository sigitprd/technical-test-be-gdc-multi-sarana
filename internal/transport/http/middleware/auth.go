package middleware

import (
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"technical-test-be-gdc-multi-sarana/internal/apperror"
)

const userIDKey = "auth_user_id"

// TokenParser verifies an access token and returns the user id it was issued to.
type TokenParser interface {
	Parse(token string) (uuid.UUID, error)
}

var (
	errMissingToken = apperror.Unauthorized("MISSING_TOKEN", "authorization header with a bearer token is required")
	errInvalidToken = apperror.Unauthorized("INVALID_TOKEN", "access token is invalid or expired")
)

// JWTAuth rejects requests without a valid "Authorization: Bearer <token>"
// header and stores the authenticated user id in the echo context.
func JWTAuth(parser TokenParser) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			header := c.Request().Header.Get(echo.HeaderAuthorization)
			scheme, tokenString, ok := strings.Cut(header, " ")
			if !ok || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(tokenString) == "" {
				return errMissingToken
			}
			userID, err := parser.Parse(strings.TrimSpace(tokenString))
			if err != nil {
				return errInvalidToken.Wrap(err)
			}
			c.Set(userIDKey, userID)
			return next(c)
		}
	}
}

// UserIDFromContext returns the authenticated user id, if any.
func UserIDFromContext(c echo.Context) (uuid.UUID, bool) {
	id, ok := c.Get(userIDKey).(uuid.UUID)
	return id, ok
}
