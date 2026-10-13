package handler

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"technical-test-be-gdc-multi-sarana/internal/apperror"
	"technical-test-be-gdc-multi-sarana/internal/domain"
	"technical-test-be-gdc-multi-sarana/internal/service"
)

var (
	team    = &domain.Team{ID: uuid.New(), Name: "Alpha", InviteCode: "invite", CreatedAt: time.Now().UTC()}
	user    = &domain.User{ID: uuid.New(), TeamID: team.ID, Name: "Alice", Email: "alice@example.com", PasswordHash: "secret-hash"}
	profile = &service.Profile{User: user, Team: team}
)

func TestAuthHandler_Register(t *testing.T) {
	stub := &stubAuth{profile: profile}
	rec, err := do(t, NewAuthHandler(stub).Register, request{
		method: http.MethodPost,
		body:   `{"name":" Alice ","email":" alice@example.com ","password":" pass word ","team_name":" Alpha ","invite_code":""}`,
	})

	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, service.RegisterInput{Name: "Alice", Email: "alice@example.com", Password: " pass word ", TeamName: "Alpha"}, stub.in,
		"text fields are trimmed, the password is not")
	assert.NotContains(t, rec.Body.String(), "secret-hash", "the password hash never leaves the server")

	got := decode[ProfileResponse](t, rec)
	assert.Equal(t, user.ID, got.ID)
	assert.Equal(t, "invite", got.Team.InviteCode)

	for name, body := range map[string]string{
		"malformed JSON":       `{`,
		"invalid fields":       `{"name":"A","email":"bad","password":"short"}`,
		"password over 72 B":   `{"name":"Alice","email":"a@example.com","password":"` + longPassword() + `","team_name":"Alpha"}`,
		"whitespace-only name": `{"name":"   ","email":"a@example.com","password":"password123","team_name":"Alpha"}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := do(t, NewAuthHandler(&stubAuth{}).Register, request{method: http.MethodPost, body: body})
			require.Error(t, err)
			assert.NotEqual(t, apperror.KindInternal, requireKindAny(t, err).Kind)
		})
	}

	_, err = do(t, NewAuthHandler(&stubAuth{err: service.ErrEmailTaken}).Register, request{
		method: http.MethodPost, body: `{"name":"Alice","email":"a@example.com","password":"password123","team_name":"Alpha"}`,
	})
	assert.ErrorIs(t, err, service.ErrEmailTaken)
}

func TestAuthHandler_Login(t *testing.T) {
	expires := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	stub := &stubAuth{result: &service.AuthResult{AccessToken: "tok", ExpiresAt: expires, User: user}}
	rec, err := do(t, NewAuthHandler(stub).Login, request{
		method: http.MethodPost, body: `{"email":" alice@example.com ","password":"password123"}`,
	})

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "alice@example.com", stub.email)
	got := decode[LoginResponse](t, rec)
	assert.Equal(t, "tok", got.AccessToken)
	assert.Equal(t, "Bearer", got.TokenType)
	assert.Equal(t, expires, got.ExpiresAt)
	assert.Equal(t, user.ID, got.User.ID)

	_, err = do(t, NewAuthHandler(&stubAuth{}).Login, request{method: http.MethodPost, body: `{"email":"x"}`})
	requireKind(t, err, apperror.KindValidation)

	_, err = do(t, NewAuthHandler(&stubAuth{err: service.ErrInvalidCredentials}).Login, request{
		method: http.MethodPost, body: `{"email":"a@example.com","password":"wrong"}`,
	})
	assert.ErrorIs(t, err, service.ErrInvalidCredentials)
}

func TestUserHandler_Me(t *testing.T) {
	rec, err := do(t, NewUserHandler(&stubUsers{profile: profile}).Me, request{method: http.MethodGet, userID: user.ID})
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, team.ID, decode[ProfileResponse](t, rec).Team.ID)

	_, err = do(t, NewUserHandler(&stubUsers{err: service.ErrUserNotFound}).Me, request{method: http.MethodGet, userID: user.ID})
	assert.ErrorIs(t, err, service.ErrUserNotFound)

	_, err = do(t, NewUserHandler(&stubUsers{}).Me, request{method: http.MethodGet})
	requireKind(t, err, apperror.KindUnauthorized)
}

func TestUserHandler_TeamMembers(t *testing.T) {
	rec, err := do(t, NewUserHandler(&stubUsers{members: []domain.User{*user}}).TeamMembers, request{method: http.MethodGet, userID: user.ID})
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	members := decode[[]UserResponse](t, rec)
	require.Len(t, members, 1)
	assert.Equal(t, user.Email, members[0].Email)

	rec, err = do(t, NewUserHandler(&stubUsers{}).TeamMembers, request{method: http.MethodGet, userID: user.ID})
	require.NoError(t, err)
	assert.JSONEq(t, `{"data":[]}`, rec.Body.String(), "no members is an empty list, not null")

	_, err = do(t, NewUserHandler(&stubUsers{err: apperror.Internal(errBoom)}).TeamMembers, request{method: http.MethodGet, userID: user.ID})
	requireKind(t, err, apperror.KindInternal)

	_, err = do(t, NewUserHandler(&stubUsers{}).TeamMembers, request{method: http.MethodGet})
	requireKind(t, err, apperror.KindUnauthorized)
}

func TestHealthHandler(t *testing.T) {
	rec, err := do(t, NewHealthHandler(stubPinger{}).Health, request{method: http.MethodGet})
	require.NoError(t, err)
	assert.JSONEq(t, `{"data":{"status":"ok"}}`, rec.Body.String())

	_, err = do(t, NewHealthHandler(stubPinger{err: errBoom}).Health, request{method: http.MethodGet})
	assert.ErrorIs(t, requireKind(t, err, apperror.KindUnavailable), errBoom)
}

func longPassword() string {
	b := make([]rune, 40)
	for i := range b {
		b[i] = 'é' // 2 bytes each
	}
	return string(b)
}

func requireKindAny(t *testing.T, err error) *apperror.Error {
	t.Helper()
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	return appErr
}

func metaJSON(t *testing.T, body []byte) string {
	t.Helper()
	var envelope struct {
		Meta json.RawMessage `json:"meta"`
	}
	require.NoError(t, json.Unmarshal(body, &envelope))
	return string(envelope.Meta)
}
