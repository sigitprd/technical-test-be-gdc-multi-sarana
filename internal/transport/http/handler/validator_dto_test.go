package handler

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"technical-test-be-gdc-multi-sarana/internal/apperror"
	"technical-test-be-gdc-multi-sarana/internal/domain"
)

func TestValidator_Messages(t *testing.T) {
	type sample struct {
		Required string  `json:"required" validate:"required"`
		Email    string  `json:"email" validate:"email"`
		Min      string  `json:"min" validate:"min=3"`
		NotEmpty *string `json:"not_empty" validate:"omitempty,min=1"`
		Max      string  `json:"max" validate:"max=2"`
		MaxBytes string  `json:"max_bytes" validate:"maxbytes=2"`
		Safe     string  `json:"safe" validate:"safetext"`
		OneOf    string  `json:"one_of" validate:"oneof=a b"`
		Gte      int     `query:"gte" validate:"gte=1"`
		Lte      int     `param:"lte" validate:"lte=1"`
		Other    string  `validate:"alpha"`
		Hidden   string  `json:"-" validate:"numeric"`
	}
	empty := ""
	in := sample{
		Email: "bad", Min: "ab", NotEmpty: &empty, Max: "abc", MaxBytes: "éé", Safe: "a\x00b",
		OneOf: "c", Gte: 0, Lte: 2, Other: "123", Hidden: "x",
	}

	err := NewValidator().Validate(in)

	appErr := requireKind(t, err, apperror.KindValidation)
	got := map[string]string{}
	for _, d := range appErr.Details.([]FieldError) {
		got[d.Field] = d.Message
	}
	assert.Equal(t, map[string]string{
		"required":  "is required",
		"email":     "must be a valid email address",
		"min":       "must be at least 3 characters",
		"not_empty": "must not be empty",
		"max":       "must be at most 2 characters",
		"max_bytes": "must be at most 2 bytes",
		"safe":      "must be valid UTF-8 text without NUL characters",
		"one_of":    "must be one of: a, b",
		"gte":       "must be greater than or equal to 1",
		"lte":       "must be less than or equal to 1",
		"Other":     "is invalid",
		"Hidden":    "is invalid",
	}, got)
}

func TestValidator_SafeTextRejectsInvalidUTF8(t *testing.T) {
	type sample struct {
		S string `json:"s" validate:"safetext"`
	}
	assert.NoError(t, NewValidator().Validate(sample{S: "ünïcödé 🎉"}))
	requireKind(t, NewValidator().Validate(sample{S: "a\xffb"}), apperror.KindValidation)
}

func TestValidator_NonStructIsAnInternalError(t *testing.T) {
	requireKind(t, NewValidator().Validate("not a struct"), apperror.KindInternal)
}

func TestOptionalTime(t *testing.T) {
	var v struct {
		Due optionalTime `json:"due_date"`
	}

	require.NoError(t, json.Unmarshal([]byte(`{}`), &v))
	assert.False(t, v.Due.Set)

	require.NoError(t, json.Unmarshal([]byte(`{"due_date":null}`), &v))
	assert.True(t, v.Due.Set)
	assert.Nil(t, v.Due.Value)

	require.NoError(t, json.Unmarshal([]byte(`{"due_date":"2026-10-20T10:00:00Z"}`), &v))
	assert.True(t, v.Due.Value.Equal(time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)))

	assert.Error(t, json.Unmarshal([]byte(`{"due_date":"soon"}`), &v))
}

func TestUpdateTaskRequest_ToPatchWithoutStatus(t *testing.T) {
	req := UpdateTaskRequest{Description: ptr("  d  ")}
	req.normalize()

	patch := req.toPatch()

	assert.Nil(t, patch.Title)
	assert.Nil(t, patch.Status)
	assert.Equal(t, "d", *patch.Description)
	assert.False(t, patch.DueDateSet)
}

func TestNewTaskResponses(t *testing.T) {
	assignee := user.ID
	due := time.Date(2026, 10, 20, 3, 0, 0, 0, time.UTC)
	tasks := []domain.Task{{ID: task.ID, AssigneeID: &assignee, DueDate: &due, Status: domain.TaskStatusDone}}

	got := newTaskResponses(tasks)

	require.Len(t, got, 1)
	assert.Equal(t, &assignee, got[0].AssigneeID)
	assert.Equal(t, "done", got[0].Status)
	assert.Empty(t, newTaskResponses(nil))
	assert.NotNil(t, newTaskResponses(nil))
}

func ptr[T any](v T) *T { return &v }
