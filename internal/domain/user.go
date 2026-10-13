package domain

import (
	"time"

	"github.com/google/uuid"
)

// Team groups users. Tasks can only be assigned between members of the same
// team. Whoever creates a team gets its secret invite code; others join the
// team by registering with that code.
type Team struct {
	ID         uuid.UUID
	Name       string
	InviteCode string
	CreatedAt  time.Time
}

// User is an account that owns tasks.
type User struct {
	ID           uuid.UUID
	TeamID       uuid.UUID
	Name         string
	Email        string
	PasswordHash string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
