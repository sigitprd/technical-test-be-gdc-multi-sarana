package servicetest

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"technical-test-be-gdc-multi-sarana/internal/domain"
)

type idempotencyKey struct {
	userID uuid.UUID
	key    uuid.UUID
}

// Store holds the data shared by the fake repositories.
type Store struct {
	mu          sync.Mutex
	teams       map[uuid.UUID]domain.Team
	users       map[uuid.UUID]domain.User
	tasks       map[uuid.UUID]domain.Task
	taskLogs    []domain.TaskLog
	idempotency map[idempotencyKey]domain.IdempotencyRecord

	rowLocks sync.Map // lock name -> *sync.Mutex
}

// NewStore creates an empty store.
func NewStore() *Store {
	return &Store{
		teams:       make(map[uuid.UUID]domain.Team),
		users:       make(map[uuid.UUID]domain.User),
		tasks:       make(map[uuid.UUID]domain.Task),
		idempotency: make(map[idempotencyKey]domain.IdempotencyRecord),
	}
}

// lockRow blocks until the named row lock is free and holds it until the
// transaction in ctx ends.
func (s *Store) lockRow(ctx context.Context, name string) {
	m, _ := s.rowLocks.LoadOrStore(name, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	mu.Lock()
	holdUntilTxEnd(ctx, mu.Unlock)
}

// AddTeam inserts a team directly, outside any transaction.
func (s *Store) AddTeam(name string) domain.Team {
	t, _ := s.TeamRepository().Create(context.Background(), name)
	return *t
}

// AddUser inserts a user directly, outside any transaction. The password hash
// matches Hasher for the password "password123".
func (s *Store) AddUser(teamID uuid.UUID, name, email string) domain.User {
	u := &domain.User{TeamID: teamID, Name: name, Email: email, PasswordHash: "hashed:password123"}
	if err := s.UserRepository().Create(context.Background(), u); err != nil {
		panic(err)
	}
	return *u
}

// PutIdempotencyRecord stores rec as is, e.g. to simulate a request that is
// still in flight or a corrupted snapshot.
func (s *Store) PutIdempotencyRecord(rec domain.IdempotencyRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.idempotency[idempotencyKey{rec.UserID, rec.Key}] = rec
}

// AllTasks returns every stored task, oldest first.
func (s *Store) AllTasks() []domain.Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		out = append(out, cloneTask(t))
	}
	slices.SortFunc(out, func(a, b domain.Task) int { return a.CreatedAt.Compare(b.CreatedAt) })
	return out
}

// Task returns a stored task.
func (s *Store) Task(id uuid.UUID) (domain.Task, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	return cloneTask(t), ok
}

// AllTaskLogs returns every stored task log in insertion order.
func (s *Store) AllTaskLogs() []domain.TaskLog {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.taskLogs)
}

// TeamCount returns the number of stored teams.
func (s *Store) TeamCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.teams)
}

// UserCount returns the number of stored users.
func (s *Store) UserCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.users)
}

// IdempotencyRecord returns the stored record for a key.
func (s *Store) IdempotencyRecord(userID, key uuid.UUID) (domain.IdempotencyRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.idempotency[idempotencyKey{userID, key}]
	return rec, ok
}

// TeamRepository returns a service.TeamRepository backed by s.
func (s *Store) TeamRepository() *TeamRepository { return &TeamRepository{s: s} }

// UserRepository returns a service.UserRepository backed by s.
func (s *Store) UserRepository() *UserRepository { return &UserRepository{s: s} }

// TaskRepository returns a service.TaskRepository backed by s.
func (s *Store) TaskRepository() *TaskRepository { return &TaskRepository{s: s} }

// TaskLogRepository returns a service.TaskLogRepository backed by s.
func (s *Store) TaskLogRepository() *TaskLogRepository { return &TaskLogRepository{s: s} }

// IdempotencyRepository returns a service.IdempotencyRepository backed by s.
func (s *Store) IdempotencyRepository() *IdempotencyRepository {
	return &IdempotencyRepository{s: s}
}

// TeamRepository is an in-memory service.TeamRepository.
type TeamRepository struct{ s *Store }

// Create inserts a team with a random invite code.
func (r *TeamRepository) Create(ctx context.Context, name string) (*domain.Team, error) {
	t := domain.Team{
		ID:         uuid.New(),
		Name:       name,
		InviteCode: strings.ReplaceAll(uuid.NewString(), "-", ""),
		CreatedAt:  dbNow(),
	}
	r.s.mu.Lock()
	r.s.teams[t.ID] = t
	r.s.mu.Unlock()
	onRollback(ctx, func() {
		r.s.mu.Lock()
		delete(r.s.teams, t.ID)
		r.s.mu.Unlock()
	})
	return &t, nil
}

