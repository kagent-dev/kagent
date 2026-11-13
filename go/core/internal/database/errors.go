package database

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

// Store errors describe resource-independent categories. Wrap them with resource
// and operation context using %w; callers match them with errors.Is.
var (
	// ErrNotFound also covers records that are not visible to the given user.
	ErrNotFound = errors.New("record not found")
	// ErrConflict reports an operation blocked by current state or a concurrent change.
	ErrConflict = errors.New("resource conflict")
	// ErrFailedPrecondition reports an unmet requirement for an operation.
	ErrFailedPrecondition = errors.New("precondition failed")
	// ErrIdempotencyConflict is distinct because clients must use a new request ID.
	ErrIdempotencyConflict = errors.New("request id was already used with different parameters")
)

// ErrObjectDeleting reports that deletion has started. The object is unavailable
// to new callers, so it also matches ErrNotFound.
var ErrObjectDeleting = fmt.Errorf("object is being deleted: %w", ErrNotFound)

// ErrActiveTurn reports a dispatch refused because an earlier turn is still open.
// It also matches ErrConflict.
var ErrActiveTurn = fmt.Errorf("an earlier turn is still open: %w", ErrConflict)

// IsLoginFailure reports whether err is a failed PostgreSQL login, rejected
// credentials included.
func IsLoginFailure(err error) bool {
	var connectErr *pgconn.ConnectError
	return errors.As(err, &connectErr)
}

// IsUnavailable reports whether err is a failed login that may succeed on retry.
// No SQL was sent, so a retry is safe. Rejected credentials are not unavailable.
func IsUnavailable(err error) bool {
	var connectErr *pgconn.ConnectError
	if !errors.As(err, &connectErr) {
		return false
	}
	var pgErr *pgconn.PgError
	if !errors.As(connectErr, &pgErr) {
		return true
	}
	switch pgErr.Code {
	case "28P01", "28000", "3D000", "42501":
		return false
	}
	return true
}
