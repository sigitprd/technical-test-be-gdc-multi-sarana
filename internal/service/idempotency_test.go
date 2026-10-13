package service_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"technical-test-be-gdc-multi-sarana/internal/apperror"
	"technical-test-be-gdc-multi-sarana/internal/domain"
	"technical-test-be-gdc-multi-sarana/internal/pkg/keylock"
	"technical-test-be-gdc-multi-sarana/internal/service"
	"technical-test-be-gdc-multi-sarana/internal/service/servicetest"
)

func TestCreateTask_Idempotency_RetryReturnsFirstResult(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	key := uuid.New()
	in := service.TaskInput{
		Title:       "Write report",
		Description: "Q3 numbers",
		DueDate:     ptr(time.Date(2026, 10, 20, 10, 0, 0, 0, time.FixedZone("WIB", 7*3600))),
	}

	first, err := e.tasks.CreateTask(ctx, e.owner.ID, in, &key)
	require.NoError(t, err)
	assert.False(t, first.Replayed)

	e.clock.Advance(23 * time.Hour) // still inside the 24h window
	second, err := e.tasks.CreateTask(ctx, e.owner.ID, in, &key)
	require.NoError(t, err)

	assert.True(t, second.Replayed)
	assert.Equal(t, first.Task, second.Task, "the retry must return exactly the first result")
	assert.Len(t, e.store.AllTasks(), 1, "the retry must not create a task")
}

func TestCreateTask_Idempotency_EquivalentPayloadIsReplayed(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	key := uuid.New()
	due := time.Date(2026, 10, 20, 3, 0, 0, 0, time.UTC)

	first, err := e.tasks.CreateTask(ctx, e.owner.ID, service.TaskInput{Title: "Report", DueDate: &due}, &key)
	require.NoError(t, err)

	// Same request after normalization: spaces, explicit default status and
	// the same instant in another time zone.
	same := service.TaskInput{Title: "  Report ", Status: domain.TaskStatusTodo, DueDate: ptr(due.In(time.FixedZone("WIB", 7*3600)))}
	second, err := e.tasks.CreateTask(ctx, e.owner.ID, same, &key)
	require.NoError(t, err)

	assert.True(t, second.Replayed)
	assert.Equal(t, first.Task.ID, second.Task.ID)
}

func TestCreateTask_Idempotency_ConcurrentDuplicatesCreateOneTask(t *testing.T) {
	tests := []struct {
		name   string
		locker service.KeyLocker
	}{
		// Both guards together, as in production.
		{name: "in-process lock and database lock", locker: keylock.New()},
		// Only the claim in the database transaction, as when duplicates hit
		// different API instances.
		{name: "database lock only", locker: servicetest.NoLock{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, func(d *service.TaskServiceDeps) { d.Locker = tc.locker })
			key := uuid.New()
			in := service.TaskInput{Title: "Concurrent", Description: "same request sent many times"}

			results, errs := createConcurrently(e, 100, func(int) (service.TaskInput, *uuid.UUID) { return in, &key })

			tasks := e.store.AllTasks()
			require.Len(t, tasks, 1, "exactly one task may be created")
			created := 0
			for i := range results {
				require.NoError(t, errs[i])
				assert.Equal(t, tasks[0], *results[i].Task, "every request gets the same task")
				if !results[i].Replayed {
					created++
				}
			}
			assert.Equal(t, 1, created, "exactly one request creates the task, the others replay it")
			assert.EqualValues(t, 100, e.tx.Commits())
		})
	}
}

func TestCreateTask_Idempotency_ConcurrentDistinctKeysCreateOneTaskEach(t *testing.T) {
	e := newEnv(t)
	keys := make([]uuid.UUID, 50)
	for i := range keys {
		keys[i] = uuid.New()
	}

	results, errs := createConcurrently(e, len(keys), func(i int) (service.TaskInput, *uuid.UUID) {
		return service.TaskInput{Title: "Task"}, &keys[i]
	})

	ids := make(map[uuid.UUID]bool)
	for i := range results {
		require.NoError(t, errs[i])
		assert.False(t, results[i].Replayed)
		ids[results[i].Task.ID] = true
	}
	assert.Len(t, ids, len(keys))
	assert.Len(t, e.store.AllTasks(), len(keys))
}

