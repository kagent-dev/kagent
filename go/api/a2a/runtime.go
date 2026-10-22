package a2a

// RuntimeIdentityPath is projected by Substrate and rebound on restore. It is
// routing metadata, not an authentication credential.
const RuntimeIdentityPath = "/run/kagent/identity/name"

// DispatchHeader carries the gateway's attempt fence to the runtime TaskStore.
const DispatchHeader = "x-kagent-dispatch-id"
