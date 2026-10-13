package domain_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"technical-test-be-gdc-multi-sarana/internal/domain"
)

func TestTaskStatus_Valid(t *testing.T) {
	for _, s := range domain.TaskStatuses {
		assert.True(t, s.Valid(), s)
	}
	for _, s := range []domain.TaskStatus{"", "TODO", "archived"} {
		assert.False(t, s.Valid(), s)
	}
}

func TestTask_Access(t *testing.T) {
	owner, assignee, other := uuid.New(), uuid.New(), uuid.New()
	unassigned := domain.Task{OwnerID: owner}
	assigned := domain.Task{OwnerID: owner, AssigneeID: &assignee}

	assert.True(t, unassigned.IsOwnedBy(owner))
	assert.False(t, unassigned.IsOwnedBy(other))

	assert.True(t, unassigned.IsVisibleTo(owner))
	assert.False(t, unassigned.IsVisibleTo(other))
	assert.True(t, assigned.IsVisibleTo(assignee))
	assert.False(t, assigned.IsOwnedBy(assignee), "the assignee does not own the task")
	assert.False(t, assigned.IsVisibleTo(other))
}

func TestTaskFilter_Offset(t *testing.T) {
	assert.Equal(t, 0, domain.TaskFilter{Page: 1, Limit: 10}.Offset())
	assert.Equal(t, 20, domain.TaskFilter{Page: 3, Limit: 10}.Offset())
}

func TestIdempotencyRecord_Completed(t *testing.T) {
	assert.False(t, (&domain.IdempotencyRecord{}).Completed())
	assert.True(t, (&domain.IdempotencyRecord{Result: []byte(`{}`)}).Completed())
}
