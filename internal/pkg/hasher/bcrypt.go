// Package hasher hashes and verifies passwords with bcrypt.
package hasher

import (
	"errors"

	"golang.org/x/crypto/bcrypt"
)

// MaxPasswordBytes is the longest password bcrypt can hash. Longer passwords
// are rejected instead of being silently truncated.
const MaxPasswordBytes = 72

var (
	// ErrMismatch is returned when a password does not match its hash.
	ErrMismatch = errors.New("password mismatch")
	// ErrPasswordTooLong is returned when a password exceeds MaxPasswordBytes.
	ErrPasswordTooLong = errors.New("password exceeds 72 bytes")
)

// Bcrypt implements password hashing using bcrypt.
type Bcrypt struct {
	cost int
}

// NewBcrypt creates a hasher. A cost outside bcrypt's range falls back to bcrypt.DefaultCost.
func NewBcrypt(cost int) *Bcrypt {
	if cost < bcrypt.MinCost || cost > bcrypt.MaxCost {
		cost = bcrypt.DefaultCost
	}
	return &Bcrypt{cost: cost}
}

// Hash returns the bcrypt hash of password.
func (b *Bcrypt) Hash(password string) (string, error) {
	if len(password) > MaxPasswordBytes {
		return "", ErrPasswordTooLong
	}
	h, err := bcrypt.GenerateFromPassword([]byte(password), b.cost)
	if err != nil {
		return "", err
	}
	return string(h), nil
}

// Compare returns nil when password matches hash, ErrMismatch otherwise.
//
// Passwords longer than MaxPasswordBytes never match: bcrypt only reads the
// first 72 bytes, so without this check "<real password>anything" would be
// accepted too.
func (b *Bcrypt) Compare(hash, password string) error {
	if len(password) > MaxPasswordBytes {
		return ErrMismatch
	}
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		return ErrMismatch
	}
	return err
}
