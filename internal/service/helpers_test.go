package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"technical-test-be-gdc-multi-sarana/internal/apperror"
	"technical-test-be-gdc-multi-sarana/internal/domain"
	"technical-test-be-gdc-multi-sarana/internal/pkg/keylock"
	"technical-test-be-gdc-multi-sarana/internal/service"
	"technical-test-be-gdc-multi-sarana/internal/service/servicetest"
)

var errBoom = errors.New("boom")

// env is a TaskService wired to in-memory fakes, with an owner and a teammate
// in team Alpha and an outsider in team Beta.
type env struct {
	store    *servicetest.Store
	tx       *servicetest.TxManager
	notifier *servicetest.Notifier
	clock    *servicetest.Clock
	tasks    *service.TaskService

	owner, teammate, outsider domain.User
}

func newEnv(t *testing.T, opts ...func(*service.TaskServiceDeps)) *env {
	t.Helper()
	store := servicetest.NewStore()
	alpha := store.AddTeam("Alpha")
	beta := store.AddTeam("Beta")
	e := &env{
		store:    store,
		tx:       &servicetest.TxManager{},
		notifier: &servicetest.Notifier{},
		clock:    servicetest.NewClock(time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)),
		owner:    store.AddUser(alpha.ID, "Owner", "owner@example.com"),
		teammate: store.AddUser(alpha.ID, "Teammate", "teammate@example.com"),
		outsider: store.AddUser(beta.ID, "Outsider", "outsider@example.com"),
	}
	deps := service.TaskServiceDeps{
		Tx:             e.tx,
		Tasks:          store.TaskRepository(),
		TaskLogs:       store.TaskLogRepository(),
		Users:          store.UserRepository(),
		Idempotency:    store.IdempotencyRepository(),
		Notifier:       e.notifier,
		Locker:         keylock.New(),
		Clock:          e.clock.Now,
		IdempotencyTTL: 24 * time.Hour,
	}
	for _, opt := range opts {
		opt(&deps)
	}
	e.tasks = service.NewTaskService(deps)
	return e
}

// createTask creates a task owned by e.owner.
func (e *env) createTask(t *testing.T, in service.TaskInput) *domain.Task {
	t.Helper()
	res, err := e.tasks.CreateTask(context.Background(), e.owner.ID, in, nil)
	require.NoError(t, err)
	return res.Task
}

// assignedTask creates a task owned by e.owner and assigned to e.teammate.
func (e *env) assignedTask(t *testing.T) *domain.Task {
	t.Helper()
	task := e.createTask(t, service.TaskInput{Title: "Assigned"})
	_, err := e.tasks.AssignTask(context.Background(), e.owner.ID, task.ID, e.teammate.ID)
	require.NoError(t, err)
	return task
}

func requireKind(t *testing.T, err error, kind apperror.Kind) {
	t.Helper()
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, kind, appErr.Kind, "error: %v", err)
}

func ptr[T any](v T) *T { return &v }

// The failing* wrappers return the configured error from one method and
// delegate everything else to the wrapped fake.

type failingTasks struct {
	service.TaskRepository
	create, getByID, getForUpdate, list, update, updateAssignee, del error
}

func (f failingTasks) Create(ctx context.Context, t *domain.Task) error {
	if f.create != nil {
		return f.create
	}
	return f.TaskRepository.Create(ctx, t)
}

func (f failingTasks) GetByID(ctx context.Context, id uuid.UUID) (*domain.Task, error) {
	if f.getByID != nil {
		return nil, f.getByID
	}
	return f.TaskRepository.GetByID(ctx, id)
}

func (f failingTasks) GetByIDForUpdate(ctx context.Context, id uuid.UUID) (*domain.Task, error) {
	if f.getForUpdate != nil {
		return nil, f.getForUpdate
	}
	return f.TaskRepository.GetByIDForUpdate(ctx, id)
}

func (f failingTasks) List(ctx context.Context, userID uuid.UUID, filter domain.TaskFilter) ([]domain.Task, int64, error) {
	if f.list != nil {
		return nil, 0, f.list
	}
	return f.TaskRepository.List(ctx, userID, filter)
}

func (f failingTasks) Update(ctx context.Context, t *domain.Task) error {
	if f.update != nil {
		return f.update
	}
	return f.TaskRepository.Update(ctx, t)
}

func (f failingTasks) UpdateAssignee(ctx context.Context, taskID, assigneeID uuid.UUID) (*domain.Task, error) {
	if f.updateAssignee != nil {
		return nil, f.updateAssignee
	}
	return f.TaskRepository.UpdateAssignee(ctx, taskID, assigneeID)
}

func (f failingTasks) Delete(ctx context.Context, id uuid.UUID) error {
	if f.del != nil {
		return f.del
	}
	return f.TaskRepository.Delete(ctx, id)
}

type failingUsers struct {
	service.UserRepository
	create, getByEmail, listByTeam error
	// getByID fails only for the listed user ids.
	getByID map[uuid.UUID]error
}

func (f failingUsers) Create(ctx context.Context, u *domain.User) error {
	if f.create != nil {
		return f.create
	}
	return f.UserRepository.Create(ctx, u)
}

func (f failingUsers) GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	if err := f.getByID[id]; err != nil {
		return nil, err
	}
	return f.UserRepository.GetByID(ctx, id)
}

func (f failingUsers) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	if f.getByEmail != nil {
		return nil, f.getByEmail
	}
	return f.UserRepository.GetByEmail(ctx, email)
}

func (f failingUsers) ListByTeam(ctx context.Context, teamID uuid.UUID) ([]domain.User, error) {
	if f.listByTeam != nil {
		return nil, f.listByTeam
	}
	return f.UserRepository.ListByTeam(ctx, teamID)
}

type failingTeams struct {
	service.TeamRepository
	create, getByID, getByInviteCode error
}

func (f failingTeams) Create(ctx context.Context, name string) (*domain.Team, error) {
	if f.create != nil {
		return nil, f.create
	}
	return f.TeamRepository.Create(ctx, name)
}

func (f failingTeams) GetByID(ctx context.Context, id uuid.UUID) (*domain.Team, error) {
	if f.getByID != nil {
		return nil, f.getByID
	}
	return f.TeamRepository.GetByID(ctx, id)
}

func (f failingTeams) GetByInviteCode(ctx context.Context, code string) (*domain.Team, error) {
	if f.getByInviteCode != nil {
		return nil, f.getByInviteCode
	}
	return f.TeamRepository.GetByInviteCode(ctx, code)
}

type failingTaskLogs struct {
	service.TaskLogRepository
	create error
}

func (f failingTaskLogs) Create(ctx context.Context, l *domain.TaskLog) error {
	if f.create != nil {
		return f.create
	}
	return f.TaskLogRepository.Create(ctx, l)
}

type failingIdempotency struct {
	service.IdempotencyRepository
	claim, saveResult error
}

func (f failingIdempotency) Claim(ctx context.Context, rec *domain.IdempotencyRecord) (*domain.IdempotencyRecord, bool, error) {
	if f.claim != nil {
		return nil, false, f.claim
	}
	return f.IdempotencyRepository.Claim(ctx, rec)
}

func (f failingIdempotency) SaveResult(ctx context.Context, userID, key uuid.UUID, result []byte) error {
	if f.saveResult != nil {
		return f.saveResult
	}
	return f.IdempotencyRepository.SaveResult(ctx, userID, key, result)
}
