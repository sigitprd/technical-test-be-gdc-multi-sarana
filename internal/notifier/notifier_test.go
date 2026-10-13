package notifier_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"technical-test-be-gdc-multi-sarana/internal/domain"
	"technical-test-be-gdc-multi-sarana/internal/notifier"
	"technical-test-be-gdc-multi-sarana/internal/pkg/logger"
)

func TestLogNotifier_LogsTheNotification(t *testing.T) {
	var buf bytes.Buffer
	ctx := logger.WithContext(context.Background(), slog.New(slog.NewJSONHandler(&buf, nil)).With("request_id", "req-1"))
	task := &domain.Task{ID: uuid.New()}
	assignee := &domain.User{ID: uuid.New(), Email: "bob@example.com"}
	actor := &domain.User{ID: uuid.New()}

	require.NoError(t, notifier.NewLogNotifier(false).NotifyTaskAssigned(ctx, task, assignee, actor))

	var line map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &line))
	assert.Equal(t, "notification sent", line["msg"])
	assert.Equal(t, "req-1", line["request_id"], "uses the request scoped logger")
	assert.Equal(t, "task_assigned", line["type"])
	assert.Equal(t, task.ID.String(), line["task_id"])
	assert.Equal(t, assignee.ID.String(), line["assignee_id"])
	assert.Equal(t, "bob@example.com", line["assignee_email"])
	assert.Equal(t, actor.ID.String(), line["actor_id"])
}

func TestLogNotifier_SimulatedFailure(t *testing.T) {
	err := notifier.NewLogNotifier(true).NotifyTaskAssigned(context.Background(), &domain.Task{}, &domain.User{}, &domain.User{})
	assert.ErrorIs(t, err, notifier.ErrSimulatedFailure)
}
