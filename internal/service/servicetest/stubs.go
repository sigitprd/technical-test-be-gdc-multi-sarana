package servicetest

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"technical-test-be-gdc-multi-sarana/internal/domain"
)

// ErrPasswordMismatch is returned by Hasher.Compare for a wrong password.
var ErrPasswordMismatch = errors.New("password mismatch")

// Hasher is a fast, deterministic service.PasswordHasher: the hash of p is
// "hashed:" + p.
type Hasher struct {
	// HashErr, when set, is returned by Hash.
	HashErr error

	compares atomic.Int64
}

// Hash returns the fake hash of password.
func (h *Hasher) Hash(password string) (string, error) {
	if h.HashErr != nil {
		return "", h.HashErr
	}
	return "hashed:" + password, nil
}

// Compare reports whether password matches hash and counts the call.
func (h *Hasher) Compare(hash, password string) error {
	h.compares.Add(1)
	if hash != "hashed:"+password {
		return ErrPasswordMismatch
	}
	return nil
}

// Compares returns how many times Compare ran.
func (h *Hasher) Compares() int64 { return h.compares.Load() }

// TokenIssuer is a service.TokenIssuer returning "token-<user id>".
type TokenIssuer struct {
	// Err, when set, is returned by Issue.
	Err error
}

// Issue returns a fake token valid for one hour.
func (t *TokenIssuer) Issue(userID uuid.UUID) (string, time.Time, error) {
	if t.Err != nil {
		return "", time.Time{}, t.Err
	}
	return "token-" + userID.String(), time.Now().Add(time.Hour), nil
}

// Notification is one recorded call to Notifier.NotifyTaskAssigned.
type Notification struct {
	TaskID     uuid.UUID
	AssigneeID uuid.UUID
	ActorID    uuid.UUID
}

// Notifier records the notifications it is asked to send.
type Notifier struct {
	// Err, when set, is returned by NotifyTaskAssigned.
	Err error

	mu   sync.Mutex
	sent []Notification
}

// NotifyTaskAssigned records the notification.
func (n *Notifier) NotifyTaskAssigned(_ context.Context, task *domain.Task, assignee, actor *domain.User) error {
	if n.Err != nil {
		return n.Err
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.sent = append(n.sent, Notification{TaskID: task.ID, AssigneeID: assignee.ID, ActorID: actor.ID})
	return nil
}

// Sent returns the recorded notifications.
func (n *Notifier) Sent() []Notification {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]Notification(nil), n.sent...)
}

// NoLock is a service.KeyLocker that never blocks. With it, only the
// database-level guarantees prevent duplicate work.
type NoLock struct{}

// Lock returns immediately.
func (NoLock) Lock(string) func() { return func() {} }

// Clock is a manually advanced clock for service.Clock.
type Clock struct {
	mu  sync.Mutex
	now time.Time
}

// NewClock creates a clock showing t.
func NewClock(t time.Time) *Clock { return &Clock{now: t} }

// Now returns the current fake time.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance moves the clock forward by d.
func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}
