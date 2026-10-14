package a2a

// RuntimeIdentityPath is projected by Substrate and rebound on restore. It is
// routing metadata. Temporary TaskStore headers use it until Substrate #1660.
const RuntimeIdentityPath = "/run/kagent/identity/name"

// InsecureRuntimeIdentityHeader temporarily carries atespace/actor-name/actor-UID
// until Substrate supplies verified actor credentials (#1660).
const InsecureRuntimeIdentityHeader = "x-kagent-insecure-runtime-identity"

// DispatchHeader carries the gateway's attempt fence to the runtime TaskStore.
const DispatchHeader = "x-kagent-dispatch-id"

// ModelAPIKeyHeader carries a caller-supplied model API key to an agent whose
// ModelConfig sets apiKeyPassthrough. The value is the key itself, with no
// scheme. It is separate from `authorization` so that header stays free for
// the caller's own credential; the gateway copies it to the runtime call, and
// the runtime prefers it over the caller's bearer token.
const ModelAPIKeyHeader = "x-kagent-model-api-key"
