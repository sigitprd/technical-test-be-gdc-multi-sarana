package service

import "technical-test-be-gdc-multi-sarana/internal/apperror"

// Client facing errors returned by the use cases.
var (
	ErrEmailTaken         = apperror.Conflict("EMAIL_ALREADY_REGISTERED", "email is already registered")
	ErrInvalidCredentials = apperror.Unauthorized("INVALID_CREDENTIALS", "invalid email or password")
	ErrUserNotFound       = apperror.NotFound("USER_NOT_FOUND", "user not found")
	ErrTeamChoice         = apperror.BadRequest("INVALID_TEAM_CHOICE", "send exactly one of team_name (to create a new team) or invite_code (to join an existing team)")
	ErrInvalidInviteCode  = apperror.Unprocessable("INVALID_INVITE_CODE", "invite code does not match any team")

	ErrTaskNotFound      = apperror.NotFound("TASK_NOT_FOUND", "task not found")
	ErrTaskForbidden     = apperror.Forbidden("TASK_FORBIDDEN", "only the task owner can perform this action")
	ErrInvalidTaskStatus = apperror.BadRequest("INVALID_TASK_STATUS", "status must be one of: todo, in_progress, done")
	ErrInvalidTaskTitle  = apperror.BadRequest("INVALID_TASK_TITLE", "title must not be empty")
	ErrNoTaskChanges     = apperror.BadRequest("NO_FIELDS_TO_UPDATE", "send at least one of title, description, status or due_date")

	ErrAssigneeNotFound  = apperror.Unprocessable("ASSIGNEE_NOT_FOUND", "assignee does not exist")
	ErrAssigneeNotInTeam = apperror.Unprocessable("ASSIGNEE_NOT_IN_TEAM", "assignee must be a member of the same team")
	ErrAssigneeIsOwner   = apperror.Unprocessable("ASSIGNEE_IS_OWNER", "a task can only be assigned to another member of the team")

	ErrIdempotencyKeyReused     = apperror.Unprocessable("IDEMPOTENCY_KEY_REUSED", "idempotency key was already used with a different request payload")
	ErrIdempotencyKeyInProgress = apperror.Conflict("IDEMPOTENCY_KEY_IN_PROGRESS", "a request with this idempotency key is still being processed")
)
