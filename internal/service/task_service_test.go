package service_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"technical-test-be-gdc-multi-sarana/internal/apperror"
	"technical-test-be-gdc-multi-sarana/internal/domain"
	"technical-test-be-gdc-multi-sarana/internal/service"
	"technical-test-be-gdc-multi-sarana/internal/service/servicetest"
)

func TestCreateTask_NormalizesInput(t *testing.T) {
	e := newEnv(t)
	due := time.Date(2026, 10, 20, 10, 0, 0, 0, time.FixedZone("WIB", 7*3600))

	res, err := e.tasks.CreateTask(context.Background(), e.owner.ID, service.TaskInput{
		Title:       "  Write report  ",
		Description: "  Q3  ",
		DueDate:     &due,
	}, nil)

	require.NoError(t, err)
	task := res.Task
	assert.False(t, res.Replayed)
	assert.Equal(t, "Write report", task.Title)
	assert.Equal(t, "Q3", task.Description)
	assert.Equal(t, domain.TaskStatusTodo, task.Status, "status defaults to todo")
	assert.Equal(t, time.UTC, task.DueDate.Location())
	assert.True(t, task.DueDate.Equal(due))
	assert.Equal(t, e.owner.ID, task.OwnerID)
	assert.Nil(t, task.AssigneeID)
	assert.EqualValues(t, 0, e.tx.Commits(), "without a key no transaction is needed")
}

func TestCreateTask_RejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name string
		in   service.TaskInput
		want *apperror.Error
	}{
		{"blank title", service.TaskInput{Title: "   "}, service.ErrInvalidTaskTitle},
		{"unknown status", service.TaskInput{Title: "x", Status: "archived"}, service.ErrInvalidTaskStatus},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			key := uuid.New()
			for _, k := range []*uuid.UUID{nil, &key} {
				_, err := e.tasks.CreateTask(context.Background(), e.owner.ID, tc.in, k)
				assert.ErrorIs(t, err, tc.want)
			}
			assert.Empty(t, e.store.AllTasks())
		})
	}
}

func TestCreateTask_StorageFailure(t *testing.T) {
	e := newEnv(t, func(d *service.TaskServiceDeps) {
		d.Tasks = failingTasks{TaskRepository: d.Tasks, create: errBoom}
	})

	_, err := e.tasks.CreateTask(context.Background(), e.owner.ID, service.TaskInput{Title: "x"}, nil)

	requireKind(t, err, apperror.KindInternal)
	assert.ErrorIs(t, err, errBoom, "the cause is kept for logging")
}

func TestListTasks(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	for _, in := range []service.TaskInput{
		{Title: "Write report", Status: domain.TaskStatusTodo},
		{Title: "Fix bug", Status: domain.TaskStatusInProgress},
		{Title: "Review report", Status: domain.TaskStatusDone},
	} {
		e.createTask(t, in)
	}
	assigned := e.assignedTask(t) // owner's 4th task, assigned to the teammate

	t.Run("defaults and newest first", func(t *testing.T) {
		page, err := e.tasks.ListTasks(ctx, e.owner.ID, service.ListTasksInput{})
		require.NoError(t, err)
		assert.Equal(t, service.DefaultPage, page.Page)
		assert.Equal(t, service.DefaultLimit, page.Limit)
		assert.EqualValues(t, 4, page.Total)
		assert.Equal(t, 1, page.TotalPages)
		require.Len(t, page.Tasks, 4)
		assert.Equal(t, assigned.ID, page.Tasks[0].ID)
	})

	t.Run("pagination", func(t *testing.T) {
		page, err := e.tasks.ListTasks(ctx, e.owner.ID, service.ListTasksInput{Page: 2, Limit: 3})
		require.NoError(t, err)
		assert.Len(t, page.Tasks, 1)
		assert.EqualValues(t, 4, page.Total)
		assert.Equal(t, 2, page.TotalPages)
	})

	t.Run("limit and page are clamped", func(t *testing.T) {
		page, err := e.tasks.ListTasks(ctx, e.owner.ID, service.ListTasksInput{Page: service.MaxPage + 1, Limit: 1000})
		require.NoError(t, err)
		assert.Equal(t, service.MaxPage, page.Page)
		assert.Equal(t, service.MaxLimit, page.Limit)
		assert.Empty(t, page.Tasks)
		assert.NotNil(t, page.Tasks, "an empty page is an empty list, not null")
	})

	t.Run("status filter and search", func(t *testing.T) {
		page, err := e.tasks.ListTasks(ctx, e.owner.ID, service.ListTasksInput{Status: "done", Search: "  REPORT "})
		require.NoError(t, err)
		require.Len(t, page.Tasks, 1)
		assert.Equal(t, "Review report", page.Tasks[0].Title)
	})

	t.Run("assignee sees the task assigned to them", func(t *testing.T) {
		page, err := e.tasks.ListTasks(ctx, e.teammate.ID, service.ListTasksInput{})
		require.NoError(t, err)
		require.Len(t, page.Tasks, 1)
		assert.Equal(t, assigned.ID, page.Tasks[0].ID)
	})

	t.Run("other users see nothing", func(t *testing.T) {
		page, err := e.tasks.ListTasks(ctx, e.outsider.ID, service.ListTasksInput{})
		require.NoError(t, err)
		assert.Empty(t, page.Tasks)
		assert.Zero(t, page.TotalPages)
	})

	t.Run("invalid status", func(t *testing.T) {
		_, err := e.tasks.ListTasks(ctx, e.owner.ID, service.ListTasksInput{Status: "archived"})
		assert.ErrorIs(t, err, service.ErrInvalidTaskStatus)
	})
}

