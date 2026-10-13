package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"technical-test-be-gdc-multi-sarana/internal/apperror"
	"technical-test-be-gdc-multi-sarana/internal/domain"
)

// Pagination bounds for task listing. MaxPage keeps the row offset far from
// integer overflow.
const (
	DefaultPage  = 1
	MaxPage      = 1_000_000
	DefaultLimit = 10
	MaxLimit     = 100
)

// DefaultIdempotencyTTL is how long an Idempotency-Key is remembered.
const DefaultIdempotencyTTL = 24 * time.Hour

// TaskInput holds the editable fields of a new task.
type TaskInput struct {
	Title       string
	Description string
	Status      domain.TaskStatus
	DueDate     *time.Time
}

// TaskPatch is a partial task update. Nil fields are left unchanged. The due
// date needs an extra flag because nil is also a valid new value: DueDateSet
// says whether to change it, and a nil DueDate then clears it.
type TaskPatch struct {
	Title       *string
	Description *string
	Status      *domain.TaskStatus
	DueDate     *time.Time
	DueDateSet  bool
}

// IsEmpty reports whether the patch changes nothing.
func (p TaskPatch) IsEmpty() bool {
	return p.Title == nil && p.Description == nil && p.Status == nil && !p.DueDateSet
}

// ListTasksInput holds the raw listing parameters.
type ListTasksInput struct {
	Status string
	Search string
	Page   int
	Limit  int
}

// TaskPage is one page of a task listing.
type TaskPage struct {
	Tasks      []domain.Task
	Page       int
	Limit      int
	Total      int64
	TotalPages int
}

// CreateTaskResult is the outcome of CreateTask. Replayed is true when the
// task comes from a previous request with the same Idempotency-Key.
type CreateTaskResult struct {
	Task     *domain.Task
	Replayed bool
}

// TaskServiceDeps groups the collaborators of TaskService.
type TaskServiceDeps struct {
	Tx             TxManager
	Tasks          TaskRepository
	TaskLogs       TaskLogRepository
	Users          UserRepository
	Idempotency    IdempotencyRepository
	Notifier       Notifier
	Locker         KeyLocker
	Clock          Clock
	IdempotencyTTL time.Duration
}

// TaskService implements the task use cases.
type TaskService struct {
	tx             TxManager
	tasks          TaskRepository
	taskLogs       TaskLogRepository
	users          UserRepository
	idempotency    IdempotencyRepository
	notifier       Notifier
	locker         KeyLocker
	now            Clock
	idempotencyTTL time.Duration
}

// NewTaskService wires a TaskService.
func NewTaskService(d TaskServiceDeps) *TaskService {
	if d.Clock == nil {
		d.Clock = time.Now
	}
	if d.IdempotencyTTL <= 0 {
		d.IdempotencyTTL = DefaultIdempotencyTTL
	}
	return &TaskService{
		tx:             d.Tx,
		tasks:          d.Tasks,
		taskLogs:       d.TaskLogs,
		users:          d.Users,
		idempotency:    d.Idempotency,
		notifier:       d.Notifier,
		locker:         d.Locker,
		now:            d.Clock,
		idempotencyTTL: d.IdempotencyTTL,
	}
}

// CreateTask creates a task owned by userID.
//
// When idempotencyKey is non-nil the call is idempotent: the first request
// creates the task and stores its snapshot, later requests with the same key
// (within the TTL) return that snapshot without creating anything.
//
// Two layers guard against duplicates under concurrency:
//   - a per-key in-process lock serializes duplicates hitting the same
//     instance, so they wait for the first request and replay its result;
//   - the key is claimed with an atomic insert inside the same transaction as
//     the task insert, so duplicates across instances block on the row lock
//     and either replay the committed result or take over after a rollback.
func (s *TaskService) CreateTask(ctx context.Context, userID uuid.UUID, in TaskInput, idempotencyKey *uuid.UUID) (*CreateTaskResult, error) {
	in, err := normalizeTaskInput(in)
	if err != nil {
		return nil, err
	}
	task := &domain.Task{
		OwnerID:     userID,
		Title:       in.Title,
		Description: in.Description,
		Status:      in.Status,
		DueDate:     in.DueDate,
	}

	if idempotencyKey == nil {
		if err := s.tasks.Create(ctx, task); err != nil {
			return nil, apperror.Internal(fmt.Errorf("create task: %w", err))
		}
		return &CreateTaskResult{Task: task}, nil
	}

	unlock := s.locker.Lock(userID.String() + ":" + idempotencyKey.String())
	defer unlock()

	requestHash := hashTaskInput(in)
	var result *CreateTaskResult

	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		now := s.now()
		existing, claimed, err := s.idempotency.Claim(ctx, &domain.IdempotencyRecord{
			UserID:      userID,
			Key:         *idempotencyKey,
			RequestHash: requestHash,
			CreatedAt:   now,
			ExpiresAt:   now.Add(s.idempotencyTTL),
		})
		if err != nil {
			return fmt.Errorf("claim idempotency key: %w", err)
		}
		if !claimed {
			result, err = replayCreateTask(existing, requestHash)
			return err
		}

		if err := s.tasks.Create(ctx, task); err != nil {
			return fmt.Errorf("create task: %w", err)
		}
		snapshot, err := encodeTaskSnapshot(task)
		if err != nil {
			return err
		}
		if err := s.idempotency.SaveResult(ctx, userID, *idempotencyKey, snapshot); err != nil {
			return fmt.Errorf("save idempotency result: %w", err)
		}
		result = &CreateTaskResult{Task: task}
		return nil
	})
	if err != nil {
		return nil, apperror.From(err)
	}
	return result, nil
}

