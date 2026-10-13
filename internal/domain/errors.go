package domain

import "errors"

// Persistence-level errors returned by repositories. Services translate them
// into application errors.
var (
	ErrNotFound  = errors.New("record not found")
	ErrDuplicate = errors.New("duplicate record")
)