func TestListTasks_StorageFailure(t *testing.T) {
	e := newEnv(t, func(d *service.TaskServiceDeps) {
		d.Tasks = failingTasks{TaskRepository: d.Tasks, list: errBoom}
	})

	_, err := e.tasks.ListTasks(context.Background(), e.owner.ID, service.ListTasksInput{})

	requireKind(t, err, apperror.KindInternal)
}

func TestGetTask(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	task := e.assignedTask(t)

	for _, user := range []domain.User{e.owner, e.teammate} {
		got, err := e.tasks.GetTask(ctx, user.ID, task.ID)
		require.NoError(t, err, user.Name)
		assert.Equal(t, task.ID, got.ID)
	}

	_, err := e.tasks.GetTask(ctx, e.outsider.ID, task.ID)
	assert.ErrorIs(t, err, service.ErrTaskNotFound, "an invisible task is reported as missing")

	_, err = e.tasks.GetTask(ctx, e.owner.ID, uuid.New())
	assert.ErrorIs(t, err, service.ErrTaskNotFound)
}

func TestGetTask_StorageFailure(t *testing.T) {
	e := newEnv(t, func(d *service.TaskServiceDeps) {
		d.Tasks = failingTasks{TaskRepository: d.Tasks, getByID: errBoom}
	})

	_, err := e.tasks.GetTask(context.Background(), e.owner.ID, uuid.New())

	requireKind(t, err, apperror.KindInternal)
}

func TestUpdateTask_PartialUpdate(t *testing.T) {
	due := time.Date(2026, 10, 20, 3, 0, 0, 0, time.UTC)
	newDue := time.Date(2026, 11, 1, 10, 0, 0, 0, time.FixedZone("WIB", 7*3600))
	original := service.TaskInput{Title: "Report", Description: "Q3", Status: domain.TaskStatusTodo, DueDate: &due}

	tests := []struct {
		name  string
		patch service.TaskPatch
		check func(t *testing.T, got domain.Task)
	}{
		{"title only", service.TaskPatch{Title: ptr("  Report v2 ")}, func(t *testing.T, got domain.Task) {
			assert.Equal(t, "Report v2", got.Title)
			assert.Equal(t, "Q3", got.Description)
			assert.Equal(t, domain.TaskStatusTodo, got.Status)
			assert.True(t, got.DueDate.Equal(due))
		}},
		{"description only", service.TaskPatch{Description: ptr("")}, func(t *testing.T, got domain.Task) {
			assert.Equal(t, "Report", got.Title)
			assert.Empty(t, got.Description)
		}},
		{"status only", service.TaskPatch{Status: ptr(domain.TaskStatusDone)}, func(t *testing.T, got domain.Task) {
			assert.Equal(t, domain.TaskStatusDone, got.Status)
			assert.Equal(t, "Report", got.Title)
		}},
		{"new due date in UTC", service.TaskPatch{DueDate: &newDue, DueDateSet: true}, func(t *testing.T, got domain.Task) {
			assert.True(t, got.DueDate.Equal(newDue))
			assert.Equal(t, time.UTC, got.DueDate.Location())
		}},
		{"clear due date", service.TaskPatch{DueDateSet: true}, func(t *testing.T, got domain.Task) {
			assert.Nil(t, got.DueDate)
			assert.Equal(t, "Q3", got.Description)
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			task := e.createTask(t, original)

			got, err := e.tasks.UpdateTask(context.Background(), e.owner.ID, task.ID, tc.patch)

			require.NoError(t, err)
			tc.check(t, *got)
			stored, _ := e.store.Task(task.ID)
			assert.Equal(t, *got, stored, "the returned task is what was stored")
		})
	}
}

