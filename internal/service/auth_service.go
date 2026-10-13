package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"technical-test-be-gdc-multi-sarana/internal/apperror"
	"technical-test-be-gdc-multi-sarana/internal/domain"
)

// RegisterInput is the data needed to create an account. Exactly one of
// TeamName (create a new team) or InviteCode (join an existing team) must be set.
type RegisterInput struct {
	Name       string
	Email      string
	Password   string
	TeamName   string
	InviteCode string
}

// Profile is a user together with their team.
type Profile struct {
	User *domain.User
	Team *domain.Team
}

// AuthResult is returned after a successful login.
type AuthResult struct {
	AccessToken string
	ExpiresAt   time.Time
	User        *domain.User
}

// AuthService implements registration and login.
type AuthService struct {
	tx     TxManager
	teams  TeamRepository
	users  UserRepository
	hasher PasswordHasher
	tokens TokenIssuer

	dummyOnce sync.Once
	dummyHash string
}

// NewAuthService wires an AuthService.
func NewAuthService(tx TxManager, teams TeamRepository, users UserRepository, hasher PasswordHasher, tokens TokenIssuer) *AuthService {
	return &AuthService{tx: tx, teams: teams, users: users, hasher: hasher, tokens: tokens}
}

// Register creates a user and, in the same transaction, either creates a new
// team or joins the team the invite code belongs to.
func (s *AuthService) Register(ctx context.Context, in RegisterInput) (*Profile, error) {
	teamName := strings.TrimSpace(in.TeamName)
	inviteCode := strings.ToLower(strings.TrimSpace(in.InviteCode))
	if (teamName == "") == (inviteCode == "") {
		return nil, ErrTeamChoice
	}

	hash, err := s.hasher.Hash(in.Password)
	if err != nil {
		return nil, apperror.Internal(fmt.Errorf("hash password: %w", err))
	}

	profile := &Profile{User: &domain.User{
		Name:         strings.TrimSpace(in.Name),
		Email:        normalizeEmail(in.Email),
		PasswordHash: hash,
	}}

	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		team, err := s.joinOrCreateTeam(ctx, teamName, inviteCode)
		if err != nil {
			return err
		}
		profile.Team = team
		profile.User.TeamID = team.ID

		if err := s.users.Create(ctx, profile.User); err != nil {
			if errors.Is(err, domain.ErrDuplicate) {
				return ErrEmailTaken
			}
			return fmt.Errorf("create user: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, apperror.From(err)
	}
	return profile, nil
}

// joinOrCreateTeam returns the team owning inviteCode, or creates a team
// named teamName when no invite code is given.
func (s *AuthService) joinOrCreateTeam(ctx context.Context, teamName, inviteCode string) (*domain.Team, error) {
	if inviteCode == "" {
		team, err := s.teams.Create(ctx, teamName)
		if err != nil {
			return nil, fmt.Errorf("create team: %w", err)
		}
		return team, nil
	}

	team, err := s.teams.GetByInviteCode(ctx, inviteCode)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return nil, ErrInvalidInviteCode
	case err != nil:
		return nil, fmt.Errorf("get team by invite code: %w", err)
	}
	return team, nil
}

// Login verifies credentials and issues an access token. The same error is
// returned for an unknown email and a wrong password, and a hash comparison
// always runs, so the response does not reveal which emails are registered.
func (s *AuthService) Login(ctx context.Context, email, password string) (*AuthResult, error) {
	user, err := s.users.GetByEmail(ctx, normalizeEmail(email))
	switch {
	case errors.Is(err, domain.ErrNotFound):
		_ = s.hasher.Compare(s.dummyPasswordHash(), password)
		return nil, ErrInvalidCredentials
	case err != nil:
		return nil, apperror.Internal(fmt.Errorf("get user by email: %w", err))
	}

	if err := s.hasher.Compare(user.PasswordHash, password); err != nil {
		return nil, ErrInvalidCredentials
	}

	accessToken, expiresAt, err := s.tokens.Issue(user.ID)
	if err != nil {
		return nil, apperror.Internal(fmt.Errorf("issue token: %w", err))
	}
	return &AuthResult{AccessToken: accessToken, ExpiresAt: expiresAt, User: user}, nil
}

func (s *AuthService) dummyPasswordHash() string {
	s.dummyOnce.Do(func() {
		s.dummyHash, _ = s.hasher.Hash("dummy-password-for-timing-safety")
	})
	return s.dummyHash
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
