package handler

import (
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
	userID = uuid.New()
	task   = &domain.Task{
		ID:        uuid.New(),
		OwnerID:   userID,
		Title:     "Write report",
		Status:    domain.TaskStatusTodo,
		CreatedAt: time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC),
	}
)

func TestTaskHandler_Create(t *testing.T) {
	key := uuid.New()

	t.Run("created", func(t *testing.T) {
		stub := &stubTasks{createRes: &service.CreateTaskResult{Task: task}}
		rec, err := do(t, NewTaskHandler(stub).Create, request{
			method: http.MethodPost, target: "/tasks", userID: userID,
			body: `{"title":"  Write report ","description":" Q3 ","status":"in_progress","due_date":"2026-10-20T10:00:00+07:00"}`,
		})

		require.NoError(t, err)
		assert.Equal(t, http.StatusCreated, rec.Code)
		assert.Empty(t, rec.Header().Get(HeaderIdempotentReplayed))
		assert.Equal(t, task.ID, decode[TaskResponse](t, rec).ID)
		assert.Equal(t, "Write report", stub.createIn.Title, "input is trimmed before validation")
		assert.Equal(t, "Q3", stub.createIn.Description)
		assert.Equal(t, domain.TaskStatusInProgress, stub.createIn.Status)
		assert.True(t, stub.createIn.DueDate.Equal(time.Date(2026, 10, 20, 3, 0, 0, 0, time.UTC)))
		assert.Nil(t, stub.createKey)
	})

	t.Run("idempotency key is passed and replays are flagged", func(t *testing.T) {
		stub := &stubTasks{createRes: &service.CreateTaskResult{Task: task, Replayed: true}}
		rec, err := do(t, NewTaskHandler(stub).Create, request{
			method: http.MethodPost, target: "/tasks", userID: userID, body: `{"title":"x"}`,
			header: map[string]string{HeaderIdempotencyKey: " " + key.String() + " "},
		})

		require.NoError(t, err)
		assert.Equal(t, http.StatusCreated, rec.Code, "a replay answers with the original status")
		assert.Equal(t, "true", rec.Header().Get(HeaderIdempotentReplayed))
		require.NotNil(t, stub.createKey)
		assert.Equal(t, key, *stub.createKey)
	})

	tests := []struct {
		name   string
		header map[string]string
		body   string
		stub   *stubTasks
		code   string
	}{
		{"idempotency key is not a UUID", map[string]string{HeaderIdempotencyKey: "abc"}, `{"title":"x"}`, &stubTasks{}, "INVALID_IDEMPOTENCY_KEY"},
		{"malformed JSON", nil, `{"title":`, &stubTasks{}, "INVALID_REQUEST_BODY"},
		{"missing title", nil, `{"description":"x"}`, &stubTasks{}, apperror.CodeValidation},
		{"blank title", nil, `{"title":"   "}`, &stubTasks{}, apperror.CodeValidation},
		{"unknown status", nil, `{"title":"x","status":"archived"}`, &stubTasks{}, apperror.CodeValidation},
		{"NUL character", nil, `{"title":"a\u0000b"}`, &stubTasks{}, apperror.CodeValidation},
		{"service error", nil, `{"title":"x"}`, &stubTasks{err: apperror.Internal(errBoom)}, apperror.CodeInternal},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := do(t, NewTaskHandler(tc.stub).Create, request{
				method: http.MethodPost, target: "/tasks", userID: userID, body: tc.body, header: tc.header,
			})
			var appErr *apperror.Error
			require.ErrorAs(t, err, &appErr)
			assert.Equal(t, tc.code, appErr.Code)
		})
	}
}

