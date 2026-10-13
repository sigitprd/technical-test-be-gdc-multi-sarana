// Package service holds the application use cases. It depends only on the
// interfaces declared here, so every use case can be unit tested with
// in-memory fakes and without a database.
package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"technical-test-be-gdc-multi-sarana/internal/domain"
)

// TxManager runs fn inside a database transaction. Repositories called with
// the ctx passed to fn take part in that transaction. The transaction is
// committed when fn returns nil and rolled back otherwise.
type TxManager interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// TeamRepository persists teams.
type TeamRepository interface {
	// Create inserts a team and generates its id and invite code.
	Create(ctx context.Context, name string) (*domain.Team, error)
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Team, error)
	GetByInviteCode(ctx context.Context, code string) (*domain.Team, error)
}

// UserRepository persists users.
type UserRepository interface {
	// Create inserts u and fills its generated fields. Returns
	// domain.ErrDuplicate when the email is taken.
	Create(ctx context.Context, u *domain.User) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error)
	GetByEmail(ctx context.Context, email string) (*domain.User, error)
	ListByTeam(ctx context.Context, teamID uuid.UUID) ([]domain.User, error)
}

// TaskRepository persists tasks.
type TaskRepository interface {
	// Create inserts t and fills its generated fields.
	Create(ctx context.Context, t *domain.Task) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Task, error)
	// GetByIDForUpdate loads the task and locks its row until the surrounding
	// transaction ends.
	GetByIDForUpdate(ctx context.Context, id uuid.UUID) (*domain.Task, error)
	// List returns the page of tasks visible to userID and the total number
	// of tasks matching the filter.
	List(ctx context.Context, userID uuid.UUID, filter domain.TaskFilter) ([]domain.Task, int64, error)
	// Update persists the editable fields of t and refreshes UpdatedAt.
	Update(ctx context.Context, t *domain.Task) error
	UpdateAssignee(ctx context.Context, taskID, assigneeID uuid.UUID) (*domain.Task, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

// TaskLogRepository persists the task audit trail.
type TaskLogRepository interface {
	Create(ctx context.Context, l *domain.TaskLog) error
}

// IdempotencyRepository persists idempotency keys.
type IdempotencyRepository interface {
	// Claim atomically reserves rec.Key for rec.UserID. It returns
	// claimed=true when the key was free (or its previous record had expired),
	// otherwise it returns the existing, still valid record.
	Claim(ctx context.Context, rec *domain.IdempotencyRecord) (existing *domain.IdempotencyRecord, claimed bool, err error)
	// SaveResult stores the result snapshot of a claimed key.
	SaveResult(ctx context.Context, userID, key uuid.UUID, result []byte) error
}

// PasswordHasher hashes and verifies passwords.
type PasswordHasher interface {
	Hash(password string) (string, error)
	Compare(hash, password string) error
}

// TokenIssuer issues access tokens.
type TokenIssuer interface {
	Issue(userID uuid.UUID) (token string, expiresAt time.Time, err error)
}

// Notifier sends notifications to users.
type Notifier interface {
	NotifyTaskAssigned(ctx context.Context, task *domain.Task, assignee *domain.User, actor *domain.User) error
}

// KeyLocker serializes work per key inside a single process.
type KeyLocker interface {
	Lock(key string) (unlock func())
}

// Clock returns the current time. Injected so tests can control time.
type Clock func() time.Time
