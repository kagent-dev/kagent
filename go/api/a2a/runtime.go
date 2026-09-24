package a2a

// RuntimeIdentityPath is projected by Substrate and rebound on restore. It is
// routing metadata, not authority; TaskStore authenticates the injected actor JWT.
const RuntimeIdentityPath = "/run/kagent/identity/name"

// InsecureTaskStoreAuthEnv enables unsigned runtime identity for isolated tests.
// Both the API and runtime must opt in. Never enable it on a shared deployment.
const InsecureTaskStoreAuthEnv = "KAGENT_INSECURE_TASK_STORE_AUTH"

// InsecureRuntimeIdentityHeader carries atespace/actor-name/actor-UID in test mode.
const InsecureRuntimeIdentityHeader = "x-kagent-insecure-runtime-identity"