func TestTaskHandler_List(t *testing.T) {
	t.Run("query is parsed, trimmed and passed on", func(t *testing.T) {
		stub := &stubTasks{listRes: &service.TaskPage{Tasks: []domain.Task{*task}, Page: 2, Limit: 5, Total: 6, TotalPages: 2}}
		rec, err := do(t, NewTaskHandler(stub).List, request{
			method: http.MethodGet, target: "/tasks?status=done&search=%20report%20&page=2&limit=5", userID: userID,
		})

		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, service.ListTasksInput{Status: "done", Search: "report", Page: 2, Limit: 5}, stub.listIn)
		assert.JSONEq(t, `{"page":2,"limit":5,"total":6,"total_pages":2}`, metaJSON(t, rec.Body.Bytes()))
		assert.Len(t, decode[[]TaskResponse](t, rec), 1)
	})

	tests := []struct {
		name   string
		target string
		stub   *stubTasks
		code   string
	}{
		{"page is not a number", "/tasks?page=abc", &stubTasks{}, "INVALID_QUERY_PARAMS"},
		{"limit above 100", "/tasks?limit=101", &stubTasks{}, apperror.CodeValidation},
		{"page above the maximum", "/tasks?page=1000001", &stubTasks{}, apperror.CodeValidation},
		{"unknown status", "/tasks?status=archived", &stubTasks{}, apperror.CodeValidation},
		{"invalid UTF-8 search", "/tasks?search=%FF", &stubTasks{}, apperror.CodeValidation},
		{"service error", "/tasks", &stubTasks{err: apperror.Internal(errBoom)}, apperror.CodeInternal},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := do(t, NewTaskHandler(tc.stub).List, request{method: http.MethodGet, target: tc.target, userID: userID})
			var appErr *apperror.Error
			require.ErrorAs(t, err, &appErr)
			assert.Equal(t, tc.code, appErr.Code)
		})
	}
}

func TestTaskHandler_Get(t *testing.T) {
	stub := &stubTasks{task: task}
	rec, err := do(t, NewTaskHandler(stub).Get, request{
		method: http.MethodGet, target: "/tasks/" + task.ID.String(), userID: userID,
		params: map[string]string{"id": task.ID.String()},
	})
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, task.ID, stub.taskID)
	assert.Equal(t, task.Title, decode[TaskResponse](t, rec).Title)

	_, err = do(t, NewTaskHandler(&stubTasks{}).Get, request{
		method: http.MethodGet, userID: userID, params: map[string]string{"id": "123"},
	})
	assert.Equal(t, "INVALID_ID", requireKind(t, err, apperror.KindBadRequest).Code)

	_, err = do(t, NewTaskHandler(&stubTasks{err: service.ErrTaskNotFound}).Get, request{
		method: http.MethodGet, userID: userID, params: map[string]string{"id": task.ID.String()},
	})
	assert.ErrorIs(t, err, service.ErrTaskNotFound)
}

func TestTaskHandler_Update(t *testing.T) {
	t.Run("only the sent fields are in the patch", func(t *testing.T) {
		stub := &stubTasks{task: task}
		rec, err := do(t, NewTaskHandler(stub).Update, request{
			method: http.MethodPut, userID: userID, params: map[string]string{"id": task.ID.String()},
			body: `{"title":"  New ","status":"done","due_date":null}`,
		})

		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, task.ID, stub.taskID)
		require.NotNil(t, stub.patch.Title)
		assert.Equal(t, "New", *stub.patch.Title)
		assert.Nil(t, stub.patch.Description)
		assert.Equal(t, domain.TaskStatusDone, *stub.patch.Status)
		assert.True(t, stub.patch.DueDateSet)
		assert.Nil(t, stub.patch.DueDate)
	})

	tests := []struct {
		name   string
		params map[string]string
		body   string
		stub   *stubTasks
		code   string
	}{
		{"invalid id", map[string]string{"id": "x"}, `{"title":"x"}`, &stubTasks{}, "INVALID_ID"},
		{"malformed due date", map[string]string{"id": task.ID.String()}, `{"due_date":"tomorrow"}`, &stubTasks{}, "INVALID_REQUEST_BODY"},
		{"blank title", map[string]string{"id": task.ID.String()}, `{"title":"  "}`, &stubTasks{}, apperror.CodeValidation},
		{"service error", map[string]string{"id": task.ID.String()}, `{"title":"x"}`, &stubTasks{err: service.ErrTaskForbidden}, "TASK_FORBIDDEN"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := do(t, NewTaskHandler(tc.stub).Update, request{method: http.MethodPut, userID: userID, params: tc.params, body: tc.body})
			var appErr *apperror.Error
			require.ErrorAs(t, err, &appErr)
			assert.Equal(t, tc.code, appErr.Code)
		})
	}
}

