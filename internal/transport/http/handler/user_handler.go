package handler

import (
	"context"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"technical-test-be-gdc-multi-sarana/internal/domain"
	"technical-test-be-gdc-multi-sarana/internal/service"
	"technical-test-be-gdc-multi-sarana/internal/transport/http/response"
)

// UserService is the use case contract used by UserHandler.
type UserService interface {
	Me(ctx context.Context, userID uuid.UUID) (*service.Profile, error)
	TeamMembers(ctx context.Context, userID uuid.UUID) ([]domain.User, error)
}

// UserHandler serves the endpoints about the authenticated user.
type UserHandler struct {
	users UserService
}

// NewUserHandler creates a UserHandler.
func NewUserHandler(users UserService) *UserHandler {
	return &UserHandler{users: users}
}

// Me handles GET /users/me.
func (h *UserHandler) Me(c echo.Context) error {
	userID, err := currentUserID(c)
	if err != nil {
		return err
	}
	profile, err := h.users.Me(c.Request().Context(), userID)
	if err != nil {
		return err
	}
	return response.OK(c, newProfileResponse(profile))
}

// TeamMembers handles GET /users/team-members.
func (h *UserHandler) TeamMembers(c echo.Context) error {
	userID, err := currentUserID(c)
	if err != nil {
		return err
	}
	members, err := h.users.TeamMembers(c.Request().Context(), userID)
	if err != nil {
		return err
	}
	out := make([]UserResponse, 0, len(members))
	for i := range members {
		out = append(out, newUserResponse(&members[i]))
	}
	return response.OK(c, out)
}