func TestUpdateTask_Rejections(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	task := e.assignedTask(t)

	tests := []struct {
		name   string
		userID uuid.UUID
		taskID uuid.UUID
		patch  service.TaskPatch
		want   *apperror.Error
	}{
		{"empty patch", e.owner.ID, task.ID, service.TaskPatch{}, service.ErrNoTaskChanges},
		{"empty status", e.owner.ID, task.ID, service.TaskPatch{Status: ptr(domain.TaskStatus(""))}, service.ErrInvalidTaskStatus},
		{"unknown status", e.owner.ID, task.ID, service.TaskPatch{Status: ptr(domain.TaskStatus("archived"))}, service.ErrInvalidTaskStatus},
		{"blank title", e.owner.ID, task.ID, service.TaskPatch{Title: ptr("  ")}, service.ErrInvalidTaskTitle},
		{"assignee is not the owner", e.teammate.ID, task.ID, service.TaskPatch{Title: ptr("x")}, service.ErrTaskForbidden},
		{"invisible task", e.outsider.ID, task.ID, service.TaskPatch{Title: ptr("x")}, service.ErrTaskNotFound},
		{"missing task", e.owner.ID, uuid.New(), service.TaskPatch{Title: ptr("x")}, service.ErrTaskNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := e.tasks.UpdateTask(ctx, tc.userID, tc.taskID, tc.patch)
			assert.ErrorIs(t, err, tc.want)
		})
	}
	stored, _ := e.store.Task(task.ID)
	assert.Equal(t, "Assigned", stored.Title, "rejected updates change nothing")
}

func TestUpdateTask_StorageFailure(t *testing.T) {
	e := newEnv(t, func(d *service.TaskServiceDeps) {
		d.Tasks = failingTasks{TaskRepository: d.Tasks, update: errBoom}
	})
	task := e.createTask(t, service.TaskInput{Title: "x"})

	_, err := e.tasks.UpdateTask(context.Background(), e.owner.ID, task.ID, service.TaskPatch{Title: ptr("y")})

	requireKind(t, err, apperror.KindInternal)
	assert.EqualValues(t, 1, e.tx.Rollbacks())
}

func TestUpdateTask_ConcurrentPatchesOfDifferentFieldsAreAllKept(t *testing.T) {
	e := newEnv(t)
	task := e.createTask(t, service.TaskInput{Title: "Original"})
	patches := []service.TaskPatch{
		{Title: ptr("New title")},
		{Description: ptr("New description")},
		{Status: ptr(domain.TaskStatusInProgress)},
	}

	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, p := range patches {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := e.tasks.UpdateTask(context.Background(), e.owner.ID, task.ID, p)
			assert.NoError(t, err)
		}()
	}
	close(start)
	wg.Wait()

	stored, _ := e.store.Task(task.ID)
	assert.Equal(t, "New title", stored.Title)
	assert.Equal(t, "New description", stored.Description)
	assert.Equal(t, domain.TaskStatusInProgress, stored.Status)
}

func TestDeleteTask(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	task := e.assignedTask(t)

	assert.ErrorIs(t, e.tasks.DeleteTask(ctx, e.teammate.ID, task.ID), service.ErrTaskForbidden)
	assert.ErrorIs(t, e.tasks.DeleteTask(ctx, e.outsider.ID, task.ID), service.ErrTaskNotFound)
	assert.ErrorIs(t, e.tasks.DeleteTask(ctx, e.owner.ID, uuid.New()), service.ErrTaskNotFound)

	require.NoError(t, e.tasks.DeleteTask(ctx, e.owner.ID, task.ID))
	_, exists := e.store.Task(task.ID)
	assert.False(t, exists)
}

