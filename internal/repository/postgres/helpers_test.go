package postgres_test

import (
	"errors"
	"testing"

	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errBoom = errors.New("boom")

// newMock returns a pgxmock pool whose expectations must all be met by the
// end of the test.
func newMock(t *testing.T) pgxmock.PgxPoolIface {
	t.Helper()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, mock.ExpectationsWereMet())
		mock.Close()
	})
	return mock
}

// anyArgs matches n query arguments of any value.
func anyArgs(n int) []any {
	args := make([]any, n)
	for i := range args {
		args[i] = pgxmock.AnyArg()
	}
	return args
}

var (
	teamColumns = []string{"id", "name", "invite_code", "created_at"}
	userColumns = []string{"id", "team_id", "name", "email", "password_hash", "created_at", "updated_at"}
	taskColumns = []string{"id", "owner_id", "assignee_id", "title", "description", "status", "due_date", "created_at", "updated_at"}
)
