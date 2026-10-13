package handler

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	"technical-test-be-gdc-multi-sarana/internal/domain"
	"technical-test-be-gdc-multi-sarana/internal/service"
)

// RegisterRequest is the body of POST /auth/register. Send team_name to
// create a new team, or invite_code to join an existing one.
type RegisterRequest struct {
	Name       string `json:"name" validate:"required,safetext,min=2,max=100"`
	Email      string `json:"email" validate:"required,safetext,email,max=255"`
	Password   string `json:"password" validate:"required,min=8,maxbytes=72"`
	TeamName   string `json:"team_name" validate:"omitempty,safetext,min=2,max=100"`
	InviteCode string `json:"invite_code" validate:"omitempty,safetext,max=64"`
}

func (r *RegisterRequest) normalize() {
	r.Name = strings.TrimSpace(r.Name)
	r.Email = strings.TrimSpace(r.Email)
	r.TeamName = strings.TrimSpace(r.TeamName)
	r.InviteCode = strings.TrimSpace(r.InviteCode)
}

// LoginRequest is the body of POST /auth/login.
type LoginRequest struct {
	Email    string `json:"email" validate:"required,safetext,email"`
	Password string `json:"password" validate:"required,maxbytes=72"`
}

func (r *LoginRequest) normalize() {
	r.Email = strings.TrimSpace(r.Email)
}

// CreateTaskRequest is the body of POST /tasks.
type CreateTaskRequest struct {
	Title       string     `json:"title" validate:"required,safetext,max=255"`
	Description string     `json:"description" validate:"safetext,max=5000"`
	Status      string     `json:"status" validate:"omitempty,oneof=todo in_progress done"`
	DueDate     *time.Time `json:"due_date"`
}

func (r *CreateTaskRequest) normalize() {
	r.Title = strings.TrimSpace(r.Title)
	r.Description = strings.TrimSpace(r.Description)
}

func (r *CreateTaskRequest) toInput() service.TaskInput {
	return service.TaskInput{
		Title:       r.Title,
		Description: r.Description,
		Status:      domain.TaskStatus(r.Status),
		DueDate:     r.DueDate,
	}
}

// UpdateTaskRequest is the body of PUT /tasks/:id. Only the fields present in
// the body change; "due_date": null removes the due date.
type UpdateTaskRequest struct {
	Title       *string      `json:"title" validate:"omitempty,safetext,min=1,max=255"`
	Description *string      `json:"description" validate:"omitempty,safetext,max=5000"`
	Status      *string      `json:"status" validate:"omitempty,oneof=todo in_progress done"`
	DueDate     optionalTime `json:"due_date"`
}

func (r *UpdateTaskRequest) normalize() {
	r.Title = trimPtr(r.Title)
	r.Description = trimPtr(r.Description)
}

func (r *UpdateTaskRequest) toPatch() service.TaskPatch {
	patch := service.TaskPatch{
		Title:       r.Title,
		Description: r.Description,
		DueDate:     r.DueDate.Value,
		DueDateSet:  r.DueDate.Set,
	}
	if r.Status != nil {
		status := domain.TaskStatus(*r.Status)
		patch.Status = &status
	}
	return patch
}

// optionalTime tells an absent JSON field (Set false) apart from an explicit
// null (Set true, Value nil) and a timestamp.
type optionalTime struct {
	Set   bool
	Value *time.Time
}

func (o *optionalTime) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(b) == "null" {
		o.Value = nil
		return nil
	}
	var t time.Time
	if err := json.Unmarshal(b, &t); err != nil {
		return err
	}
	o.Value = &t
	return nil
}

func trimPtr(s *string) *string {
	if s == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*s)
	return &trimmed
}

// ListTasksQuery holds the query parameters of GET /tasks.
type ListTasksQuery struct {
	Status string `query:"status" validate:"omitempty,oneof=todo in_progress done"`
	Search string `query:"search" validate:"safetext,max=100"`
	Page   int    `query:"page" validate:"gte=0,lte=1000000"`
	Limit  int    `query:"limit" validate:"gte=0,lte=100"`
}

func (q *ListTasksQuery) normalize() {
	q.Status = strings.TrimSpace(q.Status)
	q.Search = strings.TrimSpace(q.Search)
}

// AssignTaskRequest is the body of POST /tasks/:id/assign. AssigneeID is
// decoded by uuid.UUID itself, which accepts any letter case; a malformed id
// fails binding and a missing one fails validation.
type AssignTaskRequest struct {
	AssigneeID uuid.UUID `json:"assignee_id" validate:"required"`
}

// UserResponse is the public representation of a user.
type UserResponse struct {
	ID        uuid.UUID `json:"id"`
	TeamID    uuid.UUID `json:"team_id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

func newUserResponse(u *domain.User) UserResponse {
	return UserResponse{ID: u.ID, TeamID: u.TeamID, Name: u.Name, Email: u.Email, CreatedAt: u.CreatedAt}
}

// TeamResponse describes a team to one of its members, so it includes the
// invite code used to bring others in.
type TeamResponse struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
	InviteCode string    `json:"invite_code"`
	CreatedAt  time.Time `json:"created_at"`
}

// ProfileResponse is the authenticated user together with their team.
type ProfileResponse struct {
	UserResponse
	Team TeamResponse `json:"team"`
}

func newProfileResponse(p *service.Profile) ProfileResponse {
	return ProfileResponse{
		UserResponse: newUserResponse(p.User),
		Team: TeamResponse{
			ID:         p.Team.ID,
			Name:       p.Team.Name,
			InviteCode: p.Team.InviteCode,
			CreatedAt:  p.Team.CreatedAt,
		},
	}
}

// LoginResponse is returned by POST /auth/login.
type LoginResponse struct {
	AccessToken string       `json:"access_token"`
	TokenType   string       `json:"token_type"`
	ExpiresAt   time.Time    `json:"expires_at"`
	User        UserResponse `json:"user"`
}

// TaskResponse is the public representation of a task.
type TaskResponse struct {
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

func newTaskResponse(t *domain.Task) TaskResponse {
	return TaskResponse{
		ID:          t.ID,
		OwnerID:     t.OwnerID,
		AssigneeID:  t.AssigneeID,
		Title:       t.Title,
		Description: t.Description,
		Status:      string(t.Status),
		DueDate:     t.DueDate,
		CreatedAt:   t.CreatedAt,
		UpdatedAt:   t.UpdatedAt,
	}
}

func newTaskResponses(tasks []domain.Task) []TaskResponse {
	out := make([]TaskResponse, 0, len(tasks))
	for i := range tasks {
		out = append(out, newTaskResponse(&tasks[i]))
	}
	return out
}
