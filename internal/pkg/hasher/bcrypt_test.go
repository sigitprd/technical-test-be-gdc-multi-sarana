package hasher_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"technical-test-be-gdc-multi-sarana/internal/pkg/hasher"
)

func TestBcrypt_HashAndCompare(t *testing.T) {
	h := hasher.NewBcrypt(bcrypt.MinCost)

	hash, err := h.Hash("password123")
	require.NoError(t, err)

	assert.NotEqual(t, "password123", hash)
	assert.NoError(t, h.Compare(hash, "password123"))
	assert.ErrorIs(t, h.Compare(hash, "wrong"), hasher.ErrMismatch)
}

func TestBcrypt_RejectsPasswordsOverTheByteLimit(t *testing.T) {
	h := hasher.NewBcrypt(bcrypt.MinCost)
	exact := strings.Repeat("a", hasher.MaxPasswordBytes)
	hash, err := h.Hash(exact)
	require.NoError(t, err)

	_, err = h.Hash(strings.Repeat("é", 40)) // 40 characters, 80 bytes
	assert.ErrorIs(t, err, hasher.ErrPasswordTooLong)

	// bcrypt only reads 72 bytes, so without the check this would match.
	assert.ErrorIs(t, h.Compare(hash, exact+"suffix"), hasher.ErrMismatch)
}

func TestBcrypt_InvalidCostFallsBackToDefault(t *testing.T) {
	for _, cost := range []int{0, bcrypt.MaxCost + 1} {
		hash, err := hasher.NewBcrypt(cost).Hash("password123")
		require.NoError(t, err)
		got, err := bcrypt.Cost([]byte(hash))
		require.NoError(t, err)
		assert.Equal(t, bcrypt.DefaultCost, got)
	}
}

func TestBcrypt_CompareMalformedHash(t *testing.T) {
	err := hasher.NewBcrypt(bcrypt.MinCost).Compare("not-a-bcrypt-hash", "password123")
	require.Error(t, err)
	assert.NotErrorIs(t, err, hasher.ErrMismatch)
}
