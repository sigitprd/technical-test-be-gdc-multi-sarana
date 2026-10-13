package apperror_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"technical-test-be-gdc-multi-sarana/internal/apperror"
)

func TestConstructors(t *testing.T) {
	tests := []struct {
		err  *apperror.Error
		kind apperror.Kind
	}{
		{apperror.BadRequest("C", "m"), apperror.KindBadRequest},
		{apperror.Unauthorized("C", "m"), apperror.KindUnauthorized},
		{apperror.Forbidden("C", "m"), apperror.KindForbidden},
		{apperror.NotFound("C", "m"), apperror.KindNotFound},
		{apperror.Conflict("C", "m"), apperror.KindConflict},
		{apperror.Unprocessable("C", "m"), apperror.KindUnprocessable},
		{apperror.TooManyRequests("C", "m"), apperror.KindTooManyRequests},
		{apperror.Unavailable("C", "m"), apperror.KindUnavailable},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.kind, tc.err.Kind)
		assert.Equal(t, "C", tc.err.Code)
		assert.Equal(t, "m", tc.err.Message)
	}

	v := apperror.Validation([]string{"detail"})
	assert.Equal(t, apperror.KindValidation, v.Kind)
	assert.Equal(t, apperror.CodeValidation, v.Code)
	assert.Equal(t, []string{"detail"}, v.Details)
}

func TestError_MessageAndUnwrap(t *testing.T) {
	cause := errors.New("db down")
	plain := apperror.NotFound("TASK_NOT_FOUND", "task not found")
	wrapped := plain.Wrap(cause)

	assert.Equal(t, "TASK_NOT_FOUND: task not found", plain.Error())
	assert.Equal(t, "TASK_NOT_FOUND: task not found: db down", wrapped.Error())
	assert.ErrorIs(t, wrapped, cause)
	assert.Nil(t, plain.Err, "Wrap copies instead of mutating the sentinel")
}

func TestError_IsMatchesKindAndCode(t *testing.T) {
	sentinel := apperror.NotFound("TASK_NOT_FOUND", "task not found")

	assert.ErrorIs(t, sentinel.Wrap(errors.New("x")), sentinel)
	assert.ErrorIs(t, fmt.Errorf("context: %w", sentinel.WithDetails("d")), sentinel)
	assert.NotErrorIs(t, apperror.NotFound("USER_NOT_FOUND", "x"), sentinel)
	assert.NotErrorIs(t, apperror.Conflict("TASK_NOT_FOUND", "x"), sentinel)
	assert.False(t, sentinel.Is(errors.New("plain")))
}

func TestError_WithDetailsCopies(t *testing.T) {
	sentinel := apperror.BadRequest("C", "m")
	detailed := sentinel.WithDetails(map[string]string{"field": "x"})

	assert.Nil(t, sentinel.Details)
	assert.Equal(t, map[string]string{"field": "x"}, detailed.Details)
}

func TestError_IsServerError(t *testing.T) {
	assert.True(t, apperror.Internal(errors.New("x")).IsServerError())
	assert.True(t, apperror.Unavailable("C", "m").IsServerError())
	assert.False(t, apperror.BadRequest("C", "m").IsServerError())
}

func TestInternalHidesCause(t *testing.T) {
	cause := errors.New("pq: relation does not exist")
	err := apperror.Internal(cause)

	assert.Equal(t, apperror.CodeInternal, err.Code)
	assert.NotContains(t, err.Message, "relation")
	assert.ErrorIs(t, err, cause)
}

func TestFrom(t *testing.T) {
	assert.Nil(t, apperror.From(nil))

	known := apperror.Conflict("C", "m")
	assert.Same(t, known, apperror.From(fmt.Errorf("wrapped: %w", known)))

	unknown := apperror.From(errors.New("boom"))
	require.NotNil(t, unknown)
	assert.Equal(t, apperror.KindInternal, unknown.Kind)
}
