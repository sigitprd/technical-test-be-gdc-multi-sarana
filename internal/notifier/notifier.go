// Package notifier implements service.Notifier. The only implementation is a
// mock that writes the notification to the structured log.
package notifier

import (
	"context"
	"errors"
	"log/slog"

	"technical-test-be-gdc-multi-sarana/internal/domain"
	"technical-test-be-gdc-multi-sarana/internal/pkg/logger"
)

// ErrSimulatedFailure is returned when the notifier is configured to fail.
var ErrSimulatedFailure = errors.New("notifier: simulated failure")

// LogNotifier "sends" notifications by logging them.
type LogNotifier struct {
	failAlways bool
}

// NewLogNotifier creates a log based notifier. When failAlways is true every
// call fails, which is handy to demonstrate transaction rollback.
func NewLogNotifier(failAlways bool) *LogNotifier {
	return &LogNotifier{failAlways: failAlways}
}

// NotifyTaskAssigned logs an assignment notification.
func (n *LogNotifier) NotifyTaskAssigned(ctx context.Context, task *domain.Task, assignee *domain.User, actor *domain.User) error {
	if n.failAlways {
		return ErrSimulatedFailure
	}
	logger.FromContext(ctx).LogAttrs(ctx, slog.LevelInfo, "notification sent",
		slog.String("type", "task_assigned"),
		slog.String("task_id", task.ID.String()),
		slog.String("assignee_id", assignee.ID.String()),
		slog.String("assignee_email", assignee.Email),
		slog.String("actor_id", actor.ID.String()),
	)
	return nil
}