func TestTaskHandler_Delete(t *testing.T) {
	stub := &stubTasks{}
	rec, err := do(t, NewTaskHandler(stub).Delete, request{
		method: http.MethodDelete, userID: userID, params: map[string]string{"id": task.ID.String()},
	})
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, task.ID, stub.taskID)

	_, err = do(t, NewTaskHandler(&stubTasks{err: service.ErrTaskForbidden}).Delete, request{
		method: http.MethodDelete, userID: userID, params: map[string]string{"id": task.ID.String()},
	})
	assert.ErrorIs(t, err, service.ErrTaskForbidden)

	_, err = do(t, NewTaskHandler(&stubTasks{}).Delete, request{method: http.MethodDelete, userID: userID, params: map[string]string{"id": "x"}})
	requireKind(t, err, apperror.KindBadRequest)
}

func TestTaskHandler_Assign(t *testing.T) {
	assignee := uuid.New()
	stub := &stubTasks{task: task}
	rec, err := do(t, NewTaskHandler(stub).Assign, request{
		method: http.MethodPost, userID: userID, params: map[string]string{"id": task.ID.String()},
		body: `{"assignee_id":"` + assignee.String() + `"}`,
	})
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, assignee, stub.assignee)

	tests := []struct {
		name   string
		params map[string]string
		body   string
		stub   *stubTasks
		code   string
	}{
		{"invalid task id", map[string]string{"id": "x"}, `{}`, &stubTasks{}, "INVALID_ID"},
		{"malformed assignee id", map[string]string{"id": task.ID.String()}, `{"assignee_id":"nope"}`, &stubTasks{}, "INVALID_REQUEST_BODY"},
		{"missing assignee id", map[string]string{"id": task.ID.String()}, `{}`, &stubTasks{}, apperror.CodeValidation},
		{"service error", map[string]string{"id": task.ID.String()}, `{"assignee_id":"` + assignee.String() + `"}`, &stubTasks{err: service.ErrAssigneeNotInTeam}, "ASSIGNEE_NOT_IN_TEAM"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := do(t, NewTaskHandler(tc.stub).Assign, request{method: http.MethodPost, userID: userID, params: tc.params, body: tc.body})
			var appErr *apperror.Error
			require.ErrorAs(t, err, &appErr)
			assert.Equal(t, tc.code, appErr.Code)
		})
	}
}

func TestTaskHandler_RequiresAuthentication(t *testing.T) {
	h := NewTaskHandler(&stubTasks{})
	for name, fn := range map[string]func() error{
		"create": func() error { _, err := do(t, h.Create, request{method: http.MethodPost}); return err },
		"list":   func() error { _, err := do(t, h.List, request{method: http.MethodGet}); return err },
		"get":    func() error { _, err := do(t, h.Get, request{method: http.MethodGet}); return err },
		"update": func() error { _, err := do(t, h.Update, request{method: http.MethodPut}); return err },
		"delete": func() error { _, err := do(t, h.Delete, request{method: http.MethodDelete}); return err },
		"assign": func() error { _, err := do(t, h.Assign, request{method: http.MethodPost}); return err },
	} {
		t.Run(name, func(t *testing.T) {
			requireKind(t, fn(), apperror.KindUnauthorized)
		})
	}
}
