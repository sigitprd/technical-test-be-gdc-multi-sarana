package domain

import (
	"time"

	"github.com/google/uuid"
)

// IdempotencyRecord stores the outcome of the first request made with a given
// Idempotency-Key so that retries can be answered with the same result.
//
// Result is an opaque snapshot owned by the use case that claimed the key. A
// record without a Result has been claimed but not completed yet.
type IdempotencyRecord struct {
	UserID      uuid.UUID
	Key         uuid.UUID
	RequestHash string
	Result      []byte
	CreatedAt   time.Time
	ExpiresAt   time.Time
}

// Completed reports whether the original request finished and its result was stored.
func (r *IdempotencyRecord) Completed() bool {
	return len(r.Result) > 0
}
