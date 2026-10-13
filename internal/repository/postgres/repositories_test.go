package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"technical-test-be-gdc-multi-sarana/internal/domain"
	"technical-test-be-gdc-multi-sarana/internal/repository/postgres"
)

var (
	ctx = context.Background()
	now = time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
)

func TestTeamRepository(t *testing.T) {
	mock := newMock(t)
	repo := postgres.NewTeamRepository(mock)
	id := uuid.New()

	mock.ExpectQuery(`INSERT INTO teams \(name\) VALUES \(\$1\) RETURNING id, name, invite_code, created_at`).
		WithArgs("Alpha").WillReturnRows(mock.NewRows(teamColumns).AddRow(id, "Alpha", "code", now))
	team, err := repo.Create(ctx, "Alpha")
	require.NoError(t, err)
	assert.Equal(t, domain.Team{ID: id, Name: "Alpha", InviteCode: "code", CreatedAt: now}, *team)

	mock.ExpectQuery(`INSERT INTO teams`).WithArgs("Beta").WillReturnError(errBoom)
	_, err = repo.Create(ctx, "Beta")
	assert.ErrorIs(t, err, errBoom)

	mock.ExpectQuery(`FROM teams WHERE id = \$1`).WithArgs(id).WillReturnRows(mock.NewRows(teamColumns).AddRow(id, "Alpha", "code", now))
	team, err = repo.GetByID(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, "Alpha", team.Name)

	mock.ExpectQuery(`FROM teams WHERE invite_code = \$1`).WithArgs("missing").WillReturnRows(mock.NewRows(teamColumns))
	_, err = repo.GetByInviteCode(ctx, "missing")
	assert.ErrorIs(t, err, domain.ErrNotFound)
}

func TestUserRepository_Create(t *testing.T) {
	mock := newMock(t)
	repo := postgres.NewUserRepository(mock)
	teamID, userID := uuid.New(), uuid.New()
	newUser := func() *domain.User {
		return &domain.User{TeamID: teamID, Name: "Alice", Email: "alice@example.com", PasswordHash: "hash"}
	}

	mock.ExpectQuery(`INSERT INTO users \(team_id, name, email, password_hash\)`).
		WithArgs(teamID, "Alice", "alice@example.com", "hash").
		WillReturnRows(mock.NewRows([]string{"id", "created_at", "updated_at"}).AddRow(userID, now, now))
	u := newUser()
	require.NoError(t, repo.Create(ctx, u))
	assert.Equal(t, userID, u.ID)
	assert.Equal(t, now, u.CreatedAt)

	mock.ExpectQuery(`INSERT INTO users`).WithArgs(anyArgs(4)...).WillReturnError(&pgconn.PgError{Code: "23505"})
	assert.ErrorIs(t, repo.Create(ctx, newUser()), domain.ErrDuplicate, "a unique violation is a duplicate")

	mock.ExpectQuery(`INSERT INTO users`).WithArgs(anyArgs(4)...).WillReturnError(errBoom)
	err := repo.Create(ctx, newUser())
	assert.ErrorIs(t, err, errBoom)
	assert.NotErrorIs(t, err, domain.ErrDuplicate)
}

func TestUserRepository_Queries(t *testing.T) {
	mock := newMock(t)
	repo := postgres.NewUserRepository(mock)
	teamID, aliceID, bobID := uuid.New(), uuid.New(), uuid.New()
	alice := []any{aliceID, teamID, "Alice", "alice@example.com", "hash", now, now}

	mock.ExpectQuery(`FROM users WHERE id = \$1`).WithArgs(aliceID).WillReturnRows(mock.NewRows(userColumns).AddRow(alice...))
	u, err := repo.GetByID(ctx, aliceID)
	require.NoError(t, err)
	assert.Equal(t, "alice@example.com", u.Email)

	mock.ExpectQuery(`FROM users WHERE lower\(email\) = lower\(\$1\)`).WithArgs("ALICE@example.com").WillReturnRows(mock.NewRows(userColumns))
	_, err = repo.GetByEmail(ctx, "ALICE@example.com")
	assert.ErrorIs(t, err, domain.ErrNotFound)

	mock.ExpectQuery(`FROM users WHERE id = \$1`).WithArgs(bobID).WillReturnError(errBoom)
	_, err = repo.GetByID(ctx, bobID)
	assert.ErrorIs(t, err, errBoom)

	mock.ExpectQuery(`FROM users WHERE team_id = \$1 ORDER BY name, id`).WithArgs(teamID).WillReturnRows(
		mock.NewRows(userColumns).AddRow(alice...).AddRow(bobID, teamID, "Bob", "bob@example.com", "hash", now, now))
	members, err := repo.ListByTeam(ctx, teamID)
	require.NoError(t, err)
	require.Len(t, members, 2)
	assert.Equal(t, "Bob", members[1].Name)
}

