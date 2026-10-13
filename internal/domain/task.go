package domain

import (
	"time"

	"github.com/google/uuid"
)

// TaskStatus is the lifecycle state of a task.
type TaskStatus string

const (
	TaskStatusTodo       TaskStatus = "todo"
	TaskStatusInProgress TaskStatus = "in_progress"
	TaskStatusDone       TaskStatus = "done"
)

// TaskStatuses lists every valid status, in lifecycle order.
var TaskStatuses = []TaskStatus{TaskStatusTodo, TaskStatusInProgress, TaskStatusDone}

// Valid reports whether s is a known status.
func (s TaskStatus) Valid() bool {
	for _, v := range TaskStatuses {
		if s == v {
			return true
		}
	}
	return false
}

// Task is a unit of work owned by a user and optionally assigned to a teammate.
type Task struct {
	ID          uuid.UUID
	OwnerID     uuid.UUID
	AssigneeID  *uuid.UUID
	Title       string
	Description string
	Status      TaskStatus
	DueDate     *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// IsOwnedBy reports whether userID owns the task.
func (t *Task) IsOwnedBy(userID uuid.UUID) bool {
	return t.OwnerID == userID
}

// IsVisibleTo reports whether userID may read the task (owner or assignee).
// TaskRepository.List applies the same rule in SQL; keep them in sync.
func (t *Task) IsVisibleTo(userID uuid.UUID) bool {
	return t.OwnerID == userID || (t.AssigneeID != nil && *t.AssigneeID == userID)
}

// TaskFilter narrows a task listing. Page and Limit are 1-based and already
// normalized by the service layer.
type TaskFilter struct {
	Status *TaskStatus
	Search string
	Page   int
	Limit  int
}

// Offset returns the number of rows to skip for the current page.
func (f TaskFilter) Offset() int {
	return (f.Page - 1) * f.Limit
}

// Task log actions.
const (
	TaskLogActionAssigned = "assigned"
)

// TaskLog is an audit trail entry for a change made to a task. OldValue and
// NewValue hold the changed fields, keyed by field name.
type TaskLog struct {
	ID        uuid.UUID
	TaskID    uuid.UUID
	ActorID   uuid.UUID
	Action    string
	OldValue  map[string]any
	NewValue  map[string]any
	CreatedAt time.Time
}
