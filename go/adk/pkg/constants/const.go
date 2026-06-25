package constants

const (
	// A2A call context's NewRequestMeta normalizes header names to lowercase.
	// This is why we use "authorization" instead of "Authorization".
	AuthorizationHeader = "authorization"

	// ActorTokenHeader carries the agent's own workload token alongside a
	// forwarded end-user Authorization, so a downstream gateway can run an
	// RFC 8693 delegation (subject=user, actor=agent). It is set on the
	// outgoing request, so it uses the canonical header form.
	ActorTokenHeader = "X-Actor-Token"
)