func TestUserRepository_ListByTeamFailures(t *testing.T) {
	teamID := uuid.New()
	tests := map[string]func(mock pgxmock.PgxPoolIface){
		"query fails": func(mock pgxmock.PgxPoolIface) {
			mock.ExpectQuery(`FROM users WHERE team_id`).WithArgs(teamID).WillReturnError(errBoom)
		},
		"scan fails": func(mock pgxmock.PgxPoolIface) {
			mock.ExpectQuery(`FROM users WHERE team_id`).WithArgs(teamID).WillReturnRows(
				mock.NewRows(userColumns).AddRow("not-a-uuid", teamID, "A", "a@example.com", "h", now, now))
		},
		"iteration fails": func(mock pgxmock.PgxPoolIface) {
			mock.ExpectQuery(`FROM users WHERE team_id`).WithArgs(teamID).WillReturnRows(
				mock.NewRows(userColumns).AddRow(uuid.New(), teamID, "A", "a@example.com", "h", now, now).CloseError(errBoom))
		},
	}
	for name, expect := range tests {
		t.Run(name, func(t *testing.T) {
			mock := newMock(t)
			expect(mock)
			_, err := postgres.NewUserRepository(mock).ListByTeam(ctx, teamID)
			assert.Error(t, err)
		})
	}
}

func taskRow(id, owner uuid.UUID, assignee *uuid.UUID, title, status string) []any {
	return []any{id, owner, assignee, title, "", status, (*time.Time)(nil), now, now}
}

func TestTaskRepository_Create(t *testing.T) {
	mock := newMock(t)
	repo := postgres.NewTaskRepository(mock)
	owner, id := uuid.New(), uuid.New()
	due := now.Add(24 * time.Hour)

	mock.ExpectQuery(`INSERT INTO tasks \(owner_id, assignee_id, title, description, status, due_date\)`).
		WithArgs(owner, (*uuid.UUID)(nil), "Report", "Q3", domain.TaskStatusTodo, &due).
		WillReturnRows(mock.NewRows(taskColumns).AddRow(id, owner, (*uuid.UUID)(nil), "Report", "Q3", "todo", &due, now, now))
	task := &domain.Task{OwnerID: owner, Title: "Report", Description: "Q3", Status: domain.TaskStatusTodo, DueDate: &due}
	require.NoError(t, repo.Create(ctx, task))
	assert.Equal(t, id, task.ID)
	assert.Equal(t, domain.TaskStatusTodo, task.Status)
	assert.True(t, task.DueDate.Equal(due))

	mock.ExpectQuery(`INSERT INTO tasks`).WithArgs(anyArgs(6)...).WillReturnError(errBoom)
	assert.ErrorIs(t, repo.Create(ctx, &domain.Task{OwnerID: owner}), errBoom)
}

func TestTaskRepository_Get(t *testing.T) {
	mock := newMock(t)
	repo := postgres.NewTaskRepository(mock)
	id, owner, assignee := uuid.New(), uuid.New(), uuid.New()

	mock.ExpectQuery(`FROM tasks WHERE id = \$1$`).WithArgs(id).
		WillReturnRows(mock.NewRows(taskColumns).AddRow(taskRow(id, owner, &assignee, "Report", "done")...))
	task, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, &assignee, task.AssigneeID)
	assert.Equal(t, domain.TaskStatusDone, task.Status)

	mock.ExpectQuery(`FROM tasks WHERE id = \$1 FOR UPDATE`).WithArgs(id).WillReturnRows(mock.NewRows(taskColumns))
	_, err = repo.GetByIDForUpdate(ctx, id)
	assert.ErrorIs(t, err, domain.ErrNotFound)
}

