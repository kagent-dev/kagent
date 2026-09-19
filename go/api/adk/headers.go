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
)
