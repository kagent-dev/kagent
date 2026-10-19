package a2a

// RuntimeIdentityPath is projected by Substrate and rebound on restore. It is
// routing metadata, not an authentication credential.
const RuntimeIdentityPath = "/run/kagent/identity/name"

// RuntimeAuthorizationHeader carries the Actor JWT injected by Substrate egress.
// Runtime clients send a placeholder; the gateway replaces it with a signed token.
const RuntimeAuthorizationHeader = "x-kagent-runtime-authorization"

// DispatchHeader carries the gateway's attempt fence to the runtime TaskStore.
const DispatchHeader = "x-kagent-dispatch-id"
