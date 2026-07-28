package database

import "errors"

// ErrNotFound reports that the requested record does not exist (or is not
// visible to the given user). Match with errors.Is; implementations wrap it
// with call-site context.
var ErrNotFound = errors.New("record not found")
