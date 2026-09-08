package database

import "errors"

// ErrNotFound reports that the requested record does not exist (or is not
// visible to the given user). Match with errors.Is; implementations wrap it
// with call-site context.
var ErrNotFound = errors.New("record not found")

var ErrIdempotencyConflict = errors.New("request id was already used with different parameters")

var ErrAgentInstanceConflict = errors.New("AgentInstance lifecycle operation conflicts with its current state")

var ErrAgentInstanceTaskConflict = errors.New("AgentInstance already has an active task")

var ErrAgentInstanceNotQuiescent = errors.New("AgentInstance has no quiescent turn boundary")
