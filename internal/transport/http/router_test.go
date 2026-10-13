package http_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"technical-test-be-gdc-multi-sarana/internal/pkg/keylock"
	"technical-test-be-gdc-multi-sarana/internal/pkg/token"
	"technical-test-be-gdc-multi-sarana/internal/service"
	"technical-test-be-gdc-multi-sarana/internal/service/servicetest"
	httptransport "technical-test-be-gdc-multi-sarana/internal/transport/http"
	"technical-test-be-gdc-multi-sarana/internal/transport/http/handler"
)

// api is the real router, handlers and services on top of in-memory fakes.
type api struct {
	e      *echo.Echo
	store  *servicetest.Store
	tokens *token.Manager
}

type okPinger struct{}

func (okPinger) Ping(context.Context) error { return nil }

func newAPI(t *testing.T, authRateLimit int) *api {
	t.Helper()
	store := servicetest.NewStore()
	tokens := token.NewManager("0123456789abcdef0123456789abcdef", "test", time.Hour, nil)
	tx := &servicetest.TxManager{}

	auth := service.NewAuthService(tx, store.TeamRepository(), store.UserRepository(), &servicetest.Hasher{}, tokens)
	users := service.NewUserService(store.UserRepository(), store.TeamRepository())
	tasks := service.NewTaskService(service.TaskServiceDeps{
		Tx:          tx,
		Tasks:       store.TaskRepository(),
		TaskLogs:    store.TaskLogRepository(),
		Users:       store.UserRepository(),
		Idempotency: store.IdempotencyRepository(),
		Notifier:    &servicetest.Notifier{},
		Locker:      keylock.New(),
	})

	e := httptransport.NewRouter(httptransport.RouterConfig{
		Logger:        slog.New(slog.NewJSONHandler(io.Discard, nil)),
		AuthRateLimit: authRateLimit,
		TokenParser:   tokens,
		Health:        handler.NewHealthHandler(okPinger{}),
		Auth:          handler.NewAuthHandler(auth),
		Users:         handler.NewUserHandler(users),
		Tasks:         handler.NewTaskHandler(tasks),
	})
	return &api{e: e, store: store, tokens: tokens}
}

func (a *api) do(method, path, accessToken, body string, header map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	}
	if accessToken != "" {
		req.Header.Set(echo.HeaderAuthorization, "Bearer "+accessToken)
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	a.e.ServeHTTP(rec, req)
	return rec
}

// signUp registers a user creating a team (or joining one with inviteCode)
// and returns the user id, the team invite code and an access token.
func (a *api) signUp(t *testing.T, email, inviteCode string) (uuid.UUID, string, string) {
	t.Helper()
	team := `"team_name":"Alpha"`
	if inviteCode != "" {
		team = `"invite_code":"` + inviteCode + `"`
	}
	rec := a.do(http.MethodPost, "/api/v1/auth/register", "", `{"name":"User","email":"`+email+`","password":"password123",`+team+`}`, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var reg struct {
		Data handler.ProfileResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &reg))

	rec = a.do(http.MethodPost, "/api/v1/auth/login", "", `{"email":"`+email+`","password":"password123"}`, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var login struct {
		Data handler.LoginResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &login))
	return reg.Data.ID, reg.Data.Team.InviteCode, login.Data.AccessToken
}

// Requirement 5.1, sequential: the first request with a new key creates the
// task (201); the second with the same key creates nothing and returns an
// identical response.
func TestAPI_Idempotency_SequentialRetryReturnsIdenticalResponse(t *testing.T) {
	a := newAPI(t, 0)
	_, _, tok := a.signUp(t, "alice@example.com", "")
	header := map[string]string{"Idempotency-Key": uuid.NewString()}
	body := `{"title":"Write report","description":"Q3","due_date":"2026-10-20T10:00:00+07:00"}`

	first := a.do(http.MethodPost, "/api/v1/tasks", tok, body, header)
	second := a.do(http.MethodPost, "/api/v1/tasks", tok, body, header)

	require.Equal(t, http.StatusCreated, first.Code, first.Body.String())
	assert.Equal(t, http.StatusCreated, second.Code)
	assert.Equal(t, first.Body.String(), second.Body.String(), "the retry gets a byte-identical body")
	assert.Empty(t, first.Header().Get("Idempotent-Replayed"))
	assert.Equal(t, "true", second.Header().Get("Idempotent-Replayed"))
	assert.Len(t, a.store.AllTasks(), 1, "no second task")
}

// Requirement 5.1, concurrent duplicate: N requests with the same key at the
// same time create exactly one task.
func TestAPI_Idempotency_ConcurrentDuplicatesCreateOneTask(t *testing.T) {
	a := newAPI(t, 0)
	_, _, tok := a.signUp(t, "alice@example.com", "")
	header := map[string]string{"Idempotency-Key": uuid.NewString()}
	const n = 50

	responses := make([]*httptest.ResponseRecorder, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			responses[i] = a.do(http.MethodPost, "/api/v1/tasks", tok, `{"title":"Concurrent"}`, header)
		}()
	}
	close(start)
	wg.Wait()

	require.Len(t, a.store.AllTasks(), 1, "exactly one task in the store")
	replays := 0
	for _, rec := range responses {
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		assert.Equal(t, responses[0].Body.String(), rec.Body.String(), "every response is identical")
		if rec.Header().Get("Idempotent-Replayed") == "true" {
			replays++
		}
	}
	assert.Equal(t, n-1, replays, "all but the first are replays")
}