// GetByID loads a team.
func (r *TeamRepository) GetByID(_ context.Context, id uuid.UUID) (*domain.Team, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	t, ok := r.s.teams[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return &t, nil
}

// GetByInviteCode loads the team owning an invite code.
func (r *TeamRepository) GetByInviteCode(_ context.Context, code string) (*domain.Team, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	for _, t := range r.s.teams {
		if t.InviteCode == code {
			return &t, nil
		}
	}
	return nil, domain.ErrNotFound
}

// UserRepository is an in-memory service.UserRepository. Emails are unique
// regardless of case, like the unique index on lower(email).
type UserRepository struct{ s *Store }

// Create inserts u, or returns domain.ErrDuplicate when the email is taken.
func (r *UserRepository) Create(ctx context.Context, u *domain.User) error {
	r.s.mu.Lock()
	for _, existing := range r.s.users {
		if strings.EqualFold(existing.Email, u.Email) {
			r.s.mu.Unlock()
			return domain.ErrDuplicate
		}
	}
	now := dbNow()
	u.ID, u.CreatedAt, u.UpdatedAt = uuid.New(), now, now
	r.s.users[u.ID] = *u
	r.s.mu.Unlock()

	id := u.ID
	onRollback(ctx, func() {
		r.s.mu.Lock()
		delete(r.s.users, id)
		r.s.mu.Unlock()
	})
	return nil
}

// GetByID loads a user.
func (r *UserRepository) GetByID(_ context.Context, id uuid.UUID) (*domain.User, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	u, ok := r.s.users[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return &u, nil
}

// GetByEmail loads a user by email, ignoring case.
func (r *UserRepository) GetByEmail(_ context.Context, email string) (*domain.User, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	for _, u := range r.s.users {
		if strings.EqualFold(u.Email, email) {
			return &u, nil
		}
	}
	return nil, domain.ErrNotFound
}

// ListByTeam returns the members of a team ordered by name.
func (r *UserRepository) ListByTeam(_ context.Context, teamID uuid.UUID) ([]domain.User, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	var out []domain.User
	for _, u := range r.s.users {
		if u.TeamID == teamID {
			out = append(out, u)
		}
	}
	slices.SortFunc(out, func(a, b domain.User) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

// TaskRepository is an in-memory service.TaskRepository.
type TaskRepository struct{ s *Store }

// Create inserts t and fills its id and timestamps.
func (r *TaskRepository) Create(ctx context.Context, t *domain.Task) error {
	now := dbNow()
	t.ID, t.CreatedAt, t.UpdatedAt = uuid.New(), now, now
	r.s.mu.Lock()
	r.s.tasks[t.ID] = cloneTask(*t)
	r.s.mu.Unlock()

	id := t.ID
	onRollback(ctx, func() {
		r.s.mu.Lock()
		delete(r.s.tasks, id)
		r.s.mu.Unlock()
	})
	return nil
}

// GetByID loads a task.
func (r *TaskRepository) GetByID(_ context.Context, id uuid.UUID) (*domain.Task, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	t, ok := r.s.tasks[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	t = cloneTask(t)
	return &t, nil
}

// GetByIDForUpdate locks the task until the transaction ends, then loads it.
func (r *TaskRepository) GetByIDForUpdate(ctx context.Context, id uuid.UUID) (*domain.Task, error) {
	r.s.lockRow(ctx, "task:"+id.String())
	return r.GetByID(ctx, id)
}

// List returns one page of the tasks visible to userID, newest first.
func (r *TaskRepository) List(_ context.Context, userID uuid.UUID, f domain.TaskFilter) ([]domain.Task, int64, error) {
	r.s.mu.Lock()
	var matched []domain.Task
	for _, t := range r.s.tasks {
		if !t.IsVisibleTo(userID) {
			continue
		}
		if f.Status != nil && t.Status != *f.Status {
			continue
		}
		if f.Search != "" && !strings.Contains(strings.ToLower(t.Title), strings.ToLower(f.Search)) {
			continue
		}
		matched = append(matched, cloneTask(t))
	}
	r.s.mu.Unlock()

	slices.SortFunc(matched, func(a, b domain.Task) int {
		if c := b.CreatedAt.Compare(a.CreatedAt); c != 0 {
			return c
		}
		return strings.Compare(b.ID.String(), a.ID.String())
	})
	start := min(f.Offset(), len(matched))
	end := min(start+f.Limit, len(matched))
	return matched[start:end], int64(len(matched)), nil
}

// Update stores the editable fields of t and refreshes UpdatedAt.
func (r *TaskRepository) Update(ctx context.Context, t *domain.Task) error {
	return r.modify(ctx, t.ID, func(stored *domain.Task) {
		stored.Title = t.Title
		stored.Description = t.Description
		stored.Status = t.Status
		stored.DueDate = clonePtr(t.DueDate)
		stored.UpdatedAt = dbNow()
		*t = cloneTask(*stored)
	})
}

// UpdateAssignee sets the assignee of a task.
func (r *TaskRepository) UpdateAssignee(ctx context.Context, taskID, assigneeID uuid.UUID) (*domain.Task, error) {
	var updated domain.Task
	err := r.modify(ctx, taskID, func(stored *domain.Task) {
		stored.AssigneeID = &assigneeID
		stored.UpdatedAt = dbNow()
		updated = cloneTask(*stored)
	})
	if err != nil {
		return nil, err
	}
	return &updated, nil
}

// Delete removes a task.
func (r *TaskRepository) Delete(ctx context.Context, id uuid.UUID) error {
	r.s.mu.Lock()
	prev, ok := r.s.tasks[id]
	delete(r.s.tasks, id)
	r.s.mu.Unlock()
	if !ok {
		return domain.ErrNotFound
	}
	onRollback(ctx, func() {
		r.s.mu.Lock()
		r.s.tasks[id] = prev
		r.s.mu.Unlock()
	})
	return nil
}

// modify applies change to a stored task and registers its undo.
func (r *TaskRepository) modify(ctx context.Context, id uuid.UUID, change func(*domain.Task)) error {
	r.s.mu.Lock()
	stored, ok := r.s.tasks[id]
	if !ok {
		r.s.mu.Unlock()
		return domain.ErrNotFound
	}
	prev := cloneTask(stored)
	change(&stored)
	r.s.tasks[id] = stored
	r.s.mu.Unlock()

	onRollback(ctx, func() {
		r.s.mu.Lock()
		r.s.tasks[id] = prev
		r.s.mu.Unlock()
	})
	return nil
}

// TaskLogRepository is an in-memory service.TaskLogRepository.
type TaskLogRepository struct{ s *Store }

// Create appends a task log entry.
func (r *TaskLogRepository) Create(ctx context.Context, l *domain.TaskLog) error {
	l.ID, l.CreatedAt = uuid.New(), dbNow()
	r.s.mu.Lock()
	r.s.taskLogs = append(r.s.taskLogs, *l)
	r.s.mu.Unlock()

	id := l.ID
	onRollback(ctx, func() {
		r.s.mu.Lock()
		r.s.taskLogs = slices.DeleteFunc(r.s.taskLogs, func(e domain.TaskLog) bool { return e.ID == id })
		r.s.mu.Unlock()
	})
	return nil
}

// IdempotencyRepository is an in-memory service.IdempotencyRepository.
type IdempotencyRepository struct{ s *Store }

// Claim reserves a key. Like the PostgreSQL implementation it locks the key
// until the transaction ends: a concurrent claim of the same key waits, then
// sees the committed record, or takes the key over if the first transaction
// rolled back. An expired record is taken over in place.
func (r *IdempotencyRepository) Claim(ctx context.Context, rec *domain.IdempotencyRecord) (*domain.IdempotencyRecord, bool, error) {
	k := idempotencyKey{rec.UserID, rec.Key}
	r.s.lockRow(ctx, "idempotency:"+rec.UserID.String()+":"+rec.Key.String())

	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	prev, exists := r.s.idempotency[k]
	if exists && prev.ExpiresAt.After(rec.CreatedAt) {
		prev.Result = slices.Clone(prev.Result)
		return &prev, false, nil
	}

	claimed := *rec
	claimed.Result = nil
	r.s.idempotency[k] = claimed
	onRollback(ctx, func() {
		r.s.mu.Lock()
		defer r.s.mu.Unlock()
		if exists {
			r.s.idempotency[k] = prev
		} else {
			delete(r.s.idempotency, k)
		}
	})
	return nil, true, nil
}

// SaveResult stores the result of a claimed key.
func (r *IdempotencyRepository) SaveResult(ctx context.Context, userID, key uuid.UUID, result []byte) error {
	k := idempotencyKey{userID, key}
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	rec, ok := r.s.idempotency[k]
	if !ok {
		return domain.ErrNotFound
	}
	prev := rec.Result
	rec.Result = slices.Clone(result)
	r.s.idempotency[k] = rec
	onRollback(ctx, func() {
		r.s.mu.Lock()
		defer r.s.mu.Unlock()
		if stored, ok := r.s.idempotency[k]; ok {
			stored.Result = prev
			r.s.idempotency[k] = stored
		}
	})
	return nil
}

// dbNow returns the current time the way PostgreSQL stores it: UTC with
// microsecond precision and no monotonic clock reading.
func dbNow() time.Time {
	return time.Now().UTC().Truncate(time.Microsecond)
}

func cloneTask(t domain.Task) domain.Task {
	t.AssigneeID = clonePtr(t.AssigneeID)
	t.DueDate = clonePtr(t.DueDate)
	return t
}

func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