func TestCreateTask_Idempotency_ConcurrentConflictingPayloads(t *testing.T) {
	e := newEnv(t)
	key := uuid.New()

	// Half of the requests use another payload with the same key.
	results, errs := createConcurrently(e, 40, func(i int) (service.TaskInput, *uuid.UUID) {
		return service.TaskInput{Title: []string{"First", "Second"}[i%2]}, &key
	})

	tasks := e.store.AllTasks()
	require.Len(t, tasks, 1)
	var replays, rejected int
	for i := range results {
		if errs[i] != nil {
			assert.ErrorIs(t, errs[i], service.ErrIdempotencyKeyReused)
			rejected++
			continue
		}
		assert.Equal(t, tasks[0].ID, results[i].Task.ID)
		if results[i].Replayed {
			replays++
		}
	}
	assert.Equal(t, 20, rejected, "requests with the other payload are rejected")
	assert.Equal(t, 19, replays, "requests with the winning payload replay it")
}

func TestCreateTask_Idempotency_KeyReusedWithDifferentPayload(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	key := uuid.New()

	_, err := e.tasks.CreateTask(ctx, e.owner.ID, service.TaskInput{Title: "Original"}, &key)
	require.NoError(t, err)
	_, err = e.tasks.CreateTask(ctx, e.owner.ID, service.TaskInput{Title: "Changed"}, &key)

	assert.ErrorIs(t, err, service.ErrIdempotencyKeyReused)
	assert.Len(t, e.store.AllTasks(), 1)
}

func TestCreateTask_Idempotency_KeysAreScopedPerUser(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	key := uuid.New()
	in := service.TaskInput{Title: "Shared key"}

	mine, err := e.tasks.CreateTask(ctx, e.owner.ID, in, &key)
	require.NoError(t, err)
	theirs, err := e.tasks.CreateTask(ctx, e.teammate.ID, in, &key)
	require.NoError(t, err)

	assert.False(t, theirs.Replayed)
	assert.NotEqual(t, mine.Task.ID, theirs.Task.ID)
	assert.Equal(t, e.teammate.ID, theirs.Task.OwnerID)
}

func TestCreateTask_Idempotency_ExpiredKeyCreatesNewTask(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	key := uuid.New()
	in := service.TaskInput{Title: "Daily"}

	first, err := e.tasks.CreateTask(ctx, e.owner.ID, in, &key)
	require.NoError(t, err)
	e.clock.Advance(24 * time.Hour)
	second, err := e.tasks.CreateTask(ctx, e.owner.ID, in, &key)
	require.NoError(t, err)

	assert.False(t, second.Replayed)
	assert.NotEqual(t, first.Task.ID, second.Task.ID)
	assert.Len(t, e.store.AllTasks(), 2)
}

func TestCreateTask_Idempotency_FailedAttemptCanBeRetried(t *testing.T) {
	e := newEnv(t, func(d *service.TaskServiceDeps) {
		d.Tasks = &flakyCreate{TaskRepository: d.Tasks}
	})
	ctx := context.Background()
	key := uuid.New()
	in := service.TaskInput{Title: "Retry me"}

	_, err := e.tasks.CreateTask(ctx, e.owner.ID, in, &key)
	requireKind(t, err, apperror.KindInternal)
	_, claimed := e.store.IdempotencyRecord(e.owner.ID, key)
	assert.False(t, claimed, "the rollback releases the key")

	res, err := e.tasks.CreateTask(ctx, e.owner.ID, in, &key)
	require.NoError(t, err)
	assert.False(t, res.Replayed)
	assert.Len(t, e.store.AllTasks(), 1)
}

// flakyCreate fails the first Create and delegates afterwards.
type flakyCreate struct {
	service.TaskRepository
	calls atomic.Int32
}