func TestAPI_TaskLifecycle(t *testing.T) {
	a := newAPI(t, 0)
	_, invite, ownerTok := a.signUp(t, "alice@example.com", "")
	bobID, _, bobTok := a.signUp(t, "bob@example.com", invite)

	rec := a.do(http.MethodPost, "/api/v1/tasks", ownerTok, `{"title":"Report"}`, nil)
	require.Equal(t, http.StatusCreated, rec.Code)
	var created struct {
		Data handler.TaskResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	taskPath := "/api/v1/tasks/" + created.Data.ID.String()

	steps := []struct {
		name, method, path, tok, body string
		status                        int
	}{
		{"list", http.MethodGet, "/api/v1/tasks?status=todo&search=rep&page=1&limit=10", ownerTok, "", http.StatusOK},
		{"get", http.MethodGet, taskPath, ownerTok, "", http.StatusOK},
		{"partial update", http.MethodPut, taskPath, ownerTok, `{"status":"in_progress"}`, http.StatusOK},
		{"assign teammate", http.MethodPost, taskPath + "/assign", ownerTok, `{"assignee_id":"` + bobID.String() + `"}`, http.StatusOK},
		{"assignee can read", http.MethodGet, taskPath, bobTok, "", http.StatusOK},
		{"assignee cannot delete", http.MethodDelete, taskPath, bobTok, "", http.StatusForbidden},
		{"profile", http.MethodGet, "/api/v1/users/me", bobTok, "", http.StatusOK},
		{"team members", http.MethodGet, "/api/v1/users/team-members", bobTok, "", http.StatusOK},
		{"owner deletes", http.MethodDelete, taskPath, ownerTok, "", http.StatusNoContent},
		{"gone", http.MethodGet, taskPath, ownerTok, "", http.StatusNotFound},
	}
	for _, s := range steps {
		rec := a.do(s.method, s.path, s.tok, s.body, nil)
		assert.Equal(t, s.status, rec.Code, "%s: %s", s.name, rec.Body.String())
	}
}

func TestAPI_RoutingAndAuthentication(t *testing.T) {
	a := newAPI(t, 0)
	expired, _, err := token.NewManager("0123456789abcdef0123456789abcdef", "test", time.Hour,
		func() time.Time { return time.Now().Add(-2 * time.Hour) }).Issue(uuid.New())
	require.NoError(t, err)

	tests := []struct {
		name, method, path, tok string
		status                  int
		code                    string
	}{
		{"health", http.MethodGet, "/health", "", http.StatusOK, ""},
		{"unknown route", http.MethodGet, "/nope", "", http.StatusNotFound, "NOT_FOUND"},
		{"unknown route under /api/v1 without token", http.MethodGet, "/api/v1/nope", "", http.StatusNotFound, "NOT_FOUND"},
		{"wrong method", http.MethodPatch, "/api/v1/tasks", "", http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED"},
		{"no token", http.MethodGet, "/api/v1/tasks", "", http.StatusUnauthorized, "MISSING_TOKEN"},
		{"garbage token", http.MethodGet, "/api/v1/tasks", "garbage", http.StatusUnauthorized, "INVALID_TOKEN"},
		{"expired token", http.MethodGet, "/api/v1/tasks", expired, http.StatusUnauthorized, "INVALID_TOKEN"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := a.do(tc.method, tc.path, tc.tok, "", nil)
			assert.Equal(t, tc.status, rec.Code)
			if tc.code != "" {
				var body struct {
					Code      string `json:"code"`
					RequestID string `json:"request_id"`
					Timestamp string `json:"timestamp"`
				}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
				assert.Equal(t, tc.code, body.Code)
				assert.Equal(t, rec.Header().Get(echo.HeaderXRequestID), body.RequestID)
				assert.NotEmpty(t, body.Timestamp)
			}
			assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"), "security headers on every response")
		})
	}
}

func TestAPI_BodyLimit(t *testing.T) {
	a := newAPI(t, 0)
	_, _, tok := a.signUp(t, "alice@example.com", "")
	huge := `{"title":"` + strings.Repeat("a", 2<<20) + `"}`

	rec := a.do(http.MethodPost, "/api/v1/tasks", tok, huge, nil)

	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}

func TestAPI_AuthRateLimit(t *testing.T) {
	a := newAPI(t, 2)
	login := func() int {
		return a.do(http.MethodPost, "/api/v1/auth/login", "", `{"email":"nobody@example.com","password":"password123"}`, nil).Code
	}

	assert.Equal(t, http.StatusUnauthorized, login())
	assert.Equal(t, http.StatusUnauthorized, login())
	assert.Equal(t, http.StatusTooManyRequests, login(), "the third request in a burst is limited")
	assert.Equal(t, http.StatusOK, a.do(http.MethodGet, "/health", "", "", nil).Code, "other routes are not limited")
}
