package handler

import (
	"context"

	"github.com/labstack/echo/v4"

	"technical-test-be-gdc-multi-sarana/internal/service"
	"technical-test-be-gdc-multi-sarana/internal/transport/http/response"
)

// AuthService is the use case contract used by AuthHandler.
type AuthService interface {
	Register(ctx context.Context, in service.RegisterInput) (*service.Profile, error)
	Login(ctx context.Context, email, password string) (*service.AuthResult, error)
}

// AuthHandler serves the authentication endpoints.
type AuthHandler struct {
	auth AuthService
}

// NewAuthHandler creates an AuthHandler.
func NewAuthHandler(auth AuthService) *AuthHandler {
	return &AuthHandler{auth: auth}
}

// Register handles POST /auth/register.
func (h *AuthHandler) Register(c echo.Context) error {
	var req RegisterRequest
	if err := bindAndValidate(c, &req); err != nil {
		return err
	}
	profile, err := h.auth.Register(c.Request().Context(), service.RegisterInput{
		Name:       req.Name,
		Email:      req.Email,
		Password:   req.Password,
		TeamName:   req.TeamName,
		InviteCode: req.InviteCode,
	})
	if err != nil {
		return err
	}
	return response.Created(c, newProfileResponse(profile))
}

// Login handles POST /auth/login.
func (h *AuthHandler) Login(c echo.Context) error {
	var req LoginRequest
	if err := bindAndValidate(c, &req); err != nil {
		return err
	}
	result, err := h.auth.Login(c.Request().Context(), req.Email, req.Password)
	if err != nil {
		return err
	}
	return response.OK(c, LoginResponse{
		AccessToken: result.AccessToken,
		TokenType:   "Bearer",
		ExpiresAt:   result.ExpiresAt,
		User:        newUserResponse(result.User),
	})
}
