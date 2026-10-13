package token_test

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"technical-test-be-gdc-multi-sarana/internal/pkg/token"
)

const secret = "0123456789abcdef0123456789abcdef"

func fixedClock(t time.Time) func() time.Time { return func() time.Time { return t } }

func TestManager_IssueAndParse(t *testing.T) {
	now := time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
	m := token.NewManager(secret, "issuer", time.Hour, fixedClock(now))
	userID := uuid.New()

	signed, expiresAt, err := m.Issue(userID)
	require.NoError(t, err)
	assert.Equal(t, now.Add(time.Hour), expiresAt)

	got, err := m.Parse(signed)
	require.NoError(t, err)
	assert.Equal(t, userID, got)
}

func TestManager_DefaultClock(t *testing.T) {
	m := token.NewManager(secret, "issuer", time.Hour, nil)
	signed, expiresAt, err := m.Issue(uuid.New())
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now().Add(time.Hour), expiresAt, time.Minute)
	_, err = m.Parse(signed)
	assert.NoError(t, err)
}

func TestManager_RejectsInvalidTokens(t *testing.T) {
	now := time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
	m := token.NewManager(secret, "issuer", time.Hour, fixedClock(now))
	valid, _, err := m.Issue(uuid.New())
	require.NoError(t, err)

	sign := func(method jwt.SigningMethod, key any, claims jwt.RegisteredClaims) string {
		s, err := jwt.NewWithClaims(method, claims).SignedString(key)
		require.NoError(t, err)
		return s
	}
	base := jwt.RegisteredClaims{
		Subject:   uuid.NewString(),
		Issuer:    "issuer",
		ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
	}
	withIssuer := base
	withIssuer.Issuer = "someone-else"
	withoutExpiry := base
	withoutExpiry.ExpiresAt = nil
	badSubject := base
	badSubject.Subject = "not-a-uuid"

	tests := map[string]string{
		"garbage":           "not.a.token",
		"wrong secret":      sign(jwt.SigningMethodHS256, []byte("another-secret-of-32-characters!"), base),
		"unsigned":          sign(jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType, base),
		"other HMAC method": sign(jwt.SigningMethodHS512, []byte(secret), base),
		"other issuer":      sign(jwt.SigningMethodHS256, []byte(secret), withIssuer),
		"no expiry":         sign(jwt.SigningMethodHS256, []byte(secret), withoutExpiry),
		"bad subject":       sign(jwt.SigningMethodHS256, []byte(secret), badSubject),
	}
	for name, tok := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := m.Parse(tok)
			assert.ErrorIs(t, err, token.ErrInvalidToken)
		})
	}

	t.Run("expired", func(t *testing.T) {
		later := token.NewManager(secret, "issuer", time.Hour, fixedClock(now.Add(2*time.Hour)))
		_, err := later.Parse(valid)
		assert.ErrorIs(t, err, token.ErrInvalidToken)
	})
}