func TestDeleteTask_StorageFailure(t *testing.T) {
	tests := []struct {
		name string
		fail failingTasks
		want apperror.Kind
	}{
		{"delete fails", failingTasks{del: errBoom}, apperror.KindInternal},
		{"task vanished in between", failingTasks{del: domain.ErrNotFound}, apperror.KindNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, func(d *service.TaskServiceDeps) {
				tc.fail.TaskRepository = d.Tasks
				d.Tasks = tc.fail
			})
			task := e.createTask(t, service.TaskInput{Title: "x"})

			requireKind(t, e.tasks.DeleteTask(context.Background(), e.owner.ID, task.ID), tc.want)
		})
	}
}

func TestAssignTask_UpdatesLogsAndNotifiesInOneTransaction(t *testing.T) {
	e := newEnv(t)
	task := e.createTask(t, service.TaskInput{Title: "Report"})

	got, err := e.tasks.AssignTask(context.Background(), e.owner.ID, task.ID, e.teammate.ID)

	require.NoError(t, err)
	require.NotNil(t, got.AssigneeID)
	assert.Equal(t, e.teammate.ID, *got.AssigneeID)

	logs := e.store.AllTaskLogs()
	require.Len(t, logs, 1)
	assert.Equal(t, task.ID, logs[0].TaskID)
	assert.Equal(t, e.owner.ID, logs[0].ActorID)
	assert.Equal(t, domain.TaskLogActionAssigned, logs[0].Action)
	assert.Equal(t, map[string]any{"assignee_id": (*uuid.UUID)(nil)}, logs[0].OldValue)
	assert.Equal(t, map[string]any{"assignee_id": e.teammate.ID}, logs[0].NewValue)

	assert.Equal(t, []servicetest.Notification{{TaskID: task.ID, AssigneeID: e.teammate.ID, ActorID: e.owner.ID}}, e.notifier.Sent())
	assert.EqualValues(t, 1, e.tx.Commits())
}

func TestAssignTask_ReassignRecordsPreviousAssignee(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	third := e.store.AddUser(e.owner.TeamID, "Third", "third@example.com")
	task := e.assignedTask(t)

	_, err := e.tasks.AssignTask(ctx, e.owner.ID, task.ID, third.ID)

	require.NoError(t, err)
	logs := e.store.AllTaskLogs()
	require.Len(t, logs, 2)
	assert.Equal(t, map[string]any{"assignee_id": &e.teammate.ID}, logs[1].OldValue)
	assert.Equal(t, map[string]any{"assignee_id": third.ID}, logs[1].NewValue)
}

func TestAssignTask_SameAssigneeIsANoOp(t *testing.T) {
	e := newEnv(t)
	task := e.assignedTask(t)

	got, err := e.tasks.AssignTask(context.Background(), e.owner.ID, task.ID, e.teammate.ID)

	require.NoError(t, err)
	assert.Equal(t, e.teammate.ID, *got.AssigneeID)
	assert.Len(t, e.store.AllTaskLogs(), 1, "no new log")
	assert.Len(t, e.notifier.Sent(), 1, "no new notification")
}

func TestAssignTask_Rejections(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	task := e.createTask(t, service.TaskInput{Title: "Report"})

	tests := []struct {
		name       string
		actorID    uuid.UUID
		taskID     uuid.UUID
		assigneeID uuid.UUID
		want       *apperror.Error
	}{
		{"assign to the owner", e.owner.ID, task.ID, e.owner.ID, service.ErrAssigneeIsOwner},
		{"assignee in another team", e.owner.ID, task.ID, e.outsider.ID, service.ErrAssigneeNotInTeam},
		{"unknown assignee", e.owner.ID, task.ID, uuid.New(), service.ErrAssigneeNotFound},
		{"actor is not the owner", e.teammate.ID, e.assignedTask(t).ID, e.owner.ID, service.ErrTaskForbidden},
		{"invisible task", e.outsider.ID, task.ID, e.teammate.ID, service.ErrTaskNotFound},
		{"missing task", e.owner.ID, uuid.New(), e.teammate.ID, service.ErrTaskNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := e.tasks.AssignTask(ctx, tc.actorID, tc.taskID, tc.assigneeID)
			assert.ErrorIs(t, err, tc.want)
		})
	}
	stored, _ := e.store.Task(task.ID)
	assert.Nil(t, stored.AssigneeID)
}

