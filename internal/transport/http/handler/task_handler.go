package handler

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"technical-test-be-gdc-multi-sarana/internal/apperror"
	"technical-test-be-gdc-multi-sarana/internal/domain"
	"technical-test-be-gdc-multi-sarana/internal/service"
	"technical-test-be-gdc-multi-sarana/internal/transport/http/response"
)

// Idempotency headers.
const (
	HeaderIdempotencyKey     = "Idempotency-Key"
	HeaderIdempotentReplayed = "Idempotent-Replayed"
)

var errInvalidIdempotencyKey = apperror.BadRequest("INVALID_IDEMPOTENCY_KEY", "Idempotency-Key header must be a valid UUID")

// TaskService is the use case contract used by TaskHandler.
type TaskService interface {
	CreateTask(ctx context.Context, userID uuid.UUID, in service.TaskInput, idempotencyKey *uuid.UUID) (*service.CreateTaskResult, error)
	ListTasks(ctx context.Context, userID uuid.UUID, in service.ListTasksInput) (*service.TaskPage, error)
	GetTask(ctx context.Context, userID, taskID uuid.UUID) (*domain.Task, error)
	UpdateTask(ctx context.Context, userID, taskID uuid.UUID, patch service.TaskPatch) (*domain.Task, error)
	DeleteTask(ctx context.Context, userID, taskID uuid.UUID) error
	AssignTask(ctx context.Context, actorID, taskID, assigneeID uuid.UUID) (*domain.Task, error)
}

// TaskHandler serves the task endpoints.
type TaskHandler struct {
	tasks TaskService
}

// NewTaskHandler creates a TaskHandler.
func NewTaskHandler(tasks TaskService) *TaskHandler {
	return &TaskHandler{tasks: tasks}
}

// Create handles POST /tasks. With an Idempotency-Key header, retries of the
// same request return the original 201 response without creating a new task;
// replays are flagged with the Idempotent-Replayed header.
func (h *TaskHandler) Create(c echo.Context) error {
	userID, err := currentUserID(c)
	if err != nil {
		return err
	}
	key, err := idempotencyKey(c)
	if err != nil {
		return err
	}

	var req CreateTaskRequest
	if err := bindAndValidate(c, &req); err != nil {
		return err
	}

	result, err := h.tasks.CreateTask(c.Request().Context(), userID, req.toInput(), key)
	if err != nil {
		return err
	}
	if result.Replayed {
		c.Response().Header().Set(HeaderIdempotentReplayed, "true")
	}
	return response.Created(c, newTaskResponse(result.Task))
}

// List handles GET /tasks?status=&search=&page=&limit=.
func (h *TaskHandler) List(c echo.Context) error {
	userID, err := currentUserID(c)
	if err != nil {
		return err
	}
	var q ListTasksQuery
	if err := bindQueryAndValidate(c, &q); err != nil {
		return err
	}

	page, err := h.tasks.ListTasks(c.Request().Context(), userID, service.ListTasksInput{
		Status: q.Status,
		Search: q.Search,
		Page:   q.Page,
		Limit:  q.Limit,
	})
	if err != nil {
		return err
	}
	return response.Paginated(c, newTaskResponses(page.Tasks), response.Meta{
		Page:       page.Page,
		Limit:      page.Limit,
		Total:      page.Total,
		TotalPages: page.TotalPages,
	})
}

// Get handles GET /tasks/:id.
func (h *TaskHandler) Get(c echo.Context) error {
	userID, taskID, err := userAndTaskID(c)
	if err != nil {
		return err
	}
	task, err := h.tasks.GetTask(c.Request().Context(), userID, taskID)
	if err != nil {
		return err
	}
	return response.OK(c, newTaskResponse(task))
}

// Update handles PUT /tasks/:id, a partial update: only the fields sent change.
func (h *TaskHandler) Update(c echo.Context) error {
	userID, taskID, err := userAndTaskID(c)
	if err != nil {
		return err
	}
	var req UpdateTaskRequest
	if err := bindAndValidate(c, &req); err != nil {
		return err
	}
	task, err := h.tasks.UpdateTask(c.Request().Context(), userID, taskID, req.toPatch())
	if err != nil {
		return err
	}
	return response.OK(c, newTaskResponse(task))
}

// Delete handles DELETE /tasks/:id.
func (h *TaskHandler) Delete(c echo.Context) error {
	userID, taskID, err := userAndTaskID(c)
	if err != nil {
		return err
	}
	if err := h.tasks.DeleteTask(c.Request().Context(), userID, taskID); err != nil {
		return err
	}
	return response.NoContent(c)
}

// Assign handles POST /tasks/:id/assign.
func (h *TaskHandler) Assign(c echo.Context) error {
	userID, taskID, err := userAndTaskID(c)
	if err != nil {
		return err
	}
	var req AssignTaskRequest
	if err := bindAndValidate(c, &req); err != nil {
		return err
	}
	task, err := h.tasks.AssignTask(c.Request().Context(), userID, taskID, req.AssigneeID)
	if err != nil {
		return err
	}
	return response.OK(c, newTaskResponse(task))
}

func userAndTaskID(c echo.Context) (uuid.UUID, uuid.UUID, error) {
	userID, err := currentUserID(c)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	taskID, err := pathUUID(c, "id")
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	return userID, taskID, nil
}

// idempotencyKey reads the optional Idempotency-Key header.
func idempotencyKey(c echo.Context) (*uuid.UUID, error) {
	raw := strings.TrimSpace(c.Request().Header.Get(HeaderIdempotencyKey))
	if raw == "" {
		return nil, nil
	}
	key, err := uuid.Parse(raw)
	if err != nil {
		return nil, errInvalidIdempotencyKey
	}
	return &key, nil
}
