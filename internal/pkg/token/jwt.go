// Package token issues and verifies JWT access tokens.
package token

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// ErrInvalidToken is returned for any token that fails verification.
var ErrInvalidToken = errors.New("invalid token")

// Manager signs and verifies HS256 tokens. The user id travels in the "sub"
// claim; everything else about the user is loaded from the database so it is
// never stale.
type Manager struct {
	secret []byte
	issuer string
	ttl    time.Duration
	now    func() time.Time
}

// NewManager creates a token manager. now may be nil to use time.Now.
func NewManager(secret, issuer string, ttl time.Duration, now func() time.Time) *Manager {
	if now == nil {
		now = time.Now
	}
	return &Manager{secret: []byte(secret), issuer: issuer, ttl: ttl, now: now}
}

// Issue creates a signed token for userID.
func (m *Manager) Issue(userID uuid.UUID) (string, time.Time, error) {
	now := m.now()
	expiresAt := now.Add(m.ttl)
	claims := jwt.RegisteredClaims{
		ID:        uuid.NewString(),
		Subject:   userID.String(),
		Issuer:    m.issuer,
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(expiresAt),
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign token: %w", err)
	}
	return signed, expiresAt, nil
}

// Parse verifies a token and returns the authenticated user id. Only HS256
// tokens from this issuer with an expiry are accepted.
func (m *Manager) Parse(tokenString string) (uuid.UUID, error) {
	claims := &jwt.RegisteredClaims{}
	_, err := jwt.ParseWithClaims(tokenString, claims, func(*jwt.Token) (any, error) {
		return m.secret, nil
	},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(m.issuer),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(m.now),
	)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: bad subject", ErrInvalidToken)
	}
	return userID, nil
}
