package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"technical-test-be-gdc-multi-sarana/internal/apperror"
	"technical-test-be-gdc-multi-sarana/internal/domain"
)

// UserService exposes read use cases about the authenticated user.
type UserService struct {
	users UserRepository
	teams TeamRepository
}

// NewUserService wires a UserService.
func NewUserService(users UserRepository, teams TeamRepository) *UserService {
	return &UserService{users: users, teams: teams}
}

// Me returns the authenticated user and their team, including the invite
// code members share to bring others in.
func (s *UserService) Me(ctx context.Context, userID uuid.UUID) (*Profile, error) {
	user, err := s.user(ctx, userID)
	if err != nil {
		return nil, err
	}
	team, err := s.teams.GetByID(ctx, user.TeamID)
	if err != nil {
		return nil, apperror.Internal(fmt.Errorf("get team: %w", err))
	}
	return &Profile{User: user, Team: team}, nil
}

// TeamMembers lists the members of the authenticated user's team, i.e. the
// users a task can be assigned to.
func (s *UserService) TeamMembers(ctx context.Context, userID uuid.UUID) ([]domain.User, error) {
	user, err := s.user(ctx, userID)
	if err != nil {
		return nil, err
	}
	members, err := s.users.ListByTeam(ctx, user.TeamID)
	if err != nil {
		return nil, apperror.Internal(fmt.Errorf("list team members: %w", err))
	}
	return members, nil
}

func (s *UserService) user(ctx context.Context, userID uuid.UUID) (*domain.User, error) {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, apperror.Internal(fmt.Errorf("get user: %w", err))
	}
	return user, nil
}
