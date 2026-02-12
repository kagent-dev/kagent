package adk

import "github.com/kagent-dev/kagent/go-adk/pkg/core"

// Well-known keys for runner/executor args map (Run(ctx, args) and ConvertA2ARequestToRunArgs).
// These are aliases to core constants to avoid import cycles while maintaining a single source of truth.
const (
	ArgKeyMessage        = core.ArgKeyMessage
	ArgKeyNewMessage     = core.ArgKeyNewMessage
	ArgKeyUserID         = core.ArgKeyUserID
	ArgKeySessionID      = core.ArgKeySessionID
	ArgKeySessionService = core.ArgKeySessionService
	ArgKeySession        = core.ArgKeySession
	ArgKeyRunConfig      = core.ArgKeyRunConfig
	ArgKeyAppName        = core.ArgKeyAppName
)