func TestTaskRepository_List(t *testing.T) {
	user := uuid.New()
	listColumns := append(append([]string{}, taskColumns...), "total")
	status := domain.TaskStatusDone

	t.Run("filters, escapes the search and pages", func(t *testing.T) {
		mock := newMock(t)
		id := uuid.New()
		mock.ExpectQuery(`WHERE \(owner_id = \$1 OR assignee_id = \$1\) AND status = \$2 AND title ILIKE \$3 ESCAPE '\\' ORDER BY created_at DESC, id DESC LIMIT \$4 OFFSET \$5`).
			WithArgs(user, "done", `%50\%\_off\\%`, 10, 10).
			WillReturnRows(mock.NewRows(listColumns).AddRow(append(taskRow(id, user, nil, "50%_off\\", "done"), int64(11))...))

		tasks, total, err := postgres.NewTaskRepository(mock).List(ctx, user, domain.TaskFilter{Status: &status, Search: `50%_off\`, Page: 2, Limit: 10})

		require.NoError(t, err)
		assert.EqualValues(t, 11, total, "the total comes from the window function")
		require.Len(t, tasks, 1)
		assert.Equal(t, id, tasks[0].ID)
	})

	t.Run("page past the end counts separately", func(t *testing.T) {
		mock := newMock(t)
		mock.ExpectQuery(`LIMIT \$2 OFFSET \$3`).WithArgs(user, 10, 90).WillReturnRows(mock.NewRows(listColumns))
		mock.ExpectQuery(`SELECT COUNT\(\*\) FROM tasks WHERE \(owner_id = \$1 OR assignee_id = \$1\)$`).WithArgs(user).
			WillReturnRows(mock.NewRows([]string{"count"}).AddRow(int64(42)))

		tasks, total, err := postgres.NewTaskRepository(mock).List(ctx, user, domain.TaskFilter{Page: 10, Limit: 10})

		require.NoError(t, err)
		assert.Empty(t, tasks)
		assert.EqualValues(t, 42, total)
	})

	t.Run("empty first page needs no count", func(t *testing.T) {
		mock := newMock(t)
		mock.ExpectQuery(`LIMIT \$2 OFFSET \$3`).WithArgs(user, 10, 0).WillReturnRows(mock.NewRows(listColumns))

		_, total, err := postgres.NewTaskRepository(mock).List(ctx, user, domain.TaskFilter{Page: 1, Limit: 10})

		require.NoError(t, err)
		assert.Zero(t, total)
	})

	failures := map[string]func(mock pgxmock.PgxPoolIface){
		"query fails": func(mock pgxmock.PgxPoolIface) {
			mock.ExpectQuery(`FROM tasks`).WithArgs(anyArgs(3)...).WillReturnError(errBoom)
		},
		"scan fails": func(mock pgxmock.PgxPoolIface) {
			mock.ExpectQuery(`FROM tasks`).WithArgs(anyArgs(3)...).WillReturnRows(mock.NewRows(listColumns).AddRow(append(taskRow(uuid.New(), user, nil, "t", "todo"), "not-a-number")...))
		},
		"iteration fails": func(mock pgxmock.PgxPoolIface) {
			mock.ExpectQuery(`FROM tasks`).WithArgs(anyArgs(3)...).WillReturnRows(mock.NewRows(listColumns).AddRow(append(taskRow(uuid.New(), user, nil, "t", "todo"), int64(1))...).CloseError(errBoom))
		},
		"count fails": func(mock pgxmock.PgxPoolIface) {
			mock.ExpectQuery(`LIMIT`).WithArgs(anyArgs(3)...).WillReturnRows(mock.NewRows(listColumns))
			mock.ExpectQuery(`SELECT COUNT`).WithArgs(user).WillReturnError(errBoom)
		},
	}
	for name, expect := range failures {
		t.Run(name, func(t *testing.T) {
			mock := newMock(t)
			expect(mock)
			_, _, err := postgres.NewTaskRepository(mock).List(ctx, user, domain.TaskFilter{Page: 2, Limit: 10})
			assert.Error(t, err)
		})
	}
}

func TestTaskRepository_Update(t *testing.T) {
	mock := newMock(t)
	repo := postgres.NewTaskRepository(mock)
	id, owner := uuid.New(), uuid.New()
	task := &domain.Task{ID: id, OwnerID: owner, Title: "New", Status: domain.TaskStatusDone}

	mock.ExpectQuery(`UPDATE tasks SET title = \$2, description = \$3, status = \$4, due_date = \$5, updated_at = now\(\) WHERE id = \$1`).
		WithArgs(id, "New", "", domain.TaskStatusDone, (*time.Time)(nil)).
		WillReturnRows(mock.NewRows(taskColumns).AddRow(taskRow(id, owner, nil, "New", "done")...))
	require.NoError(t, repo.Update(ctx, task))
	assert.Equal(t, now, task.UpdatedAt, "the stored row is copied back")

	mock.ExpectQuery(`UPDATE tasks SET title`).WithArgs(anyArgs(5)...).WillReturnRows(mock.NewRows(taskColumns))
	assert.ErrorIs(t, repo.Update(ctx, task), domain.ErrNotFound)

	mock.ExpectQuery(`UPDATE tasks SET title`).WithArgs(anyArgs(5)...).WillReturnError(errBoom)
	assert.ErrorIs(t, repo.Update(ctx, task), errBoom)

	assignee := uuid.New()
	mock.ExpectQuery(`UPDATE tasks SET assignee_id = \$2, updated_at = now\(\) WHERE id = \$1`).WithArgs(id, assignee).
		WillReturnRows(mock.NewRows(taskColumns).AddRow(taskRow(id, owner, &assignee, "New", "done")...))
	assigned, err := repo.UpdateAssignee(ctx, id, assignee)
	require.NoError(t, err)
	assert.Equal(t, &assignee, assigned.AssigneeID)
}

func TestTaskRepository_Delete(t *testing.T) {
	mock := newMock(t)
	repo := postgres.NewTaskRepository(mock)
	id := uuid.New()

	mock.ExpectExec(`DELETE FROM tasks WHERE id = \$1`).WithArgs(id).WillReturnResult(pgxmock.NewResult("DELETE", 1))
	assert.NoError(t, repo.Delete(ctx, id))

	mock.ExpectExec(`DELETE FROM tasks`).WithArgs(id).WillReturnResult(pgxmock.NewResult("DELETE", 0))
	assert.ErrorIs(t, repo.Delete(ctx, id), domain.ErrNotFound)

	mock.ExpectExec(`DELETE FROM tasks`).WithArgs(id).WillReturnError(errBoom)
	assert.ErrorIs(t, repo.Delete(ctx, id), errBoom)
}

func TestTaskLogRepository_Create(t *testing.T) {
	taskID, actorID, assignee := uuid.New(), uuid.New(), uuid.New()

	t.Run("values are stored as JSON", func(t *testing.T) {
		mock := newMock(t)
		logID := uuid.New()
		mock.ExpectQuery(`INSERT INTO task_logs \(task_id, actor_id, action, old_value, new_value\)`).
			WithArgs(taskID, actorID, "assigned", []byte(`{"assignee_id":null}`), []byte(`{"assignee_id":"`+assignee.String()+`"}`)).
			WillReturnRows(mock.NewRows([]string{"id", "created_at"}).AddRow(logID, now))

		l := &domain.TaskLog{
			TaskID: taskID, ActorID: actorID, Action: "assigned",
			OldValue: map[string]any{"assignee_id": (*uuid.UUID)(nil)},
			NewValue: map[string]any{"assignee_id": assignee},
		}
		require.NoError(t, postgres.NewTaskLogRepository(mock).Create(ctx, l))
		assert.Equal(t, logID, l.ID)
		assert.Equal(t, now, l.CreatedAt)
	})

	t.Run("nil values become SQL NULL", func(t *testing.T) {
		mock := newMock(t)
		mock.ExpectQuery(`INSERT INTO task_logs`).WithArgs(taskID, actorID, "created", []byte(nil), []byte(nil)).
			WillReturnRows(mock.NewRows([]string{"id", "created_at"}).AddRow(uuid.New(), now))
		assert.NoError(t, postgres.NewTaskLogRepository(mock).Create(ctx, &domain.TaskLog{TaskID: taskID, ActorID: actorID, Action: "created"}))
	})

	unencodable := map[string]any{"bad": make(chan int)}
	failures := map[string]*domain.TaskLog{
		"old value cannot be encoded": {OldValue: unencodable},
		"new value cannot be encoded": {NewValue: unencodable},
	}
	for name, l := range failures {
		t.Run(name, func(t *testing.T) {
			assert.Error(t, postgres.NewTaskLogRepository(newMock(t)).Create(ctx, l))
		})
	}

	t.Run("insert fails", func(t *testing.T) {
		mock := newMock(t)
		mock.ExpectQuery(`INSERT INTO task_logs`).WithArgs(anyArgs(5)...).WillReturnError(errBoom)
		assert.ErrorIs(t, postgres.NewTaskLogRepository(mock).Create(ctx, &domain.TaskLog{}), errBoom)
	})
}

func TestIdempotencyRepository_Claim(t *testing.T) {
	userID, key := uuid.New(), uuid.New()
	rec := &domain.IdempotencyRecord{UserID: userID, Key: key, RequestHash: "hash", CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)}
	claimSQL := `INSERT INTO idempotency_keys \(user_id, idempotency_key, request_hash, created_at, expires_at\) VALUES \(\$1, \$2, \$3, \$4, \$5\) ON CONFLICT \(user_id, idempotency_key\) DO UPDATE .* WHERE idempotency_keys.expires_at <= EXCLUDED.created_at RETURNING user_id`

	t.Run("free key is claimed", func(t *testing.T) {
		mock := newMock(t)
		mock.ExpectQuery(claimSQL).WithArgs(userID, key, "hash", now, rec.ExpiresAt).
			WillReturnRows(mock.NewRows([]string{"user_id"}).AddRow(userID))

		existing, claimed, err := postgres.NewIdempotencyRepository(mock).Claim(ctx, rec)

		require.NoError(t, err)
		assert.True(t, claimed)
		assert.Nil(t, existing)
	})

	t.Run("valid key returns the stored record", func(t *testing.T) {
		mock := newMock(t)
		mock.ExpectQuery(claimSQL).WithArgs(anyArgs(5)...).WillReturnRows(mock.NewRows([]string{"user_id"}))
		mock.ExpectQuery(`SELECT user_id, idempotency_key, request_hash, result, created_at, expires_at FROM idempotency_keys WHERE user_id = \$1 AND idempotency_key = \$2`).
			WithArgs(userID, key).
			WillReturnRows(mock.NewRows([]string{"user_id", "idempotency_key", "request_hash", "result", "created_at", "expires_at"}).
				AddRow(userID, key, "hash", []byte(`{"id":"x"}`), now, rec.ExpiresAt))

		existing, claimed, err := postgres.NewIdempotencyRepository(mock).Claim(ctx, rec)

		require.NoError(t, err)
		assert.False(t, claimed)
		assert.Equal(t, []byte(`{"id":"x"}`), existing.Result)
		assert.Equal(t, "hash", existing.RequestHash)
	})

	failures := map[string]func(mock pgxmock.PgxPoolIface){
		"claim fails": func(mock pgxmock.PgxPoolIface) {
			mock.ExpectQuery(claimSQL).WithArgs(anyArgs(5)...).WillReturnError(errBoom)
		},
		"loading the record fails": func(mock pgxmock.PgxPoolIface) {
			mock.ExpectQuery(claimSQL).WithArgs(anyArgs(5)...).WillReturnRows(mock.NewRows([]string{"user_id"}))
			mock.ExpectQuery(`SELECT user_id, idempotency_key`).WithArgs(anyArgs(2)...).WillReturnError(errBoom)
		},
	}
	for name, expect := range failures {
		t.Run(name, func(t *testing.T) {
			mock := newMock(t)
			expect(mock)
			_, _, err := postgres.NewIdempotencyRepository(mock).Claim(ctx, rec)
			assert.ErrorIs(t, err, errBoom)
		})
	}
}

func TestIdempotencyRepository_SaveAndCleanup(t *testing.T) {
	mock := newMock(t)
	repo := postgres.NewIdempotencyRepository(mock)
	userID, key := uuid.New(), uuid.New()
	result := []byte(`{"id":"x"}`)

	mock.ExpectExec(`UPDATE idempotency_keys SET result = \$3 WHERE user_id = \$1 AND idempotency_key = \$2`).
		WithArgs(userID, key, result).WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	assert.NoError(t, repo.SaveResult(ctx, userID, key, result))

	mock.ExpectExec(`UPDATE idempotency_keys`).WithArgs(anyArgs(3)...).WillReturnResult(pgxmock.NewResult("UPDATE", 0))
	assert.ErrorIs(t, repo.SaveResult(ctx, userID, key, result), domain.ErrNotFound)

	mock.ExpectExec(`UPDATE idempotency_keys`).WithArgs(anyArgs(3)...).WillReturnError(errBoom)
	assert.ErrorIs(t, repo.SaveResult(ctx, userID, key, result), errBoom)

	mock.ExpectExec(`DELETE FROM idempotency_keys WHERE expires_at <= \$1`).WithArgs(now).WillReturnResult(pgxmock.NewResult("DELETE", 3))
	n, err := repo.DeleteExpired(ctx, now)
	require.NoError(t, err)
	assert.EqualValues(t, 3, n)

	mock.ExpectExec(`DELETE FROM idempotency_keys`).WithArgs(anyArgs(1)...).WillReturnError(errBoom)
	_, err = repo.DeleteExpired(ctx, now)
	assert.ErrorIs(t, err, errBoom)
}
