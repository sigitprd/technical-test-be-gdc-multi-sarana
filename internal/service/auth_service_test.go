package service_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"technical-test-be-gdc-multi-sarana/internal/apperror"
	"technical-test-be-gdc-multi-sarana/internal/service"
	"technical-test-be-gdc-multi-sarana/internal/service/servicetest"
)

type authEnv struct {
	store  *servicetest.Store
	tx     *servicetest.TxManager
	hasher *servicetest.Hasher
	auth   *service.AuthService
}

func newAuthEnv(teams service.TeamRepository, users service.UserRepository, tokens service.TokenIssuer) *authEnv {
	e := &authEnv{store: servicetest.NewStore(), tx: &servicetest.TxManager{}, hasher: &servicetest.Hasher{}}
	if teams == nil {
		teams = e.store.TeamRepository()
	}
	if users == nil {
		users = e.store.UserRepository()
	}
	if tokens == nil {
		tokens = &servicetest.TokenIssuer{}
	}
	e.auth = service.NewAuthService(e.tx, teams, users, e.hasher, tokens)
	return e
}

func TestRegister_CreatesTeam(t *testing.T) {
	e := newAuthEnv(nil, nil, nil)

	profile, err := e.auth.Register(context.Background(), service.RegisterInput{
		Name:     "  Alice ",
		Email:    " Alice@Example.COM ",
		Password: "password123",
		TeamName: "  Alpha ",
	})

	require.NoError(t, err)
	assert.Equal(t, "Alice", profile.User.Name)
	assert.Equal(t, "alice@example.com", profile.User.Email)
	assert.Equal(t, "hashed:password123", profile.User.PasswordHash)
	assert.Equal(t, "Alpha", profile.Team.Name)
	assert.Equal(t, profile.Team.ID, profile.User.TeamID)
	assert.NotEmpty(t, profile.Team.InviteCode)
	assert.Equal(t, 1, e.store.TeamCount())
}

func TestRegister_JoinsTeamWithInviteCode(t *testing.T) {
	e := newAuthEnv(nil, nil, nil)
	team := e.store.AddTeam("Alpha")

	profile, err := e.auth.Register(context.Background(), service.RegisterInput{
		Name:       "Bob",
		Email:      "bob@example.com",
		Password:   "password123",
		InviteCode: "  " + strings.ToUpper(team.InviteCode) + " ",
	})

	require.NoError(t, err)
	assert.Equal(t, team.ID, profile.User.TeamID)
	assert.Equal(t, team.ID, profile.Team.ID)
	assert.Equal(t, 1, e.store.TeamCount(), "no new team")
}

func TestRegister_Rejections(t *testing.T) {
	e := newAuthEnv(nil, nil, nil)
	ctx := context.Background()
	existing := e.store.AddTeam("Alpha")
	e.store.AddUser(existing.ID, "Taken", "taken@example.com")

	tests := []struct {
		name string
		in   service.RegisterInput
		want *apperror.Error
	}{
		{"neither team_name nor invite_code", service.RegisterInput{Email: "a@example.com"}, service.ErrTeamChoice},
		{"both team_name and invite_code", service.RegisterInput{Email: "a@example.com", TeamName: "New", InviteCode: existing.InviteCode}, service.ErrTeamChoice},
		{"unknown invite code", service.RegisterInput{Email: "a@example.com", InviteCode: "nope"}, service.ErrInvalidInviteCode},
		{"email already registered", service.RegisterInput{Email: "TAKEN@example.com", TeamName: "New"}, service.ErrEmailTaken},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.in.Name, tc.in.Password = "User", "password123"
			_, err := e.auth.Register(ctx, tc.in)
			assert.ErrorIs(t, err, tc.want)
		})
	}
	assert.Equal(t, 1, e.store.UserCount())
	assert.Equal(t, 1, e.store.TeamCount(), "the team created for the duplicate email is rolled back")
}

func TestRegister_Failures(t *testing.T) {
	tests := []struct {
		name  string
		env   func() *authEnv
		input service.RegisterInput
	}{
		{"hashing fails", func() *authEnv {
			e := newAuthEnv(nil, nil, nil)
			e.hasher.HashErr = errBoom
			return e
		}, service.RegisterInput{TeamName: "Alpha"}},
		{"creating the team fails", func() *authEnv {
			store := servicetest.NewStore()
			return newAuthEnv(failingTeams{TeamRepository: store.TeamRepository(), create: errBoom}, nil, nil)
		}, service.RegisterInput{TeamName: "Alpha"}},
		{"looking up the invite code fails", func() *authEnv {
			store := servicetest.NewStore()
			return newAuthEnv(failingTeams{TeamRepository: store.TeamRepository(), getByInviteCode: errBoom}, nil, nil)
		}, service.RegisterInput{InviteCode: "code"}},
		{"creating the user fails", func() *authEnv {
			store := servicetest.NewStore()
			return newAuthEnv(nil, failingUsers{UserRepository: store.UserRepository(), create: errBoom}, nil)
		}, service.RegisterInput{TeamName: "Alpha"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := tc.env()
			tc.input.Name, tc.input.Email, tc.input.Password = "User", "user@example.com", "password123"

			_, err := e.auth.Register(context.Background(), tc.input)

			requireKind(t, err, apperror.KindInternal)
		})
	}
}

func TestLogin(t *testing.T) {
	e := newAuthEnv(nil, nil, nil)
	ctx := context.Background()
	team := e.store.AddTeam("Alpha")
	user := e.store.AddUser(team.ID, "Alice", "alice@example.com")

	t.Run("valid credentials", func(t *testing.T) {
		res, err := e.auth.Login(ctx, " ALICE@example.com ", "password123")
		require.NoError(t, err)
		assert.Equal(t, "token-"+user.ID.String(), res.AccessToken)
		assert.Equal(t, user.ID, res.User.ID)
		assert.False(t, res.ExpiresAt.IsZero())
	})

	t.Run("wrong password", func(t *testing.T) {
		_, err := e.auth.Login(ctx, "alice@example.com", "wrong")
		assert.ErrorIs(t, err, service.ErrInvalidCredentials)
	})

	t.Run("unknown email still compares a hash", func(t *testing.T) {
		before := e.hasher.Compares()
		_, err := e.auth.Login(ctx, "nobody@example.com", "password123")
		assert.ErrorIs(t, err, service.ErrInvalidCredentials, "same error as a wrong password")
		assert.Equal(t, before+1, e.hasher.Compares(), "keeps the response time similar")
	})
}

func TestLogin_Failures(t *testing.T) {
	t.Run("user lookup fails", func(t *testing.T) {
		store := servicetest.NewStore()
		e := newAuthEnv(nil, failingUsers{UserRepository: store.UserRepository(), getByEmail: errBoom}, nil)
		_, err := e.auth.Login(context.Background(), "alice@example.com", "password123")
		requireKind(t, err, apperror.KindInternal)
	})

	t.Run("issuing the token fails", func(t *testing.T) {
		e := newAuthEnv(nil, nil, &servicetest.TokenIssuer{Err: errBoom})
		team := e.store.AddTeam("Alpha")
		e.store.AddUser(team.ID, "Alice", "alice@example.com")
		_, err := e.auth.Login(context.Background(), "alice@example.com", "password123")
		requireKind(t, err, apperror.KindInternal)
	})
}
