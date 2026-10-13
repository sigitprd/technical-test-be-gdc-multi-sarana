package service_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"technical-test-be-gdc-multi-sarana/internal/apperror"
	"technical-test-be-gdc-multi-sarana/internal/service"
	"technical-test-be-gdc-multi-sarana/internal/service/servicetest"
)

func TestUserService_Me(t *testing.T) {
	store := servicetest.NewStore()
	team := store.AddTeam("Alpha")
	user := store.AddUser(team.ID, "Alice", "alice@example.com")
	svc := service.NewUserService(store.UserRepository(), store.TeamRepository())

	profile, err := svc.Me(context.Background(), user.ID)
	require.NoError(t, err)
	assert.Equal(t, user.ID, profile.User.ID)
	assert.Equal(t, team, *profile.Team)

	_, err = svc.Me(context.Background(), uuid.New())
	assert.ErrorIs(t, err, service.ErrUserNotFound)
}

func TestUserService_TeamMembers(t *testing.T) {
	store := servicetest.NewStore()
	alpha := store.AddTeam("Alpha")
	beta := store.AddTeam("Beta")
	bob := store.AddUser(alpha.ID, "Bob", "bob@example.com")
	alice := store.AddUser(alpha.ID, "Alice", "alice@example.com")
	store.AddUser(beta.ID, "Eve", "eve@example.com")
	svc := service.NewUserService(store.UserRepository(), store.TeamRepository())

	members, err := svc.TeamMembers(context.Background(), bob.ID)
	require.NoError(t, err)
	require.Len(t, members, 2, "only the user's own team")
	assert.Equal(t, alice.ID, members[0].ID)
	assert.Equal(t, bob.ID, members[1].ID)

	_, err = svc.TeamMembers(context.Background(), uuid.New())
	assert.ErrorIs(t, err, service.ErrUserNotFound)
}

func TestUserService_Failures(t *testing.T) {
	store := servicetest.NewStore()
	team := store.AddTeam("Alpha")
	user := store.AddUser(team.ID, "Alice", "alice@example.com")
	users, teams := store.UserRepository(), store.TeamRepository()

	tests := []struct {
		name string
		svc  *service.UserService
		call func(*service.UserService) error
	}{
		{"me: user lookup fails", service.NewUserService(failingUsers{UserRepository: users, getByID: map[uuid.UUID]error{user.ID: errBoom}}, teams),
			func(s *service.UserService) error { _, err := s.Me(context.Background(), user.ID); return err }},
		{"me: team lookup fails", service.NewUserService(users, failingTeams{TeamRepository: teams, getByID: errBoom}),
			func(s *service.UserService) error { _, err := s.Me(context.Background(), user.ID); return err }},
		{"team members: listing fails", service.NewUserService(failingUsers{UserRepository: users, listByTeam: errBoom}, teams),
			func(s *service.UserService) error { _, err := s.TeamMembers(context.Background(), user.ID); return err }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			requireKind(t, tc.call(tc.svc), apperror.KindInternal)
		})
	}
}