func TestAssignTask_AnyFailedStepRollsBackEverything(t *testing.T) {
	tests := []struct {
		name string
		deps func(e *env, d *service.TaskServiceDeps)
		want apperror.Kind
	}{
		{"loading the task fails", func(_ *env, d *service.TaskServiceDeps) {
			d.Tasks = failingTasks{TaskRepository: d.Tasks, getForUpdate: errBoom}
		}, apperror.KindInternal},
		{"actor lookup fails", func(e *env, d *service.TaskServiceDeps) {
			d.Users = failingUsers{UserRepository: d.Users, getByID: map[uuid.UUID]error{e.owner.ID: errBoom}}
		}, apperror.KindInternal},
		{"actor no longer exists", func(e *env, d *service.TaskServiceDeps) {
			d.Users = failingUsers{UserRepository: d.Users, getByID: map[uuid.UUID]error{e.owner.ID: domain.ErrNotFound}}
		}, apperror.KindNotFound},
		{"assignee lookup fails", func(e *env, d *service.TaskServiceDeps) {
			d.Users = failingUsers{UserRepository: d.Users, getByID: map[uuid.UUID]error{e.teammate.ID: errBoom}}
		}, apperror.KindInternal},
		{"updating the assignee fails", func(_ *env, d *service.TaskServiceDeps) {
			d.Tasks = failingTasks{TaskRepository: d.Tasks, updateAssignee: errBoom}
		}, apperror.KindInternal},
		{"writing the task log fails", func(_ *env, d *service.TaskServiceDeps) {
			d.TaskLogs = failingTaskLogs{TaskLogRepository: d.TaskLogs, create: errBoom}
		}, apperror.KindInternal},
		{"sending the notification fails", func(_ *env, d *service.TaskServiceDeps) {
			d.Notifier = &servicetest.Notifier{Err: errBoom}
		}, apperror.KindInternal},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// The users are created first so the failure can target their ids.
			base := newEnv(t)
			e := newEnvFrom(t, base, func(d *service.TaskServiceDeps) { tc.deps(base, d) })
			task := e.createTask(t, service.TaskInput{Title: "Report"})

			_, err := e.tasks.AssignTask(context.Background(), e.owner.ID, task.ID, e.teammate.ID)

			requireKind(t, err, tc.want)
			stored, _ := e.store.Task(task.ID)
			assert.Nil(t, stored.AssigneeID, "the assignee change is rolled back")
			assert.Empty(t, e.store.AllTaskLogs(), "the task log is rolled back")
			assert.EqualValues(t, 1, e.tx.Rollbacks())
		})
	}
}

func TestAssignTask_ConcurrentAssignmentsKeepAConsistentHistory(t *testing.T) {
	e := newEnv(t)
	third := e.store.AddUser(e.owner.TeamID, "Third", "third@example.com")
	task := e.createTask(t, service.TaskInput{Title: "Contended"})
	assignees := []uuid.UUID{e.teammate.ID, third.ID}

	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := e.tasks.AssignTask(context.Background(), e.owner.ID, task.ID, assignees[i%2])
			assert.NoError(t, err)
		}()
	}
	close(start)
	wg.Wait()

	// Every entry must start from the assignee the previous one set: the row
	// lock prevents two assignments from reading the same old value.
	logs := e.store.AllTaskLogs()
	require.NotEmpty(t, logs)
	var previous *uuid.UUID
	for i, l := range logs {
		old := l.OldValue["assignee_id"]
		if previous == nil {
			assert.Equal(t, (*uuid.UUID)(nil), old, "entry %d", i)
		} else {
			assert.Equal(t, previous, old, "entry %d", i)
		}
		next := l.NewValue["assignee_id"].(uuid.UUID)
		previous = &next
	}
	stored, _ := e.store.Task(task.ID)
	assert.Equal(t, previous, stored.AssigneeID, "the last entry matches the stored assignee")
	assert.Len(t, e.notifier.Sent(), len(logs), "one notification per real change")
}

// newEnvFrom builds a TaskService on base's store and users.
func newEnvFrom(t *testing.T, base *env, opts ...func(*service.TaskServiceDeps)) *env {
	t.Helper()
	e := *base
	e.tx = &servicetest.TxManager{}
	e.notifier = &servicetest.Notifier{}
	deps := service.TaskServiceDeps{
		Tx:          e.tx,
		Tasks:       base.store.TaskRepository(),
		TaskLogs:    base.store.TaskLogRepository(),
		Users:       base.store.UserRepository(),
		Idempotency: base.store.IdempotencyRepository(),
		Notifier:    e.notifier,
		Locker:      servicetest.NoLock{},
		Clock:       e.clock.Now,
	}
	for _, opt := range opts {
		opt(&deps)
	}
	e.tasks = service.NewTaskService(deps)
	return &e
}
