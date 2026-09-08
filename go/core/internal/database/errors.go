package database

import (
	"errors"
	"fmt"
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
