package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"technical-test-be-gdc-multi-sarana/internal/domain"
)

// IdempotencyRepository implements service.IdempotencyRepository.
type IdempotencyRepository struct {
	db DB
}

// NewIdempotencyRepository creates an IdempotencyRepository.
func NewIdempotencyRepository(db DB) *IdempotencyRepository {
	return &IdempotencyRepository{db: db}
}

// Claim reserves the key with a single atomic statement.
//
// The insert takes the row lock on (user_id, idempotency_key). A concurrent
// transaction claiming the same key blocks on that lock until the first
// transaction ends: after a commit it sees the stored record, after a
// rollback its insert succeeds and it becomes the owner. An expired record is
// taken over in place by the ON CONFLICT ... WHERE branch.
func (r *IdempotencyRepository) Claim(ctx context.Context, rec *domain.IdempotencyRecord) (*domain.IdempotencyRecord, bool, error) {
	const claimQ = `
		INSERT INTO idempotency_keys (user_id, idempotency_key, request_hash, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (user_id, idempotency_key) DO UPDATE
			SET request_hash = EXCLUDED.request_hash,
			    result       = NULL,
			    created_at   = EXCLUDED.created_at,
			    expires_at   = EXCLUDED.expires_at
			WHERE idempotency_keys.expires_at <= EXCLUDED.created_at
		RETURNING user_id`

	db := conn(ctx, r.db)
	var owner uuid.UUID
	err := db.QueryRow(ctx, claimQ, rec.UserID, rec.Key, rec.RequestHash, rec.CreatedAt, rec.ExpiresAt).Scan(&owner)
	if err == nil {
		return nil, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, false, fmt.Errorf("claim idempotency key: %w", err)
	}

	const getQ = `
		SELECT user_id, idempotency_key, request_hash, result, created_at, expires_at
		FROM idempotency_keys
		WHERE user_id = $1 AND idempotency_key = $2`
	var existing domain.IdempotencyRecord
	err = db.QueryRow(ctx, getQ, rec.UserID, rec.Key).Scan(
		&existing.UserID, &existing.Key, &existing.RequestHash,
		&existing.Result, &existing.CreatedAt, &existing.ExpiresAt)
	if err != nil {
		return nil, false, fmt.Errorf("load idempotency key: %w", err)
	}
	return &existing, false, nil
}

// SaveResult stores the result snapshot of a claimed key.
func (r *IdempotencyRepository) SaveResult(ctx context.Context, userID, key uuid.UUID, result []byte) error {
	const q = `
		UPDATE idempotency_keys SET result = $3
		WHERE user_id = $1 AND idempotency_key = $2`
	tag, err := conn(ctx, r.db).Exec(ctx, q, userID, key, result)
	if err != nil {
		return fmt.Errorf("save idempotency result: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// DeleteExpired removes keys that expired before now and returns how many
// were deleted.
func (r *IdempotencyRepository) DeleteExpired(ctx context.Context, now time.Time) (int64, error) {
	tag, err := conn(ctx, r.db).Exec(ctx, `DELETE FROM idempotency_keys WHERE expires_at <= $1`, now)
	if err != nil {
		return 0, fmt.Errorf("delete expired idempotency keys: %w", err)
	}
	return tag.RowsAffected(), nil
}