func (f *flakyCreate) Create(ctx context.Context, t *domain.Task) error {
	if f.calls.Add(1) == 1 {
		return errBoom
	}
	return f.TaskRepository.Create(ctx, t)
}

func TestCreateTask_Idempotency_RequestStillInFlight(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	key := uuid.New()
	in := service.TaskInput{Title: "In flight"}
	_, err := e.tasks.CreateTask(ctx, e.owner.ID, in, &key)
	require.NoError(t, err)

	// A record without a result: the first request has not finished. With
	// PostgreSQL a concurrent request waits instead of seeing this state.
	rec, _ := e.store.IdempotencyRecord(e.owner.ID, key)
	rec.Result = nil
	e.store.PutIdempotencyRecord(rec)

	_, err = e.tasks.CreateTask(ctx, e.owner.ID, in, &key)
	assert.ErrorIs(t, err, service.ErrIdempotencyKeyInProgress)
}

func TestCreateTask_Idempotency_CorruptSnapshot(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	key := uuid.New()
	in := service.TaskInput{Title: "Corrupt"}
	_, err := e.tasks.CreateTask(ctx, e.owner.ID, in, &key)
	require.NoError(t, err)

	rec, _ := e.store.IdempotencyRecord(e.owner.ID, key)
	rec.Result = []byte("{not json")
	e.store.PutIdempotencyRecord(rec)

	_, err = e.tasks.CreateTask(ctx, e.owner.ID, in, &key)
	requireKind(t, err, apperror.KindInternal)
}

func TestCreateTask_Idempotency_StorageFailuresRollBack(t *testing.T) {
	tests := []struct {
		name string
		deps func(*service.TaskServiceDeps)
	}{
		{"begin fails", func(d *service.TaskServiceDeps) {
			d.Tx = &servicetest.TxManager{BeginErr: errBoom}
		}},
		{"claim fails", func(d *service.TaskServiceDeps) {
			d.Idempotency = failingIdempotency{IdempotencyRepository: d.Idempotency, claim: errBoom}
		}},
		{"saving the result fails", func(d *service.TaskServiceDeps) {
			d.Idempotency = failingIdempotency{IdempotencyRepository: d.Idempotency, saveResult: errBoom}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, tc.deps)
			key := uuid.New()

			_, err := e.tasks.CreateTask(context.Background(), e.owner.ID, service.TaskInput{Title: "x"}, &key)

			requireKind(t, err, apperror.KindInternal)
			assert.Empty(t, e.store.AllTasks(), "nothing may be left behind")
			_, claimed := e.store.IdempotencyRecord(e.owner.ID, key)
			assert.False(t, claimed)
		})
	}
}

func TestCreateTask_Idempotency_DefaultTTLIs24Hours(t *testing.T) {
	e := newEnv(t, func(d *service.TaskServiceDeps) {
		d.Clock = nil
		d.IdempotencyTTL = 0
	})
	key := uuid.New()

	_, err := e.tasks.CreateTask(context.Background(), e.owner.ID, service.TaskInput{Title: "x"}, &key)
	require.NoError(t, err)

	rec, ok := e.store.IdempotencyRecord(e.owner.ID, key)
	require.True(t, ok)
	assert.Equal(t, service.DefaultIdempotencyTTL, rec.ExpiresAt.Sub(rec.CreatedAt))
	assert.WithinDuration(t, time.Now(), rec.CreatedAt, time.Minute, "a nil clock falls back to time.Now")
}

// createConcurrently calls CreateTask n times at once for e.owner and returns
// the results in call order.
func createConcurrently(e *env, n int, input func(i int) (service.TaskInput, *uuid.UUID)) ([]*service.CreateTaskResult, []error) {
	results := make([]*service.CreateTaskResult, n)
	errs := make([]error, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			in, key := input(i)
			<-start
			results[i], errs[i] = e.tasks.CreateTask(context.Background(), e.owner.ID, in, key)
		}()
	}
	close(start)
	wg.Wait()
	return results, errs
}