func replayCreateTask(rec *domain.IdempotencyRecord, requestHash string) (*CreateTaskResult, error) {
	if rec.RequestHash != requestHash {
		return nil, ErrIdempotencyKeyReused
	}
	if !rec.Completed() {
		return nil, ErrIdempotencyKeyInProgress
	}
	task, err := decodeTaskSnapshot(rec.Result)
	if err != nil {
		return nil, err
	}
	return &CreateTaskResult{Task: task, Replayed: true}, nil
}

// ListTasks returns the tasks owned by or assigned to userID.
func (s *TaskService) ListTasks(ctx context.Context, userID uuid.UUID, in ListTasksInput) (*TaskPage, error) {
	filter := domain.TaskFilter{
		Search: strings.TrimSpace(in.Search),
		Page:   in.Page,
		Limit:  in.Limit,
	}
	if filter.Page < 1 {
		filter.Page = DefaultPage
	}
	if filter.Page > MaxPage {
		filter.Page = MaxPage
	}
	if filter.Limit < 1 {
		filter.Limit = DefaultLimit
	}
	if filter.Limit > MaxLimit {
		filter.Limit = MaxLimit
	}
	if in.Status != "" {
		status := domain.TaskStatus(in.Status)
		if !status.Valid() {
			return nil, ErrInvalidTaskStatus
		}
		filter.Status = &status
	}

	tasks, total, err := s.tasks.List(ctx, userID, filter)
	if err != nil {
		return nil, apperror.Internal(fmt.Errorf("list tasks: %w", err))
	}
	if tasks == nil {
		tasks = []domain.Task{}
	}

	totalPages := 0
	if total > 0 {
		totalPages = int((total + int64(filter.Limit) - 1) / int64(filter.Limit))
	}
	return &TaskPage{
		Tasks:      tasks,
		Page:       filter.Page,
		Limit:      filter.Limit,
		Total:      total,
		TotalPages: totalPages,
	}, nil
}

// GetTask returns a task visible to userID. Tasks the user cannot see are
// reported as not found so their existence is not leaked.
func (s *TaskService) GetTask(ctx context.Context, userID, taskID uuid.UUID) (*domain.Task, error) {
	task, err := s.tasks.GetByID(ctx, taskID)
	if err != nil {
		return nil, mapTaskLookupErr(err)
	}
	if !task.IsVisibleTo(userID) {
		return nil, ErrTaskNotFound
	}
	return task, nil
}

// UpdateTask applies a partial update to a task. Only the owner may update.
//
// The row is locked while the patch is merged, so concurrent updates of
// different fields cannot overwrite each other.
func (s *TaskService) UpdateTask(ctx context.Context, userID, taskID uuid.UUID, patch TaskPatch) (*domain.Task, error) {
	if patch.IsEmpty() {
		return nil, ErrNoTaskChanges
	}
	// Checked before merging: normalizeTaskInput would turn an empty status
	// into the default instead of rejecting it.
	if patch.Status != nil && !patch.Status.Valid() {
		return nil, ErrInvalidTaskStatus
	}

	var updated *domain.Task
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		task, err := s.ownedTask(ctx, s.tasks.GetByIDForUpdate, userID, taskID)
		if err != nil {
			return err
		}

		in, err := normalizeTaskInput(applyTaskPatch(task, patch))
		if err != nil {
			return err
		}
		task.Title = in.Title
		task.Description = in.Description
		task.Status = in.Status
		task.DueDate = in.DueDate

		if err := s.tasks.Update(ctx, task); err != nil {
			return mapTaskLookupErr(err)
		}
		updated = task
		return nil
	})
	if err != nil {
		return nil, apperror.From(err)
	}
	return updated, nil
}

// applyTaskPatch returns the editable fields of task with patch applied.
func applyTaskPatch(task *domain.Task, patch TaskPatch) TaskInput {
	in := TaskInput{
		Title:       task.Title,
		Description: task.Description,
		Status:      task.Status,
		DueDate:     task.DueDate,
	}
	if patch.Title != nil {
		in.Title = *patch.Title
	}
	if patch.Description != nil {
		in.Description = *patch.Description
	}
	if patch.Status != nil {
		in.Status = *patch.Status
	}
	if patch.DueDateSet {
		in.DueDate = patch.DueDate
	}
	return in
}

