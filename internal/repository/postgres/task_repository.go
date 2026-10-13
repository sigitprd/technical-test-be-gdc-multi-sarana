package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"technical-test-be-gdc-multi-sarana/internal/domain"
)

// TaskRepository implements service.TaskRepository.
type TaskRepository struct {
	db DB
}

// NewTaskRepository creates a TaskRepository.
func NewTaskRepository(db DB) *TaskRepository {
	return &TaskRepository{db: db}
}

const taskColumns = `id, owner_id, assignee_id, title, description, status, due_date, created_at, updated_at`

func taskScanTargets(t *domain.Task) []any {
	return []any{&t.ID, &t.OwnerID, &t.AssigneeID, &t.Title, &t.Description, &t.Status, &t.DueDate, &t.CreatedAt, &t.UpdatedAt}
}

func scanTask(row pgx.Row) (*domain.Task, error) {
	var t domain.Task
	if err := row.Scan(taskScanTargets(&t)...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &t, nil
}

// Create inserts a task.
func (r *TaskRepository) Create(ctx context.Context, t *domain.Task) error {
	q := `
		INSERT INTO tasks (owner_id, assignee_id, title, description, status, due_date)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING ` + taskColumns
	created, err := scanTask(conn(ctx, r.db).QueryRow(ctx, q,
		t.OwnerID, t.AssigneeID, t.Title, t.Description, t.Status, t.DueDate))
	if err != nil {
		return fmt.Errorf("insert task: %w", err)
	}
	*t = *created
	return nil
}

// GetByID loads a task.
func (r *TaskRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Task, error) {
	q := `SELECT ` + taskColumns + ` FROM tasks WHERE id = $1`
	return scanTask(conn(ctx, r.db).QueryRow(ctx, q, id))
}

// GetByIDForUpdate loads a task and locks its row. Must run in a transaction.
func (r *TaskRepository) GetByIDForUpdate(ctx context.Context, id uuid.UUID) (*domain.Task, error) {
	q := `SELECT ` + taskColumns + ` FROM tasks WHERE id = $1 FOR UPDATE`
	return scanTask(conn(ctx, r.db).QueryRow(ctx, q, id))
}

// List returns one page of tasks owned by or assigned to userID. The total is
// computed in the same query with a window function to avoid a second round
// trip; a separate COUNT is only needed when the page is past the last row.
func (r *TaskRepository) List(ctx context.Context, userID uuid.UUID, f domain.TaskFilter) ([]domain.Task, int64, error) {
	where, args := buildTaskFilter(userID, f)

	q := `SELECT ` + taskColumns + `, COUNT(*) OVER () AS total
		FROM tasks
		WHERE ` + where + `
		ORDER BY created_at DESC, id DESC
		LIMIT $` + strconv.Itoa(len(args)+1) + ` OFFSET $` + strconv.Itoa(len(args)+2)

	db := conn(ctx, r.db)
	rows, err := db.Query(ctx, q, append(args, f.Limit, f.Offset())...)
	if err != nil {
		return nil, 0, fmt.Errorf("query tasks: %w", err)
	}
	defer rows.Close()

	var (
		tasks []domain.Task
		total int64
	)
	for rows.Next() {
		var t domain.Task
		if err := rows.Scan(append(taskScanTargets(&t), &total)...); err != nil {
			return nil, 0, fmt.Errorf("scan task: %w", err)
		}
		tasks = append(tasks, t)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate tasks: %w", err)
	}

	if len(tasks) == 0 && f.Offset() > 0 {
		if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM tasks WHERE `+where, args...).Scan(&total); err != nil {
			return nil, 0, fmt.Errorf("count tasks: %w", err)
		}
	}
	return tasks, total, nil
}

// buildTaskFilter returns the WHERE clause and its positional arguments. The
// first condition is the visibility rule of domain.Task.IsVisibleTo.
func buildTaskFilter(userID uuid.UUID, f domain.TaskFilter) (string, []any) {
	conds := []string{"(owner_id = $1 OR assignee_id = $1)"}
	args := []any{userID}

	if f.Status != nil {
		args = append(args, string(*f.Status))
		conds = append(conds, "status = $"+strconv.Itoa(len(args)))
	}
	if f.Search != "" {
		args = append(args, "%"+escapeLike(f.Search)+"%")
		conds = append(conds, "title ILIKE $"+strconv.Itoa(len(args))+` ESCAPE '\'`)
	}
	return strings.Join(conds, " AND "), args
}

// escapeLike escapes LIKE wildcards so user input is matched literally.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// Update persists the editable fields of a task.
func (r *TaskRepository) Update(ctx context.Context, t *domain.Task) error {
	q := `
		UPDATE tasks
		SET title = $2, description = $3, status = $4, due_date = $5, updated_at = now()
		WHERE id = $1
		RETURNING ` + taskColumns
	updated, err := scanTask(conn(ctx, r.db).QueryRow(ctx, q, t.ID, t.Title, t.Description, t.Status, t.DueDate))
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return err
		}
		return fmt.Errorf("update task: %w", err)
	}
	*t = *updated
	return nil
}

// UpdateAssignee sets the assignee of a task.
func (r *TaskRepository) UpdateAssignee(ctx context.Context, taskID, assigneeID uuid.UUID) (*domain.Task, error) {
	q := `
		UPDATE tasks SET assignee_id = $2, updated_at = now()
		WHERE id = $1
		RETURNING ` + taskColumns
	return scanTask(conn(ctx, r.db).QueryRow(ctx, q, taskID, assigneeID))
}

// Delete removes a task.
func (r *TaskRepository) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := conn(ctx, r.db).Exec(ctx, `DELETE FROM tasks WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete task: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// TaskLogRepository implements service.TaskLogRepository.
type TaskLogRepository struct {
	db DB
}

// NewTaskLogRepository creates a TaskLogRepository.
func NewTaskLogRepository(db DB) *TaskLogRepository {
	return &TaskLogRepository{db: db}
}

// Create inserts a task log entry, storing the old and new values as JSONB.
func (r *TaskLogRepository) Create(ctx context.Context, l *domain.TaskLog) error {
	oldValue, err := jsonbOrNull(l.OldValue)
	if err != nil {
		return fmt.Errorf("encode old value: %w", err)
	}
	newValue, err := jsonbOrNull(l.NewValue)
	if err != nil {
		return fmt.Errorf("encode new value: %w", err)
	}

	const q = `
		INSERT INTO task_logs (task_id, actor_id, action, old_value, new_value)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at`
	err = conn(ctx, r.db).QueryRow(ctx, q, l.TaskID, l.ActorID, l.Action, oldValue, newValue).
		Scan(&l.ID, &l.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert task log: %w", err)
	}
	return nil
}

// jsonbOrNull encodes m for a JSONB column; a nil map becomes SQL NULL.
func jsonbOrNull(m map[string]any) ([]byte, error) {
	if m == nil {
		return nil, nil
	}
	return json.Marshal(m)
}
