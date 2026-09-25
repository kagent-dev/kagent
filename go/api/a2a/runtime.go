package a2a

// RuntimeIdentityPath is projected by Substrate and rebound on restore. It is
// routing metadata. Temporary TaskStore headers use it until Substrate #1660.
const RuntimeIdentityPath = "/run/kagent/identity/name"

// InsecureRuntimeIdentityHeader temporarily carries atespace/actor-name/actor-UID
// until Substrate supplies verified actor credentials (#1660).
const InsecureRuntimeIdentityHeader = "x-kagent-insecure-runtime-identity"

// DispatchHeader carries the gateway's attempt fence to the runtime TaskStore.
const DispatchHeader = "x-kagent-dispatch-id"
