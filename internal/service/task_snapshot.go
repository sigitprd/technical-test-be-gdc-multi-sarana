package service

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"technical-test-be-gdc-multi-sarana/internal/domain"
)

// taskSnapshot is the serialized form of a task stored with an idempotency
// key. It is owned by the use case, so the domain entity stays free of
// serialization concerns. Field names must stay stable: changing them breaks
// replays of keys stored before the change.
type taskSnapshot struct {
	ID          uuid.UUID  `json:"id"`
	OwnerID     uuid.UUID  `json:"owner_id"`
	AssigneeID  *uuid.UUID `json:"assignee_id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Status      string     `json:"status"`
	DueDate     *time.Time `json:"due_date"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func encodeTaskSnapshot(t *domain.Task) ([]byte, error) {
	b, err := json.Marshal(taskSnapshot{
		ID:          t.ID,
		OwnerID:     t.OwnerID,
		AssigneeID:  t.AssigneeID,
		Title:       t.Title,
		Description: t.Description,
		Status:      string(t.Status),
		DueDate:     t.DueDate,
		CreatedAt:   t.CreatedAt,
		UpdatedAt:   t.UpdatedAt,
	})
	if err != nil {
		return nil, fmt.Errorf("encode task snapshot: %w", err)
	}
	return b, nil
}

func decodeTaskSnapshot(b []byte) (*domain.Task, error) {
	var s taskSnapshot
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("decode task snapshot: %w", err)
	}
	return &domain.Task{
		ID:          s.ID,
		OwnerID:     s.OwnerID,
		AssigneeID:  s.AssigneeID,
		Title:       s.Title,
		Description: s.Description,
		Status:      domain.TaskStatus(s.Status),
		DueDate:     s.DueDate,
		CreatedAt:   s.CreatedAt,
		UpdatedAt:   s.UpdatedAt,
	}, nil
}
