// Copyright 2026 The kagent Authors
// SPDX-License-Identifier: Apache-2.0

package adk

// Request headers an agent runtime sets on every call it makes to a model
// provider. They name the agent the call is made for, so a gateway or proxy
// between the runtime and the provider can attribute the call to the agent and
// the user without reading the runtime's network identity: a runtime that
// shares a pod, a ServiceAccount or an egress with other agents is otherwise
// indistinguishable from them. The headers are an accounting identity the
// runtime asserts about itself, never an authorization input.
const (
	// AgentHeader carries the name of the AgentTemplate the runtime executes.
	AgentHeader = "x-kagent-agent"
	// AgentNamespaceHeader carries the namespace of that AgentTemplate.
	AgentNamespaceHeader = "x-kagent-agent-namespace"
	// UserHeader carries the authenticated user of the turn the call belongs
	// to: the identity the controller resolved from the caller's validated
	// token, which its gateway forwards to the runtime as request metadata of
	// the same name. A call outside a turn, or in a turn whose caller the
	// controller did not resolve, carries no such header.
	UserHeader = "x-kagent-user"
	// AgentInstanceHeader carries the id of the AgentInstance (the session: a
	// conversation, a thread) the turn the call belongs to runs in, which the
	// controller's gateway forwards to the runtime as request metadata of the
	// same name. It is the name of the header that routes a call to an
	// AgentInstance (a2a.AgentInstanceIDHeader). A call outside a turn carries
	// no such header.
	AgentInstanceHeader = "x-kagent-agent-instance-id"
	// WorkingDirectoryHeader carries the directory the turn works in, which
	// the controller's gateway forwards to the runtime on every turn: the
	// session's own directory of its workspace volume, or a directory of its
	// durable /data without one. A runtime resolves it per turn, never at boot,
	// since a runtime restored from a snapshot predates the session's mounts.
	WorkingDirectoryHeader = "x-kagent-working-directory"
	// MirrorsDirectoryHeader carries the read-only directory of the session's
	// workspace volume that holds its bare git mirrors, as
	// <owner>/<repository>.git, which the controller's gateway forwards to the
	// runtime on every turn of a session that mounts it. A turn without it has
	// no mirrors to clone.
	MirrorsDirectoryHeader = "x-kagent-mirrors-directory"
	// ChangedRepositoriesHeader carries the repositories, as <owner>/<repository>
	// relative to the working directory, whose origin moved since the mirrors'
	// last sync, comma-separated, which the controller's gateway forwards on
	// every turn of a session with a workspace volume. A runtime fetches exactly
	// these before the session's first turn; an absent header fetches nothing.
	ChangedRepositoriesHeader = "x-kagent-changed-repositories"
	// ActorJWTHostsHeader carries the hosts, comma-separated, on which the
	// egress gateway replaces the Authorization header of a request from the
	// session's Actor with the Actor's own JWT, which the controller's gateway
	// forwards on every turn of a session that binds such a provider. A
	// runtime sends a placeholder there instead of the caller's credential.
	ActorJWTHostsHeader = "x-kagent-actor-jwt-hosts"
)
