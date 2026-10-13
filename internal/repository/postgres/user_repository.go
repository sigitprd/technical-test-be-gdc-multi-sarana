package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"technical-test-be-gdc-multi-sarana/internal/domain"
)

// UserRepository implements service.UserRepository.
type UserRepository struct {
	db DB
}

// NewUserRepository creates a UserRepository.
func NewUserRepository(db DB) *UserRepository {
	return &UserRepository{db: db}
}

const userColumns = `id, team_id, name, email, password_hash, created_at, updated_at`

func scanUser(row pgx.Row) (*domain.User, error) {
	var u domain.User
	if err := row.Scan(&u.ID, &u.TeamID, &u.Name, &u.Email, &u.PasswordHash, &u.CreatedAt, &u.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &u, nil
}

// Create inserts a user.
func (r *UserRepository) Create(ctx context.Context, u *domain.User) error {
	const q = `
		INSERT INTO users (team_id, name, email, password_hash)
		VALUES ($1, $2, $3, $4)
		RETURNING id, created_at, updated_at`
	err := conn(ctx, r.db).QueryRow(ctx, q, u.TeamID, u.Name, u.Email, u.PasswordHash).
		Scan(&u.ID, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return domain.ErrDuplicate
		}
		return fmt.Errorf("insert user: %w", err)
	}
	return nil
}

// GetByID loads a user by id.
func (r *UserRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	q := `SELECT ` + userColumns + ` FROM users WHERE id = $1`
	return scanUser(conn(ctx, r.db).QueryRow(ctx, q, id))
}

// GetByEmail loads a user by email (case-insensitive).
func (r *UserRepository) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	q := `SELECT ` + userColumns + ` FROM users WHERE lower(email) = lower($1)`
	return scanUser(conn(ctx, r.db).QueryRow(ctx, q, email))
}

// ListByTeam returns all members of a team ordered by name.
func (r *UserRepository) ListByTeam(ctx context.Context, teamID uuid.UUID) ([]domain.User, error) {
	q := `SELECT ` + userColumns + ` FROM users WHERE team_id = $1 ORDER BY name, id`
	rows, err := conn(ctx, r.db).Query(ctx, q, teamID)
	if err != nil {
		return nil, fmt.Errorf("query team members: %w", err)
	}
	defer rows.Close()

	var users []domain.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, *u)
	}
	return users, rows.Err()
}