// DeleteTask removes a task. Only the owner may delete.
func (s *TaskService) DeleteTask(ctx context.Context, userID, taskID uuid.UUID) error {
	if _, err := s.ownedTask(ctx, s.tasks.GetByID, userID, taskID); err != nil {
		return err
	}
	if err := s.tasks.Delete(ctx, taskID); err != nil {
		return mapTaskLookupErr(err)
	}
	return nil
}

// AssignTask assigns a task to another member of the owner's team. Updating
// the assignee, writing the task log and sending the notification happen in
// one transaction: if any step fails everything is rolled back.
//
// Assigning a task to the user it is already assigned to changes nothing and
// is not logged or notified again.
func (s *TaskService) AssignTask(ctx context.Context, actorID, taskID, assigneeID uuid.UUID) (*domain.Task, error) {
	var assigned *domain.Task

	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		task, err := s.ownedTask(ctx, s.tasks.GetByIDForUpdate, actorID, taskID)
		if err != nil {
			return err
		}
		if assigneeID == task.OwnerID {
			return ErrAssigneeIsOwner
		}
		if task.AssigneeID != nil && *task.AssigneeID == assigneeID {
			assigned = task
			return nil
		}

		actor, assignee, err := s.teammates(ctx, actorID, assigneeID)
		if err != nil {
			return err
		}

		previousAssignee := task.AssigneeID
		assigned, err = s.tasks.UpdateAssignee(ctx, task.ID, assignee.ID)
		if err != nil {
			return fmt.Errorf("update assignee: %w", err)
		}

		if err := s.taskLogs.Create(ctx, &domain.TaskLog{
			TaskID:   task.ID,
			ActorID:  actor.ID,
			Action:   domain.TaskLogActionAssigned,
			OldValue: map[string]any{"assignee_id": previousAssignee},
			NewValue: map[string]any{"assignee_id": assignee.ID},
		}); err != nil {
			return fmt.Errorf("create task log: %w", err)
		}

		if err := s.notifier.NotifyTaskAssigned(ctx, assigned, assignee, actor); err != nil {
			return fmt.Errorf("notify assignee: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, apperror.From(err)
	}
	return assigned, nil
}

// teammates loads the acting user and the assignee and checks they belong to
// the same team.
func (s *TaskService) teammates(ctx context.Context, actorID, assigneeID uuid.UUID) (actor, assignee *domain.User, err error) {
	actor, err = s.users.GetByID(ctx, actorID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, nil, ErrUserNotFound
		}
		return nil, nil, fmt.Errorf("get actor: %w", err)
	}
	assignee, err = s.users.GetByID(ctx, assigneeID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, nil, ErrAssigneeNotFound
		}
		return nil, nil, fmt.Errorf("get assignee: %w", err)
	}
	if assignee.TeamID != actor.TeamID {
		return nil, nil, ErrAssigneeNotInTeam
	}
	return actor, assignee, nil
}

// ownedTask loads a task with load and checks userID owns it. Tasks the user
// cannot even see are reported as not found.
func (s *TaskService) ownedTask(ctx context.Context, load func(context.Context, uuid.UUID) (*domain.Task, error), userID, taskID uuid.UUID) (*domain.Task, error) {
	task, err := load(ctx, taskID)
	if err != nil {
		return nil, mapTaskLookupErr(err)
	}
	if !task.IsVisibleTo(userID) {
		return nil, ErrTaskNotFound
	}
	if !task.IsOwnedBy(userID) {
		return nil, ErrTaskForbidden
	}
	return task, nil
}

// normalizeTaskInput trims the text fields, applies the default status
// (domain.TaskStatusTodo), stores the due date in UTC and enforces the task rules.
func normalizeTaskInput(in TaskInput) (TaskInput, error) {
	in.Title = strings.TrimSpace(in.Title)
	in.Description = strings.TrimSpace(in.Description)
	if in.Title == "" {
		return TaskInput{}, ErrInvalidTaskTitle
	}
	if in.Status == "" {
		in.Status = domain.TaskStatusTodo
	}
	if !in.Status.Valid() {
		return TaskInput{}, ErrInvalidTaskStatus
	}
	if in.DueDate != nil {
		due := in.DueDate.UTC()
		in.DueDate = &due
	}
	return in, nil
}

// hashTaskInput fingerprints a normalized request payload, used to detect an
// Idempotency-Key being reused for a different request.
func hashTaskInput(in TaskInput) string {
	payload := struct {
		Title       string            `json:"title"`
		Description string            `json:"description"`
		Status      domain.TaskStatus `json:"status"`
		DueDate     *time.Time        `json:"due_date"`
	}{in.Title, in.Description, in.Status, in.DueDate}
	b, _ := json.Marshal(payload)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func mapTaskLookupErr(err error) error {
	if errors.Is(err, domain.ErrNotFound) {
		return ErrTaskNotFound
	}
	return apperror.Internal(err)
}
