package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"technical-test-be-gdc-multi-sarana/internal/apperror"
	"technical-test-be-gdc-multi-sarana/internal/domain"
	"technical-test-be-gdc-multi-sarana/internal/service"
	"technical-test-be-gdc-multi-sarana/internal/transport/http/middleware"
)

var errBoom = errors.New("boom")

// request describes one call to a handler.
type request struct {
	method string
	target string
	body   string
	header map[string]string
	params map[string]string
	// userID authenticates the request through the JWT middleware.
	userID uuid.UUID
}

// do runs h for r and returns the recorded response and h's error.
func do(t *testing.T, h echo.HandlerFunc, r request) (*httptest.ResponseRecorder, error) {
	t.Helper()
	e := echo.New()
	e.Validator = NewValidator()

	if r.target == "" {
		r.target = "/"
	}
	req := httptest.NewRequest(r.method, r.target, strings.NewReader(r.body))
	if r.body != "" {
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	}
	for k, v := range r.header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	var names, values []string
	for name, value := range r.params {
		names, values = append(names, name), append(values, value)
	}
	c.SetParamNames(names...)
	c.SetParamValues(values...)

	if r.userID != uuid.Nil {
		req.Header.Set(echo.HeaderAuthorization, "Bearer test")
		h = middleware.JWTAuth(fixedParser(r.userID))(h)
	}
	return rec, h(c)
}

type fixedParser uuid.UUID

func (p fixedParser) Parse(string) (uuid.UUID, error) { return uuid.UUID(p), nil }

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var envelope struct {
		Data T `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope), rec.Body.String())
	return envelope.Data
}

func requireKind(t *testing.T, err error, kind apperror.Kind) *apperror.Error {
	t.Helper()
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, kind, appErr.Kind, "error: %v", err)
	return appErr
}

// stubTasks records what the handler passes and returns canned results.
type stubTasks struct {
	createIn  service.TaskInput
	createKey *uuid.UUID
	createRes *service.CreateTaskResult
	listIn    service.ListTasksInput
	listRes   *service.TaskPage
	patch     service.TaskPatch
	taskID    uuid.UUID
	assignee  uuid.UUID
	task      *domain.Task
	err       error
}

func (s *stubTasks) CreateTask(_ context.Context, _ uuid.UUID, in service.TaskInput, key *uuid.UUID) (*service.CreateTaskResult, error) {
	s.createIn, s.createKey = in, key
	return s.createRes, s.err
}

func (s *stubTasks) ListTasks(_ context.Context, _ uuid.UUID, in service.ListTasksInput) (*service.TaskPage, error) {
	s.listIn = in
	return s.listRes, s.err
}

func (s *stubTasks) GetTask(_ context.Context, _, taskID uuid.UUID) (*domain.Task, error) {
	s.taskID = taskID
	return s.task, s.err
}

func (s *stubTasks) UpdateTask(_ context.Context, _, taskID uuid.UUID, patch service.TaskPatch) (*domain.Task, error) {
	s.taskID, s.patch = taskID, patch
	return s.task, s.err
}

func (s *stubTasks) DeleteTask(_ context.Context, _, taskID uuid.UUID) error {
	s.taskID = taskID
	return s.err
}

func (s *stubTasks) AssignTask(_ context.Context, _, taskID, assigneeID uuid.UUID) (*domain.Task, error) {
	s.taskID, s.assignee = taskID, assigneeID
	return s.task, s.err
}

type stubAuth struct {
	in      service.RegisterInput
	email   string
	profile *service.Profile
	result  *service.AuthResult
	err     error
}

func (s *stubAuth) Register(_ context.Context, in service.RegisterInput) (*service.Profile, error) {
	s.in = in
	return s.profile, s.err
}

func (s *stubAuth) Login(_ context.Context, email, _ string) (*service.AuthResult, error) {
	s.email = email
	return s.result, s.err
}

type stubUsers struct {
	profile *service.Profile
	members []domain.User
	err     error
}

func (s *stubUsers) Me(context.Context, uuid.UUID) (*service.Profile, error) { return s.profile, s.err }

func (s *stubUsers) TeamMembers(context.Context, uuid.UUID) ([]domain.User, error) {
	return s.members, s.err
}

type stubPinger struct{ err error }

func (p stubPinger) Ping(context.Context) error { return p.err }
